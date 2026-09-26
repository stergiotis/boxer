package leewaywidgets

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildHierarchy(t *testing.T) {
	nan := math.NaN()
	m := &ChartModel{
		Categories: []string{"a/x/p", "a/x/q", "a/y", "b/z", "c", "d/w"},
		Series:     []string{"hot", "cold"},
		Values: [][]float64{
			{1, 2, 3, 10, 4, nan},
			{1, nan, 0, 0, 0, nan},
		},
	}
	h := BuildHierarchy(m, "/", true, 0)
	assert.Equal(t, 5, h.Leaves)
	assert.Equal(t, 1, h.Skipped, "d/w has no value to size it by")
	assert.Equal(t, 3, h.Depth)
	assert.InDelta(t, 21, h.Root.Weight, 1e-9)
	require.Len(t, h.Root.Children, 3)
	assert.Equal(t, "b", h.Root.Children[0].Name, "heaviest first")
	assert.Equal(t, "a", h.Root.Children[1].Name)
	assert.InDelta(t, 7, h.Root.Children[1].Weight, 1e-9)
	assert.Equal(t, "a/x", h.Root.Children[1].Children[0].Path)

	folded := BuildHierarchy(m, "/", false, 1)
	assert.Equal(t, 1, folded.Depth, "deeper levels fold into depth 1")
	assert.Equal(t, 6, folded.Leaves, "counting sizes every entity")
	assert.InDelta(t, 3, folded.Root.Children[0].Weight, 1e-9, "a holds three entities")

	fs := h.Flows()
	assert.Equal(t, []Flow{{"b", "b/z", 10}, {"a", "a/x", 4}, {"a", "a/y", 3}, {"a/x", "a/x/p", 2}, {"a/x", "a/x/q", 2}}, fs,
		"level by level, heaviest parent first, ties by name")
}

// Each widget's input from one hierarchy passes that widget's own validation,
// including a leaf above the deepest level ("c" beside "a/x/p").
func TestHierarchyConversions(t *testing.T) {
	m := &ChartModel{
		Categories: []string{"a/x/p", "a/x/q", "a/y", "b/z", "c"},
		Series:     []string{"v"},
		Values:     [][]float64{{1, 2, 3, 10, 4}},
	}
	h := BuildHierarchy(m, "/", true, 0)

	root := toLayoutNode(h.Root, "all")
	assert.InDelta(t, 20, root.TotalSize(), 1e-9, "sizes on leaves only, counted once")

	ice, err := icicle.Compute(toIcicleTree(h), icicle.Options{Orientation: icicle.OrientIcicle, Order: icicle.OrderValueDesc})
	require.NoError(t, err)
	assert.Len(t, ice.Nodes, 8)

	d := toSankey(h)
	assert.Equal(t, sankey.ModeAlluvial, d.Mode)
	sk, err := sankey.Compute(d, sankey.Options{Align: sankey.AlignLeft})
	require.NoError(t, err)
	require.Len(t, sk.Nodes, 9, "the eight tree nodes and the root")
	for _, n := range sk.Nodes {
		if n.ID == "c" {
			assert.Positive(t, n.Y1-n.Y0, "a top-level leaf is drawn with its weight")
		}
	}
}
