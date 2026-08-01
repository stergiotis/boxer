package view

import (
	"math"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// testTree is the same call tree the layout tests use: main(18) over
// parse(9) and eval(8), each with two children.
func testTree() icicle.Tree {
	return icicle.Tree{
		Labels:  []string{"main", "parse", "lex", "ast", "eval", "walk", "emit"},
		Parents: []int32{-1, 0, 1, 1, 0, 4, 4},
		Self:    []float64{1, 2, 4, 3, 1, 5, 2},
	}
}

func mustLayout(t *testing.T, o icicle.Options) *icicle.Layout {
	t.Helper()
	lay, err := icicle.Compute(testTree(), o)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return lay
}

// linFrame builds a frame with a linear value-to-pixel mapping, standing in
// for implot's transform: [xLo,xHi] and [yLo,yHi] in plot space map onto the
// area rect, with plot y up and pixel y down.
func linFrame(xLo, xHi, yLo, yHi float64, areaX, areaY, areaW, areaH float32) frame {
	return frame{
		areaX: areaX, areaY: areaY, areaW: areaW, areaH: areaH,
		pxX: func(v float64) float32 {
			return areaX + float32((v-xLo)/(xHi-xLo))*areaW
		},
		plotX: func(px float32) float64 {
			return xLo + float64((px-areaX)/areaW)*(xHi-xLo)
		},
		pxY: func(v float64) float32 {
			return areaY + areaH - float32((v-yLo)/(yHi-yLo))*areaH
		},
		plotY: func(px float32) float64 {
			return yLo + float64((areaY+areaH-px)/areaH)*(yHi-yLo)
		},
	}
}

func newState(t *testing.T, lay *icicle.Layout, o Opts) *state {
	t.Helper()
	r := &Renderer{}
	r.s.prepare(lay, o.withDefaults(lay))
	return &r.s
}

// The whole icicle in a 360×54 area: 18 value units across, 3 rows down.
func icicleFrame() frame { return linFrame(0, 18, -3, 0, 0, 0, 360, 54) }

func TestRectOfSnapsAndLeavesAGap(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	f := icicleFrame()

	find := func(label string) *icicle.Node {
		for i := range lay.Nodes {
			if lay.Nodes[i].Label == label {
				return &lay.Nodes[i]
			}
		}
		t.Fatalf("no node %q", label)
		return nil
	}

	// lex spans [0,4] of 18 over 360 px → [0,80]; the gap takes a pixel off
	// the right edge.
	x0, y0, x1, y1, ok := rectOf(f, find("lex"))
	if !ok {
		t.Fatal("lex was culled")
	}
	if x0 != 0 || x1 != 80-gapPx {
		t.Errorf("lex x = [%v,%v], want [0,%v]", x0, x1, 80-gapPx)
	}
	// Row 2 of 3 over 54 px: rows are 18 px, depth 2 is the bottom one.
	if y0 != 36 || y1 != 54-gapPx {
		t.Errorf("lex y = [%v,%v], want [36,%v]", y0, y1, 54-gapPx)
	}

	// The neighbour starts exactly where this one's unshrunk edge was, so
	// the gap between them is exactly gapPx and both edges are integral.
	ax0, _, _, _, ok := rectOf(f, find("ast"))
	if !ok {
		t.Fatal("ast was culled")
	}
	if got := ax0 - x1; math.Abs(float64(got-gapPx)) > 1e-6 {
		t.Errorf("gap between lex and ast = %v, want %v", got, gapPx)
	}
	if ax0 != float32(math.Round(float64(ax0))) {
		t.Errorf("ast x0 = %v, want a whole pixel", ax0)
	}
}

func TestRectOfCullsSubPixelFrames(t *testing.T) {
	tr := icicle.Tree{
		Labels:  []string{"root", "big", "speck"},
		Parents: []int32{-1, 0, 0},
		Self:    []float64{0, 10000, 1},
	}
	lay, err := icicle.Compute(tr, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// 10001 units across 360 px: the speck is well under a pixel.
	f := linFrame(0, 10001, -2, 0, 0, 0, 360, 36)
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		_, _, _, _, ok := rectOf(f, n)
		if n.Label == "speck" && ok {
			t.Error("a sub-pixel frame was emitted")
		}
		if n.Label == "big" && !ok {
			t.Error("a full-width frame was culled")
		}
	}
}

func TestVisibleRowsBothOrientations(t *testing.T) {
	cases := []struct {
		name   string
		orient icicle.OrientationE
		frame  frame
		lo, hi int
	}{
		// The whole tree in view.
		{"icicle all", icicle.OrientIcicle, linFrame(0, 18, -3, 0, 0, 0, 360, 54), 0, 2},
		{"flame all", icicle.OrientFlame, linFrame(0, 18, 0, 3, 0, 0, 360, 54), 0, 2},
		// Scrolled so only the deepest two rows show.
		{"icicle scrolled", icicle.OrientIcicle, linFrame(0, 18, -3, -1, 0, 0, 360, 36), 1, 2},
		{"flame scrolled", icicle.OrientFlame, linFrame(0, 18, 1, 3, 0, 0, 360, 36), 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lay := mustLayout(t, icicle.Options{Orientation: tc.orient})
			s := newState(t, lay, Opts{})
			lo, hi, ok := s.visibleRows(tc.frame)
			if !ok {
				t.Fatal("visibleRows reported nothing on screen")
			}
			if lo != tc.lo || hi != tc.hi {
				t.Errorf("rows = [%d,%d], want [%d,%d]", lo, hi, tc.lo, tc.hi)
			}
		})
	}
}

