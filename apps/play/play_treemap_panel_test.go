package play

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_treemap_panel_test.go covers what the Treemap pane does to an accepted
// result that the Icicle pane does not: the projection onto the widget's
// POINTER tree, and the colour channel resolved over it. The column contract
// itself is play_hierarchy_test.go's.
//
// Every case that builds a tree asserts the total survives the projection. A
// panel that quietly loses value is the failure with no symptom, and this one
// converts between two tree representations to draw at all.

// treemapTestDriver builds a driver over a record, without a widget: everything
// under test here runs before the first Render.
func treemapTestDriver(t *testing.T, cols ...icicleTestCol) *treemapDriver {
	t.Helper()
	rec := icicleTestRec(t, cols...)
	defer rec.Release()

	cl, reason := resolveHierarchy(rec.Schema(), treemapForm)
	require.Empty(t, reason)

	inst := &treemapDriver{}
	inst.tree, inst.stats = buildHierarchy(rec, cl)
	inst.rebuildTree()
	return inst
}

// treemapSumSelf walks the pointer tree adding each node's OWN size — the
// quantity the flat tree carried, and the one the projection must conserve.
func treemapSumSelf(n *layout.Node) (sum float64) {
	if n == nil {
		return 0
	}
	sum = n.Size
	for _, ch := range n.Children {
		sum += treemapSumSelf(ch)
	}
	return
}

func TestTreemapProjectionConservesTheTotal(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{
			{"disk", "db", "t1"},
			{"disk", "db", "t2"},
			{"disk", "db"}, // the database's own bytes: an INTERIOR value
			{"disk", "other"},
		}},
		icicleTestCol{name: "value", num: []float64{10, 20, 5, 7}},
	)
	require.NotNil(t, inst.root)

	assert.Equal(t, 42.0, treemapSumSelf(inst.root), "every accepted row's value must reach the tree")
	// TotalSize counts a node's own size as well as its children's (ADR-0166
	// §SD3), so the root's total IS the sum of the rows.
	assert.Equal(t, 42.0, inst.root.TotalSize())
}

// A single-rooted result is handed to the widget unwrapped, so the breadcrumb
// names the result's own root rather than a placeholder.
func TestTreemapSingleRootIsNotWrapped(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"disk", "a"}, {"disk", "b"}}},
		icicleTestCol{name: "value", num: []float64{1, 2}},
	)
	require.NotNil(t, inst.root)
	assert.Equal(t, "disk", inst.root.Name)
	assert.Len(t, inst.root.Children, 2)
}

// A forest is wrapped in a synthetic container that carries NO size of its own,
// so it invents no value — it exists only because the widget navigates from a
// single root.
func TestTreemapForestGetsAValuelessContainer(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}, {"b"}, {"c"}}},
		icicleTestCol{name: "value", num: []float64{1, 2, 3}},
	)
	require.NotNil(t, inst.root)
	assert.Equal(t, treemapRootName, inst.root.Name)
	assert.Zero(t, inst.root.Size, "the wrapper must add no value of its own")
	assert.Len(t, inst.root.Children, 3)
	assert.Equal(t, 6.0, inst.root.TotalSize())
	assert.Equal(t, 6.0, treemapSumSelf(inst.root))
}

// An interior node keeps its own value through the projection — the case the
// self cell exists to draw, and the one that would silently vanish if the
// projection put interior values on a synthetic child or dropped them.
func TestTreemapInteriorValueSurvivesTheProjection(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"db", "t1"}},
		icicleTestCol{name: "parent", str: []string{"", "db"}},
		icicleTestCol{name: "value", num: []float64{3, 7}},
	)
	require.NotNil(t, inst.root)
	assert.Equal(t, "db", inst.root.Name)
	assert.Equal(t, 3.0, inst.root.Size, "the interior node keeps its own value")
	require.Len(t, inst.root.Children, 1)
	assert.Equal(t, 7.0, inst.root.Children[0].Size)
	assert.Equal(t, 10.0, inst.root.TotalSize())
}

