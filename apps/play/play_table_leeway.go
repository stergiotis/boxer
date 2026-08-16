package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// cellFrameSalt lifts a grid cell's *inset-frame* id off the cell's own
// (button) id-sequence, so the frame and the button it wraps get distinct
// widget ids. Both grids mint it for every cell.
//
// Bit 40 is clear for every cell id either grid builds: the per-attribute
// grid's stay below 2^26 (attrCellIdBase/attrColStride, play_table_attr.go),
// and the per-DB-row grid's are cellIdBase + absRow*cellColStride + col
// (renderMasterTable), which reaches bit 40 only past ~16.7 M rows in one
// result — and even then the two cells that would collide are that many rows
// apart, so they are never on screen together.
const cellFrameSalt uint64 = 1 << 40

// hdrFrameSalt does the same for a header cell's inset frame, on a bit of its
// own so a header frame can never collide with a body-cell frame. It is below
// tableSortIDSalt (1<<62), which the header *buttons* use. Header seqs are
// small (the column position), so the per-attribute grid offsets its own by
// attrHdrIdOffset rather than sharing the per-DB-row grid's band.
const hdrFrameSalt uint64 = 1 << 60

// attrHdrIdOffset keeps the per-attribute grid's header frames off the
// per-DB-row grid's. The two grids are alternatives — the granularity toggle
// shows one or the other — so this guards a future in which they are not.
const attrHdrIdOffset uint64 = 1 << 32

// attrSelFill is the accent wash painted behind a selected per-attribute cell.
// It is styletokens.AccentDefault at ~35% alpha — the same accent, and roughly
// the same weight, as egui_table's own selected-row tint
// (ACCENT_DEFAULT.gamma_multiply(0.35), interpreter.rs) that the per-DB-row grid
// gets from et.SelectedRow. Built via color.Hex(token.AsHex()) with the alpha
// byte overridden, so it stays a token reference (designlint L2 flags raw
// color.RGBA, not Hex).
var attrSelFill = color.Hex((styletokens.AccentDefault.AsHex() & 0xffffff00) | 0x59)

// play_table_leeway.go carries the Table pane's leeway display modes (ADR-0097
// Update): a collapsible options bar above the grid whose four orthogonal
// controls reshape the same result through leeway's own structure —
//
//   - row granularity: one grid row per DB row (the columnar grid, selection
//     intact) vs one grid row per tagged-value attribute (the un-pivoted walk);
//   - reveal support columns (the machine-readable card/len structure);
//   - reveal membership columns (the set-membership encoding);
//   - hide tagged sections with no attribute anywhere on the current page.
//
// The classification that drives all four comes from the CardDriver
// (ColumnClasses) — the app's single per-schema leeway reconstruction point —
// so the bar and both renderers agree on what each physical column is. A
// non-leeway result (aggregation, join, arbitrary SQL) is not classifiable, so
// the bar is hidden and the grid falls back to the plain flat view.

// tableRowGranularityE selects how result rows map to grid rows in the Table
// pane. It is deliberately an enum rather than a bool: the per-attribute view
// may later split into "one row per logical attribute" vs "one row per
// attribute value" (the collection-item unpivot), a third case this type can
// grow without churning call sites.
type tableRowGranularityE uint8

const (
	// tableRowPerDBRow keeps one grid row per result row — the columnar grid,
	// with the existing row-selection contract intact.
	tableRowPerDBRow tableRowGranularityE = iota
	// tableRowPerAttr emits one grid row per tagged-value attribute, un-pivoting
	// the leeway structure; a row click still selects the source DB row.
	tableRowPerAttr
)