func TestVisibleRowsClampsAndRejectsOffScreen(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	// A window far deeper than the tree.
	if _, _, ok := s.visibleRows(linFrame(0, 18, -40, -30, 0, 0, 360, 54)); ok {
		t.Error("a window past the deepest row reported visible rows")
	}
	// A window straddling the top: clamped to row 0 rather than negative.
	lo, hi, ok := s.visibleRows(linFrame(0, 18, -1, 2, 0, 0, 360, 54))
	if !ok || lo != 0 {
		t.Errorf("rows = [%d,%d,%v], want lo 0", lo, hi, ok)
	}
}

// Zooming the value axis must reduce the nodes considered, not just the ones
// drawn: that is what keeps frame cost tracking the view and not the tree.
func TestEachVisibleCullsByValueWindow(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})

	collect := func(f frame) []string {
		var got []string
		s.eachVisible(f, func(n *icicle.Node, _ int) { got = append(got, n.Label) })
		return got
	}
	all := collect(icicleFrame())
	if len(all) != 7 {
		t.Errorf("unzoomed view yielded %d nodes (%v), want all 7", len(all), all)
	}
	// Zoom onto eval's subtree, x in [9,17].
	zoomed := collect(linFrame(9, 17, -3, 0, 0, 0, 360, 54))
	joined := strings.Join(zoomed, ",")
	for _, want := range []string{"main", "eval", "walk", "emit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("zoomed view is missing %s (got %v)", want, joined)
		}
	}
	for _, unwanted := range []string{"lex", "ast"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("zoomed view still visits %s, which is off screen (got %v)", unwanted, joined)
		}
	}
}

func TestCollectFramesBatchesOnlyVisibleRects(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	s.collectFrames(icicleFrame())
	if got := len(s.rMinX); got != 7 {
		t.Errorf("batched %d rects, want 7", got)
	}
	if len(s.rCols) != len(s.rMinX) || len(s.rMaxY) != len(s.rMinX) {
		t.Fatal("the batched rect columns disagree in length")
	}
	for i := range s.rMinX {
		if s.rMaxX[i] <= s.rMinX[i] || s.rMaxY[i] <= s.rMinY[i] {
			t.Errorf("rect %d is degenerate: [%v,%v]-[%v,%v]", i, s.rMinX[i], s.rMinY[i], s.rMaxX[i], s.rMaxY[i])
		}
	}
	// Re-emitting must reset the buffers rather than append to them.
	s.collectFrames(icicleFrame())
	if got := len(s.rMinX); got != 7 {
		t.Errorf("second emit batched %d rects, want 7 (buffers were not reset)", got)
	}
}

