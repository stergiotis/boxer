package trailviewer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows are newest first; the neighbour in time order is the other way
// round, and without a selection the ends are the entry points.
func TestNeighbourRowWalksTimeOrder(t *testing.T) {
	rows := fixtureRows() // ids 6 (newest) … 1 (oldest)
	id, ok := neighbourRow(rows, 0, false, 1)
	assert.True(t, ok)
	assert.Equal(t, uint64(1), id, "nothing selected: forward starts at the oldest")
	id, ok = neighbourRow(rows, 0, false, -1)
	assert.True(t, ok)
	assert.Equal(t, uint64(6), id, "backward starts at the newest")
	id, ok = neighbourRow(rows, 3, true, 1)
	assert.True(t, ok)
	assert.Equal(t, uint64(4), id)
	id, ok = neighbourRow(rows, 3, true, -1)
	assert.True(t, ok)
	assert.Equal(t, uint64(2), id)
	_, ok = neighbourRow(rows, 6, true, 1)
	assert.False(t, ok, "nothing after the newest")
	_, ok = neighbourRow(rows, 1, true, -1)
	assert.False(t, ok, "nothing before the oldest")
	id, ok = neighbourRow(rows, 99, true, 1)
	assert.True(t, ok)
	assert.Equal(t, uint64(1), id, "a selection the table does not hold is no selection")
	_, ok = neighbourRow(nil, 0, false, 1)
	assert.False(t, ok)
	place, of := rowPlace(rows, 3, true)
	assert.Equal(t, [2]int{3, 6}, [2]int{place, of})
	place, _ = rowPlace(rows, 99, true)
	assert.Equal(t, 0, place)
}

// Play steps the selection through the held rows one dwell at a time, from
// the oldest when nothing is selected, and stops at the newest; pressed at
// the newest it starts over (ADR-0350 §SD13).
func TestReplayVisitsEveryHeldRowOnceAndStops(t *testing.T) {
	inst, _ := opsFixture(t)
	hold(inst, fixtureRows())
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	inst.toggleReplay(now)
	require.True(t, inst.replay.playing)
	assert.Equal(t, uint64(1), inst.selectionId(), "starts at the oldest")
	inst.tickReplay(now.Add(replayDwell / 2))
	assert.Equal(t, uint64(1), inst.selectionId(), "the dwell is not over")
	var visited []uint64
	for i := 1; i <= 6; i++ {
		now = now.Add(replayDwell)
		inst.tickReplay(now)
		visited = append(visited, inst.selectionId())
	}
	assert.Equal(t, []uint64{2, 3, 4, 5, 6, 6}, visited)
	assert.False(t, inst.replay.playing, "stops at the newest row")
	assert.True(t, inst.hasSelected, "and leaves it selected")

	inst.toggleReplay(now)
	assert.True(t, inst.replay.playing)
	assert.Equal(t, uint64(1), inst.selectionId(), "play at the newest starts over")
	inst.toggleReplay(now)
	assert.False(t, inst.replay.playing, "and pauses")
	assert.Equal(t, uint64(1), inst.selectionId(), "where it is")

	require.True(t, inst.selectById(3))
	inst.toggleReplay(now)
	assert.Equal(t, uint64(3), inst.selectionId(), "play from a row shows it first")
	inst.tickReplay(now.Add(replayDwell))
	assert.Equal(t, uint64(4), inst.selectionId())

	hold(inst, fixtureRows()[:2])
	inst.tickReplay(now.Add(2 * replayDwell))
	assert.True(t, inst.replay.playing)
	assert.Equal(t, uint64(5), inst.selectionId(), "a selected row the refill no longer holds: replay goes on from the oldest held")
}

// Prev row and Next row are the same walk, one step at a time.
func TestStepRowMovesTheSelection(t *testing.T) {
	inst, _ := opsFixture(t)
	hold(inst, fixtureRows())
	inst.stepRow(1)
	assert.Equal(t, uint64(1), inst.selectionId())
	inst.stepRow(1)
	assert.Equal(t, uint64(2), inst.selectionId())
	inst.stepRow(-1)
	assert.Equal(t, uint64(1), inst.selectionId())
	inst.stepRow(-1)
	assert.Equal(t, uint64(1), inst.selectionId(), "nothing before the oldest")
}
