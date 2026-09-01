package launcher

// The row list (ADR-0214 §SD5): icon, Display over a dimmed Summary, and
// badges that only appear when they say something.

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

var (
	selectionFill   = color.Hex(styletokens.AccentSubtle.AsHex())
	selectionStroke = color.Hex(styletokens.AccentDefault.AsHex())
	clearFill       = color.Transparent
)

// seqRowBase and seqCellBase keep the per-row Frame ids clear of the sequence
// space the pane's other PrepareSeq callers draw from, and clear of each
// other: a row draws two Frames, the click band and the padded cell it sits
// behind.
const (
	seqRowBase  = uint64(0x1a00)
	seqCellBase = uint64(0x2a00)
)

// The list column's width range. Not a fixed width: the pane is resizable, so
// the column has to be able to follow it in both directions.
const (
	rowMinWidth = float32(160)
	rowMaxWidth = float32(2000)
)

// etAutoSizeAlways is egui_table's AutoSizeMode::Always as the binding takes
// it — a discriminant, not a Go enum.
//
// The one column has to BE the pane, and only auto-size makes it so. A column
// is declared with a width, and a width is a guess about a pane the user can
// drag: guess high and every row is wider than the viewport, so the table
// scrolls sideways and the outline's right edge is clipped away — the defect
// this is here for. Always re-fits the column to the table's own parent width
// each frame, which is the same number egui_table lays the rows out in, so the
// row rect and the viewport cannot disagree.
const etAutoSizeAlways = uint8(1)

// The row band's chrome, in egui points.
//
// rowOutset is what the band leaves between its painted rect and the rect
// egui_table gave the row. Zero is the obvious value and it costs the band its
// right edge: the Frame paints its content rect grown by the stroke, and
// asking for the full available width puts that growth a fraction of a point
// outside what the table clips, so the outline drew three sides. The outset
// also keeps one selected row's outline off its neighbour's.
//
// rowStrokeW is the selection outline's width, named rather than inlined
// because the cell reserves it on every row, selected or not — an inset that
// appeared with the selection would shift the row's text as the cursor moved
// onto it.
const (
	rowOutset  = float32(2)
	rowStrokeW = float32(1)
)

// rowInset is the distance from the row's own rect to its text: past the
// band's outset, past the outline the band paints when selected, and then the
// ladder's tight padding. It is the whole of the row's chrome, so the row is
// exactly this much taller than the text it carries.
func (inst *Inst) rowInset() (v float32) {
	v = rowOutset + rowStrokeW + styletokens.PaddingTight(inst.density)
	return
}

// rowHeight is two text lines plus the chrome around them, in egui points. A
// row carries the name and the summary, and the summary is the whole reason
// the row is two lines tall (§SD2's diagnosis: a one-line row carries identity
// and nothing else).
//
// The base is a measured figure rather than a token: egui_table takes ONE row
// height for the whole table, and what has to fit is a body line, a small
// line, and the item spacing between them — text metrics that the IDS spacing
// ladder does not describe. Everything around it is rowInset, so the two lines
// get their natural height and the padding is what the row is taller by. A
// value too small does not clip, it overlaps the next row, which is how the
// first screenshot run failed.
//
// Section headings share the height and end up with air around them. That
// reads as a section break rather than as waste, so it is left alone.
func (inst *Inst) rowHeight() (h float32) {
	const twoTextLines = 44
	h = twoTextLines + 2*inst.rowInset()
	return
}

