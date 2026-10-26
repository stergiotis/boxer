package play

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// The Table pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05). The read gives the sort, the page, the leeway display options,
// the raw-cells switch, each column as the header captions it (its leeway
// handle, its gloss label and media type, or the reason the gloss was
// refused), and on request the rows of the page drawn, in the order the sort
// draws them and with each cell as its gloss renders it. The command sets
// the same things; a header click, which cycles the sort, and the pager go
// through it.

const (
	opGetTable          = "get_table"
	opSetTableOptions   = "set_table_options"
	tablePaneId         = "table"
	opsResTable         = tablePaneId
	tableReadMaxColumns = 200
	// tablePageCacheRows bounds the page rows the snapshot copies; a read
	// lists at most opsSampleMaxRows of them at a time.
	tablePageCacheRows = 100
	tableCellMaxBytes  = 300
	tableGranDBRow     = "per_db_row"
	tableGranAttr      = "per_attribute"
)

// tablePageSizes are the page sizes the Table's pager offers.
var tablePageSizes = []int64{50, 100, 500, 1000, 10000}

// TableColumnReading is one column of the result as the Table captions it.
type TableColumnReading struct {
	Name      string `desc:"the column's physical name; sample_rows and set_table_options take it"`
	Handle    string `json:",omitzero" desc:"the leeway handle the header shows in its place"`
	Label     string `json:",omitzero" desc:"the gloss label the header shows in its place"`
	Type      string `desc:"the Arrow type, as the header abbreviates it"`
	Gloss     string `json:",omitzero" desc:"the gloss media type the cells render through"`
	GlossNote string `json:",omitzero" desc:"why the gloss is not applied: refused, or not accepted for this column's values; the cells then show plain text"`
	Class     string `json:",omitzero" desc:"for a leeway result: value, support or membership"`
	Backbone  bool   `json:",omitzero" desc:"for a leeway result: a backbone (plain) column rather than a tagged section's"`
	Section   string `json:",omitzero" desc:"for a leeway result: the tagged section the column belongs to"`
	Shown     bool   `desc:"drawn in the grid; the display options hide support and membership columns, and empty sections when asked"`
}

// TableSortReading is the sort the Table draws under.
type TableSortReading struct {
	Column     string `desc:"the sorted column's physical name"`
	Handle     string `json:",omitzero" desc:"its leeway handle or gloss label, as the header shows it"`
	Descending bool   `json:",omitzero" desc:"largest first; nulls sort last ascending and first descending"`
}

// TableGroupLevelReading is one level of the grouping as set.
type TableGroupLevelReading struct {
	Column     string `desc:"the grouped column's physical name"`
	Handle     string `json:",omitzero" desc:"its leeway handle or gloss label, as the header shows it"`
	Descending bool   `json:",omitzero" desc:"the level's groups in reverse order"`
	NullsFirst bool   `json:",omitzero" desc:"the null group first rather than last"`
}

// TableRowReading is one row of the page as drawn.
type TableRowReading struct {
	Row       int64    `desc:"the record row it draws, 0 for the first, -1 on a group line; set_signal('selection', row) selects it, sample_rows reads it raw"`
	Group     string   `json:",omitzero" desc:"on a group line: the group's path, its keys from the first level down joined by ' / ', a null key as (null); expand and collapse take it"`
	Depth     int32    `json:",omitzero" desc:"on a group line: 0 for a first-level group, one more per level"`
	Leaves    int64    `json:",omitzero" desc:"on a group line: the record rows under it, drawn or folded"`
	Folded    bool     `json:",omitzero" desc:"on a group line: its rows are folded away"`
	Cells     []string `desc:"the shown columns' cells in the order columns lists them, as the grid renders them (through their gloss unless raw cells is on), each cut at 300 bytes"`
	Tones     []string `json:",omitzero" desc:"each cell's tone where it is not neutral (warning, error, ...); empty entries are neutral"`
	Nulls     []int32  `json:",omitzero" desc:"indexes into cells of the NULL cells, which are empty text"`
	Canonform string   `json:",omitzero" desc:"the row's canonform digest, when canonical hashes are on and computed"`
	Canonwire string   `json:",omitzero" desc:"the row's canonwire fingerprint, when canonical hashes are on and computed"`
}

