package play

import (
	"slices"
	"time"

	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// play_table_attr.go is the Table pane's per-attribute leeway view — the perAttr
// granularity of the options bar (play_table_leeway.go). It keeps the *same
// columns* as the per-DB-row grid but explodes each section down its own rows so
// that every cell holds one scalar:
//
//   - A section with N attributes for one DB row occupies N stacked rows; a
//     non-scalar (array/set) value column further explodes each item to its own
//     row. Sections are un-nested independently, so cells line up only *within*
//     a section (same attribute), never across sections.
//   - No value is ever repeated: the `#` gutter and the backbone columns show
//     once on the DB row's top row (blank below), a per-attribute membership /
//     support value shows once on its attribute's first row, and array items
//     fill straight down. Missing cells are empty.
//
// The exploded stream comes from the streamreadaccess.Driver — it already walks
// section → attribute → item with the nested-array cardinality it owns, so the
// value + backbone cells are re-laid-out from its walk rather than re-derived.
// Revealed membership/support columns (the (b)/(c) toggles) are read raw from
// the batch per attribute (like the per-DB-row grid, they show the same physical
// value), so this sink does not implement MembershipSinkI — the Driver skips
// membership rendering it would otherwise do.
//
// A row click selects the source DB row (the same signalSelection the per-DB-row
// grid emits), so per-attribute rows cross-filter the app exactly as DB rows do.

const (
	attrRowHeight  = 20.0
	attrCellIdBase = uint64(0x02000000)
	attrColStride  = uint64(0x00010000)
)

// attrGridRow is one exploded visual row: its cells are indexed by *position in
// visCols* ("" = empty), so a lookup is an index rather than a map probe and the
// whole row is one slice allocation instead of a map. absRow is the source DB
// row; firstOfEntity marks the entity's top row (where the `#` gutter and the
// once-per-entity backbone cells appear).
type attrGridRow struct {
	absRow        int64
	firstOfEntity bool
	cells         []string
}

// attrExplodeSink walks a leeway batch into the exploded grid rows. It
// implements streamreadaccess.SinkI (structure + values) but deliberately not
// MembershipSinkI: membership *columns* are read raw per attribute in
// flushAttr, so the Driver's rendered memberships are neither needed nor driven.
//
// It is pooled on PlayApp and re-driven every frame the per-attribute view is
// shown, so it holds all its working storage as reusable buffers: reset()
// rebinds the per-drive inputs and BeginBatch reslices the output to [:0],
// letting the steady-state render reuse last frame's backing arrays rather than
// allocating fresh maps/slices per cell.
type attrExplodeSink struct {
	rec       arrow.RecordBatch // the page slice — for raw per-attribute reads
	pageStart int64

	// Revealed membership/support columns to raw-read per attribute, grouped by
	// section identity (built from ColumnClasses ∩ visCols).
	taggedExtra map[string][]int
	plainExtra  map[common.PlainItemTypeE][]int

	// colToVis maps an Arrow column index to its position in visCols (-1 when not
	// visible); nCols is len(visCols), the width of every output row. Rebuilt in
	// place each drive by reset → setVisCols.
	colToVis []int
	nCols    int

	rows []attrGridRow // output (backing array reused across frames)

	// glossFn, when set, passes each value cell's text through the column's
	// gloss inline face (ADR-0186) — arrowIdx is the value column, text the
	// driver-marshalled item. Nil = identity. Bound per drive by reset's
	// caller, since the resolution belongs to the schema, not the sink.
	glossFn func(arrowIdx int, text string) string

	// Per-entity / per-section running state. entityStart is the index in rows
	// where the current entity's first visual row sits; secRow is the running
	// visual-row offset (from entityStart) within the current section — sections
	// overlay from offset 0, attributes stack down within a section.
	entityIdx   int
	entityStart int
	secRow      int

	// Current section.
	curExtras []int // revealed member/support arrowIdx for the section
	curAttr   int   // attribute index within the section (for raw reads)

	// Current attribute (Begin{Plain,Tagged}Value): value columns as parallel
	// arrays (Arrow index + its item list), recycled per attribute. inAttr guards
	// EndColumn against columns driven outside an attribute. itemsFree pools the
	// per-column item slices so EndColumn reuses backing arrays across attributes
	// and frames.
	inAttr       bool
	attrColIdx   []int
	attrColItems [][]string
	itemsFree    [][]string

	// Per-column scratch.
	curCol       int
	curBuf       strings.Builder
	curItems     []string
	isCollection bool

	err error
}

var _ streamreadaccess.SinkI = (*attrExplodeSink)(nil)

// reset rebinds the pooled sink to a new page drive, keeping every backing array
// so the steady-state render (same page, same schema) reuses them. The output
// slice and per-drive flags are reset in BeginBatch (the Driver's entry point).
func (inst *attrExplodeSink) reset(rec arrow.RecordBatch, pageStart int64, taggedExtra map[string][]int, plainExtra map[common.PlainItemTypeE][]int, visCols []int, nFields int) {
	inst.rec = rec
	inst.pageStart = pageStart
	inst.taggedExtra = taggedExtra
	inst.plainExtra = plainExtra
	inst.setVisCols(visCols, nFields)
}

// setVisCols rebuilds the Arrow-index → visCols-position lookup in place.
func (inst *attrExplodeSink) setVisCols(visCols []int, nFields int) {
	inst.nCols = len(visCols)
	if cap(inst.colToVis) >= nFields {
		inst.colToVis = inst.colToVis[:nFields]
	} else {
		inst.colToVis = make([]int, nFields)
	}
	for i := range inst.colToVis {
		inst.colToVis[i] = -1
	}
	for pos, arrowIdx := range visCols {
		if arrowIdx >= 0 && arrowIdx < nFields {
			inst.colToVis[arrowIdx] = pos
		}
	}
}

// visPos is the output-column index for an Arrow column, or -1 when not visible.
func (inst *attrExplodeSink) visPos(arrowIdx int) int {
	if arrowIdx < 0 || arrowIdx >= len(inst.colToVis) {
		return -1
	}
	return inst.colToVis[arrowIdx]
}

// --- batch / entity ---

func (inst *attrExplodeSink) BeginBatch() {
	inst.rows = inst.rows[:0]
	inst.entityIdx = 0
	inst.inAttr = false
	inst.err = nil
}
func (inst *attrExplodeSink) EndBatch() error { return inst.err }

func (inst *attrExplodeSink) BeginEntity() { inst.entityStart = len(inst.rows) }

// EndEntity just advances the entity counter: the exploded rows were written
// straight into inst.rows as sections/attributes were flushed (flushAttr →
// ensureRow), so no transpose or per-row map merge is needed.
func (inst *attrExplodeSink) EndEntity() error {
	inst.entityIdx++
	return inst.err
}

// ensureRow returns the cells slice for the current entity's visual row at the
// given offset (from entityStart), creating or recycling it. flushAttr fills
// offsets densely from 0, so the target index is always ≤ len(rows): an index
// below len(rows) is an existing row of this entity (a later section overlaying
// its own columns onto an earlier section's row), and one equal to len(rows)
// appends — reusing the slot's backing []string when the pooled rows slice still
// has capacity from a previous frame.
func (inst *attrExplodeSink) ensureRow(off int) []string {
	vrow := inst.entityStart + off
	if vrow < len(inst.rows) {
		return inst.rows[vrow].cells
	}
	absRow := inst.pageStart + int64(inst.entityIdx)
	first := vrow == inst.entityStart
	if vrow < cap(inst.rows) {
		inst.rows = inst.rows[:vrow+1]
		r := &inst.rows[vrow]
		r.absRow = absRow
		r.firstOfEntity = first
		r.cells = clearAndSize(r.cells, inst.nCols)
		return r.cells
	}
	cells := make([]string, inst.nCols)
	inst.rows = append(inst.rows, attrGridRow{absRow: absRow, firstOfEntity: first, cells: cells})
	return cells
}

// --- sections ---

func (inst *attrExplodeSink) BeginPlainSection(itemType common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.beginSection(inst.plainExtra[itemType])
}
func (inst *attrExplodeSink) EndPlainSection() error { return inst.endSection() }

func (inst *attrExplodeSink) BeginTaggedSections()     {}
func (inst *attrExplodeSink) EndTaggedSections() error { return inst.err }

func (inst *attrExplodeSink) BeginCoSectionGroup(naming.Key) {}
func (inst *attrExplodeSink) EndCoSectionGroup() error       { return inst.err }

func (inst *attrExplodeSink) BeginSection(name naming.StylableName, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, _ int) {
	inst.beginSection(inst.taggedExtra[string(name)])
}
func (inst *attrExplodeSink) EndSection() error { return inst.endSection() }

func (inst *attrExplodeSink) beginSection(extras []int) {
	inst.curExtras = extras
	inst.curAttr = 0
	inst.secRow = 0
}
func (inst *attrExplodeSink) endSection() error { return inst.err }

// --- attributes ---

func (inst *attrExplodeSink) BeginPlainValue() { inst.beginAttr() }
func (inst *attrExplodeSink) EndPlainValue() error {
	inst.flushAttr()
	return inst.err
}
func (inst *attrExplodeSink) BeginTaggedValue() { inst.beginAttr() }
func (inst *attrExplodeSink) EndTaggedValue() error {
	inst.flushAttr()
	inst.curAttr++
	return inst.err
}

func (inst *attrExplodeSink) beginAttr() {
	inst.inAttr = true
	inst.attrColIdx = inst.attrColIdx[:0]
	inst.attrColItems = inst.attrColItems[:0]
}

// flushAttr turns the current attribute's buffered value columns into
// K = max(item count, 1) exploded rows written straight into inst.rows at the
// section's running offset. Row j takes each value column's j-th item (empty
// when that column has fewer — no repeat); row 0 additionally carries the
// revealed membership/support columns, read raw at this attribute's index. The
// per-column item slices are then recycled into itemsFree.
func (inst *attrExplodeSink) flushAttr() {
	k := 1
	for _, items := range inst.attrColItems {
		if len(items) > k {
			k = len(items)
		}
	}
	for j := 0; j < k; j++ {
		cells := inst.ensureRow(inst.secRow + j)
		for e, arrowIdx := range inst.attrColIdx {
			items := inst.attrColItems[e]
			if j < len(items) {
				if p := inst.visPos(arrowIdx); p >= 0 {
					cells[p] = items[j]
				}
			}
		}
		if j == 0 {
			for _, ax := range inst.curExtras {
				p := inst.visPos(ax)
				if p < 0 {
					continue
				}
				if v := listInnerScalar(inst.rec, ax, inst.entityIdx, inst.curAttr); v != "" {
					cells[p] = v
				}
			}
		}
	}
	inst.secRow += k
	for _, items := range inst.attrColItems {
		inst.itemsFree = append(inst.itemsFree, items[:0])
	}
	inst.attrColIdx = inst.attrColIdx[:0]
	inst.attrColItems = inst.attrColItems[:0]
	inst.inAttr = false
}

// --- columns / values ---

func (inst *attrExplodeSink) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, _ naming.StylableName, _ canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.curCol = colAddr.Index
	inst.curItems = inst.curItems[:0]
	inst.curBuf.Reset()
	inst.isCollection = false
}
func (inst *attrExplodeSink) EndColumn() {
	if !inst.inAttr {
		return
	}
	items := inst.takeItems()
	if inst.isCollection {
		items = append(items, inst.curItems...) // may be empty (card 0)
	} else {
		items = append(items, inst.glossed(utfsafe.EnsureUTF8(inst.curBuf.String())))
	}
	inst.attrColIdx = append(inst.attrColIdx, inst.curCol)
	inst.attrColItems = append(inst.attrColItems, items)
}