func TestLabelForFitsAndDeclines(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	find := func(label string) *icicle.Node {
		for i := range lay.Nodes {
			if lay.Nodes[i].Label == label {
				return &lay.Nodes[i]
			}
		}
		t.Fatalf("no node %q", label)
		return nil
	}

	// A wide frame in a roomy area gets its label, inset from the left edge
	// and centred in its row.
	f := icicleFrame()
	text, x, y, ok := s.labelFor(f, find("main"))
	if !ok {
		t.Fatal("the root frame got no label")
	}
	if text != "main" {
		t.Errorf("label = %q, want the whole name", text)
	}
	if x != labelPadPx {
		t.Errorf("label x = %v, want the pad %v", x, labelPadPx)
	}
	// Row 0 covers pixels [0,18) less the gap, so its centre is 8.5.
	if y != 8.5 {
		t.Errorf("label y = %v, want the row centre 8.5", y)
	}

	// A label too long for its frame is elided rather than overflowing.
	long := icicle.Tree{
		Labels:  []string{"github.com/example/pkg.(*Server).ServeHTTP"},
		Parents: []int32{-1},
		Self:    []float64{1},
	}
	longLay, err := icicle.Compute(long, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	longState := newState(t, longLay, Opts{})
	text, _, _, ok = longState.labelFor(linFrame(0, 1, -1, 0, 0, 0, 120, 18), &longLay.Nodes[0])
	if !ok {
		t.Fatal("a 120 px frame got no label at all")
	}
	if !strings.HasSuffix(text, "…") {
		t.Errorf("label %q was not elided to fit 120 px", text)
	}
	if w := implot.EstimateTextWidth(text, DefaultFontSize); w > 120 {
		t.Errorf("elided label %q estimates at %v px, wider than its 120 px frame", text, w)
	}

	// Narrow still elides; narrower than one character plus an ellipsis
	// declines outright rather than drawing a bare "…".
	if got, _, _, ok := s.labelFor(linFrame(0, 18, -3, 0, 0, 0, 30, 54), find("main")); !ok || !strings.HasSuffix(got, "…") {
		t.Errorf("in a 30 px frame the label was %q (ok=%v), want it elided", got, ok)
	}
	if _, _, _, ok := s.labelFor(linFrame(0, 18, -3, 0, 0, 0, 15, 54), find("main")); ok {
		t.Error("a frame far too narrow still got a label")
	}
	// A row shorter than the glyphs gets no label however wide it is.
	if _, _, _, ok := s.labelFor(linFrame(0, 18, -3, 0, 0, 0, 1200, 12), find("main")); ok {
		t.Error("a row shorter than the font size still got a label")
	}
}

func TestElide(t *testing.T) {
	// At this size a Latin glyph is 6.2 px and a CJK one 10, which is what
	// makes the budgets below readable.
	const size = 10
	cases := []struct {
		in      string
		availPx float32
		want    string
	}{
		{"runtime.mallocgc", 200, "runtime.mallocgc"},
		{"runtime.mallocgc", 100, "runtime.mallocgc"}, // 99.2 px, fits exactly
		{"runtime.mallocgc", 50, "runtime…"},
		{"runtime.mallocgc", 13, "r…"},
		{"runtime.mallocgc", 8, ""}, // no room for a glyph beside the ellipsis
		{"runtime.mallocgc", 0, ""},
		{"runtime.mallocgc", -3, ""},
		{"", 10, ""},
		// Multi-byte: the cut lands on a rune boundary, not a byte one.
		{"日本語のフレーム", 40, "日本語…"},
		{"日本語", 30, "日本語"},
		// And the reason the budget is pixels rather than characters: a box
		// 49.6 px wide holds eight Latin glyphs, so a character budget would
		// have kept all eight of these — 80 px of them. It keeps four.
		{"日本語のフレーム", 49.6, "日本語の…"},
	}
	for _, tc := range cases {
		if got := elide(tc.in, tc.availPx, size); got != tc.want {
			t.Errorf("elide(%q, %v) = %q, want %q", tc.in, tc.availPx, got, tc.want)
		}
	}
	// Whatever comes back must actually fit what it was cut for.
	for _, tc := range cases {
		if got := elide(tc.in, tc.availPx, size); got != "" {
			if w := implot.EstimateTextWidth(got, size); w > tc.availPx {
				t.Errorf("elide(%q, %v) = %q, which is %v px wide", tc.in, tc.availPx, got, w)
			}
		}
	}
}

func TestContrastTextPicksTheReadableNeutral(t *testing.T) {
	light := styletokens.QualitativeCycle(4).AsHex() // a bright palette entry
	dark := styletokens.NeutralBgPanel.AsHex()
	if got := contrastText(0xffffffff); got != styletokens.NeutralBgExtreme.AsHex() {
		t.Errorf("ink on white = %08x, want the dark neutral", got)
	}
	if got := contrastText(0x000000ff); got != styletokens.NeutralTextExtreme.AsHex() {
		t.Errorf("ink on black = %08x, want the light neutral", got)
	}
	if got := contrastText(dark); got != styletokens.NeutralTextExtreme.AsHex() {
		t.Errorf("ink on the panel neutral = %08x, want the light neutral", got)
	}
	// Whatever it picks for a mid palette colour, it must be one of the two
	// tokens and never an invented value.
	got := contrastText(light)
	if got != styletokens.NeutralBgExtreme.AsHex() && got != styletokens.NeutralTextExtreme.AsHex() {
		t.Errorf("ink = %08x, which is neither IDS neutral", got)
	}
}

// A frame's colour must depend only on its name, so the same function is the
// same colour in two captures and at two places in one tree.
func TestFillByLabelIsPositionIndependent(t *testing.T) {
	tr := icicle.Tree{
		Labels:  []string{"root", "a", "shared", "b", "shared"},
		Parents: []int32{-1, 0, 1, 0, 3},
		Self:    []float64{0, 1, 5, 1, 5},
	}
	lay, err := icicle.Compute(tr, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	s := newState(t, lay, Opts{})
	var shared []uint32
	for i := range lay.Nodes {
		if lay.Nodes[i].Label == "shared" {
			shared = append(shared, s.fill(&lay.Nodes[i]))
		}
	}
	if len(shared) != 2 {
		t.Fatalf("found %d shared frames, want 2", len(shared))
	}
	if shared[0] != shared[1] {
		t.Errorf("the same label got two colours: %08x and %08x", shared[0], shared[1])
	}
}

func TestFillModesAndOverride(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})

	byDepth := newState(t, lay, Opts{Color: ColorByDepth})
	root, leaf := &lay.Nodes[0], &lay.Nodes[2]
	if root.Depth == leaf.Depth {
		t.Fatal("the fixture lost its depth range")
	}
	if byDepth.fill(root) == byDepth.fill(leaf) {
		t.Error("ColorByDepth gave two different depths the same colour")
	}

	// The override wins, and ok=false falls through to the scheme.
	const sentinel = 0x1234abff
	sel := newState(t, lay, Opts{
		NodeColor: func(n *icicle.Node) (uint32, bool) {
			return sentinel, n.Label == "main"
		},
	})
	if got := sel.fill(root); got != sentinel {
		t.Errorf("override ignored: %08x", got)
	}
	if got := sel.fill(leaf); got == sentinel {
		t.Error("override applied to a node it declined")
	}
}

