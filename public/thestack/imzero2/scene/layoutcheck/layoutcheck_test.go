package layoutcheck

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture is a sidecar shaped like the host's: a top bar at depth 0, a window
// with its controls one level down, and the window's resize edges.
const fixture = `{"id":1,"role":"button","name":"Layout","value":"","cx":30,"cy":11,"x":8,"y":2,"w":45,"h":18,"flags":0,"depth":0}
{"id":2,"role":"button","name":"codec: H.264","value":"","cx":1650,"cy":984,"x":1602,"y":972,"w":94,"h":26,"flags":0,"depth":0}
{"id":3,"role":"window","name":" SQL Playground","value":"","cx":700,"cy":500,"x":4,"y":27,"w":1392,"h":940,"flags":0,"depth":0}
{"id":4,"role":"button","name":"\ue1ca","value":"","cx":1380,"cy":47,"x":1373,"y":40,"w":13,"h":18,"flags":0,"depth":1}
{"id":5,"role":"button","name":"Run","value":"","cx":43,"cy":131,"x":21,"y":116,"w":44,"h":30,"flags":0,"depth":1}
{"id":6,"role":"button","name":"Load .sql…","value":"","cx":130,"cy":131,"x":60,"y":116,"w":84,"h":30,"flags":0,"depth":1}
{"id":7,"role":"button","name":"Save as applet…","value":"","cx":253,"cy":131,"x":140,"y":116,"w":117,"h":30,"flags":1,"depth":1}
{"id":8,"role":"button","name":"Editor","value":"","cx":71,"cy":169,"x":45,"y":157,"w":53,"h":24,"flags":0,"depth":1}
{"id":9,"role":"button","name":"History","value":"","cx":127,"cy":169,"x":98,"y":157,"w":60,"h":24,"flags":0,"depth":1}
{"id":10,"role":"button","name":"row 40","value":"","cx":100,"cy":2100,"x":30,"y":2085,"w":140,"h":30,"flags":0,"depth":1}
{"id":11,"role":"label","name":"","value":"auto: http://localhost:8123/","cx":1450,"cy":131,"x":1300,"y":122,"w":300,"h":18,"flags":0,"depth":1}
{"id":12,"role":"splitter","name":"Resize window left edge","value":"","cx":4,"cy":500,"x":1,"y":30,"w":6,"h":930,"flags":0,"depth":1}
{"id":13,"role":"check_box","name":"Wrap","value":"","cx":200,"cy":200,"x":150,"y":190,"w":80,"h":18,"flags":0,"depth":1}
{"id":14,"role":"button","name":"Wrap help","value":"","cx":210,"cy":200,"x":200,"y":190,"w":40,"h":18,"flags":0,"depth":2}
`

func readFixture(t *testing.T) []Node {
	t.Helper()
	nodes, err := ReadTree(strings.NewReader(fixture))
	require.NoError(t, err)
	require.Len(t, nodes, 14)
	return nodes
}

func byInvariant(fs []Finding, inv InvariantE) (out []Finding) {
	for _, f := range fs {
		if f.Invariant == inv {
			out = append(out, f)
		}
	}
	return out
}

func TestReadTreeKeepsLargeIDs(t *testing.T) {
	nodes, err := ReadTree(strings.NewReader(`{"id":16890102277250500721,"role":"button","name":"Layout","value":"","cx":1,"cy":1,"x":0,"y":0,"w":2,"h":2,"flags":0,"depth":0}` + "\n\n"))
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, uint64(16890102277250500721), nodes[0].ID)

	_, err = ReadTree(strings.NewReader("{not json}\n"))
	assert.Error(t, err)
}

