package treemap

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// -----------------------------------------------------------------------------
// HSL → RGBA pure-function tests
// -----------------------------------------------------------------------------

func TestHSLToRGBA_PureBlackWhite(t *testing.T) {
	if got := hslToRGBA(0, 0, 0, 0xff); got != 0x000000ff {
		t.Errorf("L=0 → %#08x, want 0x000000ff", got)
	}
	if got := hslToRGBA(0, 0, 1, 0xff); got != 0xffffffff {
		t.Errorf("L=1 → %#08x, want 0xffffffff", got)
	}
}

func TestHSLToRGBA_PureRedGreenBlue(t *testing.T) {
	// hue 0 / 120 / 240, saturation 1, lightness 0.5 → pure RGB primaries.
	cases := []struct {
		hue  float64
		want uint32
	}{
		{0, 0xff0000ff},
		{120, 0x00ff00ff},
		{240, 0x0000ffff},
	}
	for _, c := range cases {
		got := hslToRGBA(c.hue, 1.0, 0.5, 0xff)
		if got != c.want {
			t.Errorf("hue=%v → %#08x, want %#08x", c.hue, got, c.want)
		}
	}
}

func TestHSLToRGBA_AlphaPassthrough(t *testing.T) {
	if got := hslToRGBA(0, 1, 0.5, 0x42) & 0xff; got != 0x42 {
		t.Errorf("alpha not preserved: got %#x", got)
	}
}

func TestHSLToRGBA_HueWraps(t *testing.T) {
	// +360 wraps back to base; negative wraps forward.
	base := hslToRGBA(45, 0.6, 0.5, 0xff)
	if got := hslToRGBA(45+360, 0.6, 0.5, 0xff); got != base {
		t.Errorf("hue+360 != base: %#08x vs %#08x", got, base)
	}
	if got := hslToRGBA(45-360, 0.6, 0.5, 0xff); got != base {
		t.Errorf("hue-360 != base: %#08x vs %#08x", got, base)
	}
}

// -----------------------------------------------------------------------------
// AncestorHueColoring behavior
// -----------------------------------------------------------------------------

func TestAncestorHueColoring_PanicsOnNilRoot(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on nil root")
		}
	}()
	AncestorHueColoring(nil, 0.5, 0.8, 0.3, 0xff)
}

func TestAncestorHueColoring_PanicsOnRootWithoutChildren(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on root with no children")
		}
	}()
	AncestorHueColoring(&layout.Node{Name: "alone"}, 0.5, 0.8, 0.3, 0xff)
}

func TestAncestorHueColoring_PanicsOnInvalidSaturation(t *testing.T) {
	root := &layout.Node{Name: "r", Children: []*layout.Node{{Name: "c"}}}
	for _, s := range []float64{-0.1, 1.1, math.NaN()} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic on saturation=%v", s)
				}
			}()
			AncestorHueColoring(root, s, 0.8, 0.3, 0xff)
		}()
	}
}

func TestAncestorHueColoring_HuesEvenlySpaced(t *testing.T) {
	// 3 ancestors → hues 0, 120, 240 → pure R, G, B at depth 0.
	a := &layout.Node{Name: "a"}
	b := &layout.Node{Name: "b"}
	c := &layout.Node{Name: "c"}
	root := &layout.Node{Name: "r", Children: []*layout.Node{a, b, c}}
	// saturation=1, lightness=0.5 at root → pure primaries.
	co := AncestorHueColoring(root, 1.0, 0.5, 0.5, 0xff)

	want := map[*layout.Node]uint32{
		a: 0xff0000ff,
		b: 0x00ff00ff,
		c: 0x0000ffff,
	}
	for n, w := range want {
		cs, ok := co.Colors(CellInfo{Node: n, Depth: 0})
		if !ok {
			t.Errorf("Colors(%s) returned ok=false", n.Name)
			continue
		}
		if got := cs.Fill.Literal(); got != w {
			t.Errorf("Fill(%s, depth=0) = %#08x, want %#08x", n.Name, got, w)
		}
	}
}

func TestAncestorHueColoring_DescendantsShareAncestorHue(t *testing.T) {
	// a → a1 → a2; b → b1. Hue at depth 0 must equal hue at depth 1, 2 for
	// the same ancestor (lightness differs, but the hue family is shared).
	leaf := &layout.Node{Name: "a2", Size: 1}
	mid := &layout.Node{Name: "a1", Children: []*layout.Node{leaf}}
	a := &layout.Node{Name: "a", Children: []*layout.Node{mid}}
	b := &layout.Node{Name: "b", Size: 1}
	root := &layout.Node{Name: "r", Children: []*layout.Node{a, b}}

	co := AncestorHueColoring(root, 0.8, 0.7, 0.3, 0xff)

	// Extract the hue from each node's Fill via channel-ratio comparison:
	// the dominant channel should be the same for a, mid, leaf (all under
	// the same depth-1 ancestor `a`).
	dominantChannel := func(rgba uint32) int {
		r := (rgba >> 24) & 0xff
		g := (rgba >> 16) & 0xff
		bl := (rgba >> 8) & 0xff
		if r >= g && r >= bl {
			return 0
		}
		if g >= r && g >= bl {
			return 1
		}
		return 2
	}
	getDom := func(n *layout.Node, depth int) int {
		cs, ok := co.Colors(CellInfo{Node: n, Depth: depth})
		if !ok {
			t.Fatalf("Colors(%s) returned ok=false", n.Name)
		}
		return dominantChannel(cs.Fill.Literal())
	}
	if d1, d2, d3 := getDom(a, 0), getDom(mid, 1), getDom(leaf, 2); d1 != d2 || d2 != d3 {
		t.Errorf("descendants of `a` have different dominant channels: %d,%d,%d", d1, d2, d3)
	}
}