// Every node of the pointer tree must resolve back to its flat index, since
// that map is the only route a coloring has from a *layout.Node to the colour
// channel.
func TestTreemapIdxOfCoversEveryNode(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a", "b"}, {"a", "c"}}},
		icicleTestCol{name: "value", num: []float64{1, 2}},
	)
	require.NotNil(t, inst.root)
	require.Len(t, inst.idxOf, inst.tree.Len())

	var walk func(*layout.Node)
	seen := 0
	walk = func(n *layout.Node) {
		if i, ok := inst.idxOf[n]; ok {
			seen++
			assert.Equal(t, inst.tree.Labels[i], n.Name, "idxOf must point at the node it names")
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(inst.root)
	assert.Equal(t, inst.tree.Len(), seen, "every flat node reached the pointer tree")
}

func TestTreemapNumericColorRange(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b", "c"}},
		icicleTestCol{name: "parent", str: []string{"", "a", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1, 1}},
		icicleTestCol{name: "color", num: []float64{5, 40, 12}},
	)
	assert.Equal(t, hierColorNumeric, inst.color.kind)
	assert.Equal(t, 5.0, inst.color.min)
	assert.Equal(t, 40.0, inst.color.max)
	assert.NotNil(t, inst.dataColoring())
}

// A DECLARED scale replaces the survey outright. Without it a coverage map
// spanning 40–70% would paint 70 at the top of the ramp, reading as fully
// covered; with it the endpoints are the measure's, and out-of-range values
// clamp rather than stretching the ramp.
func TestTreemapDeclaredColorScaleOverridesTheSurvey(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b", "c"}},
		icicleTestCol{name: "parent", str: []string{"", "a", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1, 1}},
		icicleTestCol{name: "color", num: []float64{40, 55, 70}},
		icicleTestCol{name: "color_min", num: []float64{0, 0, 0}},
		icicleTestCol{name: "color_max", num: []float64{100, 100, 100}},
		icicleTestCol{name: "color_unit", str: []string{"%", "%", "%"}},
	)
	assert.Equal(t, hierColorNumeric, inst.color.kind)
	assert.True(t, inst.color.declared)
	assert.Equal(t, 0.0, inst.color.min)
	assert.Equal(t, 100.0, inst.color.max)
	assert.Equal(t, "%", inst.color.unit)

	line := inst.statusLine()
	assert.Contains(t, line, "colour 0%–100%", "the line reads in the colour channel's own unit")
	assert.Contains(t, line, "declared scale", "which range is on is invisible in the picture")
}

// Absent the columns nothing changes: the survey is still the default, and it
// is the right one for a measure whose range IS a property of this result.
func TestTreemapUndeclaredColorScaleStillSurveys(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", num: []float64{40, 70}},
	)
	assert.False(t, inst.color.declared)
	assert.Equal(t, 40.0, inst.color.min)
	assert.Equal(t, 70.0, inst.color.max)
	assert.NotContains(t, inst.statusLine(), "declared scale")
}

// An unusable pair falls back to the survey and SAYS so. Widening it the way a
// degenerate surveyed range is widened would invent an endpoint the query did
// not ask for, and drawing quietly on the survey is the silence to avoid: the
// picture would be on a scale other than the one the author wrote down.
func TestTreemapUnusableColorScaleIsRejectedAndReported(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", num: []float64{40, 70}},
		icicleTestCol{name: "color_min", num: []float64{100, 100}},
		icicleTestCol{name: "color_max", num: []float64{0, 0}},
	)
	assert.False(t, inst.color.declared)
	assert.True(t, inst.stats.colorScale.rejected)
	assert.Equal(t, 40.0, inst.color.min, "the survey is what it fell back to")
	assert.Equal(t, 70.0, inst.color.max)
	assert.Contains(t, inst.statusLine(), "declared scale ignored")
}

// A scale describes the numeric arm and nothing else: a nominal set has no
// endpoints, so the columns are inert beside a categorical `color`.
func TestTreemapColorScaleIgnoredForCategories(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", str: []string{"net", "fs"}},
		icicleTestCol{name: "color_min", num: []float64{0, 0}},
		icicleTestCol{name: "color_max", num: []float64{100, 100}},
	)
	assert.Equal(t, hierColorCategorical, inst.color.kind)
	assert.False(t, inst.color.declared)
	assert.False(t, inst.stats.colorScale.rejected, "inert, not rejected")
}

