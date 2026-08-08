package queryprogress

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBus records what was published. It is the whole consumer side these
// tests need — the plane's contract is about what is published and what is
// not.
type fakeBus struct {
	mu       sync.Mutex
	subjects []string
	ticks    []Tick
}

func (inst *fakeBus) Publish(subject string, payload []byte) (err error) {
	t, err := DecodeTick(payload)
	if err != nil {
		return
	}
	inst.mu.Lock()
	inst.subjects = append(inst.subjects, subject)
	inst.ticks = append(inst.ticks, t)
	inst.mu.Unlock()
	return
}

// The poller only publishes; the rest of BusI exists to satisfy the
// interface and must never be reached.
func (inst *fakeBus) Subscribe(string, app.MsgHandlerFunc) (func(), error) {
	panic("queryprogress must not subscribe")
}

// RequestWithTimeout delegates: the fake answers instantly, so the wait
// never matters here.
func (inst *fakeBus) RequestWithTimeout(subject string, payload []byte, _ time.Duration) ([]byte, error) {
	return inst.Request(subject, payload)
}

func (inst *fakeBus) Request(string, []byte) ([]byte, error) {
	panic("queryprogress must not make requests")
}

func (inst *fakeBus) seen() (subjects []string, ticks []Tick) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]string(nil), inst.subjects...), append([]Tick(nil), inst.ticks...)
}

// fakeServer answers the tick query with staged rows and records the
// statements it was asked.
type fakeServer struct {
	mu    sync.Mutex
	sqls  []string
	rows  func(sql string) string
	srv   *httptest.Server
	calls int
}

func newFakeServer(t *testing.T, rows func(sql string) string) *fakeServer {
	t.Helper()
	f := &fakeServer{rows: rows}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.sqls = append(f.sqls, string(body))
		f.calls++
		f.mu.Unlock()
		_, _ = w.Write([]byte(f.rows(string(body))))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (inst *fakeServer) seen() (sqls []string, calls int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]string(nil), inst.sqls...), inst.calls
}

func newTestPoller(t *testing.T, srv *fakeServer, bus *fakeBus) *Poller {
	t.Helper()
	p, err := New(Options{Endpoint: srv.srv.URL, Bus: bus, Log: zerolog.Nop()})
	require.NoError(t, err)
	return p
}

// row renders one system.processes row in the tick's TabSeparated shape.
func row(id string, readRows, readBytes, total, elapsedNs, mem uint64) string {
	return fmt.Sprintf("%s\t%d\t%d\t%d\t%d\t%d\n", id, readRows, readBytes, total, elapsedNs, mem)
}

func TestPollerPublishesTicksForWatchedRuns(t *testing.T) {
	srv := newFakeServer(t, func(string) string {
		return row("play-main-1-1", 500, 4096, 10000, 250000000, 1<<20)
	})
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)
	require.NoError(t, p.Watch("play-main-1-1"))

	p.Tick(context.Background())

	subjects, ticks := bus.seen()
	require.Len(t, ticks, 1)
	assert.Equal(t, "queryrun.progress.play-main-1-1", subjects[0])
	assert.Equal(t, "play-main-1-1", ticks[0].QueryID)
	assert.Equal(t, uint64(500), ticks[0].Progress.ReadRows)
	assert.Equal(t, uint64(10000), ticks[0].Progress.TotalRowsToRead)
	assert.Equal(t, uint64(250000000), ticks[0].Progress.ElapsedNs)

	// A tick lifts cleanly into a runstream progress frame.
	f := FrameOf[string](ticks[0])
	assert.Equal(t, runstream.KindProgress, f.Kind)
	assert.Equal(t, uint64(500), f.Progress.ReadRows)
}

// TestPollerBatchesOneQueryPerTick: thirty watched runs must not become
// thirty round trips, and the ids must share one consistent view.
func TestPollerBatchesOneQueryPerTick(t *testing.T) {
	srv := newFakeServer(t, func(string) string {
		var b strings.Builder
		for i := range 3 {
			b.WriteString(row(fmt.Sprintf("play-lane%d-1-1", i), uint64(i), 0, 0, 0, 0))
		}
		return b.String()
	})
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)
	for i := range 3 {
		require.NoError(t, p.Watch(fmt.Sprintf("play-lane%d-1-1", i)))
	}

	p.Tick(context.Background())

	sqls, calls := srv.seen()
	assert.Equal(t, 1, calls, "one query per tick, whatever the watch-set size")
	require.Len(t, sqls, 1)
	for i := range 3 {
		assert.Contains(t, sqls[0], fmt.Sprintf("'play-lane%d-1-1'", i))
	}
	_, ticks := bus.seen()
	assert.Len(t, ticks, 3, "one frame per reported run")
}