func TestAncestorHueColoring_LightnessTracksDepth(t *testing.T) {
	// One ancestor chain a → a1 → leaf (depths 0, 1, 2). lightAtRoot=0.9,
	// lightAtLeaf=0.3 → at depth 0 luminance > depth 1 > depth 2.
	leaf := &layout.Node{Name: "leaf", Size: 1}
	mid := &layout.Node{Name: "mid", Children: []*layout.Node{leaf}}
	a := &layout.Node{Name: "a", Children: []*layout.Node{mid}}
	root := &layout.Node{Name: "r", Children: []*layout.Node{a}}

	co := AncestorHueColoring(root, 0.7, 0.9, 0.3, 0xff)

	lumOf := func(n *layout.Node, depth int) float64 {
		cs, _ := co.Colors(CellInfo{Node: n, Depth: depth})
		rgba := cs.Fill.Literal()
		r := uint8((rgba >> 24) & 0xff)
		g := uint8((rgba >> 16) & 0xff)
		b := uint8((rgba >> 8) & 0xff)
		return relativeLuminance(r, g, b)
	}
	l0, l1, l2 := lumOf(a, 0), lumOf(mid, 1), lumOf(leaf, 2)
	if !(l0 > l1 && l1 > l2) {
		t.Errorf("luminance should strictly decrease with depth; got %v, %v, %v", l0, l1, l2)
	}
}

func TestAncestorHueColoring_UnknownNode_ReturnsFalse(t *testing.T) {
	root := &layout.Node{Name: "r", Children: []*layout.Node{{Name: "c"}}}
	co := AncestorHueColoring(root, 0.5, 0.8, 0.3, 0xff)
	stranger := &layout.Node{Name: "not-in-tree"}
	if _, ok := co.Colors(CellInfo{Node: stranger}); ok {
		t.Errorf("Colors(stranger) returned ok=true; want false for composite fall-through")
	}
}

func TestAncestorHueColoring_RootItselfReturnsFalse(t *testing.T) {
	// The root is not normally rendered as a cell; if it is queried, the
	// coloring should fall through (ok=false) rather than picking some
	// arbitrary hue for it.
	root := &layout.Node{Name: "r", Children: []*layout.Node{{Name: "c"}}}
	co := AncestorHueColoring(root, 0.5, 0.8, 0.3, 0xff)
	if _, ok := co.Colors(CellInfo{Node: root}); ok {
		t.Errorf("Colors(root) returned ok=true; want false")
	}
}

func TestAncestorHueColoring_CachingProducesIdenticalResults(t *testing.T) {
	// Two consecutive Colors() calls with the same input must return the
	// same Fill literal; this exercises the cache path.
	root := &layout.Node{Name: "r", Children: []*layout.Node{{Name: "a"}, {Name: "b"}}}
	co := AncestorHueColoring(root, 0.5, 0.8, 0.3, 0xff)
	info := CellInfo{Node: root.Children[0], Depth: 0}
	cs1, ok1 := co.Colors(info)
	cs2, ok2 := co.Colors(info)
	if !ok1 || !ok2 {
		t.Fatalf("ok flags differ or false: %v %v", ok1, ok2)
	}
	if cs1.Fill.Literal() != cs2.Fill.Literal() {
		t.Errorf("cached Fill differs: %#08x vs %#08x", cs1.Fill.Literal(), cs2.Fill.Literal())
	}
}

func TestAncestorHueColoring_ComposesWithCategorical(t *testing.T) {
	// In a CompositeColoring, an ancestor-hue base should be overridden by
	// a CategoricalColoring layer for nodes where the categorical returns a
	// non-negative index — standard composite fall-through.
	a := &layout.Node{Name: "a"}
	b := &layout.Node{Name: "b"}
	root := &layout.Node{Name: "r", Children: []*layout.Node{a, b}}

	cat := CategoricalColoring(
		[]uint32{0xff0000ff, 0x00ff00ff},
		func(n *layout.Node) int {
			if n == b {
				return 1 // green
			}
			return -1 // fall through to ancestor hue
		},
	)
	composite := CompositeColoring(
		AncestorHueColoring(root, 1.0, 0.5, 0.5, 0xff),
		cat,
	)
	csA, okA := composite.Colors(CellInfo{Node: a, Depth: 0})
	if !okA {
		t.Fatalf("composite.Colors(a) returned ok=false")
	}
	// a falls through to ancestor hue: hue=0 (first child), s=1, l=0.5 → red.
	if got := csA.Fill.Literal(); got != 0xff0000ff {
		t.Errorf("a: composite picked %#08x, want 0xff0000ff (ancestor-hue fallback)", got)
	}
	csB, okB := composite.Colors(CellInfo{Node: b, Depth: 0})
	if !okB {
		t.Fatalf("composite.Colors(b) returned ok=false")
	}
	// b is overridden by the categorical layer: palette[1] = green.
	if got := csB.Fill.Literal(); got != 0x00ff00ff {
		t.Errorf("b: composite picked %#08x, want 0x00ff00ff (categorical override)", got)
	}
}