// takeItems returns a reset item slice, reused from itemsFree when available.
func (inst *attrExplodeSink) takeItems() []string {
	n := len(inst.itemsFree)
	if n == 0 {
		return make([]string, 0, 4)
	}
	items := inst.itemsFree[n-1]
	inst.itemsFree = inst.itemsFree[:n-1]
	return items[:0]
}

// clearAndSize returns a length-n []string reusing s's backing array when it is
// large enough (zeroing it so no last-frame string is retained), else a fresh one.
func clearAndSize(s []string, n int) []string {
	if cap(s) >= n {
		s = s[:n]
		clear(s)
		return s
	}
	return make([]string, n)
}

func (inst *attrExplodeSink) BeginScalarValue()             { inst.isCollection = false }
func (inst *attrExplodeSink) EndScalarValue() error         { return inst.err }
func (inst *attrExplodeSink) BeginHomogenousArrayValue(int) { inst.isCollection = true }
func (inst *attrExplodeSink) EndHomogenousArrayValue()      {}
func (inst *attrExplodeSink) BeginSetValue(int)             { inst.isCollection = true }
func (inst *attrExplodeSink) EndSetValue()                  {}

// BeginValueItem starts a fresh item buffer — items become separate rows, so
// (unlike the packed view) they are not joined.
func (inst *attrExplodeSink) BeginValueItem(int) { inst.curBuf.Reset() }
func (inst *attrExplodeSink) EndValueItem() {
	inst.curItems = append(inst.curItems, inst.glossed(utfsafe.EnsureUTF8(inst.curBuf.String())))
}

