//go:build llm_generated_opus47

package fsmview

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMachine_initial confirms the initial state is observed and the
// FSM reports it via Current() before any transitions fire.
func TestNewMachine_initial(t *testing.T) {
	m := NewMachine("red", 4)
	assert.Equal(t, "red", m.Current())
	assert.Equal(t, 0, m.HistoryLen())
	_, ok := m.LastTransition()
	assert.False(t, ok, "fresh machine must report no last transition")
}

// TestAddRule_observesStates checks that AddRule populates the display
// order with both endpoints (in addition to forwarding to the FSM rules
// for CanTransition queries).
func TestAddRule_observesStates(t *testing.T) {
	m := NewMachine("a", 4)
	m.AddRule("a", "b", "c").AddRule("b", "a")
	want := []string{"a", "b", "c"}
	got := slices.Collect(m.States())
	assert.Equal(t, want, got, "States() must yield initial + AddRule endpoints in insertion order")
	assert.True(t, m.CanTransition("b"))
	assert.True(t, m.CanTransition("c"))
	assert.False(t, m.CanTransition("a"), "self-loop without rule must reject")
}

// TestWithStateOrder pins the display order independent of insertion.
// Useful when a domain wants states laid out by natural workflow order
// rather than first-seen.
func TestWithStateOrder(t *testing.T) {
	pinned := []string{"green", "yellow", "red"}
	m := NewMachine("red", 4, WithStateOrder(pinned))
	m.AddRule("red", "green").AddRule("green", "yellow")
	got := slices.Collect(m.States())
	assert.Equal(t, pinned, got, "WithStateOrder must override insertion order")
}

// TestEdges enumerates edges with their labels; unlabeled edges read as
// empty strings rather than missing entries.
func TestEdges(t *testing.T) {
	m := NewMachine("a", 4)
	m.AddRule("a", "b", "c").
		EdgeLabel("a", "b", "trigger-ab")

	type pair struct {
		k   EdgeKey[string]
		lbl string
	}
	var got []pair
	for k, lbl := range m.Edges() {
		got = append(got, pair{k, lbl})
	}
	require.Len(t, got, 2)
	assert.Equal(t, EdgeKey[string]{From: "a", To: "b"}, got[0].k)
	assert.Equal(t, "trigger-ab", got[0].lbl)
	assert.Equal(t, EdgeKey[string]{From: "a", To: "c"}, got[1].k)
	assert.Equal(t, "", got[1].lbl, "unlabeled edge must yield empty label, not skip")
}

// TestEdgeLabel_clear confirms passing "" wipes the entry rather than
// recording an empty-labeled edge.
func TestEdgeLabel_clear(t *testing.T) {
	m := NewMachine("a", 4)
	m.AddRule("a", "b").EdgeLabel("a", "b", "trig").EdgeLabel("a", "b", "")
	for k, lbl := range m.Edges() {
		assert.Equal(t, EdgeKey[string]{From: "a", To: "b"}, k)
		assert.Equal(t, "", lbl)
	}
}

// TestTransition_recordsHistory exercises the round-trip: a valid
// Transition appends to history, Current() reflects the new state,
// LastTransition() returns the most-recent record.
func TestTransition_recordsHistory(t *testing.T) {
	m := NewMachine("red", 4)
	m.AddRule("red", "green").
		AddRule("green", "yellow").
		AddRule("yellow", "red")
	before := time.Now()
	require.NoError(t, m.Transition("green"))
	require.NoError(t, m.Transition("yellow"))
	assert.Equal(t, "yellow", m.Current())
	assert.Equal(t, 2, m.HistoryLen())

	last, ok := m.LastTransition()
	require.True(t, ok)
	assert.Equal(t, "green", last.From)
	assert.Equal(t, "yellow", last.To)
	assert.False(t, last.At.IsZero(), "non-zero maxHistory must populate timestamps")
	assert.False(t, last.At.Before(before), "timestamp must be on the wall clock side of the call")
}

// TestTransition_invalid leaves the FSM and history untouched when the
// rule isn't permitted.
func TestTransition_invalid(t *testing.T) {
	m := NewMachine("red", 4)
	m.AddRule("red", "green")
	err := m.Transition("yellow")
	assert.Error(t, err, "invalid transition must surface statetrooper's error")
	assert.Equal(t, "red", m.Current())
	assert.Equal(t, 0, m.HistoryLen())
}

// TestMirror_declaredEdge behaves exactly like Transition for a declared
// edge: advances Current, records history, reports declared=true.
func TestMirror_declaredEdge(t *testing.T) {
	m := NewMachine("red", 4).AddRule("red", "green")
	assert.True(t, m.Mirror("green"), "declared edge must report declared=true")
	assert.Equal(t, "green", m.Current())
	assert.Equal(t, 1, m.HistoryLen())
}

// TestMirror_undeclaredEdgeFollows is the regression guard for the wedge a
// rejecting Transition causes when the producer is memoryless: Mirror must
// follow an undeclared edge (Current advances, the real edge is recorded in
// history) and report declared=false — never refuse and stick a state behind.
func TestMirror_undeclaredEdgeFollows(t *testing.T) {
	m := NewMachine("red", 4).AddRule("red", "green")
	assert.False(t, m.Mirror("yellow"), "undeclared edge must report declared=false")
	assert.Equal(t, "yellow", m.Current(), "Mirror must follow the undeclared edge, not wedge")
	require.Equal(t, 1, m.HistoryLen(), "the forced edge is still recorded in history")
	last, ok := m.LastTransition()
	require.True(t, ok)
	assert.Equal(t, EdgeKey[string]{From: "red", To: "yellow"},
		EdgeKey[string]{From: last.From, To: last.To})
	// The forced edge is taught to the underlying FSM once: after cycling
	// back to red, red→yellow now reads as a declared transition.
	m.Mirror("red") // forces yellow→red (no such rule either)
	assert.True(t, m.CanTransition("yellow"), "a forced edge is taught once, then declared")
}