// renderRows draws the list: sectioned by topic with no query, one ranked
// flat list with one.
//
// Both paths go through the same virtualising table (§SD5). The sectioned
// path flattens its groups into one row slice with heading rows interleaved,
// rather than emitting a table per section: egui_table's visible-range query
// culls rows within one table, so N tables would cull N times and still walk
// every section header. One table also means the keyboard cursor is one index
// into one list, whichever view is showing.
func (inst *Inst) renderRows(ids *c.WidgetIdStack, visible []app.Manifest, rows []rowT) {
	if len(rows) == 0 {
		c.Label(inst.emptyResultHint()).Send()
		return
	}
	if q := inst.query(); q != "" {
		renderSearchNotes(launcherBattery(q), countAppRows(rows), len(visible))
	}
	inst.clampCursor(rows)
	// Selection IS the cursor. A launcher where the highlighted row and the
	// described row can differ has two places to look and no way to tell which
	// Enter would act on; the surveyed launchers all fold the two together, and
	// this is where the fold happens — a click moves the cursor (see
	// renderAppRow) and so does an arrow key, and the detail pane reads it.
	if inst.cursor >= 0 && inst.cursor < len(rows) && rows[inst.cursor].heading == "" {
		inst.selected = rows[inst.cursor].m.Id
	}
	openSet := inst.openAppSet()

	// One column, declared before the table: egui_table takes its columns from
	// the EtColumn ops pushed ahead of the EndETable, and a table with none
	// renders nothing at all — which is what the first screenshot run showed.
	//
	// A single column is the whole point of the shape: a launcher row is one
	// target carrying a name over a summary, not a grid of fields. The width
	// range spans from something usable at the narrowest sensible pane to well
	// past the widest, so the column follows the pane the user sized rather
	// than fighting it; the declared width is only where it starts, since
	// etAutoSizeAlways re-fits it to the pane every frame.
	c.EtColumn(listPanelDefaultWidth).RangeMinMax(rowMinWidth, rowMaxWidth).Resizable(false).Send()
	et := c.EndETable(ids.PrepareStr("launcher-rows"), uint64(len(rows)), inst.rowHeight(), 0, 0).
		FillPane(true).
		AutoSizeMode(etAutoSizeAlways)
	rowBegin, rowEnd := 0, len(rows)
	if rb, re, _, _, _, ok := et.VisibleRange(); ok {
		rowBegin = min(int(rb), len(rows))
		rowEnd = min(int(re), rowEnd)
	}
	for i := rowBegin; i < rowEnd; i++ {
		r := rows[i]
		if r.heading != "" {
			inst.renderHeadingRow(ids, et, i, r.heading)
			continue
		}
		_, isOpen := openSet[r.m.Id]
		inst.renderAppRow(ids, et, i, r.m, isOpen, i == inst.cursor)
	}
	// The terminal. egui_table's Go surface accumulates columns, then the
	// table, then the captured cell blocks, and Send ships the lot — so a
	// table without it renders nothing at all, which is exactly what the
	// first screenshot run showed.
	et.Send()
}

// rowT is one line of the list: either a section heading or an app. A single
// row type is what lets one virtualised table serve both views.
type rowT struct {
	heading string
	m       app.Manifest
}

// buildRows resolves the current query and filters into the row slice.
func (inst *Inst) buildRows(visible []app.Manifest) (rows []rowT) {
	if q := inst.query(); q != "" {
		hits := inst.hits(visible)
		rows = make([]rowT, 0, len(hits))
		for _, m := range hits {
			rows = append(rows, rowT{m: m})
		}
		return
	}
	groups := groupByTopic(visible, inst.topicFilter)
	rows = make([]rowT, 0, len(visible)+len(groups))
	for _, g := range groups {
		rows = append(rows, rowT{heading: topicLabel(g.Topic)})
		for _, m := range g.Manifests {
			rows = append(rows, rowT{m: m})
		}
	}
	return
}

// countAppRows counts the rows that are apps rather than headings — the
// number the selectivity readout means.
func countAppRows(rows []rowT) (n int) {
	for i := range rows {
		if rows[i].heading == "" {
			n++
		}
	}
	return
}

