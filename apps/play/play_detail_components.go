package play

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
)

// play_detail_components.go is the Detail pane's typed per-component report
// (ADR-0075's play consumer): above the generic leeway card, the components
// the selected row carries — of the kinds play registers (RegisterComponents /
// playComponentStores) — each as its own collapsible panel, detected and
// decoded off the wire through the component read contract (ADR-0146).
//
// The report is a complement, not a replacement. It shows only what a bound
// kind claims; every attribute no kind claims stays visible in the card below,
// and a row carrying none of play's kinds renders no report at all. Nothing
// here infers structure: the row is read through the generated `boxer.facts`
// read access, aligned to the result's schema by physical column name, and a
// result that does not carry every facts column is simply not a facts row.
//
// Cost model: the read access is loaded once per record (it retains the
// record's arrays), components are decoded once per (record, row) — the
// reflect decode is per selected row, never per frame — and the report is
// drawn from the cached slice each frame.

// componentDetail owns the report's state. One per PlayApp.
type componentDetail struct {
	ids    *c.WidgetIdStack
	reg    *componentview.Registry
	disp   *componentview.Dispatcher
	stores []componentStore
	fields fieldview.Renderer

	// physical is the facts table's physical column name at every position of
	// the generated read access's default column-index space, and defaults
	// that space as a fresh reader reports it. Together they say which result
	// column each reader slot must bind to.
	physical []string
	defaults []uint32
	conv     common.NamingConventionFwdI

	// buildErr is why the report is unavailable for the whole session (a
	// binding or the facts schema failed to build); shown once, in the pane.
	buildErr error

	// Per-schema cache: the reader slot → result column mapping, nil when the
	// result is not facts-shaped (schemaErr says which column is missing).
	schema    *arrow.Schema
	indices   []uint32
	schemaErr error

	// Per-record cache: the loaded read access and the section readers over it.
	rec     arrow.RecordBatch
	access  *ra.ReadAccessFacts
	readers *marshallreflect.SectionReaders

	// Per-row cache.
	row    int64
	comps  []componentview.Component
	rowErr error
}

// newComponentDetail builds the report's binders, renderers and facts column
// map. A failure leaves buildErr set and the report inert, never a panic: the
// Detail pane's card does not depend on any of this.
func newComponentDetail(ids *c.WidgetIdStack) (inst *componentDetail) {
	inst = &componentDetail{
		ids:    ids,
		reg:    componentview.NewRegistry(),
		fields: fieldview.New(ids, "cd").ShowKind(false).DefaultOpen(true),
		row:    -1,
	}
	inst.disp = componentview.NewDispatcher(inst.reg)
	inst.disp.DefaultOpen = true
	inst.buildErr = inst.build()
	if inst.buildErr != nil {
		log.Warn().Err(inst.buildErr).Msg("play: component report unavailable")
	}
	return
}

func (inst *componentDetail) build() (err error) {
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		return eh.Errorf("naming convention: %w", err)
	}
	inst.conv = conv
	inst.physical, err = factsPhysicalColumns(conv)
	if err != nil {
		return eh.Errorf("facts physical columns: %w", err)
	}
	inst.defaults = ra.NewReadAccessFacts().GetColumnIndices()
	for _, d := range inst.defaults {
		if int(d) >= len(inst.physical) {
			return eb.Build().Uint32("index", d).Int("columns", len(inst.physical)).
				Errorf("facts read access binds a column the facts schema does not have")
		}
	}
	inst.stores = playComponentStores()
	for i := range inst.stores {
		st := &inst.stores[i]
		lookup, lerr := storeLookup(st.name, st.ids)
		if lerr != nil {
			return lerr
		}
		st.binder = componentview.NewBinder()
		st.binder.Lookup = lookup
		if err = st.bind(st.binder); err != nil {
			return eb.Build().Str("store", st.name).Errorf("bind components: %w", err)
		}
		for _, b := range st.binder.Bindings() {
			inst.reg.Register(&dtoRenderer{kind: b.Kind(), fields: inst.fields, state: &fieldview.State{}})
		}
	}
	return
}