// TestMirror_undeclaredEdgeNotDrawn confirms the forced edge is taught to the
// underlying FSM (so Current/CanTransition stay honest) but does NOT leak
// into the drawn rule graph — Edges() keeps only what AddRule declared, while
// States() gains the forced node so it still renders.
func TestMirror_undeclaredEdgeNotDrawn(t *testing.T) {
	m := NewMachine("red", 4).AddRule("red", "green")
	m.Mirror("yellow")
	var edges []EdgeKey[string]
	for k := range m.Edges() {
		edges = append(edges, k)
	}
	assert.Equal(t, []EdgeKey[string]{{From: "red", To: "green"}}, edges,
		"Mirror's forced edge must not appear in the drawn graph")
	assert.Contains(t, slices.Collect(m.States()), "yellow",
		"the forced state must become a known node")
}

// TestMirror_sameStateNoop guards against self-loop history spam when the
// observed state already matches Current.
func TestMirror_sameStateNoop(t *testing.T) {
	m := NewMachine("red", 4)
	assert.True(t, m.Mirror("red"))
	assert.Equal(t, 0, m.HistoryLen(), "same-state Mirror must not record a self-loop")
}

// TestMirrorWithMetadata_recordsReason confirms the metadata attached to a
// mirrored transition surfaces on the recorded Transition — the History view's
// "why did this fire" reading (e.g. a validity mirror's rejection reason).
func TestMirrorWithMetadata_recordsReason(t *testing.T) {
	m := NewMachine("red", 4).AddRule("red", "green")
	assert.True(t, m.MirrorWithMetadata("green", map[string]string{"reason": "light cycled"}),
		"declared edge must still report declared=true with metadata")
	assert.Equal(t, "green", m.Current())
	last, ok := m.LastTransition()
	require.True(t, ok)
	assert.Equal(t, "light cycled", last.Metadata["reason"],
		"mirrored metadata must surface on the recorded transition")
}

// TestMirrorWithMetadata_undeclaredCarriesMetadata confirms a forced
// (undeclared) edge records its metadata too, and still reports declared=false.
func TestMirrorWithMetadata_undeclaredCarriesMetadata(t *testing.T) {
	m := NewMachine("red", 4).AddRule("red", "green")
	assert.False(t, m.MirrorWithMetadata("yellow", map[string]string{"reason": "forced"}))
	last, ok := m.LastTransition()
	require.True(t, ok)
	assert.Equal(t, "forced", last.Metadata["reason"])
}

// TestMirrorWithMetadata_sameStateNoop confirms a same-state mirror records
// nothing even with metadata (no self-loop spam), matching Mirror.
func TestMirrorWithMetadata_sameStateNoop(t *testing.T) {
	m := NewMachine("red", 4)
	assert.True(t, m.MirrorWithMetadata("red", map[string]string{"reason": "x"}))
	assert.Equal(t, 0, m.HistoryLen(), "same-state mirror must not record even with metadata")
}

// TestHistory_orderChronological iterates oldest → newest, mirroring
// statetrooper's append-only ordering. Used by the History tab's "scroll
// back through time" reading.
func TestHistory_orderChronological(t *testing.T) {
	m := NewMachine("red", 4).
		AddRule("red", "green").
		AddRule("green", "yellow").
		AddRule("yellow", "red")
	require.NoError(t, m.Transition("green"))
	require.NoError(t, m.Transition("yellow"))
	require.NoError(t, m.Transition("red"))

	var got []EdgeKey[string]
	for tr := range m.History() {
		got = append(got, EdgeKey[string]{From: tr.From, To: tr.To})
	}
	want := []EdgeKey[string]{
		{"red", "green"},
		{"green", "yellow"},
		{"yellow", "red"},
	}
	assert.Equal(t, want, got)
}

// TestHistoryReverse iterates newest → oldest, the order the History tab
// renders so the latest activity is at the top.
func TestHistoryReverse(t *testing.T) {
	m := NewMachine("a", 4).AddRule("a", "b").AddRule("b", "a")
	require.NoError(t, m.Transition("b"))
	require.NoError(t, m.Transition("a"))
	var got []string
	for tr := range m.HistoryReverse() {
		got = append(got, tr.From+"→"+tr.To)
	}
	assert.Equal(t, []string{"b→a", "a→b"}, got)
}

// TestNodeId_stable confirms that the FNV-derived node id is stable per
// state across repeated calls — load-bearing for egui_graphs to retain
// layout positions across frames.
func TestNodeId_stable(t *testing.T) {
	m := NewMachine("red", 4)
	first := m.NodeId("red")
	second := m.NodeId("red")
	assert.Equal(t, first, second)
	assert.NotZero(t, first, "node id must not be 0 (reserved sentinel)")
}

// TestNodeId_differentStates yields distinct ids for distinct labels —
// not a guarantee in general (FNV can collide), but holds for the small
// alphabet used here and guards against an accidental constant.
func TestNodeId_differentStates(t *testing.T) {
	m := NewMachine("a", 4)
	assert.NotEqual(t, m.NodeId("a"), m.NodeId("b"))
}

// TestColor_currentVsRest uses the default StateColorFn: active state
// lights with AccentDefault, others fall to NeutralSubtle.
func TestColor_currentVsRest(t *testing.T) {
	m := NewMachine("red", 4).AddRule("red", "green")
	currentColor := m.Color("red")
	otherColor := m.Color("green")
	assert.NotEqual(t, currentColor, otherColor,
		"default scheme must distinguish active state from rest")
}