// clampCursor keeps the keyboard cursor on an app row inside the current
// list. The list changes shape under the cursor as the query changes, so this
// runs every frame rather than trusting the stored value; a cursor that
// landed on a heading advances off it, which is what makes ↓ from the last
// app of one section reach the first app of the next.
func (inst *Inst) clampCursor(rows []rowT) {
	if inst.cursor < 0 {
		inst.cursor = 0
	}
	if inst.cursor >= len(rows) {
		inst.cursor = len(rows) - 1
	}
	for inst.cursor < len(rows) && rows[inst.cursor].heading != "" {
		inst.cursor++
	}
	if inst.cursor >= len(rows) {
		// Every row from the old cursor on was a heading; fall back to the
		// first app row rather than pointing past the end.
		inst.cursor = firstAppRow(rows)
	}
}

// firstAppRow is the index of the first app row, or 0 when there is none.
func firstAppRow(rows []rowT) (idx int) {
	for i := range rows {
		if rows[i].heading == "" {
			idx = i
			return
		}
	}
	return
}

// renderHeadingRow draws a section heading as a row of the same table. It
// takes the same inset as an app row, so a heading and the names under it
// start on one left edge.
func (inst *Inst) renderHeadingRow(ids *c.WidgetIdStack, et c.EndETableFluid, rowIdx int, heading string) {
	et.BeginCells(uint64(rowIdx), 0)
	inst.paddedCell(ids, rowIdx, func() {
		for range c.Vertical().KeepIter() {
			c.LabelAtoms(c.Atoms().BeginRichText(heading).Strong().End().Keep()).
				Selectable(false).Send()
			c.Separator().Horizontal().Send()
		}
	})
	et.EndCells()
}

// paddedCell wraps a row's content in the inset its chrome needs.
//
// The padding lives here and not on the band because the band is a painted
// rect rather than a container: an inner margin on it adds to the min height
// set beside it, growing the band past the row and over its neighbour, which
// is how the second screenshot run failed. The cell is the container, so the
// cell is where the padding goes.
func (inst *Inst) paddedCell(ids *c.WidgetIdStack, rowIdx int, body func()) {
	for range c.Frame(ids.PrepareSeq(seqCellBase + uint64(rowIdx))).
		OuterMargin(0).
		InnerMargin(inst.rowInset()).
		KeepIter() {
		body()
	}
}