func TestWithDefaults(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})

	got := Opts{}.withDefaults(lay)
	if got.RowPx != DefaultRowPx || got.FontSize != DefaultFontSize {
		t.Errorf("defaults not applied: RowPx=%v FontSize=%v", got.RowPx, got.FontSize)
	}
	// The zero Hit means nothing, which is the point of it being a struct:
	// node 0 stays addressable.
	if got.Hover.Ok || got.Selected.Ok {
		t.Error("the zero Opts highlighted a node")
	}
	withRoot := Opts{Selected: NodeHit(0)}.withDefaults(lay)
	if !withRoot.Selected.Ok || withRoot.Selected.Node != 0 {
		t.Error("node 0 could not be selected")
	}
	// A stale index from a previous, larger layout is folded away rather than
	// indexing out of range at draw time.
	stale := Opts{Hover: NodeHit(len(lay.Nodes) + 5)}.withDefaults(lay)
	if stale.Hover.Ok {
		t.Error("an out-of-range hit survived normalisation")
	}
	if neg := (Opts{Hover: Hit{Node: -2, Ok: true}}).withDefaults(lay); neg.Hover.Ok {
		t.Error("a negative hit survived normalisation")
	}
	// An explicit size is left alone.
	kept := Opts{RowPx: 30, FontSize: 9}.withDefaults(lay)
	if kept.RowPx != 30 || kept.FontSize != 9 {
		t.Errorf("explicit sizes were overwritten: %v / %v", kept.RowPx, kept.FontSize)
	}
}

// The entry points are called from render code where a nil layout is a normal
// transient state, so none of them may panic.
func TestNilSafety(t *testing.T) {
	if h, c, clicked := Probe(nil, nil); h.Ok || c.Ok || clicked {
		t.Error("Probe on a nil plot reported a hit")
	}
	Setup(nil, nil, Opts{})
	Draw(nil, nil, Opts{})
	ZoomTo(nil, nil, 0)
	var r Renderer
	if h, c, clicked := r.Show(nil, "t", 10, 10, nil, Opts{}); h.Ok || c.Ok || clicked {
		t.Error("Show on a nil layout reported a hit")
	}
}
