package recordtable

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// A group line (ADR-0336 §SD5) is one etable row drawn in two layers. Its
// row block — the Ui egui_table hands over before the row's cells, spanning
// every column — paints the band, senses the click that folds it, and
// carries the chevron, the key's label and the leaf count, inset by the
// level's depth and clipped at the first column that carries an aggregate.
// The cells of the columns that aggregate are emitted on top, aligned under
// their headers; the other columns emit no cell, so the band shows through.
// A leaf line's row block paints the stripe egui_table would have, since
// the grid's own striping is off while grouped (it is painted per cell,
// over the band).

// scrollAlignCenter is egui::Align::Center as EndETable.ScrollToRow decodes
// it, the same value widgets/tree uses.
const scrollAlignCenter uint8 = 2

// GroupIndent is the band's inset per depth.
const GroupIndent float32 = 14

// The disclosure glyphs, from the Phosphor font the client loads — the
// pair widgets/tree settled on after the solid triangles fell through to
// a CJK fallback face.
var (
	glyphCollapsed = icons.PhCaretRight
	glyphExpanded  = icons.PhCaretDown
)

// Row chrome colours, from the design-system roles (ADR-0031): the band
// takes the surface, the stripe the faintest neutral background, as
// widgets/tree does.
var (
	groupBandFill = color.Hex((styletokens.NeutralBorderDefault.AsHex() & 0xffffff00) | 0x40)
	stripeFill    = color.Hex(styletokens.NeutralBgFaint.AsHex())
)

// groupLevelOf is the column level keyed on arrow's value, -1 when none: a
// level with a Key of its own does not key on the column as drawn.
func (in *Input) groupLevelOf(arrow int) int {
	if !in.grouped() {
		return -1
	}
	for i, lvl := range in.Groups.Levels() {
		if !lvl.IsPath() && lvl.Key == nil && lvl.Arrow == arrow {
			return i
		}
	}
	return -1
}

// aggregates reports whether a column draws a cell on a group line.
func (col *Column) aggregates() bool { return col.AggCell != nil || col.Agg != AggNone }

// bandLabelSpan is where a band's label goes: after the leading (sticky)
// columns, which the host does not replay the row block under (ADR-0176
// SD6: the block is replayed in the scrolled region alone, so anything
// drawn under the gutter is clipped), and as wide as the columns from there
// up to the first that aggregates. Widths are as egui_table last reported
// them, else as resolved.
func (in *Input) bandLabelSpan(et c.EndETableFluid, resolved []float64) (leadW, labelW float32) {
	lead := in.lead()
	n := lead + len(in.Columns)
	for i := range in.Columns {
		if in.Columns[i].aggregates() {
			n = lead + i
			break
		}
	}
	fetched, ok := et.ColumnWidths()
	for pos := range n {
		var w float32
		if ok && pos < len(fetched) {
			w = fetched[pos]
		} else if pos < len(resolved) {
			w = float32(resolved[pos])
		}
		if pos < lead {
			leadW += w
		} else {
			labelW += w
		}
	}
	return
}

// renderRowBlocks emits the row block of every line in [rowLo, rowHi): a
// band for a group line, a stripe for an odd leaf line. The band's
// responses are read here, so a click is seen on a frame that replays its
// cells too.
func (in *Input) renderRowBlocks(res *Result, et c.EndETableFluid, chrome *Chrome, lay *Layout, start, rowLo, rowHi int64, pad, rowHeight, leadW, labelW float32) {
	ids := in.Ids
	for local := rowLo; local < rowHi; local++ {
		ln := lay.Lines[start+local]
		switch {
		case ln.IsGroup():
			for range et.Rows(uint64(local)) {
				in.renderBand(res, chrome, lay, ln.Group, pad, rowHeight, leadW, labelW)
			}
		case local%2 == 1:
			for range et.Rows(uint64(local)) {
				for range c.IdScope(ids.PrepareStr("stripe")) {
					for range c.Frame(ids.PrepareSeq(uint64(ln.Row))).
						Fill(stripeFill).
						OuterMargin(0).
						InnerMargin(0).
						KeepIter() {
						c.UiSetMinWidthAvailable()
						c.UiSetMinHeight(rowHeight)
					}
				}
			}
		}
	}
}

