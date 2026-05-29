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
