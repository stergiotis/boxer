package tally

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// stringTable is a small result table: fixed headers, rows of cells as text,
// an optional tone per row, and a click report. It is what the Diff and
// History tabs render their lanes into — the rows are already strings
// (gloss.FormatArrowElem did the work), so a general Arrow table would be
// machinery without a job here.
type stringTable struct {
	scopeKey string
	headers  []string
	widths   []float32
	rows     [][]string
	// tone colours the first cell's text per row; nil or color.Transparent
	// leaves it as it is.
	tone     []color.Color
	selected int
	// maxHeight bounds the table (0 = let the pane decide); key remembers
	// which result the selection belongs to, so a new result drops it.
	maxHeight float32
	key       string
}

// resetFor drops the selection when the table shows a new result.
func (t *stringTable) resetFor(key string) {
	if t.key != key {
		t.key, t.selected = key, -1
	}
}

const (
	tableRowHeight     float32 = 22
	tableDefaultWidth  float32 = 140
	seqTableRowBase    uint64  = 0x0200_1100_0000_0000
	seqTableCellBase   uint64  = 0x0200_1200_0000_0000
	seqTableHeaderBase uint64  = 0x0200_1300_0000_0000
)

// render draws the table and returns the clicked row, -1 for none.
func (t *stringTable) render(ids *c.WidgetIdStack, density styletokens.DensityE) (clicked int) {
	clicked = -1
	ncols := len(t.headers)
	if ncols == 0 {
		return
	}
	for range c.IdScope(ids.PrepareStr(t.scopeKey)) {
		for i := range t.headers {
			w := tableDefaultWidth
			if i < len(t.widths) && t.widths[i] > 0 {
				w = t.widths[i]
			}
			c.EtColumn(w).RangeMinMax(w, float32(math.Inf(1))).Resizable(true).Send()
		}
		et := c.EndETable(ids.PrepareStr("t"), uint64(len(t.rows)), tableRowHeight, 1, 0)
		if t.maxHeight > 0 {
			et = et.MaxHeight(t.maxHeight)
		}
		pad := styletokens.PaddingInner(density)
		for i, h := range t.headers {
			for range et.Headers(0, uint32(i)) {
				for range c.Frame(ids.PrepareSeq(seqTableHeaderBase + uint64(i))).
					OuterMargin(0).InnerMargin(pad).KeepIter() {
					c.LabelAtoms(c.Atoms().BeginRichText(h).Strong().End().Keep()).Selectable(false).Send()
				}
			}
		}
		rowBegin, rowEnd := 0, len(t.rows)
		if rb, re, _, _, _, ok := et.VisibleRange(); ok {
			rowBegin = min(int(rb), len(t.rows))
			rowEnd = min(int(re), rowEnd)
		}
		for r := rowBegin; r < rowEnd; r++ {
			var fr c.FrameFluid
			fill := color.Transparent
			strokeW, stroke := float32(0), color.Transparent
			if r == t.selected {
				fill = color.Hex(styletokens.AccentSubtle.AsHex())
				strokeW, stroke = 1, color.Hex(styletokens.AccentDefault.AsHex())
			} else if r%2 == 1 {
				fill = color.Hex(styletokens.NeutralBgFaint.AsHex())
			}
			for range et.Rows(uint64(r)) {
				fr = c.Frame(ids.PrepareSeq(seqTableRowBase+uint64(r))).
					Fill(fill).Stroke(strokeW, stroke).OuterMargin(0).InnerMargin(0).
					SenseClick().HoverCursorPointer()
				for range fr.KeepIter() {
					c.UiSetMinWidthAvailable()
					c.UiSetMinHeight(tableRowHeight - 1)
				}
			}
			if c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id()).HasPrimaryClicked() {
				clicked = r
			}
			row := t.rows[r]
			for ci := 0; ci < ncols; ci++ {
				cell := ""
				if ci < len(row) {
					cell = row[ci]
				}
				et.BeginCells(uint64(r), uint32(ci))
				for range c.Frame(ids.PrepareSeq(seqTableCellBase+uint64(r)*uint64(ncols)+uint64(ci))).
					OuterMargin(0).InnerMarginSides(pad, pad, 0, 0).KeepIter() {
					if ci == 0 && r < len(t.tone) && t.tone[r] != color.Transparent {
						c.LabelAtoms(c.Atoms().BeginRichTextColored(t.tone[r], color.Transparent, cell).End().Keep()).
							Selectable(false).Send()
					} else {
						c.Label(cell).Selectable(false).Truncate().Send()
					}
				}
				et.EndCells()
			}
		}
		et.Send()
	}
	if clicked >= 0 {
		t.selected = clicked
	}
	return
}
