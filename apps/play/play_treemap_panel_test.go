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

// A node the colour column did not describe must fall through to the depth ramp
// rather than take an arbitrary colour. In folded mode that is every
// synthesised interior node.
func TestTreemapUndescribedNodeHasNoColorOpinion(t *testing.T) {
	inst := treemapTestDriver(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a", "b"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", num: []float64{9}},
	)
	require.NotNil(t, inst.root)
	require.Equal(t, "a", inst.root.Name, "`a` was synthesised; no row described it")

	i, ok := inst.idxOf[inst.root]
	require.True(t, ok)
	assert.True(t, math.IsNaN(inst.tree.ColorNum[i]), "a synthesised node carries no colour")

	// ContinuousColoring reads NaN as "no opinion", which is what lets the
	// composite fall back to the depth layer.
	_, opinion := inst.dataColoring().Colors(treemapCellInfo(inst.root))
	assert.False(t, opinion)
	_, opinion = inst.dataColoring().Colors(treemapCellInfo(inst.root.Children[0]))
	assert.True(t, opinion, "the described leaf does get a colour")
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

// treemapCellInfo is the minimum a ColoringI needs to be asked about a node.
// Depth and State are the widget's to fill in at render; the colorings under
// test here read neither.
func treemapCellInfo(n *layout.Node) treemap.CellInfo {
	return treemap.CellInfo{Node: n}
}