// The colour unit is a suffix on every legend tick and in the readout, and is
// spaced off the number only when it is a word.
func TestTreemapQtyUnitSpacing(t *testing.T) {
	assert.Equal(t, "72.5%", treemapQty(72.5, "%"))
	assert.Equal(t, "9.0G bytes", treemapQty(9e9, "bytes"))
	assert.Equal(t, "72.5", treemapQty(72.5, ""))
}

// A constant column would normalise to a division by zero; widening puts every
// cell at the same point of the ramp instead, which is the truthful picture.
func TestTreemapColorRangeWidensDegenerateBounds(t *testing.T) {
	lo, hi := treemapColorRange(7, 7)
	assert.Equal(t, 7.0, lo)
	assert.Greater(t, hi, lo)

	// No finite value at all — every colour cell was NaN.
	lo, hi = treemapColorRange(math.Inf(1), math.Inf(-1))
	assert.Equal(t, 0.0, lo)
	assert.Equal(t, 1.0, hi)
	assert.Greater(t, hi, lo)
}

// Categories are numbered in FIRST-SEEN order, so adding a row cannot recolour
// the rows above it.
func TestTreemapCategoriesAreFirstSeenOrdered(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b", "c", "d"}},
		icicleTestCol{name: "parent", str: []string{"", "", "", ""}},
		icicleTestCol{name: "value", num: []float64{1, 1, 1, 1}},
		icicleTestCol{name: "color", str: []string{"net", "fs", "net", "exec"}},
	)
	require.Equal(t, hierColorCategorical, inst.color.kind)
	assert.Equal(t, []string{"net", "fs", "exec"}, inst.color.catOrder)
	assert.Equal(t, 0, inst.color.cats["net"])
	assert.Equal(t, 1, inst.color.cats["fs"])
	assert.Equal(t, 2, inst.color.cats["exec"])
	assert.Zero(t, inst.color.wrapped)
}

// Past the palette's length categories share a hue. Counted rather than
// prevented — seven is the honest size of a CVD-safe qualitative set — so the
// status line can say the picture is lying about that many.
func TestTreemapCategoryWrapIsCounted(t *testing.T) {
	n := styletokens.QualitativeCycleLen + 3
	ids := make([]string, n)
	parents := make([]string, n)
	values := make([]float64, n)
	keys := make([]string, n)
	for i := range n {
		ids[i] = "n" + string(rune('a'+i))
		values[i] = 1
		keys[i] = "cat" + string(rune('a'+i))
	}
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: ids},
		icicleTestCol{name: "parent", str: parents},
		icicleTestCol{name: "value", num: values},
		icicleTestCol{name: "color", str: keys},
	)
	assert.Len(t, inst.color.catOrder, n)
	assert.Equal(t, 3, inst.color.wrapped)
	assert.Contains(t, inst.statusLine(), "share a colour")
}

// Without a usable colour column the depth ramp is the whole coloring, so
// there is no data layer for the mode switch to reach.
func TestTreemapWithoutColorHasNoDataLayer(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	assert.Equal(t, hierColorNone, inst.color.kind)
	assert.Nil(t, inst.dataColoring())
	assert.NotNil(t, inst.coloring(), "the depth ramp still colours every cell")
}

// A synthesised interior node carries no colour of its OWN — nothing in the
// result described it — and inherits one from below (ADR-0166 §SD2). The tree
// keeps the distinction; the effective slices carry what is drawn.
func TestTreemapInteriorInheritsFromBelow(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a", "b"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", num: []float64{9}},
	)
	require.NotNil(t, inst.root)
	require.Equal(t, "a", inst.root.Name, "`a` was synthesised; no row described it")

	i, ok := inst.idxOf[inst.root]
	require.True(t, ok)
	assert.True(t, math.IsNaN(inst.tree.ColorNum[i]), "the RESULT gave it no colour")
	assert.False(t, inst.ownColorAt(i))
	assert.Equal(t, 9.0, inst.effColorNum[i], "and it inherits its only child's")
	assert.Equal(t, 1, inst.inherited)

	_, opinion := inst.dataColoring().Colors(treemapCellInfo(inst.root))
	assert.True(t, opinion, "which is what stops the default view being colourless")
}