// factsPhysicalColumns walks the facts schema's IR exactly as the read-access
// generator did and returns the physical column name at every index of the
// generated column-index space (cc.IndexOffset + j for name j of a column
// group), which is also the DDL's column order.
func factsPhysicalColumns(conv common.NamingConventionFwdI) (names []string, err error) {
	manip, err := factsschema.GetSchemaInManipulator()
	if err != nil {
		return
	}
	tbl, err := manip.BuildTableDesc()
	if err != nil {
		return
	}
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()); err != nil {
		return
	}
	var buf []common.PhysicalColumnDesc
	for cc, cp := range ir.IterateColumnProps() {
		buf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, buf[:0], factsschema.TableRowConfig)
		if err != nil {
			return
		}
		for j := range cp.Names {
			if j >= len(buf) {
				err = eb.Build().Str("section", cc.SectionName.String()).Int("name", j).
					Errorf("physical column mapping is not 1:1")
				return
			}
			idx := int(cc.IndexOffset) + j
			for len(names) <= idx {
				names = append(names, "")
			}
			names[idx] = buf[j].String()
		}
	}
	return
}

// ensureSchema maps every reader slot to a result column by physical name
// (raw or canonicalised, as the leeway drivers resolve). A result missing any
// facts column is not facts-shaped; the verdict is cached per schema pointer
// like the CardDriver's, so a negative answer costs nothing per frame.
func (inst *componentDetail) ensureSchema(schema *arrow.Schema) bool {
	if schema == inst.schema {
		return inst.indices != nil
	}
	inst.dropRecord()
	inst.schema = schema
	inst.indices = nil
	inst.schemaErr = nil
	if schema == nil || inst.buildErr != nil {
		return false
	}
	nFields := schema.NumFields()
	nameToIdx := make(map[string]int, nFields*2)
	for i := range nFields {
		n := schema.Field(i).Name
		nameToIdx[n] = i
		if canon := inst.conv.CanonicalizeSchemaName(n); canon != n {
			nameToIdx[canon] = i
		}
	}
	idx := make([]uint32, len(inst.defaults))
	for k, d := range inst.defaults {
		name := inst.physical[d]
		i, ok := nameToIdx[name]
		if !ok {
			inst.schemaErr = eb.Build().Str("column", name).Errorf("result lacks a facts column")
			log.Debug().Str("column", name).Msg("play: result is not facts-shaped — no component report")
			return false
		}
		idx[k] = uint32(i)
	}
	inst.indices = idx
	return true
}

// ensureRecord loads the read access over rec, once per record.
func (inst *componentDetail) ensureRecord(rec arrow.RecordBatch) bool {
	if rec == inst.rec {
		return inst.readers != nil
	}
	inst.dropRecord()
	inst.rec = rec
	if rec == nil || inst.indices == nil {
		return false
	}
	access := ra.NewReadAccessFacts()
	access.SetColumnIndices(inst.indices)
	if err := access.LoadFromRecord(rec); err != nil {
		access.Release()
		inst.rowErr = eh.Errorf("load facts read access: %w", err)
		log.Debug().Err(err).Msg("play: facts read access refused the record — no component report")
		return false
	}
	inst.access = access
	inst.readers = factsSectionReaders(access)
	return true
}

