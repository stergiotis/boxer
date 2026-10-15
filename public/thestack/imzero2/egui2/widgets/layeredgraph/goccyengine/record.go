//go:build !(js || wasip1)

package goccyengine

import (
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz/cgraph"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// recordShape is Graphviz's record node shape, which goccy names no constant
// for.
const recordShape = cgraph.Shape("record")

// titleSubtitleGap separates a row node's title from its subtitle in the
// title field, so the field is measured wide enough to hold both with room
// between them.
const titleSubtitleGap = "  "

// recordLabel builds the Graphviz record label of a row node: one field for
// the title line, then one per row, each row's field carrying the port "rN"
// for its index. A record's top-level fields run across the rank direction —
// side by side under top-bottom ranks, stacked under left-right ones — so the
// field list is wrapped in braces exactly when that is needed to stack the
// fields top to bottom.
func recordLabel(n layeredgraph.Node, title string, dir layeredgraph.RankDir) (label string, err error) {
	if n.Shape != layeredgraph.NodeShapeBox {
		err = eb.Build().Str("node", n.ID).Int("shape", int(n.Shape)).Errorf("a row node must be a box")
		return
	}
	for what, text := range map[string]string{"title": title, "subtitle": n.Subtitle} {
		if strings.ContainsAny(text, "\r\n") {
			err = eb.Build().Str("node", n.ID).Str("line", what).Errorf("a row node's line holds no line break")
			return
		}
	}
	seen := make(map[string]int, len(n.Rows))
	for i, r := range n.Rows {
		if strings.ContainsAny(r.Text, "\r\n") {
			err = eb.Build().Str("node", n.ID).Int("row", i).Errorf("a row holds no line break")
			return
		}
		if prev, dup := seen[r.ID]; dup {
			err = eb.Build().Str("node", n.ID).Str("rowId", r.ID).Int("row", i).Int("previous", prev).Errorf("duplicate row id")
			return
		}
		seen[r.ID] = i
	}

	var b strings.Builder
	stack := dir == layeredgraph.RankDirTopBottom || dir == layeredgraph.RankDirBottomTop
	if stack {
		b.WriteByte('{')
	}
	head := title
	if n.Subtitle != "" {
		head += titleSubtitleGap + n.Subtitle
	}
	writeRecordText(&b, head)
	for i, r := range n.Rows {
		b.WriteString("|<r")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("> ")
		writeRecordText(&b, r.Text)
	}
	if stack {
		b.WriteByte('}')
	}
	label = b.String()
	return
}

// writeRecordText writes text into a record field with every character that
// means something to the record parser escaped: the field and port syntax,
// the backslash that starts Graphviz's own label escapes, and spaces, which
// the parser otherwise reads as token separators.
func writeRecordText(b *strings.Builder, text string) {
	for _, r := range text {
		switch r {
		case '\\', '{', '}', '|', '<', '>', ' ':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
}

// parseRects parses a laid-out record's "rects" attribute — one
// "x0,y0,x1,y1" per field, in Graphviz coordinates — mapping both corners
// through flip, which turns Graphviz's y-up into the output's y-down.
func parseRects(s string, flip func(x, y float64) layeredgraph.Point) (rects []layeredgraph.Rect, ok bool) {
	for tok := range strings.FieldsSeq(s) {
		c := strings.Split(tok, ",")
		if len(c) != 4 {
			return nil, false
		}
		var v [4]float64
		for i := range c {
			f, err := strconv.ParseFloat(c[i], 64)
			if err != nil {
				return nil, false
			}
			v[i] = f
		}
		// Graphviz's lower-left (x0,y0) and upper-right (x1,y1) become the
		// output's top-left and bottom-right.
		rects = append(rects, layeredgraph.Rect{Min: flip(v[0], v[3]), Max: flip(v[2], v[1])})
	}
	return rects, true
}
