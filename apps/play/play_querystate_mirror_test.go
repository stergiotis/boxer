package play

import (
	"bytes"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/logging"
)

// TestQueryFSMMirrorNeverWedges is the regression guard for the "stuck in
// idle" report. observeQueryState is memoryless, so syncQueryFSM can be
// handed any edge between the eight states — including ones newQueryFSM never
// drew (idle→rows(stale) when a sub-frame-fast first query skips the running
// observation). The mirror must always end up on the proposed state; it must
// never refuse and freeze a frame behind. Assert it for every ordered pair.
func TestQueryFSMMirrorNeverWedges(t *testing.T) {
	all := []queryStateE{
		queryStateIdle, queryStateRunning, queryStateRows, queryStateEmpty,
		queryStateFailed, queryStateRowsStale, queryStateEmptyStale, queryStateFailedStale,
	}
	for _, from := range all {
		for _, to := range all {
			m := newQueryFSM()
			m.Mirror(from) // reach `from` tolerantly
			m.Mirror(to)
			if got := m.Current(); got != to {
				t.Errorf("Mirror(%v) after Mirror(%v) left FSM in %v, want %v", to, from, got, to)
			}
		}
	}
}

// TestQueryFSMIdleToRowsStale reproduces the exact reported edge end to end.
// The memoryless observer yields running → idle → rows(stale): the middle
// idle is the pre-finish-snapshot artifact the store fix removes, but the
// mirror must cope even if one slips through. The FSM must land on
// rows(stale), not wedge in idle.
func TestQueryFSMIdleToRowsStale(t *testing.T) {
	app := &PlayApp{sql: "SELECT 2", lastSentSql: "SELECT 1", queryFSM: newQueryFSM()}
	ran := time.Unix(1_700_000_000, 0) // a non-zero "executed" token

	frames := []struct {
		loading  bool
		numRows  int64
		executed time.Time
	}{
		{true, 0, time.Time{}},  // query in flight        → running
		{false, 0, time.Time{}}, // loading cleared, snapshot still pre-finish → idle
		{false, 5, ran},         // first result lands, editor diverged → rows(stale)
	}
	for _, f := range frames {
		app.syncQueryFSM(f.loading, f.numRows, f.executed, nil)
	}
	if got := app.queryFSM.Current(); got != queryStateRowsStale {
		t.Fatalf("FSM wedged: Current()=%v, want %v", got, queryStateRowsStale)
	}
}

// TestQueryFSMHappyPathStaysDeclared confirms the ordinary lifecycle still
// flows entirely over declared edges (Mirror reports declared=true throughout),
// so the diagnostic log only fires on genuine surprises.
func TestQueryFSMHappyPathStaysDeclared(t *testing.T) {
	m := newQueryFSM()
	steps := []queryStateE{
		queryStateRunning,   // Run
		queryStateRows,      // result
		queryStateRowsStale, // edit
		queryStateRows,      // revert
		queryStateRunning,   // re-run
		queryStateEmpty,     // 0 rows
	}
	for _, s := range steps {
		if declared := m.Mirror(s); !declared {
			t.Errorf("happy-path edge to %v was undeclared (would log)", s)
		}
	}
}

// TestQueryFSMSubFrameRunIsASkipNotAWarning is the reported log line:
//
//	WRN play: query result FSM observed an undeclared edge (mirrored) from=idle to=rows
//
// A local ClickHouse answers a small query in a couple of milliseconds, and
// executeRun fires at the END of a frame (play_renderer.go), after that
// frame's syncQueryFSM — so a run that finishes inside the ~16 ms until the
// next repaint is never sampled as `running` and the observer hands the
// mirror idle→rows. Nothing is wrong: idle→running→rows is precisely the path
// it walked between two samples. It must be graded a skip (debug), leaving
// the warning for a target the declared graph cannot reach at all.
func TestQueryFSMSubFrameRunIsASkipNotAWarning(t *testing.T) {
	prev := log.Logger
	var buf eventBuffer
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = prev }()

	app := &PlayApp{queryFSM: newQueryFSM()}
	ran := time.Unix(1_700_000_000, 0)
	app.syncQueryFSM(false, 0, time.Time{}, nil) // no query yet          → idle
	app.syncQueryFSM(false, 5, ran, nil)         // it ran AND landed here → rows

	require.Equal(t, queryStateRows, app.queryFSM.Current(), "the mirror still follows the edge")
	out := buf.String()
	require.NotContains(t, out, "cannot reach",
		"a sub-frame-fast run is an unsampled skip, not a contradiction of the graph")
	require.Contains(t, out, "skipped states no frame sampled")
	require.Equal(t, "debug", buf.firstLevel(), "the skip must not log at warn")
}

// eventBuffer keeps each zerolog event as the one Write zerolog hands it,
// because under binary_log the events are CBOR and cannot be split on
// newlines; String still serves the substring checks, since CBOR carries
// text strings as plain UTF-8.
type eventBuffer struct {
	bytes.Buffer
	events [][]byte
}

func (inst *eventBuffer) Write(p []byte) (n int, err error) {
	inst.events = append(inst.events, bytes.Clone(p))
	return inst.Buffer.Write(p)
}

// firstLevel decodes the level of the first event in whichever encoding the
// build tags select; empty when there is none.
func (inst *eventBuffer) firstLevel() (level string) {
	if len(inst.events) == 0 {
		return
	}
	v, err := logging.UnmarshallZerologMsg(inst.events[0])
	if err != nil {
		return
	}
	switch m := v.(type) {
	case map[string]any:
		level, _ = m[zerolog.LevelFieldName].(string)
	case map[any]any:
		level, _ = m[zerolog.LevelFieldName].(string)
	}
	return
}

// TestQueryFSMIdleIsTheOnlyUnreachableState pins the invariant the grading
// rests on. Every settled state can re-Run and every run settles, so the
// declared graph reaches every state from every other one — except idle,
// which has no in-edges at all: a lane that has run once never goes back to
// "never ran". So *→idle is the one observation the model calls impossible,
// and the one that keeps the warning — which is exactly how the torn
// (loading, executed) read announced itself.
func TestQueryFSMIdleIsTheOnlyUnreachableState(t *testing.T) {
	m := newQueryFSM()
	all := []queryStateE{
		queryStateIdle, queryStateRunning, queryStateRows, queryStateEmpty,
		queryStateFailed, queryStateRowsStale, queryStateEmptyStale, queryStateFailedStale,
	}
	for _, from := range all {
		for _, to := range all {
			if from == to {
				continue
			}
			require.Equalf(t, to != queryStateIdle, m.CanReach(from, to),
				"CanReach(%v, %v)", from, to)
		}
	}
}
