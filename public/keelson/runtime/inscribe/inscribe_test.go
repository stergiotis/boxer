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
	for _, s := range Layout(items, bounds, nil, EstimateMeasure) {
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
	for _, s := range Layout(items, bounds, nil, EstimateMeasure) {
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

func TestASketchedStrokeIsTheSameForTheSameMarkAndStaysNearItsLine(t *testing.T) {
	seed := seedOf("task", "clear", 0)
	a := sketchLine(10, 10, 210, 10, newSketchRng(seed))
	b := sketchLine(10, 10, 210, 10, newSketchRng(seed))
	assert.Equal(t, a, b, "no shimmer from frame to frame")
	require.Len(t, a, 2, "drawn twice, as a pen goes over a line")
	c := sketchLine(10, 10, 210, 10, newSketchRng(seedOf("task", "other", 0)))
	assert.NotEqual(t, a, c, "another mark wobbles differently")
	// The stroke strays by at most a few points from the line it sketches.
	limit := 2.5 * roughness(200)
	for _, p := range a {
		for _, q := range p {
			assert.LessOrEqual(t, abs(q.Y-10), limit)
			assert.GreaterOrEqual(t, q.X, float32(10)-limit)
			assert.LessOrEqual(t, q.X, float32(210)+limit)
		}
	}
}

func TestAMarkKeepsItsShapeWhenItsWindowMoves(t *testing.T) {
	r1 := sketchRect(Rect{X: 0, Y: 0, W: 100, H: 40}, newSketchRng(7))
	r2 := sketchRect(Rect{X: 300, Y: 200, W: 100, H: 40}, newSketchRng(7))
	require.Equal(t, len(r1), len(r2))
	for i := range r1 {
		for j := range r1[i] {
			assert.InDelta(t, r1[i][j].X+300, r2[i][j].X, 1e-3)
			assert.InDelta(t, r1[i][j].Y+200, r2[i][j].Y, 1e-3)
		}
	}
}

func TestDashesCoverTheirShareOfTheLine(t *testing.T) {
	x0s, _, x1s, _ := dashes(polyline{{0, 0}, {60, 0}, {120, 0}}, 7, 5)
	var on float32
	for i := range x0s {
		on += x1s[i] - x0s[i]
	}
	assert.InDelta(t, 120*7.0/12.0, on, 7, "about dash/(dash+gap) of the length is drawn")
}

func TestANoteKeepsClearOfOtherMarksTargets(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 1000, H: 800}
	button := Rect{X: 400, Y: 300, W: 60, H: 20}
	field := Rect{X: 480, Y: 300, W: 120, H: 20}
	window := Rect{X: 300, Y: 200, W: 400, H: 300}
	items := []Resolved{
		{Item: Item{Mark: Mark{Task: "a", Id: "w", Op: OpHighlight}}, Rects: []Rect{window}, Vis: []VisibilityE{VisibilityShown}},
		{Item: Item{Mark: Mark{Task: "a", Id: "f", Op: OpHighlight}}, Rects: []Rect{field}, Vis: []VisibilityE{VisibilityShown}},
		{Item: Item{Mark: Mark{Task: "a", Id: "b", Op: OpCallout, Text: "press here"}}, Rects: []Rect{button}, Vis: []VisibilityE{VisibilityShown}},
	}
	for _, s := range Layout(items, bounds, nil, EstimateMeasure) {
		if s.Kind == ShapeNote && s.Text == "press here" {
			assert.False(t, s.Rect.Intersects(field.Inflate(outlineGap)), "the note is not on the field another mark points at")
			assert.False(t, s.Rect.Intersects(button), "nor on its own target")
		}
	}
}

func TestAMarkWithoutTextGetsATabOnItsCornerAndANoteGoesBesideItsWindow(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 1600, H: 900}
	window := Rect{X: 100, Y: 100, W: 600, H: 500}
	cell := Rect{X: 200, Y: 300, W: 300, H: 20}
	items := []Resolved{
		{Item: Item{Mark: Mark{Task: "a", Id: "h", Op: OpHighlight}}, Rects: []Rect{cell}, Vis: []VisibilityE{VisibilityShown}, Windows: []Rect{window}},
		{Item: Item{Mark: Mark{Task: "a", Id: "c", Op: OpCallout, Text: "the selected row"}}, Rects: []Rect{cell}, Vis: []VisibilityE{VisibilityShown}, Windows: []Rect{window}},
	}
	var tab, note *Shape
	shapes := Layout(items, bounds, []Rect{window}, EstimateMeasure)
	for i := range shapes {
		switch shapes[i].Kind {
		case ShapeTab:
			tab = &shapes[i]
		case ShapeNote:
			note = &shapes[i]
		}
	}
	require.NotNil(t, tab)
	assert.InDelta(t, cell.X-outlineGap, tab.Rect.X, 0.01, "the tab sits on the outline's corner")
	assert.InDelta(t, cell.Y-outlineGap, tab.Rect.MaxY(), 0.01)
	require.NotNil(t, note)
	assert.False(t, note.Rect.Intersects(window), "with desktop beside the window, the note covers none of it")
	assert.GreaterOrEqual(t, note.Rect.X, window.MaxX())
}

