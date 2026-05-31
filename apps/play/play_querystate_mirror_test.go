package play

import (
	"testing"
	"time"
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