// Column-width identity is keyed on (name, type) (ADR-0151 §SD1), and the two
// granularities render the *same* column differently: the per-DB-row grid
// shows a List column packed as `[len=N]`, the per-attribute grid explodes it
// to its inner scalar. A width chosen for one is therefore wrong for the
// other, and without a discriminator the column tier — which matches on
// identity alone, across tables — would carry it from whichever grid the user
// dragged into the one they did not.
//
// §SD1 allows "an app-chosen format tag" where a column has no single
// canonical render type, which is exactly this case. These are that tag. They
// are *appended* to the Arrow type rather than replacing it, so a genuine type
// change still invalidates the override as the ADR intends; they must differ
// from each other, which is the whole point, and both are spelled out here
// rather than derived from the enum so that renaming a view cannot silently
// re-key every stored width.
const (
	tableViewTagRow  = ";view=row"
	tableViewTagAttr = ";view=attr"
)

// tableDisplayOpts is the Table pane options-bar state (see PlayApp.tableOpts).
// The zero value — per-DB-row, both reveals off — reproduces the plain value
// grid, so a result that has just become leeway-shaped renders the same columns
// it always did minus the machine-readable encoding detail.
type tableDisplayOpts struct {
	granularity       tableRowGranularityE
	showSupport       bool // reveal the card / len / cusum support columns
	showMembership    bool // reveal the ref / verbatim / parametrized membership columns
	hideEmptySections bool // suppress tagged sections with no attribute on the current page
	rawCells          bool // ADR-0186: bypass every gloss and show the plain rendering
}

// leewayColumnClasses returns the per-Arrow-column leeway classification for the
// current result schema, or nil when the result is not leeway-shaped. It ensures
// the shared CardDriver is built for schema first (a cheap pointer-compare cache
// once warmed), so callers get a classification consistent with the Detail and
// Schema panes without re-running discovery.
func (inst *PlayApp) leewayColumnClasses(schema *arrow.Schema) []streamreadaccess.ColumnClass {
	if inst.cards == nil || schema == nil {
		return nil
	}
	inst.cards.EnsureFor(schema)
	return inst.cards.ColumnClasses()
}

// visibleTableCols returns the Arrow column indices the per-DB-row grid should
// show, in schema order, honouring the support/membership reveal toggles and
// the hide-empty-sections toggle. For a non-leeway result every column is
// shown (unchanged from the plain grid). For a leeway result, value and
// backbone columns always show, unless hide-empty-sections is on and their
// tagged section has no attribute anywhere in [pageStart, pageEnd); support and
// membership columns additionally require their own reveal toggle. A column
// the classifier did not recognise (an implicit `_`-column, a projected-in
// expression) is treated as data and always shown. rec and the page bounds
// (absolute row indices) are only consulted when hide-empty-sections is on.
func (inst *PlayApp) visibleTableCols(rec arrow.RecordBatch, schema *arrow.Schema, pageStart, pageEnd int64) []int {
	ncols := schema.NumFields()
	classes := inst.leewayColumnClasses(schema)
	if classes == nil {
		vis := make([]int, ncols)
		for i := range vis {
			vis[i] = i
		}
		return vis
	}
	classOf := make(map[int]streamreadaccess.ColumnClass, len(classes))
	for _, cl := range classes {
		classOf[cl.ArrowIdx] = cl
	}
	opts := inst.tableOpts
	var emptySections map[string]bool
	if opts.hideEmptySections {
		emptySections = emptyTaggedSections(classes, rec, pageStart, pageEnd)
	}
	vis := make([]int, 0, ncols)
	for col := range ncols {
		cl, classified := classOf[col]
		if !classified {
			vis = append(vis, col) // unclassified → data, always shown
			continue
		}
		if !cl.Backbone() && emptySections[string(cl.SectionName)] {
			continue
		}
		switch cl.Class {
		case streamreadaccess.ColumnRoleClassValue:
			vis = append(vis, col)
		case streamreadaccess.ColumnRoleClassSupport:
			if opts.showSupport {
				vis = append(vis, col)
			}
		case streamreadaccess.ColumnRoleClassMembership:
			if opts.showMembership {
				vis = append(vis, col)
			}
		}
	}
	return vis
}