// factsSectionReaders binds every facts section under its lw: section name,
// so any DTO over the facts schema finds the readers its plan declares.
func factsSectionReaders(a *ra.ReadAccessFacts) *marshallreflect.SectionReaders {
	return marshallreflect.NewSectionReaders(a.EntityId.ValueId.Len()).
		PlainColumn("id", a.EntityId.ValueId).
		PlainColumn("naturalKey", a.EntityId.ValueNaturalKey).
		PlainColumn("ts", a.EntityTimestamp.ValueTs).
		PlainColumn("expiresAt", a.EntityLifecycle.ValueExpiresAt).
		Section("foreignKey", a.ForeignKey.GetAttributes(), a.ForeignKey.GetMemberships()).
		Section("textArray", a.TextArray.GetAttributes(), a.TextArray.GetMemberships()).
		Section("stringArray", a.StringArray.GetAttributes(), a.StringArray.GetMemberships()).
		Section("symbol", a.Symbol.GetAttributes(), a.Symbol.GetMemberships()).
		Section("symbolArray", a.SymbolArray.GetAttributes(), a.SymbolArray.GetMemberships()).
		Section("blobArray", a.BlobArray.GetAttributes(), a.BlobArray.GetMemberships()).
		Section("u8Array", a.U8Array.GetAttributes(), a.U8Array.GetMemberships()).
		Section("u16Array", a.U16Array.GetAttributes(), a.U16Array.GetMemberships()).
		Section("u32Array", a.U32Array.GetAttributes(), a.U32Array.GetMemberships()).
		Section("u32Set", a.U32Set.GetAttributes(), a.U32Set.GetMemberships()).
		Section("u64Array", a.U64Array.GetAttributes(), a.U64Array.GetMemberships()).
		Section("u64Set", a.U64Set.GetAttributes(), a.U64Set.GetMemberships()).
		Section("i8Array", a.I8Array.GetAttributes(), a.I8Array.GetMemberships()).
		Section("i16Array", a.I16Array.GetAttributes(), a.I16Array.GetMemberships()).
		Section("i32Array", a.I32Array.GetAttributes(), a.I32Array.GetMemberships()).
		Section("i64Array", a.I64Array.GetAttributes(), a.I64Array.GetMemberships()).
		Section("f32Array", a.F32Array.GetAttributes(), a.F32Array.GetMemberships()).
		Section("f64Array", a.F64Array.GetAttributes(), a.F64Array.GetMemberships()).
		Section("u32Range", a.U32Range.GetAttributes(), a.U32Range.GetMemberships()).
		Section("timeArray", a.TimeArray.GetAttributes(), a.TimeArray.GetMemberships()).
		Section("bool", a.Bool.GetAttributes(), a.Bool.GetMemberships())
}

func (inst *componentDetail) dropRecord() {
	if inst.access != nil {
		inst.access.Release()
	}
	inst.access = nil
	inst.readers = nil
	inst.rec = nil
	inst.row = -1
	inst.comps = nil
	inst.rowErr = nil
}

// release drops the retained record arrays. Called from PlayApp.Close.
func (inst *componentDetail) release() {
	if inst == nil {
		return
	}
	inst.dropRecord()
	inst.schema = nil
	inst.indices = nil
}

// componentsFor returns the components row of rec carries, of play's kinds,
// decoded once per (rec, row). A row that carries a kind but does not conform
// to it (ADR-0146 D4) is an error, not an omission: the report would misstate
// the archetype without it.
func (inst *componentDetail) componentsFor(rec arrow.RecordBatch, row int64) (comps []componentview.Component, err error) {
	if inst.buildErr != nil {
		return nil, inst.buildErr
	}
	if rec == nil || !inst.ensureSchema(rec.Schema()) {
		return nil, nil
	}
	if !inst.ensureRecord(rec) {
		return nil, inst.rowErr
	}
	if row == inst.row {
		return inst.comps, inst.rowErr
	}
	inst.row = row
	inst.comps = nil
	inst.rowErr = nil
	if row < 0 || row >= rec.NumRows() {
		return nil, nil
	}
	for i := range inst.stores {
		st := &inst.stores[i]
		var got []componentview.Component
		got, err = st.binder.Components(inst.readers, int(row))
		if err != nil {
			inst.rowErr = eb.Build().Str("store", st.name).Errorf("%w", err)
			return nil, inst.rowErr
		}
		inst.comps = append(inst.comps, got...)
	}
	return inst.comps, nil
}

// render draws the report for row of rec into the current scope and reports
// whether it drew anything, so the caller can place a separator only when
// there is something to separate. Absent kinds are not listed: play binds
// twenty, and a row carries one or two.
func (inst *componentDetail) render(rec arrow.RecordBatch, row int64) (shown bool) {
	if inst == nil {
		return false
	}
	comps, err := inst.componentsFor(rec, row)
	if err != nil {
		if inst.buildErr == nil && inst.readers == nil {
			// The record refused the read access: the card still renders,
			// and the reason is a debug line, not a pane-wide notice.
			return false
		}
		for rt := range c.RichTextLabel("components: " + err.Error()) {
			rt.Weak().Small()
		}
		return true
	}
	if len(comps) == 0 {
		return false
	}
	for rt := range c.RichTextLabel(fmt.Sprintf("components · %d", len(comps))) {
		rt.Weak().Small()
	}
	inst.disp.RenderReport(inst.ids, comps)
	return true
}

