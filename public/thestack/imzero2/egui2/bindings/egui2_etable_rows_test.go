package bindings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A table that has reported no visible range emits a screenful, never the
// whole table: one that never reaches the screen reports nothing for as long
// as it stays hidden.
func TestEtFallbackRows(t *testing.T) {
	type rows struct{ lo, hi uint64 }
	got := func(n, screenful uint64, anchor int64) rows {
		lo, hi := EtFallbackRows(n, screenful, anchor)
		return rows{lo, hi}
	}
	assert.Equal(t, rows{0, 30}, got(5000, 30, -1), "no anchor: the first screenful")
	assert.Equal(t, rows{0, 10}, got(10, 30, -1), "a short table: all of it")
	assert.Equal(t, rows{0, 0}, got(0, 30, -1), "an empty table: nothing")
	assert.Equal(t, rows{4970, 5000}, got(5000, 30, 4999), "anchored at the tail: the last screenful")
	assert.Equal(t, rows{2471, 2530}, got(5000, 30, 2500), "anchored mid-table: a screenful either side")
	assert.Equal(t, rows{0, 31}, got(5000, 30, 1), "anchored near the top: clamped at 0")
	assert.Equal(t, rows{0, 10}, got(10, 30, 99), "an anchor past the end is the last row")
}

func TestEtScreenfulRows(t *testing.T) {
	assert.Equal(t, uint64(21), EtScreenfulRows(400, 20), "whole rows plus one cut by the edge")
	assert.Equal(t, uint64(22), EtScreenfulRows(410, 20), "a partial row rounds up")
	assert.Equal(t, uint64(etUnreportedPaneHeight/20+1), EtScreenfulRows(0, 20), "no height: a tall display's")
	assert.Equal(t, uint64(etUnreportedPaneHeight+1), EtScreenfulRows(0, 0), "a degenerate row height stays bounded")
}