// emptyTaggedSections returns the tagged section names with no attribute
// instance anywhere in rows [lo, hi) of rec — sections whose columns would
// render entirely blank on the current page. Only tagged sections are
// considered: a plain/backbone column carries exactly one attribute per
// entity by construction (Driver.drivePlainSection always drives nAttrs=1),
// so it is never "empty" in this sense, and hiding it would just discard a
// backbone value that happens to be NULL on this page.
//
// leeway's physical mapping wraps every tagged column in List/LargeList
// regardless of scalar/array/set shape (streamreadaccess/EXPLANATION.md), so
// a row's outer list length is that entity's attribute count for the
// section — 0 when the entity lacks the tag — and every value column in a
// section shares that same outer length. So checking any one Value-class
// column per section suffices; the first one found in classes is used.
func emptyTaggedSections(classes []streamreadaccess.ColumnClass, rec arrow.RecordBatch, lo, hi int64) map[string]bool {
	rep := make(map[string]int, len(classes))
	for _, cl := range classes {
		if cl.Class != streamreadaccess.ColumnRoleClassValue || cl.Backbone() {
			continue
		}
		name := string(cl.SectionName)
		if _, ok := rep[name]; !ok {
			rep[name] = cl.ArrowIdx
		}
	}
	empty := make(map[string]bool, len(rep))
	for name, arrowIdx := range rep {
		if !sectionHasAttrInRange(rec.Column(arrowIdx), lo, hi) {
			empty[name] = true
		}
	}
	return empty
}

// sectionHasAttrInRange reports whether a tagged value column carries at
// least one attribute instance across rows [lo, hi) of arr — the outer
// List/LargeList length at each row is the entity's attribute count (see
// emptyTaggedSections). Any other Arrow shape is treated as always present:
// this codebase's leeway DDL never produces a non-list tagged value column,
// so hitting one here means the classification doesn't match the physical
// layout as expected — safer to show the column than to guess it away.
func sectionHasAttrInRange(arr arrow.Array, lo, hi int64) bool {
	switch a := arr.(type) {
	case *array.List:
		for row := lo; row < hi; row++ {
			beg, end := a.ValueOffsets(int(row))
			if end > beg {
				return true
			}
		}
	case *array.LargeList:
		for row := lo; row < hi; row++ {
			beg, end := a.ValueOffsets(int(row))
			if end > beg {
				return true
			}
		}
	default:
		return true
	}
	return false
}

// renderTableOptionsBar draws the collapsible leeway display-mode bar above the
// grid. The caller renders it only when the result is leeway-shaped (there is
// nothing to configure otherwise). Controls write their state back into
// inst.tableOpts with the usual one-frame binding delay; the grid re-lays out
// on the next frame.
func (inst *PlayApp) renderTableOptionsBar() {
	ids := inst.ids
	for range c.CollapsingHeader(ids.PrepareStr("table-leeway-opts"),
		c.WidgetText().Text("Leeway display").Keep()).DefaultOpen(true).KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			for rt := range c.RichTextLabel("Rows") {
				rt.Weak().Small()
			}
			// Row granularity as an exclusive segmented bar over the enum, via
			// the selector helper (the egui radio_value analogue: it owns the
			// compare/assign and the child-id scoping). Inline() keeps the two
			// options in this same HorizontalTop row rather than opening a
			// nested one, so they stay aligned with the "Rows" label and the
			// sibling checkboxes.
			selector.Segmented(ids, "table-gran", &inst.tableOpts.granularity).
				Option(tableRowPerDBRow, "per DB row").
				Option(tableRowPerAttr, "per attribute").
				Inline().
				SendResp()
			// A plain horizontal gap, NOT c.Separator(): a separator inside a
			// horizontal row is a *vertical* rule that egui sizes to the
			// available height, and this row sits in the dock's unbounded-height
			// body ScrollArea, so the rule balloons and shoves the grid off the
			// bottom of the pane. Panel magnitude on the spacing ladder: the
			// gap stands in for a rule between control groups, so it wants to
			// read as a break rather than as an item gap, and it scales with
			// density like everything else in the row.
			c.AddSpace(styletokens.GapPanels(styletokens.DensityFromEnv()))
			c.Checkbox(ids.PrepareStr("table-show-support"),
				inst.tableOpts.showSupport, "Support columns").
				SendRespVal(&inst.tableOpts.showSupport)
			c.Checkbox(ids.PrepareStr("table-show-membership"),
				inst.tableOpts.showMembership, "Membership columns").
				SendRespVal(&inst.tableOpts.showMembership)
			c.AddSpace(styletokens.GapPanels(styletokens.DensityFromEnv()))
			for range c.HoverText("Drops a tagged section once none of its attributes appear on the current page — which sections that is can change as you page through.").KeepIter() {
				c.Checkbox(ids.PrepareStr("table-hide-empty-sections"),
					inst.tableOpts.hideEmptySections, "Hide empty sections").
					SendRespVal(&inst.tableOpts.hideEmptySections)
			}
		}
	}
}