// TableReading is get_table's result.
type TableReading struct {
	Drawn             PaneDraw                 `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Rows              int64                    `desc:"rows in the result the pane draws"`
	Page              int64                    `desc:"the page drawn, 1 for the first"`
	Pages             int64                    `desc:"pages in all"`
	PageSize          int64                    `desc:"rows per page"`
	PageSizes         []int64                  `desc:"the page sizes set_table_options takes"`
	First             int64                    `desc:"the first display position on the page, 0 for the first; under a sort a position is not a record row"`
	Last              int64                    `desc:"one past the last display position on the page"`
	Sort              *TableSortReading        `json:",omitzero" desc:"the sort; absent when the rows draw in the result's own order"`
	GroupBy           []TableGroupLevelReading `json:",omitzero" desc:"the grouping, first level first; absent when the rows draw flat"`
	Lines             int64                    `json:",omitzero" desc:"grouped: the lines drawn — a line per group, and per record row under an open group — which the page windows instead of rows"`
	Leeway            bool                     `json:",omitzero" desc:"the result is leeway-shaped, so the leeway display options apply"`
	Granularity       string                   `json:",omitzero" desc:"leeway only: per_db_row, or per_attribute (each tagged attribute on a row of its own)"`
	SupportColumns    bool                     `json:",omitzero" desc:"leeway only: the support (cardinality, length) columns are shown"`
	MembershipColumns bool                     `json:",omitzero" desc:"leeway only: the membership columns are shown"`
	HideEmptySections bool                     `json:",omitzero" desc:"leeway only: tagged sections with no attribute on the page are hidden"`
	CanonicalHashes   bool                     `json:",omitzero" desc:"leeway only: each row's canonform and canonwire identities are appended"`
	Hashes            string                   `json:",omitzero" desc:"the canonical hashes' state while they are on: computing, done, or failed with why"`
	RawCells          bool                     `json:",omitzero" desc:"every gloss is bypassed, in the Table, Detail, Cards and Chat panes alike"`
	Columns           []TableColumnReading     `desc:"the result's columns in schema order, at most 200"`
	ColumnsMore       int32                    `json:",omitzero" desc:"columns past the 200 listed"`
	PageRows          []TableRowReading        `json:",omitzero" desc:"with rows: the page's rows in the order drawn, from offset"`
	More              int32                    `json:",omitzero" desc:"page rows past the ones listed; read on with offset"`
	RowsNote          string                   `json:",omitzero" desc:"why no page rows are listed"`
}

// GetTableArgs is get_table's argument.
type GetTableArgs struct {
	Rows   bool  `json:",omitzero" desc:"list the page's rows as drawn: sorted, through the glosses"`
	Offset int32 `json:",omitzero" desc:"the first page row to list, 0 for the first"`
	Limit  int32 `json:",omitzero" desc:"page rows to list, 20 by default and at most 50"`
}

// TableGroupLevelArgs is one level of a grouping to set.
type TableGroupLevelArgs struct {
	Column     string `desc:"the column to group by: its physical name, handle or gloss label"`
	Descending bool   `json:",omitzero" desc:"the level's groups in reverse order"`
	NullsFirst bool   `json:",omitzero" desc:"the null group first rather than last"`
}

// SetTableOptionsArgs is set_table_options' argument.
type SetTableOptionsArgs struct {
	SortBy            *string               `json:",omitzero" desc:"the column to sort by: its physical name, handle or gloss label; empty sorts nothing, giving back the result's own order. The sort is drawn over the rows held; nothing is rerun"`
	Descending        *bool                 `json:",omitzero" desc:"sort largest first; without sort_by it turns the current sort around"`
	GroupBy           []TableGroupLevelArgs `json:",omitzero" desc:"group the rows by these columns, first level first, each level nested in the one before; an empty list draws the rows flat. Groups draw over the rows held with a band per group — its key and row count — and the sort orders the rows within their group; a grouped column's header click reverses its level instead"`
	Expand            *string               `json:",omitzero" desc:"grouped: unfold the group with this path as get_table prints it, or 'all'"`
	Collapse          *string               `json:",omitzero" desc:"grouped: fold the group with this path as get_table prints it, or 'all'"`
	Page              *int64                `json:",omitzero" desc:"the page to turn to, 1 for the first"`
	PageSize          *int64                `json:",omitzero" desc:"rows per page: 50, 100, 500, 1000 or 10000"`
	Row               *int64                `json:",omitzero" desc:"turn to the page that draws this record row, under the sort; instead of page"`
	RawCells          *bool                 `json:",omitzero" desc:"bypass every gloss and show plain values; Detail, Cards and Chat follow the same switch"`
	Granularity       *string               `json:",omitzero" desc:"leeway only: per_db_row or per_attribute"`
	SupportColumns    *bool                 `json:",omitzero" desc:"leeway only: show the support columns"`
	MembershipColumns *bool                 `json:",omitzero" desc:"leeway only: show the membership columns"`
	HideEmptySections *bool                 `json:",omitzero" desc:"leeway only: hide tagged sections with no attribute on the page"`
	CanonicalHashes   *bool                 `json:",omitzero" desc:"leeway only: append each row's canonform and canonwire identities, computed over the whole result in the background"`
}

// tableDrawnMark is what the Table's last draw drew from: the schema and
// row count, the columns the grid showed, and whether it was the
// per-attribute grid. Render-goroutine state; visCols is a fresh slice per
// draw, so a snapshot shares it.
type tableDrawnMark struct {
	schema  *arrow.Schema
	rows    int64
	visCols []int
	perAttr bool
	// lines is what a grouped draw drew: the group lines and the rows under
	// open groups, which the page windows; 0 when flat.
	lines int64
}

// tableColumnsView is the column captions of one schema, built once per
// schema and gloss resolution and shared by every later snapshot.
type tableColumnsView struct {
	schema *arrow.Schema
	glossK *glossColumn
	cols   []TableColumnReading
	leeway bool
}

// tablePageKey names what a page copy was taken of.
type tablePageKey struct {
	result     ResultID
	schema     *arrow.Schema
	start, end int64
	sortActive bool
	sortCol    int
	sortDesc   bool
	raw        bool
	glossK     *glossColumn
	vis        string
	groupGen   uint64
}

// tablePageView is the page as drawn: rows in display order with their
// faces, copied once per page key.
type tablePageView struct {
	rows []TableRowReading
	note string
}

// tableOpsView is what get_table reads.
type tableOpsView struct {
	drawn              tableDrawnMark
	columns            *tableColumnsView
	page               *tablePageView
	opts               tableDisplayOpts
	sortActive         bool
	sortCol            int
	sortDesc           bool
	pageIdx, pageSize  int64
	pages              int64
	pageStart, pageEnd int64
	hashes             *identityJob
	hashesFor          ResultID
	groupBy            []TableGroupLevelReading
}

// tableColumnsOf builds the captions of the drawn schema.
func (inst *PlayApp) tableColumnsOf(schema *arrow.Schema, glossCols []glossColumn) (v *tableColumnsView) {
	v = &tableColumnsView{schema: schema}
	if len(glossCols) > 0 {
		v.glossK = &glossCols[0]
	}
	inst.ensureColLabels(schema)
	classes := inst.leewayColumnClasses(schema)
	v.leeway = classes != nil
	classOf := make(map[int]streamreadaccess.ColumnClass, len(classes))
	for _, cl := range classes {
		classOf[cl.ArrowIdx] = cl
	}
	for i, f := range schema.Fields() {
		col := TableColumnReading{Name: f.Name, Handle: inst.colLabels[f.Name], Type: shortArrowType(f.Type)}
		if i < len(glossCols) {
			gc := &glossCols[i]
			col.Label, col.Gloss = gc.label, gc.mediaType
			switch {
			case gc.mediaType == "":
			case gc.reason != "":
				col.GlossNote = "refused: " + gc.reason
			case gc.inst != nil && !gc.rowOK:
				col.GlossNote = "not applied: " + gc.rowReason
			}
		}
		if cl, ok := classOf[i]; ok {
			col.Class, col.Backbone, col.Section = cl.Class.String(), cl.Backbone(), string(cl.SectionName)
		}
		v.cols = append(v.cols, col)
	}
	return
}

// tableView copies what get_table reads, on the render goroutine. The
// captions are rebuilt per schema; the page's faces per page, sort and
// gloss resolution, from the result the pane was last fed.
func (inst *PlayApp) tableView(mark paneDrawnMark) (v tableOpsView) {
	d := inst.tableDrawn
	v = tableOpsView{drawn: d, opts: inst.tableOpts, sortActive: inst.tableSort.Active, sortCol: inst.tableSort.Col,
		sortDesc: inst.tableSort.Desc, pageIdx: inst.pager.CurrentPage(), pageSize: inst.pager.PageSize(),
		pages: inst.pager.NumPages(), hashes: inst.identityJob}
	v.pageStart, v.pageEnd = inst.pager.Range()
	if inst.identityJob != nil {
		v.hashesFor = inst.identityJob.result
	}
	if d.schema == nil {
		return
	}
	glossCols := inst.glossColumns(d.schema)
	var glossK *glossColumn
	if len(glossCols) > 0 {
		glossK = &glossCols[0]
	}
	cache := &inst.paneViews
	if cache.tableColumns == nil || cache.tableColumns.schema != d.schema || cache.tableColumns.glossK != glossK {
		cache.tableColumns = inst.tableColumnsOf(d.schema, glossCols)
	}
	v.columns = cache.tableColumns
	var vis strings.Builder
	for _, c := range d.visCols {
		vis.WriteString(strconv.Itoa(c))
		vis.WriteByte(',')
	}
	key := tablePageKey{result: mark.result, schema: d.schema, start: v.pageStart, end: v.pageEnd,
		sortActive: v.sortActive, sortCol: v.sortCol, sortDesc: v.sortDesc, raw: v.opts.rawCells, glossK: glossK, vis: vis.String()}
	if d.lines > 0 {
		key.groupGen = inst.tableGroups.Gen()
		if d.perAttr {
			key.groupGen = inst.attrGroups.Gen()
		}
	}
	v.groupBy = inst.tableGroupReadings(d.schema)
	if cache.tablePage == nil || cache.tablePageKey != key {
		cache.tablePage, cache.tablePageKey = inst.tablePageOf(key, d, glossCols), key
	}
	v.page = cache.tablePage
	return
}

// tablePageOf copies the page the Table drew: each row's cells as the grid
// renders them. The record is the one the pane is fed now; a result that has
// replaced the one drawn is not copied until the pane draws it.
func (inst *PlayApp) tablePageOf(key tablePageKey, d tableDrawnMark, glossCols []glossColumn) (v *tablePageView) {
	v = &tablePageView{}
	if d.perAttr {
		return inst.attrPageOf(key, d)
	}
	res := snapshotResults(inst)
	lr, err := res.read(tablePaneId, "")
	if err != nil {
		v.note = err.Error()
		return
	}
	defer lr.release()
	rec := lr.rec
	if rec == nil || lr.id != key.result || rec.Schema() != d.schema {
		v.note = "the pane has not drawn the result it holds now; read again after its next frame"
		return
	}
	n := rec.NumRows()
	order := inst.tableSort.OrderFor(rec, n)
	// A grouped draw's page is a window over its lines: a group line is
	// copied as its path and counts, a leaf line as the row it draws.
	var lay *recordtable.Layout
	if d.lines > 0 {
		lay = inst.tableGroups.Layout()
		n = lay.Len()
	}
	end := min(key.end, n, key.start+tablePageCacheRows)
	for pos := key.start; pos < end; pos++ {
		var row int64
		if lay != nil {
			ln := lay.Lines[pos]
			if ln.IsGroup() {
				g := &lay.Groups[ln.Group]
				v.rows = append(v.rows, TableRowReading{Row: -1, Group: tableGroupPath(lay, ln.Group),
					Depth: g.Depth, Leaves: g.Leaves, Folded: !g.Open})
				continue
			}
			row = ln.Row
		} else {
			row = inst.tableSort.RowAt(order, pos)
		}
		r := TableRowReading{Row: row, Cells: make([]string, 0, len(d.visCols))}
		anyTone := false
		tones := make([]string, 0, len(d.visCols))
		for k, col := range d.visCols {
			if col < 0 || col >= int(rec.NumCols()) {
				continue
			}
			arr := rec.Column(col)
			if arr.IsNull(int(row)) {
				r.Nulls = append(r.Nulls, int32(k))
				r.Cells = append(r.Cells, "")
				tones = append(tones, "")
				continue
			}
			text, tone := inst.glossCell(inst.rowGloss(rec, glossCols, col, row), arr, row, false)
			r.Cells = append(r.Cells, strings.Clone(truncateBytes(text, tableCellMaxBytes)))
			if tone != gloss.ToneNeutral {
				anyTone = true
				tones = append(tones, tone.String())
			} else {
				tones = append(tones, "")
			}
		}
		if anyTone {
			r.Tones = tones
		}
		v.rows = append(v.rows, r)
	}
	return
}

// attrPageOf copies the per-attribute grid's page: its lines over the
// exploded record the grid drew — a group line as its path and counts, a
// leaf as the source row it came from and the shown columns' cells.
func (inst *PlayApp) attrPageOf(key tablePageKey, d tableDrawnMark) (v *tablePageView) {
	v = &tablePageView{}
	xrec := inst.attrView.rec
	if xrec == nil || d.lines == 0 || inst.attrView.start != key.start {
		v.note = "the per-attribute grid has not drawn this page; read again after its next frame"
		return
	}
	lay := inst.attrGroups.Layout()
	src := xrec.Column(attrXSrc).(*array.Int64)
	end := min(lay.Len(), tablePageCacheRows)
	for pos := int64(0); pos < end; pos++ {
		ln := lay.Lines[pos]
		if ln.IsGroup() {
			g := &lay.Groups[ln.Group]
			v.rows = append(v.rows, TableRowReading{Row: -1, Group: tableGroupPath(lay, ln.Group),
				Depth: g.Depth, Leaves: g.Leaves, Folded: !g.Open})
			continue
		}
		r := TableRowReading{Row: src.Value(int(ln.Row)), Cells: make([]string, 0, len(d.visCols))}
		for k := range d.visCols {
			col := xrec.Column(attrXCells + k).(*array.String)
			if col.IsNull(int(ln.Row)) {
				r.Nulls = append(r.Nulls, int32(k))
				r.Cells = append(r.Cells, "")
				continue
			}
			r.Cells = append(r.Cells, strings.Clone(truncateBytes(col.Value(int(ln.Row)), tableCellMaxBytes)))
		}
		v.rows = append(v.rows, r)
	}
	return
}

// tableGranularityName is the granularity as set_table_options spells it.
func tableGranularityName(g tableRowGranularityE) string {
	if g == tableRowPerAttr {
		return tableGranAttr
	}
	return tableGranDBRow
}

// tableReading is get_table.
func tableReading(sn *opsSnap, in GetTableArgs) (out TableReading, err error) {
	d, readable, err := paneDrawOf(sn, tablePaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.table
	out = TableReading{Drawn: d, Rows: v.drawn.rows, Page: v.pageIdx + 1, Pages: v.pages, PageSize: v.pageSize,
		PageSizes: tablePageSizes, First: v.pageStart, Last: v.pageEnd, RawCells: v.opts.rawCells}
	if !readable || v.columns == nil {
		return
	}
	cols := v.columns.cols
	if v.sortActive && v.sortCol >= 0 && v.sortCol < len(cols) {
		c := &cols[v.sortCol]
		s := &TableSortReading{Column: c.Name, Handle: c.Handle, Descending: v.sortDesc}
		if c.Label != "" {
			s.Handle = c.Label
		}
		out.Sort = s
	}
	out.GroupBy, out.Lines = v.groupBy, v.drawn.lines
	if v.columns.leeway {
		o := v.opts
		out.Leeway, out.Granularity = true, tableGranularityName(o.granularity)
		out.SupportColumns, out.MembershipColumns, out.HideEmptySections, out.CanonicalHashes =
			o.showSupport, o.showMembership, o.hideEmptySections, o.showHashes
	}
	hashes := out.CanonicalHashes && v.hashes != nil && v.hashesFor == ResultID(d.ResultId) && !v.drawn.perAttr
	if hashes {
		switch text, hover := v.hashes.cell(identityColCanonform, 0); text {
		case "…":
			out.Hashes = "computing"
		case "error":
			out.Hashes = "failed: " + hover
		default:
			out.Hashes = "done"
		}
	}
	shown := make(map[int]bool, len(v.drawn.visCols))
	for _, c := range v.drawn.visCols {
		shown[c] = true
	}
	for i := range cols {
		if len(out.Columns) >= tableReadMaxColumns {
			out.ColumnsMore = int32(len(cols) - i)
			break
		}
		c := cols[i]
		c.Shown = shown[i]
		out.Columns = append(out.Columns, c)
	}
	if !in.Rows {
		return
	}
	p := v.page
	if p == nil || len(p.rows) == 0 {
		out.RowsNote = "the page drew no rows"
		if p != nil && p.note != "" {
			out.RowsNote = p.note
		}
		return
	}
	n := len(p.rows)
	off := int(in.Offset)
	if off < 0 || (off > 0 && off >= n) {
		return out, app.RefuseOperation("the page copy has " + strconv.Itoa(n) + " rows; offset " + strconv.Itoa(off) + " is past them")
	}
	limit := 20
	if in.Limit > 0 {
		limit = min(int(in.Limit), opsSampleMaxRows)
	}
	used := 0
	for i := off; i < n; i++ {
		r := p.rows[i]
		if hashes {
			r.Canonform, _ = v.hashes.cell(identityColCanonform, r.Row)
			r.Canonwire, _ = v.hashes.cell(identityColCanonwire, r.Row)
			if r.Canonform == "…" || r.Canonform == "error" {
				r.Canonform, r.Canonwire = "", ""
			}
		}
		b := 32 + len(r.Canonform) + len(r.Canonwire)
		for _, c := range r.Cells {
			b += len(c) + 3
		}
		used += b
		if i-off >= limit || (used > opsSampleMaxBytes && i > off) {
			out.More = int32(n - i)
			break
		}
		out.PageRows = append(out.PageRows, r)
	}
	if out.More == 0 && int64(n) < v.pageEnd-v.pageStart {
		out.More = int32(v.pageEnd - v.pageStart - int64(n))
		out.RowsNote = "a read lists the first " + strconv.Itoa(tablePageCacheRows) + " rows of a page; set a smaller page size to read the rest"
	}
	return
}

// tableColumnIndex resolves a column by physical name, leeway handle or
// gloss label; -1 when nothing matches.
func (inst *PlayApp) tableColumnIndex(schema *arrow.Schema, name string) int {
	name = strings.TrimSpace(name)
	if i := schema.FieldIndices(name); len(i) > 0 {
		return i[0]
	}
	inst.ensureColLabels(schema)
	glossCols := inst.glossColumns(schema)
	for i, f := range schema.Fields() {
		if inst.colLabels[f.Name] == name || (i < len(glossCols) && glossCols[i].label != "" && glossCols[i].label == name) {
			return i
		}
	}
	return -1
}

// setTableOptions is set_table_options: every option named is checked
// before any is applied. The page goes after the page size and the sort, so
// a row is found where the new order draws it.
func (inst *PlayApp) setTableOptions(in SetTableOptionsArgs) (err error) {
	if in.SortBy == nil && in.Descending == nil && in.GroupBy == nil && in.Expand == nil && in.Collapse == nil &&
		in.Page == nil && in.PageSize == nil && in.Row == nil &&
		in.RawCells == nil && in.Granularity == nil && in.SupportColumns == nil && in.MembershipColumns == nil &&
		in.HideEmptySections == nil && in.CanonicalHashes == nil {
		return noOptionsRefusal(tablePaneId, "sort_by", "descending", "group_by", "expand", "collapse", "page", "page_size", "row", "raw_cells",
			"granularity", "support_columns", "membership_columns", "hide_empty_sections", "canonical_hashes")
	}
	d := inst.tableDrawn
	needsResult := in.SortBy != nil || in.Descending != nil || in.GroupBy != nil || in.Expand != nil || in.Collapse != nil ||
		in.Page != nil || in.Row != nil ||
		in.Granularity != nil || in.SupportColumns != nil || in.MembershipColumns != nil ||
		in.HideEmptySections != nil || in.CanonicalHashes != nil
	if needsResult && d.schema == nil {
		return app.RefuseOperation("the Table has not drawn a result: run the buffer, or show_pane table once it has one")
	}
	leeway := in.Granularity != nil || in.SupportColumns != nil || in.MembershipColumns != nil ||
		in.HideEmptySections != nil || in.CanonicalHashes != nil
	if leeway && inst.leewayColumnClasses(d.schema) == nil {
		return app.RefuseOperation("the result is not leeway-shaped, so the leeway display options do not apply")
	}
	if in.Page != nil && in.Row != nil {
		return app.RefuseOperation("page and row both name the page to show; give one of them")
	}
	gran := inst.tableOpts.granularity
	if in.Granularity != nil {
		switch strings.TrimSpace(*in.Granularity) {
		case tableGranDBRow:
			gran = tableRowPerDBRow
		case tableGranAttr:
			gran = tableRowPerAttr
		default:
			return app.RefuseOperation("granularity is " + tableGranDBRow + " or " + tableGranAttr)
		}
	}
	active, col, desc := inst.tableSort.Active, inst.tableSort.Col, inst.tableSort.Desc
	if in.SortBy != nil {
		if name := strings.TrimSpace(*in.SortBy); name == "" {
			active, desc = false, false
		} else {
			i := inst.tableColumnIndex(d.schema, name)
			if i < 0 {
				return app.RefuseOperation("no column of the result the Table draws is named " + strconv.Quote(name) + "; get_table lists them")
			}
			active, col, desc = true, i, false
		}
	}
	if in.Descending != nil {
		if !active {
			return app.RefuseOperation("descending needs a sort: name sort_by")
		}
		desc = *in.Descending
	}
	if gran == tableRowPerAttr && in.SortBy != nil && active {
		return app.RefuseOperation("the per-attribute grid does not sort; set granularity per_db_row first")
	}
	// The grouping a fold applies to: the per-DB-row grid's spec, or the
	// per-attribute grid's fixed levels — the entity, then the path.
	groups, perAttr := &inst.tableGroups, gran == tableRowPerAttr && d.perAttr
	if perAttr {
		groups = &inst.attrGroups
	}
	spec := inst.tableGroupSpec
	if in.GroupBy != nil {
		if gran == tableRowPerAttr {
			return app.RefuseOperation("the per-attribute grid groups by the entity and then the membership path; set granularity per_db_row to group by columns")
		}
		spec = make([]tableGroupLevel, 0, len(in.GroupBy))
		seen := make(map[string]bool, len(in.GroupBy))
		for _, lvl := range in.GroupBy {
			i := inst.tableColumnIndex(d.schema, lvl.Column)
			if i < 0 {
				return app.RefuseOperation("no column of the result the Table draws is named " + strconv.Quote(lvl.Column) + "; get_table lists them")
			}
			name := d.schema.Field(i).Name
			if seen[name] {
				return app.RefuseOperation("group_by names " + strconv.Quote(name) + " twice")
			}
			seen[name] = true
			spec = append(spec, tableGroupLevel{column: name, desc: lvl.Descending, nullsFirst: lvl.NullsFirst})
		}
	}
	if in.Expand != nil && in.Collapse != nil {
		return app.RefuseOperation("expand and collapse both name a group to fold; give one of them")
	}
	fold, open := in.Collapse, false
	if in.Expand != nil {
		fold, open = in.Expand, true
	}
	foldGroup := int32(-1)
	if fold != nil {
		switch {
		case len(spec) == 0 && !perAttr:
			return app.RefuseOperation("the rows draw flat; set group_by first")
		case in.GroupBy != nil:
			return app.RefuseOperation("expand or collapse a group once the grouping has drawn; set group_by on its own first")
		case d.lines == 0:
			return app.RefuseOperation("the grouping has not drawn yet; read get_table after the pane's next frame")
		case strings.TrimSpace(*fold) != tableGroupAll:
			foldGroup = tableGroupByPath(groups.Layout(), *fold)
			if foldGroup < 0 {
				return app.RefuseOperation("no group drawn has the path " + strconv.Quote(*fold) + "; get_table lists the page's")
			}
		}
	}
	size := inst.pager.PageSize()
	if in.PageSize != nil {
		ok := false
		for _, s := range tablePageSizes {
			ok = ok || s == *in.PageSize
		}
		if !ok {
			return app.RefuseOperation("page_size is 50, 100, 500, 1000 or 10000")
		}
		size = *in.PageSize
	}
	total := d.rows
	if d.lines > 0 && len(spec) > 0 && in.GroupBy == nil && !perAttr {
		total = d.lines // the per-attribute grid pages by source row
	}
	if in.Page != nil {
		pages := max((total+size-1)/size, 1)
		if *in.Page < 1 || *in.Page > pages {
			return app.RefuseOperation("page is 1 to " + strconv.FormatInt(pages, 10) + " at " + strconv.FormatInt(size, 10) + " rows a page")
		}
	}
	if in.Row != nil && (*in.Row < 0 || *in.Row >= d.rows) {
		return app.RefuseOperation("row " + strconv.FormatInt(*in.Row, 10) + " is not in the result; it has " +
			strconv.FormatInt(d.rows, 10) + " rows, 0 for the first")
	}

	o := &inst.tableOpts
	o.granularity = gran
	if in.RawCells != nil {
		o.rawCells = *in.RawCells
	}
	if in.SupportColumns != nil {
		o.showSupport = *in.SupportColumns
	}
	if in.MembershipColumns != nil {
		o.showMembership = *in.MembershipColumns
	}
	if in.HideEmptySections != nil {
		o.hideEmptySections = *in.HideEmptySections
	}
	if in.CanonicalHashes != nil {
		o.showHashes = *in.CanonicalHashes
	}
	inst.tableSort.Active, inst.tableSort.Col, inst.tableSort.Desc = active, col, desc
	if in.GroupBy != nil {
		inst.tableGroupSpec = spec
		// A spec applied to no result yet is resolved when the grid draws;
		// one applied now lets the expansion keyed by path carry over.
		inst.applyTableGrouping(d.schema)
	}
	if fold != nil {
		switch {
		case foldGroup >= 0:
			groups.SetOpen(groups.Layout().Groups[foldGroup], open)
		case open:
			groups.ExpandAll()
		default:
			groups.CollapseAll()
		}
	}
	if in.PageSize != nil {
		inst.pager.SetPageSize(size)
	}
	switch {
	case in.Page != nil:
		inst.pager.GoToIndex((*in.Page - 1) * size)
	case in.Row != nil && len(spec) > 0 && !perAttr:
		// Grouped, a row's page is the one its line falls on once its
		// ancestors are open: the grid's reveal turns to it (play_table_group.go).
		inst.tableGroups.Reveal(*in.Row)
	case in.Row != nil:
		pos := *in.Row
		if active {
			res := snapshotResults(inst)
			if lr, rerr := res.read(tablePaneId, ""); rerr == nil {
				if lr.rec != nil && lr.rec.Schema() == d.schema {
					order := inst.tableSort.OrderFor(lr.rec, lr.rec.NumRows())
					pos = inst.tableSort.DisplayPos(order, pos)
				}
				lr.release()
			}
		}
		inst.pager.GoToIndex(pos)
	}
	return
}

// requestTableOptions is a header click or a pager change: through
// set_table_options as the person's gesture where play's launcher routes it,
// directly otherwise.
func (inst *PlayApp) requestTableOptions(in SetTableOptionsArgs) {
	playGesture(inst, opSetTableOptions, in, func() { _ = inst.setTableOptions(in) })
}

// tableSortClick is the set_table_options a header click asks for: the
// three-state cycle tableSortState.Clicked draws, ascending, descending,
// the result's order.
func (inst *PlayApp) tableSortClick(schema *arrow.Schema, col int) (in SetTableOptionsArgs) {
	s := inst.tableSort
	name := schema.Field(col).Name
	desc := false
	switch {
	case !s.Active || s.Col != col:
	case !s.Desc:
		desc = true
	default:
		name = ""
	}
	in.SortBy = &name
	if name != "" {
		in.Descending = &desc
	}
	return
}

// tableOptionsDigest is the table resource: the display options, the sort
// and the page.
func tableOptionsDigest(p *PlayApp) string {
	o := p.tableOpts
	s := p.tableSort
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	return "g=" + strconv.Itoa(int(o.granularity)) + "|s=" + b(o.showSupport) + "|m=" + b(o.showMembership) +
		"|e=" + b(o.hideEmptySections) + "|r=" + b(o.rawCells) + "|h=" + b(o.showHashes) +
		"|sort=" + b(s.Active) + ":" + strconv.Itoa(s.Col) + ":" + b(s.Desc) +
		"|group=" + tableGroupDigest(p) +
		"|page=" + strconv.FormatInt(p.pager.CurrentPage(), 10) + "|size=" + strconv.FormatInt(p.pager.PageSize(), 10)
}

// tableGroupDigest is the grouping's part of the digest: each level and the
// expansion's generation.
func tableGroupDigest(p *PlayApp) string {
	var sb strings.Builder
	for i, lvl := range p.tableGroupSpec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(lvl.column)
		if lvl.desc {
			sb.WriteString(":d")
		}
		if lvl.nullsFirst {
			sb.WriteString(":n")
		}
	}
	sb.WriteString("@" + strconv.FormatUint(p.tableGroups.Gen(), 10))
	return sb.String()
}

// tableGroupReadings is the grouping as get_table lists it.
func (inst *PlayApp) tableGroupReadings(schema *arrow.Schema) (out []TableGroupLevelReading) {
	for _, lvl := range inst.tableGroupSpec {
		r := TableGroupLevelReading{Column: lvl.column, Descending: lvl.desc, NullsFirst: lvl.nullsFirst}
		if schema != nil {
			if idx := schema.FieldIndices(lvl.column); len(idx) > 0 {
				if caption := inst.tableColumnCaption(schema, idx[0]); caption != lvl.column {
					r.Handle = caption
				}
			}
		}
		out = append(out, r)
	}
	return
}

func addTableOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[TableReading, GetTableArgs, SetTableOptionsArgs]{
		pane:     tablePaneId,
		resource: "the Table pane's settings: the sort, the page and its size, raw cells and the leeway display options",
		digest:   tableOptionsDigest,
		get:      opGetTable,
		getSummary: "read what the Table pane last drew: the sort, the page, the display options, each column as the header " +
			"captions it (leeway handle, gloss label and media type, or why its gloss is not applied), and on request the page's " +
			"rows in the order drawn with each cell as its gloss renders it",
		set:        opSetTableOptions,
		setSummary: "sort the Table pane by a column or give back the result's order, group its rows by columns and fold or unfold a group, turn its page, set rows per page, raw cells, or the leeway display options",
		gesture:    "clicking a column header to sort or to reverse a grouped level, a group's band or chevron to fold it, the grouping picker, and the pager above the table",
		follows: []string{"the pane draws with the new settings from its next frame; a sort or a grouping reorders the rows held and reruns nothing",
			"grouped, the page windows the lines drawn — group lines and the rows under open groups — and get_table lists each group's path",
			"raw cells also changes what the Detail, Cards and Chat panes show",
			"a new result turns back to the first page and keeps the sort"},
		read:  tableReading,
		apply: func(p *PlayApp, in SetTableOptionsArgs) error { return p.setTableOptions(in) },
	})
}
