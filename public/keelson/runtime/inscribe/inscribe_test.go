package inscribe

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func win(k uint64) Anchor { return Anchor{Window: k} }

func TestAPutReplacesByTaskAndIdAndRefusesWhatTheOpDoesNotTake(t *testing.T) {
	s := NewScene()
	require.NoError(t, s.Put(Mark{Task: "a", Id: "x", Op: OpHighlight, Targets: []Anchor{win(1)}}))
	require.NoError(t, s.Put(Mark{Task: "a", Id: "x", Op: OpCallout, Targets: []Anchor{win(1)}, Text: "look\nhere"}))
	items := s.Snapshot()
	require.Len(t, items, 1)
	assert.Equal(t, OpCallout, items[0].Op)
	assert.Equal(t, "look here", items[0].Text, "text is single-line")

	assert.Error(t, s.Put(Mark{Task: "a", Id: "y", Op: OpArrow, Targets: []Anchor{win(1)}}), "an arrow takes two")
	assert.Error(t, s.Put(Mark{Task: "a", Id: "y", Op: OpCallout, Targets: []Anchor{win(1)}}), "a callout needs text")
	assert.Error(t, s.Put(Mark{Task: "a", Id: "y", Op: OpHighlight, Targets: []Anchor{{}}}), "an anchor has a form")
	assert.Error(t, s.Put(Mark{Task: "a", Id: "y", Op: OpHighlight, Targets: []Anchor{win(1)},
		Text: strings.Repeat("x", MaxText+1)}))
}

func TestBudgetsRefuseAndNeverEvict(t *testing.T) {
	s := NewScene()
	for i := 0; i < MaxPerTask; i++ {
		require.NoError(t, s.Put(Mark{Task: "a", Id: string(rune('a' + i)), Op: OpHighlight, Targets: []Anchor{win(1)}}))
	}
	assert.Error(t, s.Put(Mark{Task: "a", Id: "over", Op: OpHighlight, Targets: []Anchor{win(1)}}))
	require.NoError(t, s.Put(Mark{Task: "b", Id: "x", Op: OpHighlight, Targets: []Anchor{win(1)}}))
	assert.Equal(t, MaxPerTask+1, s.Len())
}

func TestATaskClearsOnlyItsOwnAndKeepsItsHueWhileItHoldsAny(t *testing.T) {
	s := NewScene()
	require.NoError(t, s.Put(Mark{Task: "a", Id: "x", Op: OpHighlight, Targets: []Anchor{win(1)}}))
	require.NoError(t, s.Put(Mark{Task: "b", Id: "x", Op: OpHighlight, Targets: []Anchor{win(2)}}))
	hues := map[string]int{}
	for _, it := range s.Snapshot() {
		hues[it.Task] = it.Hue
	}
	assert.NotEqual(t, hues["a"], hues["b"], "two tasks, two hues")
	assert.Equal(t, 1, s.Clear("a", ""))
	assert.Equal(t, []string{"b"}, s.Tasks())
	require.NoError(t, s.Put(Mark{Task: "b", Id: "y", Op: OpHighlight, Targets: []Anchor{win(2)}}))
	for _, it := range s.Snapshot() {
		assert.Equal(t, hues["b"], it.Hue)
	}
}

func TestStepsNumberPerTaskAndAReplacedStepKeepsItsNumber(t *testing.T) {
	s := NewScene()
	put := func(task, id string) {
		require.NoError(t, s.Put(Mark{Task: task, Id: id, Op: OpStep, Targets: []Anchor{win(1)}, Text: id}))
	}
	put("a", "one")
	put("b", "one")
	put("a", "two")
	put("a", "one")
	steps := map[string]int{}
	for _, it := range s.Snapshot() {
		steps[it.Task+"/"+it.Id] = it.Step
	}
	assert.Equal(t, map[string]int{"a/one": 1, "a/two": 2, "b/one": 1}, steps)
}

func TestClearWindowRetiresWhatPointsAtIt(t *testing.T) {
	s := NewScene()
	require.NoError(t, s.Put(Mark{Task: "a", Id: "x", Op: OpArrow, Targets: []Anchor{win(1), win(2)}}))
	require.NoError(t, s.Put(Mark{Task: "a", Id: "y", Op: OpHighlight, Targets: []Anchor{win(3)}}))
	require.NoError(t, s.Put(Mark{Task: "b", Id: "x", Op: OpHighlight, Targets: []Anchor{win(2)}}))
	assert.Equal(t, 1, s.ClearWindow("a", 2))
	assert.Equal(t, 1, s.ClearWindow("", 2))
	assert.Equal(t, 1, s.Len())
}

func TestNotesOfTwoTasksAreLaidOutClearOfEachOther(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 1000, H: 800}
	target := Rect{X: 400, Y: 300, W: 100, H: 40}
	items := []Resolved{
		{Item: Item{Mark: Mark{Task: "a", Id: "x", Op: OpCallout, Text: "first"}, Hue: 0}, Rects: []Rect{target}, Vis: []VisibilityE{VisibilityShown}},
		{Item: Item{Mark: Mark{Task: "b", Id: "x", Op: OpCallout, Text: "second"}, Hue: 1}, Rects: []Rect{target}, Vis: []VisibilityE{VisibilityShown}},
	}
	var notes []Rect
	for _, s := range Layout(items, bounds, EstimateMeasure) {
		if s.Kind == ShapeNote {
			notes = append(notes, s.Rect)
			assert.True(t, s.Rect.Inside(bounds))
			assert.True(t, strings.HasPrefix(s.Tag, "agent · "))
		}
	}
	require.Len(t, notes, 2)
	assert.False(t, notes[0].Intersects(notes[1]))
}

func TestABehindTargetIsDashedAndSpotlightsDimOnceOutsideEveryTarget(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 100, H: 100}
	items := []Resolved{
		{Item: Item{Mark: Mark{Task: "a", Op: OpSpotlight}}, Rects: []Rect{{X: 10, Y: 10, W: 10, H: 10}}, Vis: []VisibilityE{VisibilityBehind}},
		{Item: Item{Mark: Mark{Task: "b", Op: OpSpotlight}}, Rects: []Rect{{X: 60, Y: 60, W: 10, H: 10}}, Vis: []VisibilityE{VisibilityShown}},
	}
	var dimArea float32
	dashed := 0
	for _, s := range Layout(items, bounds, EstimateMeasure) {
		switch s.Kind {
		case ShapeDim:
			dimArea += s.Rect.W * s.Rect.H
			for _, it := range items {
				assert.False(t, s.Rect.Intersects(it.Rects[0]), "a target is never dimmed")
			}
		case ShapeOutline:
			if s.Dashed {
				dashed++
			}
		}
	}
	assert.Equal(t, 1, dashed)
	hole := float32(10 + 2*(outlineGap+4))
	assert.InDelta(t, 100*100-2*hole*hole, dimArea, 0.01, "the dimmed area is the bounds less both holes, once")
}

func TestASubtractedRectLeavesNoOverlap(t *testing.T) {
	parts := subtract([]Rect{{X: 0, Y: 0, W: 10, H: 10}}, []Rect{{X: 3, Y: 3, W: 4, H: 4}})
	var area float32
	for i, p := range parts {
		area += p.W * p.H
		for j := i + 1; j < len(parts); j++ {
			assert.False(t, p.Intersects(parts[j]))
		}
	}
	assert.InDelta(t, 84, area, 0.001)
}
