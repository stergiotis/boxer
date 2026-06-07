package godepview

import (
	"strconv"
	"testing"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
)

// TestLayoutNeighborhood_FixtureLaysOut drives the layered-engine data path
// end to end without any UI: build the dot model from a bounded neighborhood of
// the tour fixture, lay it out via the WASM Graphviz engine, and check the
// result is well-formed. Because goccyengine errors on an edge whose endpoint
// is not a declared node, a non-error layout also proves the both-endpoints
// edge filter in layoutNeighborhood holds.
func TestLayoutNeighborhood_FixtureLaysOut(t *testing.T) {
	m := fixtureManifest()
	idx := m.BuildIndex()
	focus := m.Packages[2].Id // "store" — has both imports and importers
	reached, _ := idx.BoundedNeighborhood(focus, godep.NeighborhoodOpts{
		MaxDepth: 2, Dir: godep.DirBoth, MaxNodes: maxGraphNodes,
	})
	if len(reached) < 2 {
		t.Fatalf("fixture neighborhood too small (%d) to exercise layout", len(reached))
	}

	lay, err := layoutNeighborhood(reached, idx)
	if err != nil {
		t.Fatalf("layoutNeighborhood: %v", err)
	}
	if lay == nil {
		t.Fatal("nil layout for a non-empty neighborhood")
	}
	if len(lay.Nodes) != len(reached) {
		t.Errorf("layout has %d nodes, want %d (one per reached package)", len(lay.Nodes), len(reached))
	}
	if lay.Width <= 0 || lay.Height <= 0 {
		t.Errorf("degenerate bounding box %gx%g", lay.Width, lay.Height)
	}
	for _, n := range lay.Nodes {
		id, perr := strconv.ParseUint(n.ID, 10, 64)
		if perr != nil {
			t.Errorf("node id %q is not a decimal package id", n.ID)
			continue
		}
		if _, ok := reached[id]; !ok {
			t.Errorf("laid-out node %d is not in the reached set", id)
		}
	}
}

// An empty neighborhood (no focus) or a nil index must yield no layout and no
// error, so renderGraphLayered shows nothing rather than an error label.
func TestLayoutNeighborhood_EmptyIsNoOp(t *testing.T) {
	m := fixtureManifest()
	idx := m.BuildIndex()
	if lay, err := layoutNeighborhood(nil, idx); lay != nil || err != nil {
		t.Errorf("empty reached: got (%v, %v), want (nil, nil)", lay, err)
	}
	if lay, err := layoutNeighborhood(map[uint64]int{1: 0}, nil); lay != nil || err != nil {
		t.Errorf("nil index: got (%v, %v), want (nil, nil)", lay, err)
	}
}