// renderAppRow draws one app: the full-width click layer, then the two text
// lines and the badges.
//
// The whole row senses the click, not a button inside it — a launcher row is
// one target, and a person aiming at the summary line means the same thing as
// one aiming at the name. A single click selects (filling the detail pane); a
// double click opens. Selecting on single click is what makes the detail pane
// reachable at all without committing to an open, which is §SD6's whole
// premise.
//
// That the row is one target has to be true of the POINTER too, and every
// label says so with Selectable(false). egui makes labels selectable by
// default and a selectable label senses click-and-drag: it sits over the band
// and takes both the click and the cursor, so aiming at the name gave an
// I-beam and a text selection where aiming two points higher gave a hand and
// an open. Non-selectable labels take neither, and the band's
// HoverCursorPointer reaches the whole row.
func (inst *Inst) renderAppRow(ids *c.WidgetIdStack, et c.EndETableFluid, rowIdx int, m app.Manifest, isOpen bool, isCursor bool) {
	selected := m.Id == inst.selected || isCursor
	fill, stroke, strokeW := clearFill, clearFill, float32(0)
	if selected {
		fill, stroke, strokeW = selectionFill, selectionStroke, rowStrokeW
	}
	var flags c.ResponseFlagsE
	for range et.Rows(uint64(rowIdx)) {
		fr := c.Frame(ids.PrepareSeq(seqRowBase+uint64(rowIdx))).
			Fill(fill).
			Stroke(strokeW, stroke).
			OuterMargin(rowOutset).
			// Outer margin, never an inner one. This Frame is a painted band
			// and a click target, not a content container — the row's text is
			// emitted separately into the table's cell, behind which the band
			// sits. An INNER margin adds to the min height set below, so the
			// band would grow past the row and paint over its neighbour, which
			// is how the second screenshot run failed; the outer margin comes
			// off the min height instead. Padding belongs on the cell.
			InnerMargin(0).
			SenseClick().
			HoverCursorPointer()
		for range fr.KeepIter() {
			c.UiSetMinWidthAvailable()
			// Both strokes and both outsets: a Frame paints its content rect
			// grown by the stroke width on every side and allocates that grown
			// by the outer margin, so this is what makes the painted rect the
			// row minus its outset (the fsbrowser/tree precedent, plus the
			// outset).
			c.UiSetMinHeight(inst.rowHeight() - 2*(rowOutset+strokeW))
		}
		flags = c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id())
	}
	et.BeginCells(uint64(rowIdx), 0)
	inst.paddedCell(ids, rowIdx, func() {
		for range c.Vertical().KeepIter() {
			for range c.Horizontal().KeepIter() {
				if m.Icon != "" {
					c.Label(m.Icon).Selectable(false).Send()
				}
				c.LabelAtoms(c.Atoms().BeginRichText(rowLabel(m)).Strong().End().Keep()).
					Selectable(false).Send()
				inst.renderRowBadges(ids, m, isOpen, rowIdx)
			}
			if m.Summary != "" {
				// Truncate, not wrap: the row height is fixed, so a summary
				// allowed to wrap is a summary clipped mid-line.
				c.LabelAtoms(c.Atoms().BeginRichText(m.Summary).Small().Weak().End().Keep()).
					Selectable(false).Truncate().Send()
			}
		}
	})
	et.EndCells()

	// Only the cursor is written: selection follows it at the top of the next
	// frame's renderRows, so there is one assignment rather than two that
	// could disagree.
	if flags.HasDoubleClicked() {
		inst.cursor = rowIdx
		inst.open(m.Id)
		return
	}
	if flags.HasPrimaryClicked() {
		inst.cursor = rowIdx
	}
}

// renderRowBadges draws the accessories, and only the ones that say
// something (§SD5). A plain app gets none: a badge reading "app" on 16 of 72
// rows is chrome, and the provenance that matters is the exception.
//
// Ids are keyed by ROW INDEX, not by app id. Under ADR-0158 §SD3 a manifest
// appears under every topic it declares, so the browse view renders the same
// app id on two rows — and keying by it derives one id for both, which egui
// reports as a duplicate and resolves by sharing state between them. This is
// the hazard the pre-0214 launcher solved by keying its buttons with the
// section name; the row index is the same fix in a list that has a stable
// index and no section string.
func (inst *Inst) renderRowBadges(ids *c.WidgetIdStack, m app.Manifest, isOpen bool, rowIdx int) {
	key := func(suffix string) string {
		return "row-badge-" + suffix + "-" + strconv.Itoa(rowIdx)
	}
	if isOpen {
		badge.New(ids.PrepareStr(key("open")), "open").
			Tone(badge.ToneSuccess).
			Size(badge.SizeSm).
			Tooltip("a window for this app is already open — clicking raises it").
			Send()
	}
	switch m.Kind {
	case app.KindApplet:
		badge.New(ids.PrepareStr(key("kind")), "applet").
			Tone(badge.ToneInfo).
			Size(badge.SizeSm).
			Tooltip("minted from a committed SQL-applet document").
			Send()
	case app.KindDemo:
		badge.New(ids.PrepareStr(key("kind")), "demo").
			Tone(badge.ToneNeutral).
			Size(badge.SizeSm).
			Tooltip("demonstrates a widget or a runtime facility rather than doing a user's work").
			Send()
	}
}

// rowLabel is the name on the row: Display, falling back to the Id when a
// manifest somehow has none (Validate refuses that, so this is a belt).
func rowLabel(m app.Manifest) (s string) {
	s = m.Display
	if s == "" {
		s = string(m.Id)
	}
	return
}