// A node with no described descendant at all still has no opinion, so the depth
// ramp keeps it — inheritance fills silence, it does not invent.
func TestTreemapNoDescribedDescendantStaysUncoloured(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"root", "kid"}},
		icicleTestCol{name: "parent", str: []string{"", "root"}},
		icicleTestCol{name: "value", num: []float64{1, 2}},
		icicleTestCol{name: "color", num: []float64{math.Inf(1), math.Inf(1)}}, // both unreadable
	)
	require.NotNil(t, inst.root)
	i := inst.idxOf[inst.root]
	assert.True(t, math.IsNaN(inst.effColorNum[i]))
	assert.Zero(t, inst.inherited)
	_, opinion := inst.dataColoring().Colors(treemapCellInfo(inst.root))
	assert.False(t, opinion)
}

// A node's OWN colour always wins: inheritance never overwrites what the query
// said, even when the children disagree with it.
func TestTreemapOwnColourBeatsInheritance(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"root", "kid"}},
		icicleTestCol{name: "parent", str: []string{"", "root"}},
		icicleTestCol{name: "value", num: []float64{1, 2}},
		icicleTestCol{name: "color", num: []float64{100, 5}},
	)
	i := inst.idxOf[inst.root]
	assert.True(t, inst.ownColorAt(i))
	assert.Equal(t, 100.0, inst.effColorNum[i], "the row described this node; nothing below overrides it")
	assert.Zero(t, inst.inherited)
}

// The numeric mean is weighted by what each child OCCUPIES, since area is what
// the reader compares — an unweighted mean would let a sliver outvote the
// subtree beside it.
func TestTreemapNumericInheritanceIsValueWeighted(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"p", "big"}, {"p", "small"}}},
		icicleTestCol{name: "value", num: []float64{90, 10}},
		icicleTestCol{name: "color", num: []float64{100, 0}},
	)
	require.NotNil(t, inst.root)
	i := inst.idxOf[inst.root]
	// (90*100 + 10*0) / 100 = 90, not the unweighted 50.
	assert.InDelta(t, 90.0, inst.effColorNum[i], 1e-9)
}

// Categories agree -> inherit. A mean of nominal categories does not exist, so
// agreement is the only thing that can transfer.
func TestTreemapCategoricalInheritsWhenDescendantsAgree(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"p", "a"}, {"p", "b"}}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", str: []string{"fs", "fs"}},
	)
	require.NotNil(t, inst.root)
	i := inst.idxOf[inst.root]
	assert.Equal(t, "fs", inst.effColorKey[i])
	assert.Equal(t, 1, inst.inherited)
	assert.Zero(t, inst.mixed)
}

// Categories disagree -> the container stays on the depth ramp and is COUNTED.
// Neutral then means "look inside", which is a reading; the modal category
// would be a claim the query never made.
func TestTreemapCategoricalMixedStaysNeutralAndIsCounted(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"p", "a"}, {"p", "b"}}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", str: []string{"fs", "net"}},
	)
	require.NotNil(t, inst.root)
	i := inst.idxOf[inst.root]
	assert.Empty(t, inst.effColorKey[i])
	assert.Zero(t, inst.inherited)
	assert.Equal(t, 1, inst.mixed)
	assert.Contains(t, inst.statusLine(), "1 mixed")

	_, opinion := inst.dataColoring().Colors(treemapCellInfo(inst.root))
	assert.False(t, opinion, "a mixed container must fall through to the depth ramp")
}

// Agreement propagates through a level that has no colour of its own, so a deep
// single-category subtree colours all the way up.
func TestTreemapCategoricalInheritanceIsTransitive(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a", "b", "c"}, {"a", "b", "d"}}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", str: []string{"exec", "exec"}},
	)
	require.NotNil(t, inst.root)
	for _, path := range [][]string{{"a"}, {"a", "b"}} {
		j := hierNodeByPath(inst.tree, path...)
		require.GreaterOrEqual(t, j, 0)
		assert.Equal(t, "exec", inst.effColorKey[j], "%v should inherit through", path)
	}
	assert.Equal(t, 2, inst.inherited)
}

