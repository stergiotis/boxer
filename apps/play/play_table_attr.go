package play

import (
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// play_table_attr.go is the Table pane's per-attribute leeway view — the
// perAttr granularity of the options bar (play_table_leeway.go). The page's
// entities are exploded into an Arrow record of one row per attribute (per
// item of a non-scalar value), carrying the source row, the section, the
// attribute's membership path and parameters, and the visible columns' cells
// as text; recordtable.RenderGrid draws it grouped two levels deep
// (ADR-0336 §SD12): the entity, whose band stands for the source row and
// selects it, then the membership path, whose steps nest as the jsonshred
// grammar reads them. Sections stack — one attribute per row, the other
// sections' columns blank on it — as the Detail pane's record card does.
//
// The exploded record is rebuilt when the page, the columns shown or the
// glosses change, and kept otherwise, so the grid's layout and cells cache
// hold between frames.

// attrExplodeSink walks a leeway batch into the exploded record. It
// implements streamreadaccess.SinkI for the values and MembershipSinkI for
// the path; the revealed membership and support columns are read raw per
// attribute, as the per-DB-row grid shows them.
type attrExplodeSink struct {
	rec       arrow.RecordBatch // the page slice — for raw per-attribute reads
	pageStart int64

	// Revealed membership/support columns to raw-read per attribute, grouped
	// by section identity (built from ColumnClasses ∩ visCols).
	taggedExtra map[string][]int
	plainExtra  map[common.PlainItemTypeE][]int

	// colToVis maps an Arrow column index to its position in visCols (-1 when
	// not visible); nCols is len(visCols).
	colToVis []int
	nCols    int

	// glossFn passes each value cell's text through the column's gloss
	// inline face (ADR-0186). Nil = identity.
	glossFn func(arrowIdx int, text string) string

	// The record under construction: the fixed columns, then one text
	// column per visible column.
	mem     memory.Allocator
	srcB    *array.Int64Builder
	secB    *array.StringBuilder
	attrB   *array.Int32Builder
	pathB   *array.StringBuilder
	paramsB *array.StringBuilder
	cellB   []*array.StringBuilder
	rows    int64
	out     arrow.RecordBatch

	// Per-entity / per-section / per-attribute running state.
	entityIdx  int
	curSection string
	curExtras  []int
	curAttr    int
	inAttr     bool
	attrColIdx []int
	attrItems  [][]string
	itemsFree  [][]string
	path       string
	params     string
	hasPath    bool
	hasParams  bool

	// Per-column scratch.
	curCol       int
	curBuf       strings.Builder
	curItems     []string
	isCollection bool

	err error
}

var (
	_ streamreadaccess.SinkI           = (*attrExplodeSink)(nil)
	_ streamreadaccess.MembershipSinkI = (*attrExplodeSink)(nil)
)

// The exploded record's fixed columns; the visible columns' cells follow.
const (
	attrXSrc = iota
	attrXSection
	attrXAttr
	attrXPath
	attrXParams
	attrXCells
)

// reset binds the sink to a page drive. The builders are made per drive —
// an Arrow builder is not reusable past NewArray — and the row count starts
// over; the lookup slices keep their backing arrays.
func (inst *attrExplodeSink) reset(rec arrow.RecordBatch, pageStart int64, taggedExtra map[string][]int, plainExtra map[common.PlainItemTypeE][]int, visCols []int, nFields int) {
	inst.rec = rec
	inst.pageStart = pageStart
	inst.taggedExtra = taggedExtra
	inst.plainExtra = plainExtra
	inst.setVisCols(visCols, nFields)
	if inst.mem == nil {
		inst.mem = memory.NewGoAllocator()
	}
	inst.srcB = array.NewInt64Builder(inst.mem)
	inst.secB = array.NewStringBuilder(inst.mem)
	inst.attrB = array.NewInt32Builder(inst.mem)
	inst.pathB = array.NewStringBuilder(inst.mem)
	inst.paramsB = array.NewStringBuilder(inst.mem)
	inst.cellB = inst.cellB[:0]
	for range visCols {
		inst.cellB = append(inst.cellB, array.NewStringBuilder(inst.mem))
	}
	inst.rows = 0
	inst.out = nil
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

// attrExplodedSchema is the exploded record's schema for a set of visible
// columns: the fixed columns, then one text column per visible column under
// the source field's name, so a stored width keyed on the name carries over.
func attrExplodedSchema(schema *arrow.Schema, visCols []int) *arrow.Schema {
	fields := []arrow.Field{
		{Name: "src", Type: arrow.PrimitiveTypes.Int64},
		{Name: "section", Type: arrow.BinaryTypes.String},
		{Name: "attr", Type: arrow.PrimitiveTypes.Int32},
		{Name: "path", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "params", Type: arrow.BinaryTypes.String, Nullable: true},
	}
	for _, col := range visCols {
		name := strconv.Itoa(col)
		if schema != nil && col >= 0 && col < schema.NumFields() {
			name = schema.Field(col).Name
		}
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
	}
	return arrow.NewSchema(fields, nil)
}

// record finishes the drive: the exploded record over the schema, which the
// caller owns, or nil when the drive failed.
func (inst *attrExplodeSink) record(schema *arrow.Schema) arrow.RecordBatch {
	if inst.err != nil || inst.srcB == nil {
		return nil
	}
	arrs := []arrow.Array{inst.srcB.NewArray(), inst.secB.NewArray(), inst.attrB.NewArray(), inst.pathB.NewArray(), inst.paramsB.NewArray()}
	for _, b := range inst.cellB {
		arrs = append(arrs, b.NewArray())
	}
	rec := array.NewRecordBatch(schema, arrs, inst.rows)
	for _, a := range arrs {
		a.Release()
	}
	for _, b := range []interface{ Release() }{inst.srcB, inst.secB, inst.attrB, inst.pathB, inst.paramsB} {
		b.Release()
	}
	for _, b := range inst.cellB {
		b.Release()
	}
	inst.srcB, inst.secB, inst.attrB, inst.pathB, inst.paramsB, inst.cellB = nil, nil, nil, nil, nil, inst.cellB[:0]
	return rec
}

// --- batch / entity ---

func (inst *attrExplodeSink) BeginBatch() {
	inst.entityIdx = 0
	inst.inAttr = false
	inst.err = nil
}
func (inst *attrExplodeSink) EndBatch() error { return inst.err }

func (inst *attrExplodeSink) BeginEntity() {}
func (inst *attrExplodeSink) EndEntity() error {
	inst.entityIdx++
	return inst.err
}

// --- sections ---

func (inst *attrExplodeSink) BeginPlainSection(itemType common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.beginSection(itemType.String(), inst.plainExtra[itemType])
}
func (inst *attrExplodeSink) EndPlainSection() error { return inst.err }

func (inst *attrExplodeSink) BeginTaggedSections()     {}
func (inst *attrExplodeSink) EndTaggedSections() error { return inst.err }

func (inst *attrExplodeSink) BeginCoSectionGroup(naming.Key) {}
func (inst *attrExplodeSink) EndCoSectionGroup() error       { return inst.err }

func (inst *attrExplodeSink) BeginSection(name naming.StylableName, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, _ int) {
	inst.beginSection(string(name), inst.taggedExtra[string(name)])
}
func (inst *attrExplodeSink) EndSection() error { return inst.err }

func (inst *attrExplodeSink) beginSection(name string, extras []int) {
	inst.curSection = name
	inst.curExtras = extras
	inst.curAttr = 0
}

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
	inst.attrItems = inst.attrItems[:0]
	inst.path, inst.params, inst.hasPath, inst.hasParams = "", "", false, false
}

// flushAttr appends the current attribute's K = max(item count, 1) rows:
// row j takes each value column's j-th item (null where that column has
// fewer); row 0 additionally carries the revealed membership/support
// columns, read raw at this attribute's index. The per-column item slices
// are then recycled.
func (inst *attrExplodeSink) flushAttr() {
	if inst.srcB == nil {
		return
	}
	k := 1
	for _, items := range inst.attrItems {
		if len(items) > k {
			k = len(items)
		}
	}
	for j := 0; j < k; j++ {
		inst.srcB.Append(inst.pageStart + int64(inst.entityIdx))
		inst.secB.Append(inst.curSection)
		inst.attrB.Append(int32(inst.curAttr))
		if inst.hasPath {
			inst.pathB.Append(inst.path)
		} else {
			inst.pathB.AppendNull()
		}
		if inst.hasParams {
			inst.paramsB.Append(inst.params)
		} else {
			inst.paramsB.AppendNull()
		}
		set := make([]bool, inst.nCols)
		for e, arrowIdx := range inst.attrColIdx {
			items := inst.attrItems[e]
			if j < len(items) {
				if p := inst.visPos(arrowIdx); p >= 0 {
					inst.cellB[p].Append(items[j])
					set[p] = true
				}
			}
		}
		if j == 0 {
			for _, ax := range inst.curExtras {
				p := inst.visPos(ax)
				if p < 0 || set[p] {
					continue
				}
				if v := listInnerScalar(inst.rec, ax, inst.entityIdx, inst.curAttr); v != "" {
					inst.cellB[p].Append(v)
					set[p] = true
				}
			}
		}
		for p, ok := range set {
			if !ok {
				inst.cellB[p].AppendNull()
			}
		}
		inst.rows++
	}
	for _, items := range inst.attrItems {
		inst.itemsFree = append(inst.itemsFree, items[:0])
	}
	inst.attrColIdx = inst.attrColIdx[:0]
	inst.attrItems = inst.attrItems[:0]
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
	inst.attrItems = append(inst.attrItems, items)
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

func (inst *attrExplodeSink) BeginScalarValue()             { inst.isCollection = false }
func (inst *attrExplodeSink) EndScalarValue() error         { return inst.err }
func (inst *attrExplodeSink) BeginHomogenousArrayValue(int) { inst.isCollection = true }
func (inst *attrExplodeSink) EndHomogenousArrayValue()      {}
func (inst *attrExplodeSink) BeginSetValue(int)             { inst.isCollection = true }
func (inst *attrExplodeSink) EndSetValue()                  {}

// BeginValueItem starts a fresh item buffer — items become separate rows.
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

// --- memberships: the attribute's path and parameters ---

func (inst *attrExplodeSink) BeginTags(int) {}
func (inst *attrExplodeSink) EndTags()      {}

// The first low-cardinality membership is the attribute's path; a
// high-cardinality one, or a parametrized one's parameters, its parameters.
// A ref is spelled as the record card spells it, "#" and the number.
func (inst *attrExplodeSink) setPath(p string) {
	if !inst.hasPath {
		inst.path, inst.hasPath = p, true
	}
}
func (inst *attrExplodeSink) setParams(p string) {
	if !inst.hasParams && p != "" {
		inst.params, inst.hasParams = p, true
	}
}
func (inst *attrExplodeSink) AddMembershipRef(lowCard bool, ref uint64) {
	if lowCard {
		inst.setPath("#" + strconv.FormatUint(ref, 10))
	} else {
		inst.setParams("#" + strconv.FormatUint(ref, 10))
	}
}
func (inst *attrExplodeSink) AddMembershipVerbatim(lowCard bool, verbatim string) {
	verbatim = utfsafe.EnsureUTF8(verbatim)
	if lowCard {
		inst.setPath(verbatim)
	} else {
		inst.setParams(verbatim)
	}
}
func (inst *attrExplodeSink) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	inst.AddMembershipRef(lowCard, ref)
	inst.setParams(utfsafe.EnsureUTF8(params))
}
func (inst *attrExplodeSink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.AddMembershipRefParametrized(true, ref, params)
}
func (inst *attrExplodeSink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.AddMembershipVerbatim(true, verbatim)
	inst.setParams(utfsafe.EnsureUTF8(params))
}

// listInnerScalar returns the k-th inner value of a List column for one entity
// row, formatted as its per-DB-row cell would be (empty when absent). A non-list
// (scalar/backbone) column has one value, treated as attribute 0.
func listInnerScalar(rec arrow.RecordBatch, arrowIdx int, entityRow int, k int) string {
	if rec == nil || arrowIdx < 0 || arrowIdx >= int(rec.NumCols()) || entityRow < 0 || k < 0 {
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

// --- the path level's segmenter ---

// attrPathSegments reads a membership path by the jsonshred grammar: a JSON
// Pointer split on "/", each step's "~0" / "~1" / "~2" escapes undone, a
// "~n" number marker dropped, and each "_" step replaced by the next of the
// attribute's parameters, spelled "[i]". The parameters are read as the
// writer spelled them, a list of indices in order, whatever joins them. An
// empty path is the level's null group.
func attrPathSegments(path, params string) (segs []recordtable.Key) {
	path = strings.TrimSuffix(strings.TrimSpace(path), jsonshred.NumberSuffix)
	if path == "" {
		return nil
	}
	var ps []string
	if params != "" {
		ps = strings.FieldsFunc(params, func(r rune) bool {
			return r == ',' || r == ' ' || r == '/' || r == '[' || r == ']' || r == '\t'
		})
	}
	for _, step := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if step == jsonshred.Placeholder {
			idx := jsonshred.Placeholder
			if len(ps) > 0 {
				idx, ps = ps[0], ps[1:]
			}
			segs = append(segs, recordtable.Key{Value: idx, Label: "[" + idx + "]"})
			continue
		}
		segs = append(segs, recordtable.Key{Value: jsonshred.UnescapeKey(step)})
	}
	return
}

// --- rendering ---

// attrExplodedView is the exploded record the grid draws, and what it was
// built from, so it is rebuilt only when that changes.
type attrExplodedView struct {
	rec     arrow.RecordBatch
	schema  *arrow.Schema
	source  arrow.RecordBatch
	start   int64
	end     int64
	visCols []int
	opts    tableDisplayOpts
}

// renderAttrTable drives the current page through the CardDriver's Driver
// into an attrExplodeSink and draws the exploded record grouped by entity and
// path. Falls back to the per-DB-row grid when the Driver or classification
// is unavailable.
func (inst *PlayApp) renderAttrTable(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, selectedRow int64, emit SignalEmitterI) {
	inst.cards.EnsureFor(schema)
	driver := inst.cards.Driver()
	classes := inst.cards.ColumnClasses()
	if driver == nil || classes == nil {
		inst.renderMasterTable(rec, schema, numRows, selectedRow, emit)
		return
	}

	totalRows := min(rec.NumRows(), numRows)
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

	visCols := inst.visibleTableCols(rec, schema, pageStart, pageEnd)
	inst.tableDrawn.visCols = visCols
	v := &inst.attrView
	if v.rec == nil || v.source != rec || v.start != pageStart || v.end != pageEnd || !slices.Equal(v.visCols, visCols) || v.opts != inst.tableOpts {
		xrec, err := inst.explodeAttrPage(rec, schema, classes, driver, pageStart, pageEnd, visCols)
		if err != nil {
			log.Warn().Err(err).Msg("play: per-attribute drive failed — falling back to per-DB-row grid")
			inst.renderMasterTable(rec, schema, numRows, selectedRow, emit)
			return
		}
		if v.rec != nil {
			v.rec.Release()
		}
		*v = attrExplodedView{rec: xrec, schema: xrec.Schema(), source: rec, start: pageStart, end: pageEnd,
			visCols: append([]int(nil), visCols...), opts: inst.tableOpts}
		// The levels read the exploded record, so they are set with it —
		// and only then: a SetLevels repartitions. The expansion, keyed by
		// path, carries over to the new record.
		inst.attrGroups.SetLevels([]recordtable.GroupLevel{
			{Arrow: attrXSrc, Key: attrEntityKeyOf(xrec), KeyIsRow: true, Order: recordtable.GroupOrder{Appearance: true}},
			{Segments: attrSegmentsOf(xrec)},
		})
	}
	inst.renderAttrExplodeGrid(schema, visCols, v, selectedRow, emit)
}

// explodeAttrPage builds the exploded record of one page.
func (inst *PlayApp) explodeAttrPage(rec arrow.RecordBatch, schema *arrow.Schema, classes []streamreadaccess.ColumnClass, driver *streamreadaccess.Driver, pageStart, pageEnd int64, visCols []int) (xrec arrow.RecordBatch, err error) {
	slice := rec.NewSlice(pageStart, pageEnd)
	defer slice.Release()
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
	if err = driver.DriveRecordBatch(sink, slice); err != nil {
		return
	}
	xrec = sink.record(attrExplodedSchema(schema, visCols))
	return
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

// attrGridColumns lays the exploded record out: the section, the path and
// the parameters, then the visible columns under the per-DB-row grid's
// captions. A cell's width identity keeps the source column's type with the
// per-attribute view tag, so a width stored before the exploded record
// carries over.
func (inst *PlayApp) attrGridColumns(schema *arrow.Schema, visCols []int, xrec arrow.RecordBatch) (cols []recordtable.Column) {
	cols = make([]recordtable.Column, 0, attrXCells-attrXSection+len(visCols))
	cols = append(cols,
		recordtable.Column{Arrow: attrXSection, Header: "section", Hover: "the attribute's section", NoSort: true},
		recordtable.Column{Arrow: attrXPath, Header: "path", Hover: "the attribute's membership path, as the grouping reads it", NoSort: true},
		recordtable.Column{Arrow: attrXParams, Header: "params", Hover: "the membership's high-cardinality parameters", NoSort: true},
	)
	inst.ensureColLabels(schema)
	glossCols := inst.glossColumns(schema)
	raw := inst.tableOpts.rawCells
	for pos, arrowCol := range visCols {
		field := schema.Field(arrowCol)
		gc := &glossCols[arrowCol]
		col := recordtable.Column{Arrow: attrXCells + pos, Header: field.Name, WidthType: field.Type.String(), NoSort: true}
		if label := inst.colLabels[field.Name]; label != "" {
			col.Header = label
		}
		col.Hover = field.Name + " — " + field.Type.String()
		if gh := glossHover(gc); gh != "" {
			col.Hover += " — " + gh
		}
		col.Sub = []string{shortArrowType(field.Type)}
		if gc.mediaType != "" {
			col.Sub = append(col.Sub, gc.mediaType)
		}
		leftAlign := recordtable.StringLike(listElemType(field.Type))
		xcol := xrec.Column(attrXCells + pos).(*array.String)
		linkable := gc.mediaType == gloss.MediaTypeURL && gc.glossedElem() && !raw
		col.Cell = func(row int64) (cell recordtable.Cell) {
			if xcol.IsNull(int(row)) {
				return recordtable.Cell{LeftAlign: leftAlign}
			}
			text := xcol.Value(int(row))
			cell = recordtable.Cell{Text: text, LeftAlign: leftAlign}
			if linkable {
				cell.Link = text
			}
			return
		}
		cols = append(cols, col)
	}
	return
}

// attrSegmentsOf is the path level's segmenter over an exploded record.
func attrSegmentsOf(xrec arrow.RecordBatch) func(row int64) []recordtable.Key {
	paths := xrec.Column(attrXPath).(*array.String)
	params := xrec.Column(attrXParams).(*array.String)
	return func(row int64) []recordtable.Key {
		if paths.IsNull(int(row)) {
			return nil
		}
		p := ""
		if !params.IsNull(int(row)) {
			p = params.Value(int(row))
		}
		return attrPathSegments(paths.Value(int(row)), p)
	}
}

// attrEntityKeyOf keys the entity level on the source row (KeyIsRow), the
// band captioned by the row's number as the per-DB-row gutter shows it.
func attrEntityKeyOf(xrec arrow.RecordBatch) func(row int64) recordtable.Key {
	src := xrec.Column(attrXSrc).(*array.Int64)
	return func(row int64) recordtable.Key {
		s := src.Value(int(row))
		return recordtable.Key{Value: strconv.FormatInt(s, 10), Label: "row " + strconv.FormatInt(s+1, 10)}
	}
}

// renderAttrExplodeGrid draws the exploded record through RenderGrid with the
// two fixed levels. A leaf click and a band click both select the source
// row; the fold gestures go through set_table_options as the per-DB-row
// grid's do; a header click has nothing to sort.
func (inst *PlayApp) renderAttrExplodeGrid(schema *arrow.Schema, visCols []int, v *attrExplodedView, selectedRow int64, emit SignalEmitterI) {
	xrec := v.rec
	cols := inst.attrGridColumns(schema, visCols, xrec)
	refit := inst.attrColsChanged(schema, visCols)
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: attrTableTag, Record: xrec, Columns: cols,
		Widths: inst.colWidthRes, TableTag: attrTableTag, WidthView: tableViewTagAttr,
		NoRowNumbers: true, Refit: refit,
		Groups: &inst.attrGroups, SelectedKeyRow: selectedRow, HasSelectedKeyRow: selectedRow >= 0,
		CellsKey: attrCellsKey{rec: xrec},
		State:    &inst.attrGrid,
	})
	if res.Err != nil {
		log.Warn().Err(res.Err).Msg("play: per-attribute grid refused its input")
		return
	}
	if res.WidthsErr != nil {
		log.Warn().Err(res.WidthsErr).Msg("play: storing column widths failed; will retry")
	}
	inst.tableDrawn.lines = inst.attrGroups.Layout().Len()
	inst.routeAttrGroupGestures(res)
	if res.Clicked >= 0 {
		emit.Emit(signalSelection, xrec.Column(attrXSrc).(*array.Int64).Value(int(res.Clicked)))
	}
	if res.GroupClicked >= 0 {
		emit.Emit(signalSelection, res.GroupClicked)
	}
}

// attrCellsKey names what the exploded grid's cells read that the grid
// cannot see: the exploded record, which is rebuilt whenever the page, the
// columns or the glosses change.
type attrCellsKey struct {
	rec arrow.RecordBatch
}

// routeAttrGroupGestures folds a group through set_table_options, as the
// per-DB-row grid does; the levels are fixed, so a header has no level to
// reverse.
func (inst *PlayApp) routeAttrGroupGestures(res recordtable.Result) {
	lay := inst.attrGroups.Layout()
	if g := res.GroupToggled; g >= 0 && int(g) < len(lay.Groups) {
		path := tableGroupPath(lay, g)
		if lay.Groups[g].Open {
			inst.requestTableOptions(SetTableOptionsArgs{Collapse: &path})
		} else {
			inst.requestTableOptions(SetTableOptionsArgs{Expand: &path})
		}
	}
}

// attrTableTag is the stable instance-tier scope for the attr grid's
// column-width overrides, and the grid's scope key. One constant so the two
// cannot drift apart.
const attrTableTag = "attr-results"

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
		MaxPoints: float64(recordtable.MaxColumnWidth),
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

// attrColumnKeys is the resolver's column identities for the attr grid, as
// RenderGrid builds them: the fixed columns, then the visible data columns
// under the source field's name and type, every type carrying
// tableViewTagAttr so this grid's widths never reach the per-DB-row grid
// through the column tier — the two show the same column's contents
// differently and a width fitted to one does not fit the other.
func (inst *PlayApp) attrColumnKeys(schema *arrow.Schema, visCols []int) (cols []colwidth.Column) {
	xs := attrExplodedSchema(schema, visCols)
	cols = recordtable.WidthColumns(xs, inst.attrGridColumnsForKeys(schema, visCols), tableViewTagAttr, false)
	return
}

// attrGridColumnsForKeys is attrGridColumns without a record: the column
// identities alone.
func (inst *PlayApp) attrGridColumnsForKeys(schema *arrow.Schema, visCols []int) (cols []recordtable.Column) {
	cols = append(cols,
		recordtable.Column{Arrow: attrXSection, Header: "section"},
		recordtable.Column{Arrow: attrXPath, Header: "path"},
		recordtable.Column{Arrow: attrXParams, Header: "params"},
	)
	for pos, arrowCol := range visCols {
		cols = append(cols, recordtable.Column{Arrow: attrXCells + pos, WidthType: schema.Field(arrowCol).Type.String()})
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