// TestPollerSelfExcludes is structural rather than a filter: the poller
// reports only ids that were registered, and its own statement is never
// one of them. A server that echoes an unregistered row must not produce a
// frame for it.
func TestPollerSelfExcludes(t *testing.T) {
	srv := newFakeServer(t, func(string) string {
		return row("play-main-1-1", 1, 0, 0, 0, 0) +
			row("queryprogress-own-tick", 99, 0, 0, 0, 0)
	})
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)
	require.NoError(t, p.Watch("play-main-1-1"))

	p.Tick(context.Background())

	_, ticks := bus.seen()
	require.Len(t, ticks, 1)
	assert.Equal(t, "play-main-1-1", ticks[0].QueryID, "an unregistered run is never reported")
}

// TestPollerVanishIsNotATerminal is the R8 rule. A run leaving
// system.processes is ambiguous — finished, killed, or failed — so the
// poller must neither publish anything about it nor deregister it. Only the
// party holding the result path knows, and it says so by calling Unwatch.
func TestPollerVanishIsNotATerminal(t *testing.T) {
	present := true
	srv := newFakeServer(t, func(string) string {
		if !present {
			return ""
		}
		return row("play-main-1-1", 10, 0, 0, 0, 0)
	})
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)
	require.NoError(t, p.Watch("play-main-1-1"))

	p.Tick(context.Background())
	_, ticks := bus.seen()
	require.Len(t, ticks, 1)

	// The run vanishes.
	present = false
	p.Tick(context.Background())
	p.Tick(context.Background())

	_, ticks = bus.seen()
	assert.Len(t, ticks, 1, "vanishing publishes nothing — not a terminal, not anything")
	assert.Equal(t, []string{"play-main-1-1"}, p.Watched(),
		"the poller must not deregister on its own; only the result path knows the outcome")

	// It comes back (a long-running run the server briefly did not list):
	// ticks resume, because nothing was concluded.
	present = true
	p.Tick(context.Background())
	_, ticks = bus.seen()
	assert.Len(t, ticks, 2)
}

// TestPollerSubTickRunProducesNothing: a run that starts and finishes
// between ticks never appears. Absence of a frame carries no meaning, and
// this is the case that proves it.
func TestPollerSubTickRunProducesNothing(t *testing.T) {
	srv := newFakeServer(t, func(string) string { return "" })
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)
	require.NoError(t, p.Watch("play-main-1-1"))

	p.Tick(context.Background())
	p.Unwatch("play-main-1-1") // the result path delivered the terminal

	_, ticks := bus.seen()
	assert.Empty(t, ticks)
	assert.Empty(t, p.Watched())
}

func TestPollerNoWatchedRunsSkipsTheQuery(t *testing.T) {
	srv := newFakeServer(t, func(string) string { return "" })
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)

	p.Tick(context.Background())

	_, calls := srv.seen()
	assert.Zero(t, calls, "an empty watch set must not query the server")
}

// TestPollerSequenceStrictlyIncreasesPerRun keeps the ticks collectible: a
// runstream collector rejects a repeated or reordered sequence number.
func TestPollerSequenceStrictlyIncreasesPerRun(t *testing.T) {
	srv := newFakeServer(t, func(string) string {
		return row("a", 1, 0, 0, 0, 0) + row("b", 2, 0, 0, 0, 0)
	})
	bus := &fakeBus{}
	p := newTestPoller(t, srv, bus)
	require.NoError(t, p.Watch("a"))
	require.NoError(t, p.Watch("b"))

	for range 3 {
		p.Tick(context.Background())
	}

	_, ticks := bus.seen()
	perRun := map[string][]uint64{}
	for _, tk := range ticks {
		perRun[tk.QueryID] = append(perRun[tk.QueryID], tk.Seq)
	}
	for id, seqs := range perRun {
		require.Len(t, seqs, 3, "id=%s", id)
		for i := 1; i < len(seqs); i++ {
			assert.Greater(t, seqs[i], seqs[i-1], "id=%s sequence must strictly increase", id)
		}
	}

	// And they collect without complaint.
	var col runstream.Collector[string]
	for _, seq := range perRun["a"] {
		require.NoError(t, col.Push(runstream.ProgressFrame[string](runstream.Seq(seq), runstream.Progress{})))
	}
	_, err := col.Terminal()
	assert.ErrorIs(t, err, runstream.ErrIncomplete, "progress alone never completes a stream")
}

