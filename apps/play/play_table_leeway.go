package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_table_leeway.go carries the Table pane's leeway display modes (ADR-0097
// Update): a collapsible options bar above the grid whose three orthogonal
// controls reshape the same result through leeway's own structure —
//
//   - row granularity: one grid row per DB row (the columnar grid, selection
//     intact) vs one grid row per tagged-value attribute (the un-pivoted walk);
//   - reveal support columns (the machine-readable card/len structure);
//   - reveal membership columns (the set-membership encoding).
//
// The classification that drives all three comes from the CardDriver
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

// tableDisplayOpts is the Table pane options-bar state (see PlayApp.tableOpts).
// The zero value — per-DB-row, both reveals off — reproduces the plain value
// grid, so a result that has just become leeway-shaped renders the same columns
// it always did minus the machine-readable encoding detail.
type tableDisplayOpts struct {
	granularity    tableRowGranularityE
	showSupport    bool // reveal the card / len / cusum support columns
	showMembership bool // reveal the ref / verbatim / parametrized membership columns
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
// show, in schema order, honouring the support/membership reveal toggles. For a
// non-leeway result every column is shown (unchanged from the plain grid). For a
// leeway result, value and backbone columns always show; support and membership
// columns show only when their toggle is on; and a column the classifier did not
// recognise (an implicit `_`-column, a projected-in expression) is treated as
// data and shown.
func (inst *PlayApp) visibleTableCols(schema *arrow.Schema) []int {
	ncols := schema.NumFields()
	classes := inst.leewayColumnClasses(schema)
	if classes == nil {
		vis := make([]int, ncols)
		for i := range vis {
			vis[i] = i
		}
		return vis
	}
	classOf := make(map[int]streamreadaccess.ColumnRoleClassE, len(classes))
	for _, cl := range classes {
		classOf[cl.ArrowIdx] = cl.Class
	}
	opts := inst.tableOpts
	vis := make([]int, 0, ncols)
	for col := range ncols {
		cls, classified := classOf[col]
		if !classified {
			vis = append(vis, col) // unclassified → data, always shown
			continue
		}
		switch cls {
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
			// Segmented selector for the row granularity: selectable buttons
			// rather than RadioButton, whose *bool databinding does not model a
			// mutually-exclusive enum cleanly (the ComboBox-option idiom).
			if c.Button(ids.PrepareStr("table-gran-dbrow"),
				c.Atoms().Text("per DB row").Keep()).
				Selected(inst.tableOpts.granularity == tableRowPerDBRow).
				SendResp().HasPrimaryClicked() {
				inst.tableOpts.granularity = tableRowPerDBRow
			}
			if c.Button(ids.PrepareStr("table-gran-attr"),
				c.Atoms().Text("per attribute").Keep()).
				Selected(inst.tableOpts.granularity == tableRowPerAttr).
				SendResp().HasPrimaryClicked() {
				inst.tableOpts.granularity = tableRowPerAttr
			}
			// A plain horizontal gap, NOT c.Separator(): a separator inside a
			// horizontal row is a *vertical* rule that egui sizes to the
			// available height, and this row sits in the dock's unbounded-height
			// body ScrollArea, so the rule balloons and shoves the grid off the
			// bottom of the pane.
			c.AddSpace(24)
			c.Checkbox(ids.PrepareStr("table-show-support"),
				inst.tableOpts.showSupport, "Support columns").
				SendRespVal(&inst.tableOpts.showSupport)
			c.Checkbox(ids.PrepareStr("table-show-membership"),
				inst.tableOpts.showMembership, "Membership columns").
				SendRespVal(&inst.tableOpts.showMembership)
		}
	}
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