// glossed applies the current value column's gloss to one item's text.
func (inst *attrExplodeSink) glossed(text string) string {
	if inst.glossFn == nil {
		return text
	}
	return inst.glossFn(inst.curCol, text)
}

func (inst *attrExplodeSink) Write(pp []byte) (int, error)       { return inst.curBuf.Write(pp) }
func (inst *attrExplodeSink) WriteString(ss string) (int, error) { return inst.curBuf.WriteString(ss) }

// BeginTags/EndTags are driven for the tag count even without MembershipSinkI;
// this view reads membership columns raw, so both are no-ops.
func (inst *attrExplodeSink) BeginTags(int) {}
func (inst *attrExplodeSink) EndTags()      {}

// listInnerScalar returns the k-th inner value of a List column for one entity
// row, formatted as its per-DB-row cell would be (empty when absent). A non-list
// (scalar/backbone) column has one value, treated as attribute 0.
func listInnerScalar(rec arrow.RecordBatch, arrowIdx int, entityRow int, k int) string {
	if arrowIdx < 0 || arrowIdx >= int(rec.NumCols()) || entityRow < 0 || k < 0 {
		return ""
	}
	switch l := rec.Column(arrowIdx).(type) {
	case *array.List:
		if entityRow >= l.Len() || l.IsNull(entityRow) {
			return ""
		}
		start, end := l.ValueOffsets(entityRow)
		idx := start + int64(k)
		if idx >= end {
			return ""
		}
		return formatArrayElem(l.ListValues(), idx)
	case *array.LargeList:
		if entityRow >= l.Len() || l.IsNull(entityRow) {
			return ""
		}
		start, end := l.ValueOffsets(entityRow)
		idx := start + int64(k)
		if idx >= end {
			return ""
		}
		return formatArrayElem(l.ListValues(), idx)
	default:
		if k == 0 {
			return formatArrayElem(rec.Column(arrowIdx), int64(entityRow))
		}
		return ""
	}
}