// TestPollerFailedTickSaysNothing: a tick that could not reach the server
// is a missed observation, not a statement about any run.
func TestPollerFailedTickSaysNothing(t *testing.T) {
	srv := newFakeServer(t, func(string) string { return "" })
	bus := &fakeBus{}
	p, err := New(Options{Endpoint: srv.srv.URL, Bus: bus, Log: zerolog.Nop()})
	require.NoError(t, err)
	require.NoError(t, p.Watch("play-main-1-1"))
	srv.srv.Close() // the server goes away

	p.Tick(context.Background())

	_, ticks := bus.seen()
	assert.Empty(t, ticks)
	assert.Equal(t, []string{"play-main-1-1"}, p.Watched(), "a failed tick deregisters nothing")
}

func TestPollerRejectsUnsafeQueryIDs(t *testing.T) {
	srv := newFakeServer(t, func(string) string { return "" })
	p := newTestPoller(t, srv, &fakeBus{})

	for _, bad := range []string{
		"", "has space", "quote'injection", "wild*card", "dot.>", "semi;colon",
		strings.Repeat("x", 129),
	} {
		assert.Error(t, p.Watch(bad), "id=%q", bad)
	}
	for _, good := range []string{"play-main-7-3", "a", "a.b:c_d-e"} {
		assert.NoError(t, p.Watch(good), "id=%q", good)
	}
}

func TestPollSQLQuotesEveryId(t *testing.T) {
	sql := pollSQL([]string{"a", "b"})
	assert.Contains(t, sql, "query_id IN ('a','b')")
	assert.Contains(t, sql, "FROM system.processes")
	assert.Contains(t, sql, "FORMAT TabSeparated")
}

func TestParseProcessRowsSkipsMalformedLines(t *testing.T) {
	raw := row("a", 1, 2, 3, 4, 5) + "not\ta\trow\n" + "\n" + row("b", 6, 7, 8, 9, 10)
	rows := parseProcessRows(raw)
	require.Len(t, rows, 2, "a malformed line is skipped, not fatal")
	assert.Equal(t, "a", rows[0].queryID)
	assert.Equal(t, "b", rows[1].queryID)
	assert.Equal(t, uint64(6), rows[1].progress.ReadRows)
}

func TestNewValidatesAndClampsInterval(t *testing.T) {
	bus := &fakeBus{}
	_, err := New(Options{Bus: bus})
	assert.Error(t, err, "Endpoint is required")
	_, err = New(Options{Endpoint: "http://x.invalid"})
	assert.Error(t, err, "Bus is required")

	p, err := New(Options{Endpoint: "http://x.invalid", Bus: bus, Interval: time.Nanosecond})
	require.NoError(t, err)
	assert.Equal(t, MinInterval, p.interval)

	p, err = New(Options{Endpoint: "http://x.invalid", Bus: bus, Interval: time.Hour})
	require.NoError(t, err)
	assert.Equal(t, MaxInterval, p.interval)

	p, err = New(Options{Endpoint: "http://x.invalid", Bus: bus})
	require.NoError(t, err)
	assert.Equal(t, DefaultInterval, p.interval)
}

func TestPollerStartStop(t *testing.T) {
	srv := newFakeServer(t, func(string) string { return row("a", 1, 0, 0, 0, 0) })
	bus := &fakeBus{}
	p, err := New(Options{Endpoint: srv.srv.URL, Bus: bus, Interval: MinInterval, Log: zerolog.Nop()})
	require.NoError(t, err)
	require.NoError(t, p.Watch("a"))

	p.Start()
	require.Eventually(t, func() bool {
		_, ticks := bus.seen()
		return len(ticks) > 0
	}, 3*time.Second, 5*time.Millisecond, "the loop never ticked")
	require.NoError(t, p.Close())
	require.NoError(t, p.Close(), "Close is idempotent")
}
