//go:build integration

package chserver

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// The engine's three roles only mean anything against a real server: the
// process list, the query log, and `KILL QUERY` are precisely the things a
// fake cannot have. These run in the integration lane
// (scripts/ci/gotest-integration.sh) and skip without CLICKHOUSE_URL.

func liveEndpoint(t *testing.T) (endpoint string) {
	t.Helper()
	raw, set := clickhouseenv.URL.Lookup()
	if !set {
		t.Skip("CLICKHOUSE_URL unset; skipping live engine test")
	}
	endpoint = raw
	return
}

// recordingBus keeps every published progress tick so a test can assert what
// an observer would have seen.
type recordingBus struct {
	mu    sync.Mutex
	ticks []queryprogress.Tick
}

func (inst *recordingBus) Publish(subject string, payload []byte) (err error) {
	tick, err := queryprogress.DecodeTick(payload)
	if err != nil {
		return
	}
	inst.mu.Lock()
	inst.ticks = append(inst.ticks, tick)
	inst.mu.Unlock()
	return
}

func (inst *recordingBus) Subscribe(string, app.MsgHandlerFunc) (unsubscribe func(), err error) {
	unsubscribe = func() {}
	return
}

func (inst *recordingBus) Request(string, []byte) (reply []byte, err error) { return }

func (inst *recordingBus) RequestWithTimeout(string, []byte, time.Duration) (reply []byte, err error) {
	return
}

func (inst *recordingBus) seen(queryID string) (ticks []queryprogress.Tick) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, t := range inst.ticks {
		if t.QueryID == queryID {
			ticks = append(ticks, t)
		}
	}
	return
}

var _ app.BusI = (*recordingBus)(nil)

// TestLiveRunIdReachesQueryLog is R7 end to end through the delivery role: an
// id this process minted, joined against the server's own record of the run.
// Nothing else in the system works if this does not — progress, pins,
// cancellation and the captured facts all correlate by exactly this key.
func TestLiveRunIdReachesQueryLog(t *testing.T) {
	endpoint := liveEndpoint(t)
	eng, err := New(Config{Endpoint: endpoint})
	require.NoError(t, err)
	ctx := context.Background()

	id := runid.Mint("chservertest", "querylog")
	st, _, err := eng.Deliver(ctx, queryengine.Request{
		SQL:    "SELECT 1",
		Format: "TabSeparated",
		RunID:  id,
	})
	require.NoError(t, err)
	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	require.Equal(t, runstream.TerminalComplete, term.State)
	require.Equal(t, "1\n", string(body))

	// query_log is flushed on a timer; ask for it rather than wait it out.
	flush, _, err := eng.Deliver(ctx, queryengine.Request{SQL: "SYSTEM FLUSH LOGS"})
	require.NoError(t, err)
	_, _, err = queryengine.Collect(flush)
	require.NoError(t, err)
	require.NoError(t, flush.Close())

	rows := liveScalar(t, eng, "SELECT count() FROM system.query_log WHERE query_id = {id:String}",
		map[string]string{"id": id})
	assert.NotEqual(t, "0", rows, "the client-minted id must be the server's id for the run")
}

// TestLiveObserveThenKill exercises the two optional roles together, which is
// how they are actually used: watch a run you are not the connection holder
// of, then stop it by the same id.
func TestLiveObserveThenKill(t *testing.T) {
	endpoint := liveEndpoint(t)
	bus := &recordingBus{}
	eng, err := NewObserving(Config{Endpoint: endpoint},
		ObservationConfig{Bus: bus, Interval: queryprogress.MinInterval, Log: zerolog.New(zerolog.NewTestWriter(t))})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id := runid.Mint("chservertest", "killme")
	require.NoError(t, eng.Watch(id))
	defer eng.Unwatch(id)

	// Long enough that the poller gets several ticks and the kill lands
	// while it still runs; `max_block_size` keeps the read chatty so
	// read_rows moves.
	const slow = "SELECT sum(number) FROM numbers_mt(100000000000) SETTINGS max_block_size = 65536"
	type outcome struct {
		term runstream.Terminal
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		st, _, dErr := eng.Deliver(ctx, queryengine.Request{SQL: slow, Format: "TabSeparated", RunID: id})
		if dErr != nil {
			done <- outcome{err: dErr}
			return
		}
		defer func() { _ = st.Close() }()
		_, term, cErr := queryengine.Collect(st)
		done <- outcome{term: term, err: cErr}
	}()

	// Observation: a party holding no connection to this run sees it move.
	//
	// The wait is for a tick reporting rows read, not merely for a tick. A
	// run caught in its first instants honestly reports zero — an
	// observation of nothing yet is still an observation (R8) — so asserting
	// on whichever tick happened to be last is a race with the server, not a
	// property of the poller.
	require.Eventually(t, func() bool {
		eng.Poller().Tick(ctx)
		for _, tick := range bus.seen(id) {
			if tick.Progress.ReadRows > 0 {
				return true
			}
		}
		return false
	}, 15*time.Second, 200*time.Millisecond, "the run must become visible in system.processes, and move")

	// Control: addressed by the same id, on the member that ran it.
	require.NoError(t, eng.Kill(ctx, id))

	select {
	case got := <-done:
		require.NoError(t, got.err, "a killed run still ends with a terminal frame")
		assert.Equal(t, runstream.TerminalFailed, got.term.State,
			"a killed run did not produce a usable result, and must not read as complete")
	case <-time.After(30 * time.Second):
		t.Fatal("the killed run never terminated")
	}

	// The poller never invented that outcome: terminal truth came from the
	// result path, and the watch set is still exactly what was registered.
	assert.Equal(t, []string{id}, eng.Poller().Watched(),
		"a run leaving system.processes must not deregister itself")
}

// TestLiveKillOfAFinishedRunIsNotAnError pins the ambiguity the contract
// refuses to hide: nothing distinguishes a run that already ended from one
// that never existed.
func TestLiveKillOfAFinishedRunIsNotAnError(t *testing.T) {
	endpoint := liveEndpoint(t)
	eng, err := New(Config{Endpoint: endpoint})
	require.NoError(t, err)
	assert.NoError(t, eng.Kill(context.Background(), runid.Mint("chservertest", "ghost")),
		"a kill matching nothing succeeds; a nil error is not evidence of a kill")
}

// liveScalar runs sql and returns its single TabSeparated cell.
func liveScalar(t *testing.T, eng *Engine, sql string, params map[string]string) (value string) {
	t.Helper()
	st, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL: sql, Format: "TabSeparated", Params: params,
	})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	require.Equal(t, runstream.TerminalComplete, term.State, "%s", string(body))
	value = strings.TrimSpace(string(body))
	return
}
