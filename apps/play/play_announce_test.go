package play

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A run that settles is announced once, in words a screen reader speaks;
// moving to or from a stale twin is not a new result (ADR-0363 §M2,
// proposed).
func TestQueryAnnouncement(t *testing.T) {
	cases := []struct {
		prev, cur queryStateE
		rows      int64
		err       error
		text      string
		assertive bool
	}{
		{queryStateRunning, queryStateRows, 1200, nil, "1,200 rows", false},
		{queryStateIdle, queryStateRows, 3, nil, "3 rows", false},
		{queryStateRunning, queryStateEmpty, 0, nil, "0 rows", false},
		{queryStateRunning, queryStateFailed, 0, errors.New("Code: 60. Unknown table\nmore"), "Query failed: Code: 60. Unknown table", true},
		{queryStateRows, queryStateRows, 3, nil, "", false},
		{queryStateRowsStale, queryStateRows, 3, nil, "", false},
		{queryStateRows, queryStateRowsStale, 3, nil, "", false},
		{queryStateIdle, queryStateRunning, 0, nil, "", false},
	}
	for _, c := range cases {
		text, assertive := queryAnnouncement(c.prev, c.cur, c.rows, c.err)
		assert.Equal(t, c.text, text, "%v → %v", c.prev, c.cur)
		assert.Equal(t, c.assertive, assertive, "%v → %v", c.prev, c.cur)
	}
}
