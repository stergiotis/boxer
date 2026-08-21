package play

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
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
	var buf bytes.Buffer
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
	require.Equal(t, "debug", firstLogLevel(out), "the skip must not log at warn")
}

// firstLogLevel plucks the level of the first line zerolog wrote, without a JSON
// dependency: the field is first in zerolog's fixed field order.
func firstLogLevel(out string) string {
	const key = `"level":"`
	i := strings.Index(out, key)
	if i < 0 {
		return ""
	}
	rest := out[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
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