// --- rendering ---

// renderAttrTable drives the current page through the CardDriver's Driver into
// an attrExplodeSink and renders the exploded grid. Falls back to the per-DB-row
// grid when the Driver or classification is unavailable.
func (inst *PlayApp) renderAttrTable(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, selectedRow int64, emit SignalEmitterI) {
	inst.cards.EnsureFor(schema)
	driver := inst.cards.Driver()
	classes := inst.cards.ColumnClasses()
	if driver == nil || classes == nil {
		inst.renderMasterTable(rec, schema, numRows, selectedRow, emit)
		return
	}

	totalRows := rec.NumRows()
	if totalRows > numRows {
		totalRows = numRows
	}
	pageStart, pageEnd := inst.pager.Range()
	if pageEnd > totalRows {
		pageEnd = totalRows
	}
	if pageStart > pageEnd {
		pageStart = pageEnd
	}
	if pageStart >= pageEnd {
		for rt := range c.RichTextLabel("No rows on this page.") {
			rt.Small().Weak()
		}
		return
	}

	slice := rec.NewSlice(pageStart, pageEnd)
	defer slice.Release()

	visCols := inst.visibleTableCols(rec, schema, pageStart, pageEnd)
	taggedExtra, plainExtra := buildAttrExtras(classes, visCols)
	sink := &inst.attrSink
	sink.reset(slice, pageStart, taggedExtra, plainExtra, visCols, schema.NumFields())
	// ADR-0186: value cells render through the column's gloss on the items —
	// the element kind, since this grid explodes a list down its rows.
	glossCols := inst.glossColumns(schema)
	sink.glossFn = func(arrowIdx int, text string) string {
		if arrowIdx < 0 || arrowIdx >= len(glossCols) {
			return text
		}
		return inst.glossText(&glossCols[arrowIdx], text, gloss.KindOfArrow(listElemType(schema.Field(arrowIdx).Type)))
	}
	if err := driver.DriveRecordBatch(sink, slice); err != nil {
		log.Warn().Err(err).Msg("play: per-attribute drive failed — falling back to per-DB-row grid")
		inst.renderMasterTable(rec, schema, numRows, selectedRow, emit)
		return
	}
	inst.renderAttrExplodeGrid(schema, visCols, sink.rows, selectedRow, emit)
}

