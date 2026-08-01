package view

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
)

// Custom closures never run on a detached plot, so these tests cover the
// decision logic — colour resolution, emphasis, option normalization — and
// nil-safety. What the closures paint is verified by the gallery demo's tour
// capture instead.

func testLayout(t *testing.T) *sankey.Layout {
	t.Helper()
	lay, err := sankey.Compute(sankey.Diagram{
		Nodes: []sankey.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Links: []sankey.Link{
			{Source: "a", Target: "b", Value: 3},
			{Source: "a", Target: "c", Value: 1},
			{Source: "b", Target: "c", Value: 3},
		},
	}, sankey.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return lay
}

func nodeIndex(lay *sankey.Layout, id string) int {
	for i := range lay.Nodes {
		if lay.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}

func TestNilSafety(t *testing.T) {
	lay := testLayout(t)
	p := implot.NewDetached()
	// None of these may panic, and all must report nothing.
	if h, cl, ok := Probe(nil, lay, 0); !h.None() || !cl.None() || ok {
		t.Errorf("Probe(nil plot) = %v, %v, %v", h, cl, ok)
	}
	if h, cl, ok := Probe(p, nil, 0); !h.None() || !cl.None() || ok {
		t.Errorf("Probe(nil layout) = %v, %v, %v", h, cl, ok)
	}
	if h, cl, ok := Probe(p, lay, 0); !h.None() || !cl.None() || ok {
		t.Errorf("Probe with no pointer = %v, %v, %v", h, cl, ok)
	}
	var r Renderer
	if h, cl, ok := r.Probe(nil, lay, 0); !h.None() || !cl.None() || ok {
		t.Errorf("Renderer.Probe(nil plot) = %v, %v, %v", h, cl, ok)
	}
	if h, cl, ok := r.Probe(p, nil, 0); !h.None() || !cl.None() || ok {
		t.Errorf("Renderer.Probe(nil layout) = %v, %v, %v", h, cl, ok)
	}
	// Setup before Draw: the implot protocol wants every Setup* call ahead of
	// the first item, and Draw declares items.
	Setup(p, Opts{})
	Setup(p, Opts{Layers: true})
	Draw(nil, lay, Opts{})
	Draw(p, nil, Opts{})
	Draw(p, lay, Opts{})
	if h, cl, ok := Show(nil, "t", 10, 10, nil, Opts{}); !h.None() || !cl.None() || ok {
		t.Errorf("Show(nil layout) = %v, %v, %v", h, cl, ok)
	}
}

// TestZeroOptsEmphasisesNothing: Opts{} must not read as "node 0 is hovered".
// The Hit kind makes that true by construction; this pins it anyway, since it
// is the property the shape was chosen for.
func TestZeroOptsEmphasisesNothing(t *testing.T) {
	lay := testLayout(t)
	s := newState(lay, normalizeOpts(lay, Opts{}))
	if s.focusActive() {
		t.Error("Opts{} reports an active focus")
	}
	for li := range lay.Links {
		if got := s.emphasis(li, false); got != defaultRibbonAlpha {
			t.Errorf("link %d alpha %#x under Opts{}, want the default %#x", li, got, defaultRibbonAlpha)
		}
	}
}

// TestStaleHitIsFoldedAway: a pin held across a diagram swap must not address
// whatever now sits at that index.
func TestStaleHitIsFoldedAway(t *testing.T) {
	lay := testLayout(t) // three nodes, three links
	for _, h := range []Hit{NodeHit(99), LinkHit(42), NodeHit(-1)} {
		if got := normalizeOpts(lay, Opts{Selected: h}).Selected; !got.None() {
			t.Errorf("stale Selected %v survived as %v", h, got)
		}
	}
	// An index the layout does hold is left alone.
	if got := normalizeOpts(lay, Opts{Hover: NodeHit(1)}).Hover; got != NodeHit(1) {
		t.Errorf("live Hover came back as %v", got)
	}
	s := newState(lay, normalizeOpts(lay, Opts{Selected: NodeHit(99)}))
	if s.focusActive() {
		t.Error("a stale selection still reports an active focus")
	}
}

// TestIndexZeroIsAddressable is why Hit carries a kind rather than a pair of
// -1-defaulted indices: selecting link 0 has to be distinguishable from
// selecting nothing.
func TestIndexZeroIsAddressable(t *testing.T) {
	lay := testLayout(t)
	if LinkHit(0).None() {
		t.Error("link 0 reads as no hit")
	}
	s := newState(lay, normalizeOpts(lay, Opts{Selected: LinkHit(0)}))
	if got := s.emphasis(0, false); got != 0xff {
		t.Errorf("selected link 0 alpha %#x, want 0xff", got)
	}
	if got := s.emphasis(1, false); got != dimAlpha {
		t.Errorf("unselected link 1 alpha %#x, want the dim %#x", got, dimAlpha)
	}
}

func TestHoverLinkDimsTheRest(t *testing.T) {
	lay := testLayout(t)
	s := newState(lay, normalizeOpts(lay, Opts{Hover: LinkHit(1)}))
	if got := s.emphasis(1, false); got != 0xff {
		t.Errorf("hovered link alpha %#x, want 0xff", got)
	}
	for _, li := range []int{0, 2} {
		if got := s.emphasis(li, false); got != dimAlpha {
			t.Errorf("link %d alpha %#x, want the dim %#x", li, got, dimAlpha)
		}
	}
}

// TestHoverNodeEmphasisesItsLinks checks that hovering a bar lights up
// everything joined to it, in both directions.
func TestHoverNodeEmphasisesItsLinks(t *testing.T) {
	lay := testLayout(t)
	b := nodeIndex(lay, "b")
	s := newState(lay, normalizeOpts(lay, Opts{Hover: NodeHit(b)}))
	for li := range lay.Links {
		l := &lay.Links[li]
		want := uint8(dimAlpha)
		if l.Source == b || l.Target == b {
			want = 0xff
		}
		if got := s.emphasis(li, false); got != want {
			t.Errorf("link %s->%s alpha %#x, want %#x",
				lay.Nodes[l.Source].ID, lay.Nodes[l.Target].ID, got, want)
		}
	}
}

func TestNodeColorPrecedence(t *testing.T) {
	lay := testLayout(t)
	// Bottom of the chain: the qualitative palette. With nothing else set,
	// every node reaches it, so the slot is the node's own index.
	s := newState(lay, normalizeOpts(lay, Opts{}))
	for i := range lay.Nodes {
		want := styletokens.QualitativeCycle(i).AsHex()
		if s.nodeCol[i] != want {
			t.Errorf("node %s palette colour %#x, want %#x", lay.Nodes[i].ID, s.nodeCol[i], want)
		}
	}
	// Node.Color beats the palette.
	lay.Nodes[0].Color = 0x11223344
	s = newState(lay, normalizeOpts(lay, Opts{}))
	if s.nodeCol[0] != 0x11223344 {
		t.Errorf("Node.Color ignored: got %#x", s.nodeCol[0])
	}
	// The callback beats Node.Color.
	s = newState(lay, normalizeOpts(lay, Opts{
		NodeColor: func(n *sankey.NodeLayout) (uint32, bool) {
			if n.ID == "a" {
				return 0xaabbccdd, true
			}
			return 0, false
		},
	}))
	if s.nodeCol[0] != 0xaabbccdd {
		t.Errorf("NodeColor callback ignored: got %#x", s.nodeCol[0])
	}
	// Node "a" was claimed by the callback, so it consumed no palette slot and
	// node "b" gets the first one. Advancing the cycle only on the nodes that
	// reach it is what keeps a diagram from wrapping earlier than it must.
	if s.nodeCol[1] != styletokens.QualitativeCycle(0).AsHex() {
		t.Errorf("a callback returning ok=false did not fall through to slot 0: got %#x", s.nodeCol[1])
	}
}

// TestPaletteRepeatsCountsTheWrap: the palette is seven entries, and a flow
// diagram routinely has more nodes than that. The widget will not invent an
// eighth hue, so it has to say when bars start sharing one.
func TestPaletteRepeatsCountsTheWrap(t *testing.T) {
	build := func(n int) *sankey.Layout {
		t.Helper()
		d := sankey.Diagram{Nodes: []sankey.Node{{ID: "src"}}}
		for i := range n {
			id := fmt.Sprintf("leaf%02d", i)
			d.Nodes = append(d.Nodes, sankey.Node{ID: id})
			d.Links = append(d.Links, sankey.Link{Source: "src", Target: id, Value: 1})
		}
		lay, err := sankey.Compute(d, sankey.Options{})
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		return lay
	}
	const cycle = styletokens.QualitativeCycleLen
	if got := PaletteRepeats(build(cycle-1), Opts{}); got != 0 {
		t.Errorf("a diagram inside the palette reports %d repeats", got)
	}
	// One node past the cycle is one bar that must share.
	if got := PaletteRepeats(build(cycle), Opts{}); got != 1 {
		t.Errorf("PaletteRepeats = %d at one node past the cycle, want 1", got)
	}
	// Colouring the overflow yourself is the documented answer, and it works:
	// a claimed node consumes no slot.
	big := build(cycle + 5)
	opts := Opts{NodeColor: func(n *sankey.NodeLayout) (uint32, bool) {
		if n.ID > "leaf01" { // everything but src, leaf00 and leaf01
			return 0x11223344, true
		}
		return 0, false
	}}
	if got := PaletteRepeats(big, opts); got != 0 {
		t.Errorf("PaletteRepeats = %d once the caller colours the overflow, want 0", got)
	}
	if got := PaletteRepeats(nil, Opts{}); got != 0 {
		t.Errorf("PaletteRepeats(nil) = %d", got)
	}
}

func TestLinkColorFallsBackToSource(t *testing.T) {
	lay := testLayout(t)
	lay.Nodes[nodeIndex(lay, "a")].Color = 0x01020304
	s := newState(lay, normalizeOpts(lay, Opts{}))
	for li := range lay.Links {
		if lay.Links[li].Source != nodeIndex(lay, "a") {
			continue
		}
		if got := s.linkColor(li); got != 0x01020304 {
			t.Errorf("link colour %#x, want the source node's %#x", got, uint32(0x01020304))
		}
	}
	lay.Links[0].Color = 0x0a0b0c0d
	s = newState(lay, normalizeOpts(lay, Opts{}))
	if got := s.linkColor(0); got != 0x0a0b0c0d {
		t.Errorf("Link.Color ignored: got %#x", got)
	}
}

func TestWithAlpha(t *testing.T) {
	if got := withAlpha(0x11223344, 0xff); got != 0x112233ff {
		t.Errorf("withAlpha = %#x", got)
	}
	if got := withAlpha(0x112233ff, 0x00); got != 0x11223300 {
		t.Errorf("withAlpha = %#x", got)
	}
}

func TestLerpRGB(t *testing.T) {
	const a, b = uint32(0x00000080), uint32(0xffffff10)
	if got := lerpRGB(a, b, 0); got != a {
		t.Errorf("t=0 gave %#x, want %#x", got, a)
	}
	// The alpha channel belongs to the caller, so the blend keeps a's.
	if got := lerpRGB(a, b, 1); got != 0xffffff80 {
		t.Errorf("t=1 gave %#x, want 0xffffff80", got)
	}
	mid := lerpRGB(a, b, 0.5)
	for _, shift := range []uint{24, 16, 8} {
		if ch := (mid >> shift) & 0xff; ch < 0x7e || ch > 0x80 {
			t.Errorf("midpoint channel at shift %d = %#x, want ~0x7f", shift, ch)
		}
	}
	if mid&0xff != 0x80 {
		t.Errorf("midpoint alpha %#x, want 0x80", mid&0xff)
	}
}

func TestStateDefaults(t *testing.T) {
	lay := testLayout(t)
	s := newState(lay, normalizeOpts(lay, Opts{}))
	if s.samples != sankey.DefaultSamples {
		t.Errorf("samples = %d, want %d", s.samples, sankey.DefaultSamples)
	}
	if s.fontSize != defaultFontSize {
		t.Errorf("fontSize = %v, want %v", s.fontSize, float32(defaultFontSize))
	}
	if s.alpha != defaultRibbonAlpha {
		t.Errorf("alpha = %#x, want %#x", s.alpha, uint8(defaultRibbonAlpha))
	}
	s = newState(lay, normalizeOpts(lay, Opts{Samples: 4, FontSize: 20, RibbonAlpha: 0x40}))
	if s.samples != 4 || s.fontSize != 20 || s.alpha != 0x40 {
		t.Errorf("explicit options not honoured: %+v", *s)
	}
}