// cellInset gives one grid cell — header or body — the same horizontal margin
// on each side and cuts its content at the inner edge.
//
// egui_table adds no margins of its own ("Does not add any margins to cells",
// its docs say, "add them yourself") and clips each cell to the column rect.
// Both grids used to lead with a bare AddSpace, which pads the left only: the
// column reads off-centre, and anything wider than the column — a header
// label, a truncated value — is cut on the gridline itself, so the vertical
// border sits in the glyphs. It is most obvious at the narrowest width a
// column can be dragged to, where every header is wider than its column.
//
// A Frame carries the margin on both sides. Its background, when it has one,
// still spans the whole cell: egui paints the frame's *outer* rect, margins
// included, so a selected cell's accent band stays continuous across the row
// while its text is inset. UiClipToMaxRect then moves the cut from the
// gridline to the inner edge, which is what keeps the border off the glyphs;
// without it the Frame would pad content that fits and change nothing for
// content that does not. The margin also joins what a sizing pass measures,
// so an auto-fitted column now fits its content *and* its insets.
//
// fill is painted behind the cell when filled is set; an unfilled Frame is
// transparent and paints nothing.
func (inst *PlayApp) cellInset(id uint64, cellPadX float32, fill color.Color, filled bool, body func()) {
	fr := c.Frame(inst.ids.PrepareSeq(id)).
		InnerMarginSides(cellPadX, cellPadX, 0, 0)
	if filled {
		fr = fr.Fill(fill)
	}
	for range fr.KeepIter() {
		c.UiClipToMaxRect()
		body()
	}
}

// headerCell insets a header cell the same way body cells are inset, so a
// column title lines up with the values beneath it on both edges.
func (inst *PlayApp) headerCell(id uint64, cellPadX float32, body func()) {
	inst.cellInset(id^hdrFrameSalt, cellPadX, color.Transparent, false, body)
}

// colDragMinWidth is the narrowest a resizable column may get: the content
// floor plus the inset cellInset spends on *each* side. It is handed both to
// egui_table (EtColumn.RangeMinMax, the live drag floor) and to the width
// resolver (colwidth.Opts.MinPoints, the floor stored overrides are clamped
// to), because a drag floor below the stored floor would be corrected only on
// the next load and a column would appear to jump on restart.
func (inst *PlayApp) colDragMinWidth() float32 {
	return colMinContentPx + 2*styletokens.PaddingTight(inst.density)
}