func TestOutsideIsHorizontalAndInteractiveOnly(t *testing.T) {
	fs := byInvariant(Check(readFixture(t), Options{ViewportW: 1400, ViewportH: 1000}), InvariantOutside)
	// The status-bar button past the right edge; not the label past it (not
	// interactive) and not the row below the fold (vertical is scroll).
	require.Len(t, fs, 1)
	assert.Equal(t, uint64(2), fs[0].Node.ID)
	assert.Contains(t, fs[0].Detail, "1400 wide")

	assert.Empty(t, byInvariant(Check(readFixture(t), Options{}), InvariantOutside), "no viewport and every control inside its window")
	// A control past its window's edge, inside the viewport: the window
	// clips it.
	inWindow := []Node{
		{ID: 1, Role: "window", Name: " mdedit", X: 4, W: 500, H: 400},
		{ID: 2, Role: "text_input", X: 410, Y: 120, W: 190, H: 24, Depth: 1},
		{ID: 3, Role: "button", Name: "Open", X: 20, Y: 120, W: 60, H: 24, Depth: 1},
	}
	fs = Check(inWindow, Options{ViewportW: 900})
	require.Len(t, fs, 1)
	assert.Equal(t, uint64(2), fs[0].Node.ID)
	assert.Equal(t, `x 410…600 in window "mdedit" at x 4…504`, fs[0].Detail)
	// Within the tolerance of the edge is inside.
	edge := []Node{{ID: 1, Role: "button", X: 1300, W: 100.5, H: 20}}
	assert.Empty(t, Check(edge, Options{ViewportW: 1400}))
}

func TestHitSize(t *testing.T) {
	fs := byInvariant(Check(readFixture(t), Options{ViewportW: 1400}), InvariantHitSize)
	require.Len(t, fs, 1, "the 13-wide icon button; the 6-wide splitter is not a control")
	assert.Equal(t, uint64(4), fs[0].Node.ID)
	assert.Equal(t, "13×18 under 14", fs[0].Detail)

	assert.Empty(t, byInvariant(Check(readFixture(t), Options{MinHit: 12}), InvariantHitSize))
	// egui's window close button is 14×14: at the floor, not under it.
	assert.Empty(t, Check([]Node{{ID: 1, Role: "button", Name: "Close window", W: 14, H: 14}}, Options{}))
	// A node never laid out has no bounds to measure.
	assert.Empty(t, Check([]Node{{ID: 1, Role: "button"}}, Options{}))
}

func TestOverlapIsAmongEnabledSiblings(t *testing.T) {
	fs := byInvariant(Check(readFixture(t), Options{ViewportW: 1400}), InvariantOverlap)
	// Run (21…65) and Load .sql… (60…144) overlap by 5 points. Load .sql…
	// and the disabled Save as applet… overlap too, and are not reported.
	// Editor and History share an edge. The check box's help button is its
	// child, not its sibling.
	require.Len(t, fs, 1)
	assert.Equal(t, uint64(5), fs[0].Node.ID)
	require.NotNil(t, fs[0].Other)
	assert.Equal(t, uint64(6), fs[0].Other.ID)
	assert.Equal(t, "5×30", fs[0].Detail)

	// Siblings at depth 0 under no parent are compared too.
	top := []Node{
		{ID: 1, Role: "button", X: 0, Y: 0, W: 50, H: 20},
		{ID: 2, Role: "window", X: 0, Y: 30, W: 500, H: 500},
		{ID: 3, Role: "button", X: 10, Y: 40, W: 50, H: 20, Depth: 1},
		{ID: 4, Role: "link", X: 40, Y: 5, W: 50, H: 20},
	}
	fs = Check(top, Options{})
	require.Len(t, fs, 1)
	assert.Equal(t, uint64(1), fs[0].Node.ID)
	assert.Equal(t, uint64(4), fs[0].Other.ID)
}

