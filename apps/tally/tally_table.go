package tally

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
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
	// res persists the column widths (ADR-0151) under scopeKey as the table
	// tag, the headers as column names and the table's own view as the
	// type; nil keeps the widths as given. widthsSeen and widthSig are the
	// resolver's first-show and column-set bookkeeping.
	res        *colwidth.Resolver
	widthsSeen bool
	widthSig   string
}

// widthColumns keys the table's columns for the resolver: the header is the
// name, the table's scope the view, so a "path" column in the Diff table and
// one in the Find table can hold different widths.
func (t *stringTable) widthColumns() []colwidth.Column {
	cols := make([]colwidth.Column, len(t.headers))
	for i, h := range t.headers {
		cols[i] = colwidth.Column{Name: h, Type: "text;view=" + t.scopeKey}
	}
	return cols
}

// widthDefaults is what the columns are without an override.
func (t *stringTable) widthDefaults() []float64 {
	out := make([]float64, len(t.headers))
	for i := range t.headers {
		w := tableDefaultWidth
		if i < len(t.widths) && t.widths[i] > 0 {
			w = t.widths[i]
		}
		out[i] = float64(w)
	}
	return out
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
	floor := fsbrowser.MinColumnWidth(density)
	cols := t.widthColumns()
	widths := t.widthDefaults()
	var epoch uint32
	if t.res != nil {
		if sig := widthSignature(cols); sig != t.widthSig {
			if t.widthSig != "" {
				t.res.MarkReseed(t.scopeKey)
			}
			t.widthSig = sig
		}
		widths = t.res.Resolve(t.scopeKey, cols, 0, widths)
		epoch = t.res.Epoch(t.scopeKey)
	}
	for range c.IdScope(ids.PrepareStr(t.scopeKey)) {
		for _, w := range widths {
			width := float32(w)
			if width < floor {
				width = floor
			}
			c.EtColumn(width).RangeMinMax(floor, fsbrowser.MaxColumnWidth).Resizable(true).Send()
		}
		et := c.EndETable(ids.PrepareStr("t"), uint64(len(t.rows)), tableRowHeight, 1, 0)
		if t.maxHeight > 0 {
			et = et.MaxHeight(t.maxHeight)
		}
		if t.res != nil {
			et = et.ApplyWidths(epoch)
			if fetched, ok := et.ColumnWidths(); ok {
				firstShow := !t.widthsSeen
				t.widthsSeen = true
				f64 := make([]float64, len(fetched))
				for i, w := range fetched {
					f64[i] = float64(w)
				}
				t.res.Observe(t.scopeKey, cols, f64, 0, firstShow, time.Now())
			}
		}
		pad := styletokens.PaddingInner(density)
		for i, h := range t.headers {
			col := uint32(i)
			header := func() {
				for range c.Frame(ids.PrepareSeq(seqTableHeaderBase + uint64(col))).
					OuterMargin(0).InnerMargin(pad).KeepIter() {
					c.LabelAtoms(c.Atoms().BeginRichText(h).Strong().End().Keep()).Selectable(false).Send()
				}
			}
			for range et.Headers(0, col) {
				if t.res == nil {
					header()
					continue
				}
				// The reset gesture (ADR-0151): this column, or all of them,
				// back to their defaults.
				c.ContextMenu().Render(func() {
					if c.Button(ids.PrepareSeq(seqTableHeaderBase+0x100+uint64(col)), c.Atoms().Text("Reset column width").Keep()).
						SendResp().HasPrimaryClicked() {
						_ = t.res.Clear(t.scopeKey, cols[col])
					}
					if c.Button(ids.PrepareSeq(seqTableHeaderBase+0x200+uint64(col)), c.Atoms().Text("Reset all column widths").Keep()).
						SendResp().HasPrimaryClicked() {
						_ = t.res.ClearAll(t.scopeKey, cols)
					}
				}, header)
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

// widthSignature is the column set as one string, so a change of result shape
// opens the resolver's settle window before the next report.
func widthSignature(cols []colwidth.Column) string {
	var b []byte
	for _, col := range cols {
		b = append(b, col.Name...)
		b = append(b, 0)
		b = append(b, col.Type...)
		b = append(b, 1)
	}
	return string(b)
}