// buildAttrExtras groups the revealed (in visCols) membership and support
// columns by their section identity, so the sink can raw-read them per attribute.
func buildAttrExtras(classes []streamreadaccess.ColumnClass, visCols []int) (tagged map[string][]int, plain map[common.PlainItemTypeE][]int) {
	vis := make(map[int]struct{}, len(visCols))
	for _, col := range visCols {
		vis[col] = struct{}{}
	}
	tagged = make(map[string][]int)
	plain = make(map[common.PlainItemTypeE][]int)
	for i := range classes {
		cl := classes[i]
		if cl.Class == streamreadaccess.ColumnRoleClassValue {
			continue
		}
		if _, ok := vis[cl.ArrowIdx]; !ok {
			continue
		}
		if cl.Backbone() {
			plain[cl.PlainItemType] = append(plain[cl.PlainItemType], cl.ArrowIdx)
		} else {
			tagged[string(cl.SectionName)] = append(tagged[string(cl.SectionName)], cl.ArrowIdx)
		}
	}
	return
}

// renderAttrExplodeGrid lays the exploded rows out with the *same* columns as
// the per-DB-row grid — the visCols order, the friendly leeway labels, the same
// left inset. Widths are sampled from the exploded (scalar) cells rather than
// the packed per-DB-row content. A row click selects the source DB row.
func (inst *PlayApp) renderAttrExplodeGrid(schema *arrow.Schema, visCols []int, rows []attrGridRow, selectedRow int64, emit SignalEmitterI) {
	ids := inst.ids
	cellPadX := styletokens.PaddingTight(inst.density)
	inst.ensureColLabels(schema)
	widths := inst.attrColWidths(schema, visCols, rows)

	// ADR-0151: a width the user set outranks the sampled estimator, which
	// becomes the default for columns they have not touched. cols and the
	// EtColumn sequence stay index-aligned — the leading "#" column takes
	// part so the read-back lines up, and since it is not resizable the
	// binding reports our own width back for it and nothing is captured.
	cols := inst.attrColumnKeys(schema, visCols)
	resolved := make([]float64, 0, len(cols))
	resolved = append(resolved, attrRowNumColWidth)
	for _, w := range widths {
		resolved = append(resolved, float64(w))
	}
	if inst.colWidthRes != nil {
		// Font size 0: play has no single text size to attribute a width
		// to, and passing one it does not render at would rescale stored
		// widths against a fiction. Zero disables rescaling, which is the
		// documented meaning.
		resolved = inst.colWidthRes.Resolve(attrTableTag, cols, 0, resolved)
	}

	// One-frame re-fit on the frame the column set changes, as the per-DB-row
	// grid does (tableColsChanged): the sampled seed is an estimate, and this
	// is the frame the crate measures the real header and cells. Two things
	// keep it inside ADR-0151's contract. A column whose resolved width is
	// not the seed's carries a user override, which outranks any fit, so
	// only seed-width columns are auto-sized; and captureAttrWidths adopts
	// this frame's read-back as the crate's idea, not the user's, so nothing
	// it settles on is frozen as an override. Cells go out untruncated on
	// this frame (selectableCell), else the fit would see their truncated
	// width and only ever fit the header.
	refit := inst.attrColsChanged(schema, visCols)
	if refit && inst.colWidthRes != nil {
		// See the per-DB-row grid: the fit's result lands a report after this
		// frame, so the window has to be opened here rather than inferred
		// from a flag at capture time.
		inst.colWidthRes.MarkReseed(attrTableTag)
	}

	// Leading "#" (source DB row) column + the data columns, same order as the
	// per-DB-row grid.
	c.EtColumn(float32(resolved[0])).Resizable(false).Send()
	for i := range visCols {
		col := c.EtColumn(float32(resolved[i+1])).
			Resizable(true).
			RangeMinMax(inst.colDragMinWidth(), colDragMaxWidth)
		if refit && resolved[i+1] == float64(widths[i]) {
			col = col.AutoSizeThisFrame(true)
		}
		col.Send()
	}

	et := c.EndETable(ids.PrepareStr(attrTableTag), uint64(len(rows)), attrRowHeight, 1, 1).Striped(true)
	if inst.colWidthRes != nil {
		et = et.ApplyWidths(inst.colWidthRes.Epoch(attrTableTag))
	}

	if vis, _ := et.ColVisible(0); vis {
		for range et.Headers(0, 0) {
			inst.headerCell(attrHdrIdOffset, cellPadX, func() {
				for rt := range c.RichTextLabel("#") {
					rt.Weak().Monospace()
				}
			})
		}
	}
	glossCols := inst.glossColumns(schema)
	for pos, arrowCol := range visCols {
		colPos := uint32(pos + 1)
		if vis, _ := et.ColVisible(colPos); !vis {
			continue
		}
		for range et.Headers(0, colPos) {
			field := schema.Field(arrowCol)
			gc := &glossCols[arrowCol]
			// The full type goes on hover and the header carries the short
			// tag, for the same reason the per-DB-row grid does it: whatever
			// the header renders is a floor under the column's width, and the
			// spelled-out type is routinely wider than the values below it.
			// A glossed column (ADR-0186) adds its media type, small, and its
			// resolution to the hover.
			hover := field.Type.String()
			if label := inst.colLabels[field.Name]; label != "" {
				hover = field.Name + " — " + hover
			}
			if gh := glossHover(gc); gh != "" {
				hover += " — " + gh
			}
			inst.headerCell(attrHdrIdOffset+uint64(arrowCol)+1, cellPadX, func() {
				caption := field.Name
				if label := inst.colLabels[field.Name]; label != "" {
					caption = label
				}
				for range c.HoverText(hover).KeepIter() {
					for rt := range c.RichTextLabel(caption) {
						rt.Strong().Monospace()
					}
				}
				for rt := range c.RichTextLabel(shortArrowType(field.Type)) {
					rt.Small().Weak().Monospace()
				}
				if gc.mediaType != "" {
					for rt := range c.RichTextLabel(gc.mediaType) {
						rt.Small().Weak().Monospace()
					}
				}
			})
		}
	}

	rowLo, rowHi := uint64(0), uint64(len(rows))
	if rb, re, _, _, _, ok := et.VisibleRange(); ok {
		rowLo, rowHi = rb, re
		if rowHi > uint64(len(rows)) {
			rowHi = uint64(len(rows))
		}
	}
	for local := rowLo; local < rowHi; local++ {
		row := rows[local]
		selected := row.absRow == selectedRow
		rowBase := attrCellIdBase + local*attrColStride

		if vis, _ := et.ColVisible(0); vis {
			for range et.Cells(local, 0) {
				marker := ""
				if row.firstOfEntity {
					marker = strconv.FormatInt(row.absRow+1, 10)
				}
				if inst.selectableCell(rowBase, cellPadX, marker, true, selected, true, false, gloss.ToneNeutral, "", true) {
					emit.Emit(signalSelection, row.absRow)
				}
			}
		}
		for pos, arrowCol := range visCols {
			colPos := uint32(pos + 1)
			if vis, _ := et.ColVisible(colPos); !vis {
				continue
			}
			leftAlign := stringLikeArrowType(listElemType(schema.Field(arrowCol).Type))
			for range et.Cells(local, colPos) {
				// A gloss/url item is a hyperlink to itself (its text is the
				// URL: the face is the first line of the value).
				link := ""
				if gc := &glossCols[visCols[pos]]; gc.mediaType == gloss.MediaTypeURL && gc.glossedElem() && !inst.tableOpts.rawCells {
					link = row.cells[pos]
				}
				text := row.cells[pos]
				if refit {
					// Bounded like the per-DB-row grid: the fit must not size
					// a column to a paragraph.
					text = fitRunes(text, colMaxRunes)
				}
				if inst.selectableCell(rowBase+uint64(pos)+1, cellPadX, text, false, selected, true, leftAlign, gloss.ToneNeutral, link, !refit) {
					emit.Emit(signalSelection, row.absRow)
				}
			}
		}
	}
	et.Send()
	inst.captureAttrWidths(et, cols)
}