// selectableCell renders one grid cell as a full-width, cross-justified
// selectable button so a primary click anywhere in the cell — not only on the
// painted glyphs — selects the row. Both Table grids need this: egui sizes a
// frameless button to its content, and egui_table senses no clicks of its own
// (our delegate implements no row_ui), so a bare per-cell button leaves the
// cell's left inset and every blank cell as a dead click target. That reads as
// "finicky" in the per-DB-row grid (short values sit in wide columns) and
// outright breaks the per-attribute grid, whose design blanks most cells (no
// value is ever repeated). The cross-justified wrapper stretches the button to
// the column width; a non-small egui Button already floors its height at
// interact_size.y, so the hit area covers the whole row-height cell. The button
// id and its click routing are unchanged by the justify wrapper: it adds no
// id-stack scope, so ids stay keyed exactly as before. Returns true on a
// primary click. cellPadX is the same inset the headers get, on both sides
// (cellInset), so cell text still aligns under its header.
//
// selBg turns on a per-cell accent background when the cell is selected. The
// per-DB-row grid leaves it off (false): its whole-row highlight comes from
// et.SelectedRow, which egui_table paints per row_nr. The per-attribute grid
// turns it on (true): there one source DB row explodes to several egui_table
// rows, which a single SelectedRow index cannot cover, so each selected cell
// paints its own accent band — together highlighting the whole entity. A
// frameless egui Button never paints a background even when Selected (see
// button.rs), which is why the band is a fill on the inset Frame rather than
// the button's own; the band still spans the column because egui paints the
// frame's outer rect, and the text does not shift versus an unselected cell
// because every cell wears the same Frame either way.
//
// leftAlign moves the text to the cell's left edge instead of centering it, for
// free-text (string) columns so their values line up under the left-aligned
// header. It only swaps the cross-axis alignment of the justifying wrapper
// (Center → Min); everything else — the full-width justify that grows the hit
// area, the button id, the height flooring, the selection band — is unchanged,
// since VerticalCenteredJustified is exactly this same top-down cross-justified
// layout with Center rather than Min cross-alignment.
//
// tone colours the text per ADR-0186's inline face (a Luhn ✓ in success, a
// ✗ in error); gloss.ToneNeutral leaves the cell's text style alone. link,
// when non-empty, makes the cell a hyperlink to that URL instead of a button
// (a gloss/url cell): clicking it opens the link through the host's opener
// and does not select the row — the row's other cells still do — since a
// hyperlink's click is consumed by the widget and not reported back.
//
// truncate is normally true. It is false on the one frame the grid asks
// egui_table to re-fit its columns: egui_table sizes a column in a sizing
// pass with each cell rect shrunk to the column minimum and takes the cell's
// allocated width, so a Truncate()d button reports its truncated width and
// only the header ever sets the column — a cell wider than its header
// (`4111 •••• •••• 1111 ✓`, a long URL) truncated for good. Untruncated on
// that frame, the button reports its intrinsic width and the column fits it;
// egui_table discards and re-runs the frame, so the overflow is never shown.
func (inst *PlayApp) selectableCell(id uint64, cellPadX float32, text string, weak bool, selected, selBg, leftAlign bool, tone gloss.ToneE, link string, truncate bool) (clicked bool) {
	emitButton := func() {
		if link != "" {
			c.HyperlinkTo(text, link).OpenInNewTab(true).Send()
			return
		}
		var rt c.RichTextScope
		if col, toned := toneColor(tone); toned {
			rt = c.Atoms().BeginRichTextColored(col, color.Transparent, text).Monospace()
		} else {
			rt = c.Atoms().BeginRichText(text).Monospace()
		}
		if weak {
			rt = rt.Weak()
		}
		btn := c.Button(inst.ids.PrepareSeq(id), rt.End().Keep()).
			Frame(false).
			Selected(selected)
		if truncate {
			btn = btn.Truncate()
		}
		clicked = btn.SendResp().HasPrimaryClicked()
	}
	inst.cellInset(id^cellFrameSalt, cellPadX, attrSelFill, selBg && selected, func() {
		if leftAlign {
			for range c.UiWithLayout().
				MainDirTopDown().
				CrossAlignMin().
				CrossJustify(true).
				KeepIter() {
				emitButton()
			}
		} else {
			for range c.VerticalCenteredJustified().KeepIter() {
				emitButton()
			}
		}
	})
	return
}

// renderTableBody dispatches the Table pane's grid to the granularity the
// options bar selected. The per-attribute view exists only for a leeway-shaped
// result; a non-leeway result always renders the per-DB-row grid.
func (inst *PlayApp) renderTableBody(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, selectedRow int64, emit SignalEmitterI) {
	if inst.tableOpts.granularity == tableRowPerAttr && inst.leewayColumnClasses(schema) != nil {
		inst.renderAttrTable(rec, schema, numRows, selectedRow, emit)
		return
	}
	inst.renderMasterTable(rec, schema, numRows, selectedRow, emit)
}