// Inheritance cannot widen the range or add a category, which is why
// resolveColorInfo may survey the result's own colours alone.
func TestTreemapInheritanceStaysInsideTheSurveyedRange(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"p", "a"}, {"p", "b"}}},
		icicleTestCol{name: "value", num: []float64{3, 7}},
		icicleTestCol{name: "color", num: []float64{2, 8}},
	)
	assert.Equal(t, 2.0, inst.color.min)
	assert.Equal(t, 8.0, inst.color.max)
	for _, v := range inst.effColorNum {
		if math.IsNaN(v) {
			continue
		}
		assert.GreaterOrEqual(t, v, inst.color.min)
		assert.LessOrEqual(t, v, inst.color.max)
	}
}

func TestTreemapPalettesAreWellFormed(t *testing.T) {
	assert.Len(t, treemapDepthPalette(), treemapDepthStops)
	assert.Len(t, treemapValuePalette(), treemapValueStops)
	assert.Len(t, treemapCategoryPalette(), styletokens.QualitativeCycleLen)
	for _, p := range [][]uint32{treemapDepthPalette(), treemapValuePalette(), treemapCategoryPalette()} {
		for _, rgba := range p {
			assert.NotZero(t, rgba&0xFF, "every palette entry must be opaque, not a transparent 0")
		}
	}
}

func TestTreemapStatusLineReportsWhatDidNotReachThePicture(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "a", "b", ""}},
		icicleTestCol{name: "parent", str: []string{"", "", "nowhere", ""}},
		icicleTestCol{name: "value", num: []float64{1, 2, 3, 4}},
	)
	line := inst.statusLine()
	assert.Contains(t, line, "duplicate id")
	assert.Contains(t, line, "unknown parent")
	assert.Contains(t, line, "without a path")
	assert.Contains(t, line, "nodes input")
}

func TestTreemapNestingDepth(t *testing.T) {
	assert.Equal(t, 1, treemapNestDrill.depth(), "the default bounds the cells by the frontier's fanout")
	assert.Equal(t, 0, treemapNestAll.depth(), "0 is the widget's unlimited")
}

// An empty result must drop the tree with the stats that produced it: a status
// line still quoting the previous result's total beside an empty pane is the
// one lie it must not tell.
func TestTreemapEmptyResultHasNoRoot(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{nil}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	assert.Zero(t, inst.tree.Len())
	assert.Nil(t, inst.root)
	assert.NotContains(t, inst.statusLine(), "total")
}

// The legend and the cells must sample ONE Colormap. A legend built from the
// same two numbers rather than the same object would drift the moment either
// side changed how it samples the palette, and a legend that disagrees with the
// picture it explains is worse than no legend.
func TestTreemapLegendSharesTheCellColormap(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"p", "a"}, {"p", "b"}}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", num: []float64{10, 90}},
	)
	require.NotNil(t, inst.cmap, "a numeric colour column builds a colormap")
	lo, hi := inst.cmap.Range()
	assert.Equal(t, inst.color.min, lo)
	assert.Equal(t, inst.color.max, hi)

	// Every cell colour the coloring can produce comes from that same instance,
	// so sampling it directly reproduces them.
	for _, v := range []float64{10, 50, 90} {
		assert.Equal(t, inst.cmap.At(v), inst.cmap.Config().At(v),
			"the legend reads Config(); the cells read the Colormap — same object, same answer")
	}
}

// No numeric column, no colormap and no legend to build — the categorical and
// absent arms must not leave a stale scale behind either.
func TestTreemapColormapIsDroppedWhenNotNumeric(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", str: []string{"fs"}},
	)
	assert.Nil(t, inst.cmap)
	assert.Nil(t, inst.scale, "a categorical result has a key, not a colour bar")

	none := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	assert.Nil(t, none.cmap)
	assert.Nil(t, none.scale)
}