// captureAttrWidths feeds the widths egui_table settled on back into the
// resolver and lets the debounce write them.
//
// The first report a table makes is its force-autofit frame, whose widths
// are the crate's idea rather than the user's; passing firstShow on that
// frame is what stops the estimator's first result being frozen as an
// override nobody chose. A re-fit frame is the same kind of frame, and is
// covered by the MarkReseed at the emission site rather than by a flag here —
// its result arrives a report later than the frame that asked for it.
func (inst *PlayApp) captureAttrWidths(et c.EndETableFluid, cols []colwidth.Column) {
	if inst.colWidthRes == nil {
		return
	}
	now := time.Now()
	if fetched, ok := et.ColumnWidths(); ok {
		widths := make([]float64, len(fetched))
		for i, w := range fetched {
			widths[i] = float64(w)
		}
		firstShow := !inst.attrWidthsSeen
		inst.attrWidthsSeen = true
		inst.colWidthRes.Observe(attrTableTag, cols, widths, 0, firstShow, now)
	}
	if _, err := inst.colWidthRes.Flush(now); err != nil {
		log.Warn().Err(err).Msg("play: storing column widths failed; will retry")
	}
}

// attrColWidths sizes each data column to its exploded (scalar) content, sampled
// over the leading rows, floored to the friendly header label. The per-DB-row
// cache is sampled from the packed representation (`[len=N]`), so it under-sizes
// the un-packed scalars; this samples the grid the exploded view actually shows.
func (inst *PlayApp) attrColWidths(schema *arrow.Schema, visCols []int, rows []attrGridRow) []float32 {
	// charW is the per-DB-row grid's colCharPx: the measured monospace advance
	// (Hack at 13 px). Runes, not bytes — a glossed `••••••` is six glyphs and
	// eighteen bytes.
	const charW = colCharPx
	const maxW = 420.0
	const sampleRows = 96
	// pad is the button's own padding plus the inset cellInset spends on each
	// side of the cell; minW keeps the seed at or above the floor a column can
	// be dragged to, so a seeded column never starts below its own minimum.
	pad := 18.0 + 2*styletokens.PaddingTight(inst.density)
	minW := max(float32(44.0), inst.colDragMinWidth())
	widths := make([]float32, len(visCols))
	for i, arrowCol := range visCols {
		field := schema.Field(arrowCol)
		maxChars := utf8.RuneCountInString(field.Name)
		if lbl := inst.colLabels[field.Name]; lbl != "" {
			maxChars = utf8.RuneCountInString(lbl)
		}
		for r := 0; r < len(rows) && r < sampleRows; r++ {
			if v := rows[r].cells[i]; utf8.RuneCountInString(v) > maxChars {
				maxChars = utf8.RuneCountInString(v)
			}
		}
		w := float32(maxChars)*charW + pad
		if w < minW {
			w = minW
		}
		if w > maxW {
			w = maxW
		}
		widths[i] = w
	}
	return widths
}

