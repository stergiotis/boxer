package play

import (
	"errors"
	"testing"
	"time"
)

// TestObserveQueryState pins the result↔input lifecycle classification:
// idle vs the three result kinds, the running short-circuit, and the
// stale-on-edit (revert-on-match) behaviour. observeQueryState is pure given
// (loading, numRows, executed, err) plus inst.sql/lastSentSql, so a bare
// PlayApp suffices — no store/FSM needed.
func TestObserveQueryState(t *testing.T) {
	ran := time.Unix(1_700_000_000, 0) // a non-zero "executed" token
	boom := errors.New("boom")
	cases := []struct {
		name      string
		sql, sent string
		loading   bool
		numRows   int64
		executed  time.Time
		err       error
		want      queryStateE
	}{
		{"idle (never ran)", "", "", false, 0, time.Time{}, nil, queryStateIdle},
		{"running", "SELECT 1", "SELECT 1", true, 0, time.Time{}, nil, queryStateRunning},
		{"running wins over edit", "SELECT 2", "SELECT 1", true, 0, ran, nil, queryStateRunning},
		{"rows", "SELECT 1", "SELECT 1", false, 5, ran, nil, queryStateRows},
		{"empty", "SELECT 1 WHERE 0", "SELECT 1 WHERE 0", false, 0, ran, nil, queryStateEmpty},
		{"failed", "bad", "bad", false, 0, ran, boom, queryStateFailed},
		{"rows stale (edited)", "SELECT 2", "SELECT 1", false, 5, ran, nil, queryStateRowsStale},
		{"empty stale (edited)", "SELECT 2", "SELECT 1", false, 0, ran, nil, queryStateEmptyStale},
		{"failed stale (edited)", "SELECT 2", "SELECT 1", false, 0, ran, boom, queryStateFailedStale},
		{"trailing space is not an edit", "SELECT 1  ", "SELECT 1", false, 5, ran, nil, queryStateRows},
		{"empty lastSentSql is not stale", "SELECT 2", "", false, 5, ran, nil, queryStateRows},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &PlayApp{sql: tc.sql, lastSentSql: tc.sent}
			if got := app.observeQueryState(tc.loading, tc.numRows, tc.executed, tc.err); got != tc.want {
				t.Fatalf("observeQueryState(loading=%v numRows=%d executed.zero=%v err=%v) = %v, want %v",
					tc.loading, tc.numRows, tc.executed.IsZero(), tc.err != nil, got, tc.want)
			}
		})
	}
}