// renderBand draws one group line's band and reads its clicks. The band is
// keyed by the group's path, so a click made on the previous frame lands
// on the same group after a sort or a new record.
func (in *Input) renderBand(res *Result, chrome *Chrome, lay *Layout, group int32, pad, rowHeight, leadW, labelW float32) {
	ids := in.Ids
	g := &lay.Groups[group]
	lvl := in.Groups.Levels()[g.Level]
	keyRow, isRow := int64(-1), false
	if lvl.KeyIsRow {
		keyRow, isRow = g.KeyRow()
	}
	fill := groupBandFill
	if isRow && in.HasSelectedKeyRow && keyRow == in.SelectedKeyRow {
		fill = SelectionFill
	}
	for range c.IdScope(ids.PrepareStr(g.Path)) {
		// The row block is live on every frame, so its ids are not noted
		// into the cells replay gate: noting them would miss the cache
		// whenever the pointer rests on a band.
		fr := c.Frame(ids.PrepareStr("band")).
			Fill(fill).
			OuterMargin(0).
			InnerMargin(0).
			SenseClick().
			HoverCursorPointer()
		chevron := false
		for range fr.KeepIter() {
			c.UiSetMinWidthAvailable()
			c.UiSetMinHeight(rowHeight)
			if leadW > 0 {
				c.AddSpace(leadW)
			}
			for range c.Frame(ids.PrepareStr("label")).
				OuterMargin(0).
				InnerMarginSides(pad, pad, 0, 0).
				KeepIter() {
				c.UiSetMaxWidth(max(labelW-2*pad, 0))
				c.UiClipToMaxRect()
				if d := float32(g.Depth) * GroupIndent; d > 0 {
					c.AddSpace(d)
				}
				glyph := glyphCollapsed
				if g.Open {
					glyph = glyphExpanded
				}
				// Frameless, so the band shows through; still a Button, so
				// it wins the pointer over the band's sense behind it.
				wire := ids.PrepareStr("disc").Derive()
				chevron = c.Button(c.MakeAbsoluteIdHighEntropy(wire), c.Atoms().Text(glyph).Keep()).
					Frame(false).
					Small().
					SendResp().
					HasPrimaryClicked()
				label := g.Key.Label
				if label == "" {
					label = g.Key.Value
				}
				rt := c.Atoms().BeginRichText(label).Strong()
				switch {
				case g.Key.Null:
					rt = c.Atoms().BeginRichText("(null)").Weak()
				case label == "":
					rt = c.Atoms().BeginRichText("(empty)").Weak()
				}
				// Selectable(false): a selectable label senses drags and
				// would eat the band's click on its own rect.
				c.LabelAtoms(rt.End().Keep()).Selectable(false).Truncate().Send()
				for rt := range c.RichTextLabel("(" + strconv.FormatInt(g.Leaves, 10) + ")") {
					rt.Weak().Small()
				}
			}
		}
		switch {
		case chevron:
			res.GroupToggled = group
		case c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id()).HasPrimaryClicked():
			if isRow {
				res.GroupClicked = keyRow
			} else {
				res.GroupToggled = group
			}
		}
	}
}

// renderGroupCells emits a group line's cells: the aggregates under their
// columns, and the key row in the gutter of a KeyIsRow level.
func (in *Input) renderGroupCells(et c.EndETableFluid, chrome *Chrome, lay *Layout, group int32, local int64, pad float32, refit bool) {
	ids := in.Ids
	lead := in.lead()
	g := &lay.Groups[group]
	cellLabel := func(pos uint32, cell Cell) {
		for range et.Cells(uint64(local), pos) {
			for range c.IdScope(ids.PrepareStr(g.Path)) {
				chrome.Inset(uint64(pos)^CellFrameSalt, pad, cell.Fill, cell.Filled, func() {
					rt := c.Atoms().BeginRichText(cell.Text).Monospace()
					if cell.Weak {
						rt = rt.Weak()
					}
					if cell.Toned {
						rt = c.Atoms().BeginRichTextColored(cell.Tone, color.Transparent, cell.Text).Monospace()
					}
					emit := func() {
						lb := c.LabelAtoms(rt.End().Keep()).Selectable(false)
						if !cell.Untruncated {
							lb = lb.Truncate()
						}
						lb.Send()
					}
					if cell.LeftAlign {
						for range c.UiWithLayout().MainDirTopDown().CrossAlignMin().CrossJustify(true).KeepIter() {
							emit()
						}
					} else {
						for range c.VerticalCenteredJustified().KeepIter() {
							emit()
						}
					}
				})
			}
		}
	}
	if lead > 0 && in.Groups.Levels()[g.Level].KeyIsRow {
		if vis, _ := et.ColVisible(0); vis {
			if row, ok := g.KeyRow(); ok {
				cellLabel(0, Cell{Text: strconv.FormatInt(row+1, 10), Weak: true})
			}
		}
	}
	for i := range in.Columns {
		col := &in.Columns[i]
		pos := uint32(lead + i)
		if !col.aggregates() {
			continue
		}
		if vis, _ := et.ColVisible(pos); !vis {
			continue
		}
		var cell Cell
		if col.AggCell != nil {
			cell = col.AggCell(*g)
		} else if v, ok := lay.Aggregate(group, i); ok {
			cell = Cell{Text: FormatAgg(col.Agg, v)}
		}
		cell.Untruncated = refit
		if refit {
			cell.Text = FitRunes(cell.Text, FitMaxRunes)
		}
		cellLabel(pos, cell)
	}
}

// FormatAgg is the text a built-in aggregate draws: an integer plainly, a
// fraction to three places, nothing for a numeric aggregate over no value.
func FormatAgg(agg AggE, v AggValue) string {
	x, ok := v.Of(agg)
	if !ok {
		return ""
	}
	if x == math.Trunc(x) && math.Abs(x) < 1e15 {
		return strconv.FormatInt(int64(x), 10)
	}
	s := strconv.FormatFloat(x, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}
