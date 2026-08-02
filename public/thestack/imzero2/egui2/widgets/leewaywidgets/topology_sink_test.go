package leewaywidgets

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
	"github.com/stretchr/testify/require"
)

// renderTopology renders the accumulated tree as indented text: one line per
// node, carrying the treemap-relevant facts (subtree size, and attribute state
// for leaves). Asserting on this rather than on the node graph keeps the
// expectation readable and fails with a legible diff.
func renderTopology(sink *TopologySink) (s string) {
	b := &strings.Builder{}
	var walk func(n *layout.Node, depth int)
	walk = func(n *layout.Node, depth int) {
		fmt.Fprintf(b, "%s%s %.0f", strings.Repeat("  ", depth), n.Name, n.TotalSize())
		if st, ok := sink.StateOf(n); ok {
			fmt.Fprintf(b, " [%s]", st)
		}
		b.WriteByte('\n')
		for _, ch := range n.Children {
			walk(ch, depth+1)
		}
	}
	walk(sink.Root(), 0)
	return b.String()
}

const fixtureTopology = `batch 7
  entity 0 7
    id 3
      id 1 [value only]
      internalKey 1 [value only]
      naturalKey 1 [value only]
    co · geo 1
      geoPoint 1
        lat·lng… 1 [value + tags]
    metric 3
      value·rawBlob… 1 [value + tags]
      value·rawBlob… #2 1 [value + tags]
      value·rawBlob… #3 1 [value + tags]
`

// TestTopologySinkFixtureShape pins the containment hierarchy the sink derives
// from the shared fixture: a plain id section of three single-column
// attributes, a co-section group wrapping geoPoint, and a repeated metric
// section. Entity area is the sum of attribute leaves (7), which is the
// property the treemap's layout depends on.
func TestTopologySinkFixtureShape(t *testing.T) {
	sink := NewTopologySink()
	RunFixture(sink)
	require.Equal(t, fixtureTopology, renderTopology(sink))
}

// TestTopologySinkResetsPerBatch guards the accumulation reset: BeginBatch must
// drop the previous tree, otherwise a re-driven sink doubles every entity and
// the treemap silently shows stale shape alongside fresh.
func TestTopologySinkResetsPerBatch(t *testing.T) {
	sink := NewTopologySink()
	RunFixture(sink)
	first := renderTopology(sink)
	RunFixture(sink)
	require.Equal(t, first, renderTopology(sink), "re-driving must reproduce the tree, not append to it")
	require.Len(t, sink.Root().Children, 1, "exactly one entity after a re-drive")
}

// TestTopologySinkStatesAreLeafOnly pins the fall-through contract the Coloring
// depends on: only attribute leaves carry a state, so structural nodes return
// ok=false and keep the treemap's depth ramp.
func TestTopologySinkStatesAreLeafOnly(t *testing.T) {
	sink := NewTopologySink()
	RunFixture(sink)

	var walk func(n *layout.Node)
	nLeaf, nStruct := 0, 0
	walk = func(n *layout.Node) {
		_, ok := sink.StateOf(n)
		if len(n.Children) == 0 {
			require.True(t, ok, "leaf %q must carry a state", n.Name)
			nLeaf++
		} else {
			require.False(t, ok, "structural node %q must not carry a state", n.Name)
			nStruct++
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(sink.Root())
	require.Equal(t, 7, nLeaf)
	require.Equal(t, 6, nStruct, "batch + entity + id + co-group + geoPoint + metric")
}

// TestTopologyPaletteCoversEveryState guards the palette against an added
// AttrStateE going unpainted — CategoricalColoring indexes it directly, so a
// short palette would panic or silently reuse a colour at render time.
func TestTopologyPaletteCoversEveryState(t *testing.T) {
	p := topologyPalette()
	require.Len(t, p, attrStateCount)
	seen := make(map[uint32]AttrStateE, attrStateCount)
	for _, st := range []AttrStateE{AttrStateEmpty, AttrStateTagsOnly, AttrStateValueOnly, AttrStateValueAndTags} {
		require.Less(t, int(st), len(p), "state %s has no palette slot", st)
		col := p[st]
		require.NotZero(t, col, "state %s has a zero colour", st)
		prev, dup := seen[col]
		require.False(t, dup, "states %s and %s share a colour", prev, st)
		seen[col] = st
	}
}