func TestATargetIsMarkedByWhatItIs(t *testing.T) {
	button := Rect{X: 0, Y: 0, W: 60, H: 22}
	row := Rect{X: 0, Y: 0, W: 400, H: 18}
	assert.Equal(t, OutlineCircle, styleFor(Anchor{Window: 1, Local: &button}, button))
	assert.Equal(t, OutlineSwipe, styleFor(Anchor{Window: 1, Local: &row}, row))
	assert.Equal(t, OutlineBrackets, styleFor(Anchor{Window: 1}, Rect{W: 600, H: 400}), "a whole window")
	big := Rect{W: 500, H: 300}
	assert.Equal(t, OutlineBrackets, styleFor(Anchor{Window: 1, Local: &big}, big))
}

func TestACircleAndASwipeCoverTheirTarget(t *testing.T) {
	rc := Rect{X: 100, Y: 100, W: 80, H: 24}
	for seed := uint64(1); seed < 20; seed++ {
		minX, minY, maxX, maxY := float32(1e9), float32(1e9), float32(-1e9), float32(-1e9)
		for _, p := range sketchEllipse(rc, newSketchRng(seed)) {
			for _, q := range p {
				minX, minY, maxX, maxY = min(minX, q.X), min(minY, q.Y), max(maxX, q.X), max(maxY, q.Y)
			}
		}
		assert.True(t, minX <= rc.X && minY <= rc.Y && maxX >= rc.MaxX() && maxY >= rc.MaxY(), "the circle goes round the target")
		xs, ys := swipe(rc, newSketchRng(seed))
		assert.LessOrEqual(t, slicesMin(xs), rc.X)
		assert.GreaterOrEqual(t, slicesMax(xs), rc.MaxX())
		assert.LessOrEqual(t, slicesMin(ys), rc.Y+3, "the swipe covers the line, give or take its slant")
		assert.GreaterOrEqual(t, slicesMax(ys), rc.MaxY()-3)
	}
}

func slicesMin(v []float32) float32 {
	m := v[0]
	for _, x := range v {
		m = min(m, x)
	}
	return m
}

func slicesMax(v []float32) float32 {
	m := v[0]
	for _, x := range v {
		m = max(m, x)
	}
	return m
}

func TestATabKeepsOffAnotherMarksTarget(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 1000, H: 800}
	button := Rect{X: 100, Y: 280, W: 50, H: 22}
	status := Rect{X: 100, Y: 310, W: 200, H: 18}
	items := []Resolved{
		{Item: Item{Mark: Mark{Task: "a", Id: "b", Op: OpCallout, Text: "press"}}, Rects: []Rect{button}, Vis: []VisibilityE{VisibilityShown}},
		{Item: Item{Mark: Mark{Task: "a", Id: "s", Op: OpHighlight}}, Rects: []Rect{status}, Vis: []VisibilityE{VisibilityShown}},
	}
	for _, s := range Layout(items, bounds, nil, EstimateMeasure) {
		if s.Kind == ShapeTab {
			assert.False(t, s.Rect.Intersects(button.Inflate(outlineGap)), "the status line's tab is not on the button above it")
		}
	}
}

func TestANotesDotGridStaysInsideItsPlate(t *testing.T) {
	rc := Rect{X: 10, Y: 20, W: 120, H: 40}
	xs, ys := dotGrid(rc)
	require.NotEmpty(t, xs)
	for i := range xs {
		assert.True(t, xs[i] > rc.X && xs[i] < rc.MaxX() && ys[i] > rc.Y && ys[i] < rc.MaxY())
	}
	assert.Empty(t, func() []float32 { x, _ := dotGrid(Rect{W: 5, H: 5}); return x }(), "no dots on a plate too small for them")
}

func TestANoteSlidesPastWhatBlocksItBesideATargetNearTheEdge(t *testing.T) {
	// The demo's layout: Save at the stage's left edge, Cancel right beside
	// it, both marked; two notes for Save must find room without covering
	// Cancel or each other.
	bounds := Rect{X: 50, Y: 332, W: 1352, H: 297}
	save := Rect{X: 50, Y: 434, W: 44, H: 26}
	cancel := Rect{X: 102, Y: 441, W: 56, H: 26}
	items := []Resolved{
		{Item: Item{Mark: Mark{Task: "a", Id: "s2", Op: OpStep, Text: "save the note"}, Step: 2}, Rects: []Rect{save}, Vis: []VisibilityE{VisibilityShown}},
		{Item: Item{Mark: Mark{Task: "a", Id: "c", Op: OpCallout, Text: "Save writes the note"}}, Rects: []Rect{save}, Vis: []VisibilityE{VisibilityShown}},
		{Item: Item{Mark: Mark{Task: "a", Id: "a", Op: OpArrow, Text: "this row"}}, Rects: []Rect{cancel, {X: 50, Y: 543, W: 169, H: 18}}, Vis: []VisibilityE{VisibilityShown, VisibilityShown}},
	}
	var notes []Rect
	for _, s := range Layout(items, bounds, nil, EstimateMeasure) {
		if s.Kind == ShapeNote {
			notes = append(notes, s.Rect)
			assert.False(t, s.Rect.Intersects(cancel), "no note covers Cancel: %+v", s.Rect)
			assert.True(t, s.Rect.Inside(bounds))
		}
	}
	require.Len(t, notes, 3)
	for i := range notes {
		for j := i + 1; j < len(notes); j++ {
			assert.False(t, notes[i].Intersects(notes[j]), "notes %d and %d overlap", i, j)
		}
	}
}
