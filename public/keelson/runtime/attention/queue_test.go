package attention

import (
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct{ items []Item }

func (inst *fakeSource) WaitingItems() iter.Seq[Item] { return slices.Values(inst.items) }

var t0 = time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)

func keys(es []Entry) (out []string) {
	for _, e := range es {
		out = append(out, e.Key)
	}
	return
}

func TestOrderByLevelThenAge(t *testing.T) {
	src := &fakeSource{items: []Item{
		{Key: "q-old", Kind: KindQuestion, Created: t0},
		{Key: "confirm", Kind: KindConfirmation, Created: t0.Add(3 * time.Minute)},
		{Key: "grant", Kind: KindGrantRequest, Created: t0.Add(time.Minute)},
	}}
	q := NewQueue(nil, 0)
	q.AddSource(src)
	open, shelved := q.Snapshot(t0.Add(4 * time.Minute))
	assert.Equal(t, []string{"confirm", "q-old", "grant"}, keys(open))
	assert.Empty(t, shelved)
}

func TestWaitingNeverBelowYourTurn(t *testing.T) {
	src := &fakeSource{items: []Item{{Key: "odd", Kind: "turn", Created: t0}}}
	q := NewQueue(nil, 0)
	q.AddSource(src)
	open, _ := q.Snapshot(t0)
	require.Len(t, open, 1)
	assert.Equal(t, LevelYourTurn, open[0].Level)
}

func TestShelveIsBoundedAndReturns(t *testing.T) {
	it := Item{Key: "w", Kind: KindWidening, Created: t0, Bound: t0.Add(20 * time.Minute), BoundWhy: "expires"}
	src := &fakeSource{items: []Item{it}}
	q := NewQueue(nil, 0)
	q.AddSource(src)
	q.Snapshot(t0)
	until, ok := q.Shelve(it, t0.Add(time.Hour), t0)
	require.True(t, ok)
	assert.Equal(t, it.Bound, until, "shelving extends no clock")
	open, shelved := q.Snapshot(t0.Add(10 * time.Minute))
	assert.Empty(t, open)
	assert.Equal(t, []string{"w"}, keys(shelved))
	open, _ = q.Snapshot(t0.Add(20 * time.Minute))
	require.Len(t, open, 1)
	assert.False(t, open[0].Acked, "back from the shelf, unacknowledged")
	_, ok = q.Shelve(it, t0.Add(time.Hour), t0.Add(25*time.Minute))
	assert.False(t, ok, "past its bound")
}

func TestEscalationAndOpens(t *testing.T) {
	src := &fakeSource{items: []Item{
		{Key: "q", Kind: KindQuestion, Created: t0},
		{Key: "c", Kind: KindConfirmation, Created: t0},
	}}
	q := NewQueue(nil, 5*time.Minute)
	q.AddSource(src)
	open, _ := q.Snapshot(t0.Add(time.Minute))
	assert.Equal(t, []string{"c"}, q.TakeOpens(open), "a critical item opens at once")
	assert.Empty(t, q.TakeOpens(open), "once")
	open, _ = q.Snapshot(t0.Add(6 * time.Minute))
	assert.Equal(t, []string{"q"}, q.TakeOpens(open), "escalated after the interval")

	src.items = append(src.items, Item{Key: "q2", Kind: KindQuestion, Created: t0})
	open, _ = q.Snapshot(t0.Add(6 * time.Minute))
	q.Acknowledge(open[2].Item, t0.Add(6*time.Minute))
	open, _ = q.Snapshot(t0.Add(7 * time.Minute))
	assert.Empty(t, q.TakeOpens(open), "acknowledged items do not escalate")
}

func TestStateDroppedWithItem(t *testing.T) {
	it := Item{Key: "g", Kind: KindGrantRequest, Created: t0}
	src := &fakeSource{items: []Item{it}}
	q := NewQueue(nil, 0)
	q.AddSource(src)
	q.Snapshot(t0)
	var acts []ActE
	q.SetOnAct(func(a ActE, _ Item, _ time.Time) { acts = append(acts, a) })
	q.Acknowledge(it, t0)
	src.items = nil
	q.Snapshot(t0)
	src.items = []Item{it}
	open, _ := q.Snapshot(t0)
	assert.False(t, open[0].Acked)
	assert.Equal(t, []ActE{ActAcknowledge}, acts)
}

func TestFlood(t *testing.T) {
	f := NewFlood(2, 3)
	ok, _ := f.Admit("a", t0)
	assert.True(t, ok)
	ok, _ = f.Admit("a", t0)
	assert.True(t, ok)
	ok, why := f.Admit("a", t0)
	assert.False(t, ok, "per stream")
	assert.Contains(t, why, "this conversation")
	ok, _ = f.Admit("b", t0)
	assert.True(t, ok)
	ok, why = f.Admit("c", t0)
	assert.False(t, ok, "per person")
	assert.Contains(t, why, "decisions were asked")
	ok, _ = f.Admit("a", t0.Add(FloodWindow+time.Second))
	assert.True(t, ok, "the window slides")
	var none *Flood
	ok, _ = none.Admit("a", t0)
	assert.True(t, ok)
}
