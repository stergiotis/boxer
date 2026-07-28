package fsmview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// newHistoryTestWidget builds a Widget over a machine with the given history
// cap. Nothing here renders — the tests exercise the History tab's data
// derivation, which touches no c.* op.
func newHistoryTestWidget(t *testing.T, maxHistory int) *Widget[string] {
	t.Helper()
	m := NewMachine("a", maxHistory).
		AddRule("a", "b").
		AddRule("b", "c").
		AddRule("c", "a")
	return New(c.NewWidgetIdStack(), "hist-test", m)
}

func TestDwellBetween(t *testing.T) {
	base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	d, ok := dwellBetween(base, base.Add(1500*time.Millisecond))
	assert.True(t, ok)
	assert.Equal(t, 1500*time.Millisecond, d)

	// Same instant is a legitimate zero dwell, not an unknown.
	d, ok = dwellBetween(base, base)
	assert.True(t, ok)
	assert.Zero(t, d)

	// No predecessor — the oldest retained row.
	_, ok = dwellBetween(time.Time{}, base)
	assert.False(t, ok, "no predecessor must report unknown, not zero")

	// No timestamp on the row itself (maxHistory=0 shape).
	_, ok = dwellBetween(base, time.Time{})
	assert.False(t, ok)

	// Clock stepped back between the two — refuse rather than report negative.
	_, ok = dwellBetween(base, base.Add(-time.Second))
	assert.False(t, ok, "a backwards pair must report unknown")
}

func TestCompactDuration(t *testing.T) {
	assert.Equal(t, "420ms", compactDuration(420*time.Millisecond))
	assert.Equal(t, "0s", compactDuration(0))
	assert.Equal(t, "12.3s", compactDuration(12345*time.Millisecond))
	assert.Equal(t, "2m30s", compactDuration(150*time.Second))
	assert.Equal(t, "1h1m1s", compactDuration(3661*time.Second))
	assert.Equal(t, "—", compactDuration(-time.Second), "a negative dwell never renders as a number")
}

// TestHistoryRows_OrderSeqAndDwell pins the build order the renderer depends
// on: oldest-first with 1-based seq, and a dwell on every row but the first.
func TestHistoryRows_OrderSeqAndDwell(t *testing.T) {
	w := newHistoryTestWidget(t, 8)
	require.NoError(t, w.machine.Transition("b"))
	require.NoError(t, w.machine.Transition("c"))
	require.NoError(t, w.machine.Transition("a"))

	rows := w.historyRows()
	require.Len(t, rows, 3)

	assert.Equal(t, "a", rows[0].tr.From, "historyRows builds oldest-first")
	assert.Equal(t, "a", rows[2].tr.To)
	for i := range rows {
		assert.Equal(t, i+1, rows[i].seq, "seq is 1-based over the retained window")
	}

	assert.False(t, rows[0].hasDwell, "the oldest retained row has no predecessor to measure against")
	for _, r := range rows[1:] {
		assert.True(t, r.hasDwell)
		assert.GreaterOrEqual(t, r.dwell, time.Duration(0))
	}
}

// TestHistoryRows_ReusesBuffer guards the receiver-held buffer: it must be
// truncated on each rebuild, not appended to, or the History tab would grow a
// duplicate row set every frame.
func TestHistoryRows_ReusesBuffer(t *testing.T) {
	w := newHistoryTestWidget(t, 8)
	require.NoError(t, w.machine.Transition("b"))

	require.Len(t, w.historyRows(), 1)
	require.Len(t, w.historyRows(), 1)

	require.NoError(t, w.machine.Transition("c"))
	rows := w.historyRows()
	require.Len(t, rows, 2)
	assert.Equal(t, 2, rows[1].seq)
}

// TestHistoryRows_RingEviction pins the capped case: seq restarts at 1 for
// whatever the ring still holds, and the surviving oldest row loses its dwell
// along with its evicted predecessor.
func TestHistoryRows_RingEviction(t *testing.T) {
	w := newHistoryTestWidget(t, 2)
	require.NoError(t, w.machine.Transition("b"))
	require.NoError(t, w.machine.Transition("c"))
	require.NoError(t, w.machine.Transition("a"))

	rows := w.historyRows()
	require.Len(t, rows, 2, "the ring caps the retained window")
	assert.Equal(t, 1, rows[0].seq)
	assert.Equal(t, "b", rows[0].tr.From, "the a→b transition was evicted")
	assert.False(t, rows[0].hasDwell)
	assert.True(t, rows[1].hasDwell)
}

// TestHistorySnapshot_NewestFirstAndIndependent pins the contract the footer
// hook rests on: the caller gets the displayed order, and a copy it can hand
// to a worker goroutine that outlives the frame.
func TestHistorySnapshot_NewestFirstAndIndependent(t *testing.T) {
	w := newHistoryTestWidget(t, 8)
	require.NoError(t, w.machine.Transition("b"))
	require.NoError(t, w.machine.Transition("c"))

	snap := w.HistorySnapshot()
	require.Len(t, snap, 2)
	assert.Equal(t, "b", snap[0].From, "snapshot is newest-first, the order the table shows")
	assert.Equal(t, "c", snap[0].To)
	assert.Equal(t, "a", snap[1].From)

	// A later rebuild of the render-side buffer must not disturb the copy.
	require.NoError(t, w.machine.Transition("a"))
	_ = w.historyRows()
	assert.Equal(t, "b", snap[0].From)
	assert.Len(t, snap, 2)
}

func TestHistorySnapshot_Empty(t *testing.T) {
	w := newHistoryTestWidget(t, 8)
	assert.Empty(t, w.HistorySnapshot())
	assert.Empty(t, w.historyRows())
}

// TestHistoryFooter_Chains keeps the setter in the fluent family and pins the
// nil default (no footer, so no separator either).
func TestHistoryFooter_Chains(t *testing.T) {
	w := newHistoryTestWidget(t, 8)
	assert.Nil(t, w.historyFooterFn)
	assert.Same(t, w, w.HistoryFooter(func() {}))
	assert.NotNil(t, w.historyFooterFn)
}
