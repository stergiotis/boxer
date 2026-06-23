package play

import (
	"bytes"
	"encoding/json/jsontext"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// CardDriver bridges the current Arrow schema to the leeway
// streamreadaccess.Driver + a Table2CardEmitter.
//
// Two-stage caching:
//   - The Driver + TableDesc are rebuilt only when the Arrow schema object
//     changes (cheap pointer compare).
//   - Each Render call walks a single-row slice of the record batch,
//     producing the same Begin*/End* sequence the HtmlCardEmitter consumes,
//     but emitting ImZero2 widgets through the Table2CardEmitter.
type CardDriver struct {
	alloc memory.Allocator
	ids   *c.WidgetIdStack

	// Cached per-schema.
	schema  *arrow.Schema
	driver  *streamreadaccess.Driver
	emitter *leewaywidgets.Table2CardEmitter
	usable  bool // false if the schema is not leeway-shaped

	// JSON-view cache (ADR-0018 canonical card-JSON). Re-uses the same Driver
	// as Render but feeds a JsonCardEmitter. Single-slot keyed by (rec, row);
	// navigating rows or running a new query drops the slot. Holders are
	// interned via unique.Make, so overwriting the slot doesn't leak.
	jsonBuf        *bytes.Buffer
	jsonCacheRec   arrow.RecordBatch
	jsonCacheRow   int64
	jsonCacheView  typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	jsonCacheValid bool
}

// NewCardDriver returns an empty driver. EnsureFor must be called before the
// first Render.
func NewCardDriver(ids *c.WidgetIdStack, alloc memory.Allocator) *CardDriver {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	return &CardDriver{alloc: alloc, ids: ids}
}

// EnsureFor (re)builds the driver if the schema changed. Returns true iff
// the schema is leeway-shaped and Render can proceed.
func (inst *CardDriver) EnsureFor(schema *arrow.Schema) bool {
	if schema == nil {
		inst.schema = nil
		inst.driver = nil
		inst.emitter = nil
		inst.usable = false
		inst.invalidateJSON()
		return false
	}
	if schema == inst.schema && inst.driver != nil {
		return inst.usable
	}
	inst.schema = schema
	inst.driver = nil
	inst.emitter = nil
	inst.usable = false
	inst.invalidateJSON()

	nFields := schema.NumFields()
	colNames := make([]string, 0, nFields)
	for i := 0; i < nFields; i++ {
		colNames = append(colNames, schema.Field(i).Name)
	}
	// Pick the naming-convention separator from the schema. The leeway
	// flat-name format spells nested tags as `<head><sep><tag>` (e.g.
	// `metric:env`); the canonical separator is `:`, but ClickHouse
	// table dumps mangle it to `_` because `:` is illegal in CH
	// column identifiers. The first non-leading-underscore column
	// settles the question: a `:` anywhere in the name picks the
	// canonical convention, otherwise the CH-mangled fallback `_`.
	//
	// Columns whose name starts with `_` are reserved for later /
	// implicit / opaque schema columns that aren't authored under
	// either convention, so they can't be used as evidence either way
	// — we skip them and look at the next column.
	sep := "_"
	for _, n := range colNames {
		if strings.HasPrefix(n, "_") {
			continue
		}
		if strings.ContainsRune(n, ':') {
			sep = ":"
		}
		break
	}
	conv, err := ddl.NewHumanReadableNamingConvention(sep)
	if err != nil {
		return false
	}
	tblDesc, tableRowConfig, err := conv.DiscoverTableFromColumnNames(colNames)
	if err != nil {
		log.Warn().Err(err).Msg("play: leeway discovery failed — falling back")
		return false
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	if err != nil {
		log.Warn().Err(err).Msg("play: ir load failed — falling back")
		return false
	}
	driver, err := streamreadaccess.NewDriverFromSchema(
		&tblDesc, ir,
		streamreadaccess.DefaultFormatters(),
		schema, conv, tableRowConfig)
	if err != nil {
		log.Warn().Err(err).Msg("play: driver construction failed — falling back")
		return false
	}
	inst.driver = driver
	inst.emitter = leewaywidgets.NewTable2CardEmitter(inst.ids, leewaywidgets.ColorPaletteViridis, nil)
	inst.usable = true
	return true
}

// Driver returns the underlying leeway streamreadaccess.Driver iff the schema
// is leeway-shaped (EnsureFor returned true). Otherwise nil. Used by the
// Projector to drive a FeatureExtractor over the same record batch the card
// view consumes, so we don't pay for a second schema-discovery round.
func (inst *CardDriver) Driver() *streamreadaccess.Driver {
	if !inst.usable {
		return nil
	}
	return inst.driver
}

// SetTagClickHandler wires a clipboard / filter pivot callback through to the
// emitter. Passing nil clears it. Note: Table2CardEmitter renders chips as
// comma-joined strings, so the callback never fires in practice — kept for
// API parity with the older two-emitter model.
func (inst *CardDriver) SetTagClickHandler(fn func(display, detail string)) {
	if inst.emitter != nil {
		inst.emitter.OnTagClicked = fn
	}
}

// Render walks a single-row slice of rec through the Driver, which drives
// the Table2CardEmitter. The emitter pushes ImZero2 widgets into the current
// ui scope — call this inside a ScrollArea or Vertical container.
func (inst *CardDriver) Render(rec arrow.RecordBatch, row int64) error {
	if !inst.usable || inst.driver == nil {
		return nil
	}
	if rec == nil || row < 0 || row >= rec.NumRows() {
		return nil
	}
	if inst.emitter == nil {
		return nil
	}
	slice := rec.NewSlice(row, row+1)
	defer slice.Release()
	err := inst.driver.DriveRecordBatch(inst.emitter, slice)
	if err != nil {
		log.Warn().Err(err).Int64("row", row).Msg("play: driver error")
		return eh.Errorf("unable to drive record batch: %w", err)
	}
	return nil
}

// JSONFor returns a syntax-highlighted CodeViewJob holder for the canonical
// Leeway card-JSON of (rec, row), per ADR-0018. The retained holder is cached
// for the most-recently requested (rec, row); navigating rows or running a new
// query invalidates the slot. ok=false with err=nil means "not applicable"
// (driver not usable, nil rec, or out-of-range row) — caller should skip
// rendering. ok=false with err!=nil means encoding failed.
func (inst *CardDriver) JSONFor(rec arrow.RecordBatch, row int64) (view typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool, err error) {
	if !inst.usable || inst.driver == nil {
		return
	}
	if rec == nil || row < 0 || row >= rec.NumRows() {
		return
	}
	if inst.jsonCacheValid && inst.jsonCacheRec == rec && inst.jsonCacheRow == row {
		view = inst.jsonCacheView
		ok = true
		return
	}
	if inst.jsonBuf == nil {
		inst.jsonBuf = bytes.NewBuffer(make([]byte, 0, 4096))
	} else {
		inst.jsonBuf.Reset()
	}
	enc := jsontext.NewEncoder(inst.jsonBuf,
		jsontext.Multiline(true),
		jsontext.WithIndent("  "))
	sink := card.NewJsonCardEmitter(enc, nil)
	slice := rec.NewSlice(row, row+1)
	defer slice.Release()
	err = inst.driver.DriveRecordBatch(sink, slice)
	if err != nil {
		log.Warn().Err(err).Int64("row", row).Msg("play: json driver error")
		err = eh.Errorf("unable to drive record batch for json: %w", err)
		return
	}
	view = codeview.PrepareJson(inst.jsonBuf.String())
	inst.jsonCacheRec = rec
	inst.jsonCacheRow = row
	inst.jsonCacheView = view
	inst.jsonCacheValid = true
	ok = true
	return
}

// invalidateJSON drops the (rec, row) cache slot. Called whenever the driver
// is rebuilt — old rows belong to a different schema and must not be served.
func (inst *CardDriver) invalidateJSON() {
	inst.jsonCacheValid = false
	inst.jsonCacheRec = nil
	inst.jsonCacheView = typed.RetainedFffiHolderTyped[c.CodeViewJobS]{}
}