// attrTableTag is the stable instance-tier scope for the attr grid's
// column-width overrides, and the etable's id. One constant so the two
// cannot drift apart.
const attrTableTag = "attr-results"

// attrRowNumColWidth is the fixed width of the leading "#" column. It is
// not resizable, so it never acquires an override; it takes part in the
// resolver's column list only to keep indices aligned with the EtColumn
// sequence and with the width read-back.
const attrRowNumColWidth = 48.0

// ensureColWidthRes acquires the column-width resolver once, on the first
// Frame. The capability rides the frame context (ADR-0155 §SD1), so it
// cannot be picked up in Mount.
//
// A host without the capability, or a construction failure, leaves the
// resolver nil and widths come from the estimator alone — what play did
// before ADR-0151. A failed Load is likewise non-fatal: stored overrides
// are simply not applied this session rather than the grid failing.
func (inst *PlayApp) ensureColWidthRes(ctx app.FrameContextI) {
	if inst.colWidthResInit {
		return
	}
	inst.colWidthResInit = true
	h, ok := ctx.(colwidth.HostI)
	if !ok {
		return
	}
	store := h.ColumnWidthStore()
	if store == nil {
		return
	}
	res, err := colwidth.New(store, colwidth.Opts{
		// The mount context's id, never a composed one: ADR-0155 §SD3 makes
		// this the keying identity, so a column dragged in an embedded
		// instance follows the content to a windowed one.
		AppId: ctx.AppId(),
		// Which window did the dragging (ADR-0191 §SD4). Provenance on the
		// trail only — the override itself stays keyed by app, so this does
		// not change what a second window resolves.
		InstanceKey: ctx.InstanceKey(),
		// Below the minimum a column cannot be grabbed to drag back out;
		// above the maximum one column pushes every other off-screen. Both
		// match the range the grids hand egui_table (EtColumn.RangeMinMax),
		// so what the user can drag to is exactly what can be stored — the
		// floor counts the cell inset on *both* sides, which it did not
		// before: a column at the old floor put the gridline in the glyphs.
		MinPoints: float64(inst.colDragMinWidth()),
		MaxPoints: colDragMaxWidth,
	})
	if err != nil {
		log.Warn().Err(err).Msg("play: column-width resolver unavailable")
		return
	}
	if lErr := res.Load(); lErr != nil {
		log.Warn().Err(lErr).Msg("play: stored column widths could not be loaded")
	}
	inst.colWidthRes = res
}

