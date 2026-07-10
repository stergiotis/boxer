package play

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// CardDriver bridges the current Arrow schema to the leeway
// streamreadaccess.Driver + a Table2CardEmitter.
//
// It is also the play app's single leeway-schema reconstruction point: the
// leeway physical column names carry the whole authored structure (sections,
// membership roles, co-section groups, canonical types, encoding hints), and
// EnsureFor recovers the [common.TableDesc] from them via
// DiscoverTableFromColumnNames. That TableDesc is exposed through [TableDesc]
// so schema-only consumers (the Schema pane) share this one derivation instead
// of re-running discovery — the Driver they don't need is built once anyway for
// the Detail card.
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
	usable  bool              // false if the schema is not leeway-shaped
	table   *common.TableDesc // reconstructed leeway schema, nil when not leeway-shaped
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
		inst.table = nil
		return false
	}
	// Pointer-identity cache (same idiom as the Projector's forSchema and
	// syncSchemaModel): once a schema has been probed, return the cached
	// verdict. Caching the *negative* result is the point — a non-leeway
	// schema leaves driver nil, and EnsureFor runs every frame from the
	// Detail tab, so gating this on driver != nil would re-run discovery and
	// re-log the fallback on every frame.
	if schema == inst.schema {
		return inst.usable
	}
	inst.schema = schema
	inst.driver = nil
	inst.emitter = nil
	inst.usable = false
	inst.table = nil

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
		// A non-leeway result (aggregation, join, arbitrary SQL) is an
		// expected, fully-supported case — the caller renders the ad-hoc
		// detail view. Debug, not Warn: a normal fallback, not a fault. The
		// pointer cache above means this logs at most once per result schema.
		log.Debug().Err(err).Msg("play: result not leeway-shaped — using ad-hoc view")
		return false
	}
	// Publish the reconstructed schema now, before the (heavier, and card-only)
	// Driver construction: the Schema pane wants the TableDesc even on a schema
	// where the Driver build later fails.
	inst.table = &tblDesc
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

// TableDesc returns the leeway schema reconstructed from the current result's
// physical column names, or nil when the schema is not leeway-shaped. This is
// the play app's single leeway-schema derivation — the Schema pane renders it
// rather than re-running discovery. EnsureFor must have been called for the
// current schema first (it is, every frame, via the Detail card).
func (inst *CardDriver) TableDesc() *common.TableDesc {
	return inst.table
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