const svgFixture = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 900 500" width="900" height="500">
  <defs><clipPath id="c0"><rect x="0" y="0" width="900" height="23"/></clipPath></defs>
  <g clip-path="url(#c0)">
    <text y="130.00" x="172.16 179.16 186.16" font-size="13.00" xml:space="preserve">Save as…</text>
    <text y="160.00" x="20.00 28.00 36.00" font-size="13.00" xml:space="preserve">SELECT * FR…</text>
    <text y="190.00" x="20.00" font-size="13.00" xml:space="preserve">running…</text>
    <text y="220.00" x="20.00" font-size="13.00" xml:space="preserve">plain text</text>
  </g>
</svg>`

func TestCheckElided(t *testing.T) {
	runs, err := ReadSVGText(strings.NewReader(svgFixture))
	require.NoError(t, err)
	require.Len(t, runs, 4)
	assert.Equal(t, "SELECT * FR…", runs[1].Text)
	assert.InDelta(t, 20, runs[1].X, 0.01)
	assert.InDelta(t, 160, runs[1].Baseline, 0.01)

	nodes := []Node{
		// The app's own ellipsis: drawn as it reads.
		{ID: 1, Role: "button", Name: "Save as…", X: 165, Y: 115, W: 70, H: 22},
		// Elided by the host: the tree has the whole text.
		{ID: 2, Role: "label", Value: "SELECT * FROM events WHERE x = 1", X: 15, Y: 147, W: 90, H: 18},
		// A status the app writes with an ellipsis.
		{ID: 3, Role: "label", Value: "running…", X: 15, Y: 177, W: 90, H: 18},
		{ID: 4, Role: "label", Value: "plain text", X: 15, Y: 207, W: 90, H: 18},
	}
	fs := CheckElided(nodes, runs)
	require.Len(t, fs, 1)
	assert.Equal(t, InvariantClipped, fs[0].Invariant)
	assert.Equal(t, uint64(2), fs[0].Node.ID)
	assert.Contains(t, fs[0].Detail, `"SELECT * FR…"`)

	// The run must be over the node.
	nodes[1].Y = 400
	assert.Empty(t, CheckElided(nodes, runs))
}

func TestExceptions(t *testing.T) {
	fs := Check(readFixture(t), Options{ViewportW: 1400})
	require.Len(t, fs, 4)

	excs := []Exception{
		{Invariant: InvariantHitSize, Role: "button", Name: "\ue1ca", Reason: "a glyph button beside a field"},
		// Either node of an overlap matches.
		{Invariant: InvariantOverlap, Name: "Load .sql…", Capture: "table", Reason: "drawn over on purpose"},
		// A label is anchored by its value.
		{Invariant: InvariantCut, ValueContains: "localhost", Reason: "the endpoint scrolls sideways"},
	}
	kept, excused := Apply("table", fs, excs)
	require.Len(t, kept, 1)
	assert.Equal(t, InvariantOutside, kept[0].Invariant)
	assert.Len(t, excused, 3)

	// The capture restriction holds.
	kept, _ = Apply("history", fs, excs)
	assert.Len(t, kept, 2)

	// An exception for another invariant does not cover the node.
	kept, _ = Apply("table", fs, []Exception{{Invariant: InvariantOverlap, Name: "codec: H.264", Reason: "x"}})
	assert.Len(t, kept, 4)
}

func TestExceptionValidate(t *testing.T) {
	assert.NoError(t, Exception{Invariant: InvariantOutside, Role: "button", Reason: "a wide table scrolls sideways"}.Validate())
	assert.Error(t, Exception{Invariant: "overlaps", Name: "x", Reason: "r"}.Validate(), "unknown invariant")
	assert.Error(t, Exception{Invariant: InvariantOverlap, Reason: "r"}.Validate(), "no anchor")
	assert.Error(t, Exception{Invariant: InvariantOverlap, Name: "x"}.Validate(), "no reason")
}

// A table cell and the button in it share a rect: the tree lists one control
// twice, which is not two controls drawn over each other.
func TestOverlapSkipsOneControlListedTwice(t *testing.T) {
	nodes := []Node{
		{ID: 1, Role: "window", X: 0, Y: 0, W: 400, H: 300, Depth: 0},
		{ID: 2, Role: "button", Name: "3", X: 40, Y: 60, W: 32, H: 14.5, Depth: 1},
		{ID: 3, Role: "button", Name: "3", X: 40, Y: 60, W: 32, H: 14.5, Depth: 1},
		{ID: 4, Role: "button", Name: "Detail", X: 50, Y: 62, W: 40, H: 20, Depth: 1},
	}
	fs := byInvariant(Check(nodes, Options{ViewportW: 400}), InvariantOverlap)
	require.Len(t, fs, 2, "the third button overlaps each copy; the copies do not overlap each other")
	for _, f := range fs {
		assert.True(t, f.Node.ID == 4 || f.Other.ID == 4)
	}
}

func TestOverlapAndCutAreAdvisory(t *testing.T) {
	for _, inv := range AllInvariants {
		assert.Equal(t, inv != InvariantOverlap && inv != InvariantCut, inv.Blocking(), string(inv))
	}
}

func TestCutIsTextPastTheRightEdge(t *testing.T) {
	fs := byInvariant(Check(readFixture(t), Options{ViewportW: 1400}), InvariantCut)
	// The endpoint label runs from 1300 to 1600 in a window that ends at
	// 1396.
	require.Len(t, fs, 1)
	assert.Equal(t, uint64(11), fs[0].Node.ID)
	assert.Equal(t, `x 1300…1600 past window "SQL Playground" at x 4…1396 by 204`, fs[0].Detail)

	win := Node{ID: 1, Role: "window", Name: " chat", X: 100, Y: 0, W: 400, H: 300}
	at := func(n Node) []Finding {
		n.Depth = 1
		return byInvariant(Check([]Node{win, n}, Options{ViewportW: 1000}), InvariantCut)
	}
	label := func(x, w float32) Node {
		return Node{ID: 2, Role: "label", Value: "Confirming also endorses it for publishing.", X: x, Y: 40, W: w, H: 18}
	}
	assert.Empty(t, at(label(110, 380)), "inside the window")
	assert.Empty(t, at(label(110, 390.5)), "within the tolerance of the edge")
	assert.Len(t, at(label(110, 420)), 1, "past the right edge")
	assert.Empty(t, at(label(60, 300)), "past the left edge only: text is cut on the right")
	heading := label(110, 420)
	heading.Role = "heading"
	assert.Len(t, at(heading), 1, "a heading is text")
	image := label(110, 420)
	image.Role = "image"
	assert.Empty(t, at(image), "an image is not text")
	assert.Empty(t, at(Node{ID: 2, Role: "label", Value: "never laid out"}), "no bounds")

	// A control past the edge is outside's, not cut's.
	button := Node{ID: 2, Role: "button", Name: "Export", X: 450, Y: 40, W: 80, H: 20, Depth: 1}
	all := Check([]Node{win, button}, Options{ViewportW: 1000})
	require.Len(t, all, 1)
	assert.Equal(t, InvariantOutside, all[0].Invariant)

	// Text in no window is held to the viewport, when there is one.
	top := []Node{{ID: 1, Role: "label", Value: "status", X: 900, Y: 980, W: 160, H: 18}}
	fs = byInvariant(Check(top, Options{ViewportW: 1000}), InvariantCut)
	require.Len(t, fs, 1)
	assert.Equal(t, "x 900…1060 in a viewport 1000 wide, by 60", fs[0].Detail)
	assert.Empty(t, Check(top, Options{}), "no viewport, no edge")

	// The tree carries no scroll area, so a row of a table that scrolls
	// sideways is reported although the person can scroll to it: the reason
	// the invariant is advisory.
	wide := label(110, 900)
	wide.Value = "a cell in the twelfth column of a table that scrolls sideways"
	assert.Len(t, at(wide), 1)
}