// attrColumnKeys builds the resolver's column identities for the attr
// grid, index-aligned with the EtColumn sequence: the leading "#" column
// first, then the visible data columns.
//
// Identity is the raw Arrow field name and type, not the friendly label
// the header renders: the label is derived, so an override keyed on it
// would change identity if the label builder ever did, while the field
// name is what the query actually returned. The type participates so a
// column that changes type drops its stored width.
//
// Every type carries tableViewTagAttr, so this grid's widths never reach the
// per-DB-row grid through the column tier: the two show the same column's
// contents differently and a width fitted to one does not fit the other.
func (inst *PlayApp) attrColumnKeys(schema *arrow.Schema, visCols []int) (cols []colwidth.Column) {
	cols = make([]colwidth.Column, 0, len(visCols)+1)
	cols = append(cols, colwidth.Column{Name: "#", Type: "rownum" + tableViewTagAttr})
	for _, arrowCol := range visCols {
		f := schema.Field(arrowCol)
		cols = append(cols, colwidth.Column{Name: f.Name, Type: f.Type.String() + tableViewTagAttr})
	}
	return
}

// attrColsChanged reports whether the per-attribute grid's column set —
// the schema and the visible columns — differs from the last frame's, the
// trigger for its one-frame re-fit; the per-DB-row grid's tableColsChanged,
// with its own memory, since the two grids show different column sets of the
// same result and must each answer once per change.
func (inst *PlayApp) attrColsChanged(schema *arrow.Schema, visCols []int) (changed bool) {
	changed = schema != inst.attrFitSchema || !slices.Equal(visCols, inst.attrFitCols)
	if changed {
		inst.attrFitSchema = schema
		inst.attrFitCols = append(inst.attrFitCols[:0], visCols...)
	}
	return
}