// dtoRenderer draws any component DTO as a field outline: one row per exported
// field, containers (slices, nested structs) collapsible beneath their name.
// It is the one renderer behind every kind play binds — a bespoke widget per
// kind (a gauge for a battery, chips for tags) is what ADR-0075's seed
// renderers show and what a fact-component may register instead, later.
type dtoRenderer struct {
	kind   componentview.ComponentKindE
	fields fieldview.Renderer
	state  *fieldview.State
}

var _ componentview.RendererI = (*dtoRenderer)(nil)

func (inst *dtoRenderer) Kind() componentview.ComponentKindE { return inst.kind }
func (inst *dtoRenderer) Title() string                      { return string(inst.kind) }

func (inst *dtoRenderer) Render(_ *c.WidgetIdStack, value any) {
	inst.fields.Render(inst.state, dtoFields(value))
}

// dtoFields projects a decoded DTO onto fieldview's typed fields. Only the
// exported fields a DTO declares are shown; the `_` kind marker is skipped, an
// absent option reads as absent (not as its zero value), and bytes are left
// to fieldview's own bounded rendering.
func dtoFields(value any) (fields []fieldview.Field) {
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return []fieldview.Field{fieldFor("value", rv, false)}
	}
	rt := rv.Type()
	fields = make([]fieldview.Field, 0, rt.NumField())
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		fv := rv.Field(i)
		if isOption(fv) {
			if !fv.FieldByName("Has").Bool() {
				fields = append(fields, fieldview.Field{Name: sf.Name, Kind: fieldview.KindString, Str: "absent"})
				continue
			}
			fv = fv.FieldByName("Val")
		}
		fields = append(fields, fieldFor(sf.Name, fv, bytesAreBlob(sf.Tag.Get("lw"))))
	}
	return
}

// bytesAreBlob says whether a []byte field is opaque bytes (a natural key, a
// blob-section value) rather than a list of small numbers. Go cannot tell a
// per-core percentage lane (`[]uint8` over u8Array) from a hash by type, but
// the lw: tag can: its second element names the section.
func bytesAreBlob(lwTag string) bool {
	if strings.HasPrefix(lwTag, ",naturalKey") {
		return true
	}
	parts := strings.Split(lwTag, ",")
	return len(parts) > 1 && parts[1] == "blobArray"
}

// isOption recognises option.Option[T] by shape — a struct with a bool Has
// and a Val — so the projection needs no import of every instantiation.
func isOption(v reflect.Value) bool {
	if v.Kind() != reflect.Struct {
		return false
	}
	has := v.FieldByName("Has")
	return has.IsValid() && has.Kind() == reflect.Bool && v.FieldByName("Val").IsValid()
}

var timeType = reflect.TypeFor[time.Time]()

func fieldFor(name string, v reflect.Value, blob bool) (f fieldview.Field) {
	f.Name = name
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			f.Kind = fieldview.KindString
			f.Str = "nil"
			return
		}
		v = v.Elem()
	}
	if v.Type() == timeType {
		f.Kind = fieldview.KindTime
		f.Time = v.Interface().(time.Time)
		return
	}
	switch v.Kind() {
	case reflect.String:
		f.Kind = fieldview.KindString
		f.Str = v.String()
	case reflect.Bool:
		f.Kind = fieldview.KindBool
		f.Bool = v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.Kind = fieldview.KindInt
		f.Int = v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		f.Kind = fieldview.KindUint
		f.Uint = v.Uint()
	case reflect.Float32, reflect.Float64:
		f.Kind = fieldview.KindFloat
		f.Float = v.Float()
	case reflect.Slice, reflect.Array:
		if blob && v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			f.Kind = fieldview.KindBytes
			f.Bytes = v.Bytes()
			return
		}
		f.Kind = fieldview.KindArray
		f.Children = make([]fieldview.Field, 0, v.Len())
		for i := range v.Len() {
			f.Children = append(f.Children, fieldFor("["+strconv.Itoa(i)+"]", v.Index(i), false))
		}
	case reflect.Struct:
		if isOption(v) {
			if !v.FieldByName("Has").Bool() {
				f.Kind = fieldview.KindString
				f.Str = "absent"
				return
			}
			return fieldFor(name, v.FieldByName("Val"), blob)
		}
		f.Kind = fieldview.KindObject
		f.Children = dtoFields(v.Interface())
	default:
		f.Kind = fieldview.KindString
		f.Str = fmt.Sprint(v.Interface())
	}
	return
}