// A rebuild must drop the old scale: a ColorScale binds its Config at
// construction, so one kept across a range change would draw the previous
// result's axis beside the new result's cells.
func TestTreemapRebuildDropsTheStaleScale(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", num: []float64{5}},
	)
	require.NotNil(t, inst.cmap)
	first := inst.cmap

	// Stand in for a render having built the legend, then rebuild.
	inst.scale = nil
	inst.color.min, inst.color.max = 100, 200
	inst.rebuildColormap()
	require.NotNil(t, inst.cmap)
	assert.NotSame(t, first, inst.cmap, "a new range is a new colormap")
	assert.Nil(t, inst.scale, "and the legend bound to the old one is gone")
	lo, hi := inst.cmap.Range()
	assert.Equal(t, 100.0, lo)
	assert.Equal(t, 200.0, hi)
}

// treemapCellInfo is the minimum a ColoringI needs to be asked about a node.
// Depth and State are the widget's to fill in at render; the colorings under
// test here read neither.
func treemapCellInfo(n *layout.Node) treemap.CellInfo {
	return treemap.CellInfo{Node: n}
}

// The readout under the colour bar reports where the DESCRIBED colours sit —
// the result's own, NaN-free — at the seven points the prototype's toolbar
// showed. Linear interpolation between order statistics, so an even ladder
// reads back as itself.
func TestTreemapColorQuantilesSurveyDescribedCellsOnly(t *testing.T) {
	qs, n := treemapColorQuantiles([]float64{9, math.NaN(), 1, 5, 3, 7, math.Inf(1)})
	assert.Equal(t, 5, n, "NaN and Inf are not described colours")
	require.Len(t, qs, len(treemapQuantileProbs))
	assert.Equal(t, 1.0, qs[0], "min")
	assert.Equal(t, 3.0, qs[1], "P25 of 1,3,5,7,9")
	assert.Equal(t, 5.0, qs[2], "median")
	assert.Equal(t, 7.0, qs[3], "P75")
	assert.InDelta(t, 8.2, qs[4], 1e-9, "P90 interpolates between 7 and 9")
	assert.InDelta(t, 8.92, qs[5], 1e-9, "P99")
	assert.Equal(t, 9.0, qs[6], "max")

	qs, n = treemapColorQuantiles([]float64{math.NaN()})
	assert.Nil(t, qs, "nothing described, nothing to report")
	assert.Zero(t, n)
}

// The readout is carried on the colour info and formatted in the channel's
// unit; a categorical colour has no spread to report.
func TestTreemapQuantileLineFollowsTheColourKind(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b", "c"}},
		icicleTestCol{name: "parent", str: []string{"", "a", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1, 1}},
		icicleTestCol{name: "color", num: []float64{5, 40, 12}},
		icicleTestCol{name: "color_unit", str: []string{"w", "w", "w"}},
	)
	require.Len(t, inst.color.quantiles, len(treemapQuantileProbs))
	assert.Equal(t, 3, inst.color.described)
	line := inst.quantileLine()
	assert.Contains(t, line, "min 5 w")
	assert.Contains(t, line, "median 12 w")
	assert.Contains(t, line, "max 40 w")
	assert.Contains(t, line, "over 3 described cells")

	cat := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", str: []string{"x", "y"}},
	)
	assert.Empty(t, cat.color.quantiles)
	assert.Empty(t, cat.quantileLine())
}

// A declared scale pins the ramp, and the spread is surveyed anyway — there most
// of all, since the ticks then say nothing about where this result's values are.
func TestTreemapQuantilesSurveyedUnderADeclaredScale(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", num: []float64{40, 70}},
		icicleTestCol{name: "color_min", num: []float64{0, 0}},
		icicleTestCol{name: "color_max", num: []float64{100, 100}},
	)
	require.True(t, inst.color.declared)
	require.Len(t, inst.color.quantiles, len(treemapQuantileProbs))
	assert.Equal(t, 40.0, inst.color.quantiles[0])
	assert.Equal(t, 70.0, inst.color.quantiles[len(inst.color.quantiles)-1])
}

// The nesting ladder's middle rungs are named by the levels they show below the
// frontier; the widget counts preview levels below the frontier's CHILDREN, so
// each is one less.
func TestTreemapNestingLadder(t *testing.T) {
	assert.Equal(t, 2, treemapNestThree.depth())
	assert.Equal(t, 3, treemapNestFour.depth())
	assert.Less(t, treemapNestDrill.depth(), treemapNestThree.depth())
	assert.Less(t, treemapNestThree.depth(), treemapNestFour.depth())
}
