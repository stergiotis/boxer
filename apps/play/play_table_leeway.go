package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// attrHdrIdOffset keeps the per-attribute grid's header frames off the
// per-DB-row grid's (recordtable.Chrome.HeaderCell salts both with
// recordtable.HeaderFrameSalt; header seqs are small, the column position).
// The two grids are alternatives — the granularity toggle shows one or the
// other — so this guards a future in which they are not.
const attrHdrIdOffset uint64 = 1 << 32

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
	showHashes        bool // ADR-0219: append the canonform / canonwire identity columns
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
			c.AddSpace(styletokens.GapPanels(styletokens.ActiveDensity()))
			c.Checkbox(ids.PrepareStr("table-show-support"),
				inst.tableOpts.showSupport, "Support columns").
				SendRespVal(&inst.tableOpts.showSupport)
			c.Checkbox(ids.PrepareStr("table-show-membership"),
				inst.tableOpts.showMembership, "Membership columns").
				SendRespVal(&inst.tableOpts.showMembership)
			c.AddSpace(styletokens.GapPanels(styletokens.ActiveDensity()))
			for range c.HoverText("Drops a tagged section once none of its attributes appear on the current page — which sections that is can change as you page through.").KeepIter() {
				c.Checkbox(ids.PrepareStr("table-hide-empty-sections"),
					inst.tableOpts.hideEmptySections, "Hide empty sections").
					SendRespVal(&inst.tableOpts.hideEmptySections)
			}
			for range c.HoverText("Appends two columns: every row's canonform digest (its content identity) and canonwire fingerprint (its wire identity), computed for the whole result in the background. The Detail pane shows the same values for the selected row, with the CBOR behind them.").KeepIter() {
				c.Checkbox(ids.PrepareStr("table-show-hashes"),
					inst.tableOpts.showHashes, "Canonical hashes").
					SendRespVal(&inst.tableOpts.showHashes)
			}
		}
	}
}

// renderTableBody dispatches the Table pane's grid to the granularity the
// options bar selected. The per-attribute view exists only for a leeway-shaped
// result; a non-leeway result always renders the per-DB-row grid.
func (inst *PlayApp) renderTableBody(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, selectedRow int64, emit SignalEmitterI) {
	perAttr := inst.tableOpts.granularity == tableRowPerAttr && inst.leewayColumnClasses(schema) != nil
	inst.tableDrawn = tableDrawnMark{schema: schema, rows: min(rec.NumRows(), numRows), perAttr: perAttr}
	if perAttr {
		inst.renderAttrTable(rec, schema, numRows, selectedRow, emit)
		return
	}
	inst.attrView.release() // the exploded record is rebuilt when the view is next shown
	inst.renderMasterTable(rec, schema, numRows, selectedRow, emit)
}
