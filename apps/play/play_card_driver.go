package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwread"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// CardDriver bridges the current Arrow schema to the leeway
// streamreadaccess.Driver, the read model (lwread) and the Detail card
// (leewaywidgets.RecordCard).
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
//   - Each Prepare reads a single-row slice of the record batch into the
//     read model (ADR-0289 §SD3), its memberships named through
//     the session's registries, and lays it out on the card.
type CardDriver struct {
	alloc memory.Allocator
	ids   *c.WidgetIdStack

	// Cached per-schema.
	schema  *arrow.Schema
	driver  *streamreadaccess.Driver
	card    *leewaywidgets.RecordCard
	usable  bool                           // false if the schema is not leeway-shaped
	table   *common.TableDesc              // reconstructed leeway schema, nil when not leeway-shaped
	classes []streamreadaccess.ColumnClass // per-Arrow-column leeway classification, nil when not leeway-shaped
	// The recipe a second driver over the same schema needs (ADR-0219 SD1):
	// the IR the driver was built with and the classification it resolved
	// column names under. A Driver is not goroutine-safe, so a background
	// job builds its own through NewDetachedDriver rather than sharing.
	ir        *common.IntermediateTableRepresentation
	conv      common.NamingConventionFwdI
	rowConfig common.TableRowConfigE
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
		inst.card = nil
		inst.usable = false
		inst.table = nil
		inst.classes = nil
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
	inst.card = nil
	inst.usable = false
	inst.table = nil
	inst.classes = nil
	inst.ir = nil
	inst.conv = nil

	r, ok := discoverCardRecipe(schema)
	// Published before the Driver is checked: the Schema pane wants the
	// TableDesc, and the Table pane the classification, even on a schema
	// where the Driver build failed.
	inst.table, inst.classes = r.table, r.classes
	if !ok {
		return false
	}
	inst.driver, inst.ir, inst.conv, inst.rowConfig = r.driver, r.ir, r.conv, r.rowConfig
	inst.card = leewaywidgets.NewRecordCard(inst.ids, "card", leewaywidgets.ColorPaletteViridis)
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

// IR returns the intermediate representation the current driver was built
// from, or nil when the schema is not leeway-shaped. It is what a second
// sink over the same result — the canonical-form encoders (ADR-0219) —
// is constructed with, so it and the card agree on every column.
func (inst *CardDriver) IR() *common.IntermediateTableRepresentation {
	if !inst.usable {
		return nil
	}
	return inst.ir
}

// NewDetachedDriver builds a second Driver over the current schema for a
// caller that will drive it off the render thread (ADR-0219 SD7): the same
// table, IR, naming convention and row config as Driver, and nothing shared
// with it. nil when the schema is not leeway-shaped.
func (inst *CardDriver) NewDetachedDriver() (d *streamreadaccess.Driver, err error) {
	if !inst.usable {
		return nil, nil
	}
	return streamreadaccess.NewDriverFromSchema(inst.table, inst.ir, streamreadaccess.DefaultFormatters(), inst.schema, inst.conv, inst.rowConfig)
}

// TableDesc returns the leeway schema reconstructed from the current result's
// physical column names, or nil when the schema is not leeway-shaped. This is
// the play app's single leeway-schema derivation — the Schema pane renders it
// rather than re-running discovery. EnsureFor must have been called for the
// current schema first (it is, every frame, via the Detail card).
func (inst *CardDriver) TableDesc() *common.TableDesc {
	return inst.table
}

// ColumnClasses returns the per-Arrow-column leeway classification for the
// current result — each column's section, role bucket (value / support /
// membership), backbone flag, and collection shape — or nil when the schema is
// not leeway-shaped. Like TableDesc it is derived once per schema in EnsureFor
// (the play app's single leeway-schema reconstruction point); the Table pane
// reads it to group and filter result columns by section for its leeway display
// modes. Keyed by ArrowIdx — a schema column absent from the slice is
// un-classified (non-leeway, implicit, or projected-in) and shown verbatim.
func (inst *CardDriver) ColumnClasses() []streamreadaccess.ColumnClass {
	return inst.classes
}

// SetCellGloss installs the per-value gloss on the card (ADR-0186 §SD4) —
// the host's inline face over the driver's text of each value. Passing nil
// clears it. A no-op until EnsureFor has built a card.
func (inst *CardDriver) SetCellGloss(fn leewaywidgets.CellGlossFunc) {
	if inst.card != nil {
		inst.card.SetCellGloss(fn)
	}
}

// SetCellBlock installs the per-value block face on the card (ADR-0186,
// block faces on the card) — the host's renderer for the values whose gloss
// has one. Passing nil clears it. A no-op until EnsureFor has built a card.
func (inst *CardDriver) SetCellBlock(fn leewaywidgets.CellBlockFunc) {
	if inst.card != nil {
		inst.card.SetCellBlock(fn)
	}
}

// Prepare reads a single-row slice of rec into the read model and lays it
// out on the card. Call it before SectionDigests and before Render. A no-op
// on a non-leeway or out-of-range input. The pane prepares every frame; the
// gloss and block seams run here, once per value.
func (inst *CardDriver) Prepare(rec arrow.RecordBatch, row int64) error {
	if !inst.usable || inst.driver == nil || inst.card == nil {
		return nil
	}
	if rec == nil || row < 0 || row >= rec.NumRows() {
		return nil
	}
	slice := rec.NewSlice(row, row+1)
	defer slice.Release()
	sink := lwread.NewSink(lwread.Options{Renderer: registryRenderer()})
	if err := inst.driver.DriveRecordBatch(sink, slice); err != nil {
		log.Warn().Err(err).Int64("row", row).Msg("play: driver error")
		return eh.Errorf("unable to drive record batch: %w", err)
	}
	m := sink.Model()
	m.Qualify()
	inst.card.Prepare(m)
	return nil
}

// SectionDigests returns the per-tagged-section summaries (the attribute's
// name, its labels and its co-attribute values) of the last Prepare, or
// nil when the schema is not leeway-shaped. The Detail timeline reuses these to
// label its temporal flags with the same content the card draws below.
func (inst *CardDriver) SectionDigests() []leewaywidgets.SectionDigest {
	if !inst.usable || inst.card == nil {
		return nil
	}
	return inst.card.SectionDigests()
}

// Render draws the rows buffered by the last Prepare into the current ui scope.
// Call it after Prepare (and after any SectionDigests read), inside a ScrollArea
// or Vertical container.
func (inst *CardDriver) Render() {
	if !inst.usable || inst.card == nil {
		return
	}
	inst.card.Render()
}

// cardRecipe is one schema's leeway reading: the reconstructed table, the
// IR, the per-column classification and a Driver over it. table and classes
// may be set while driver is nil, when the Driver build failed.
type cardRecipe struct {
	table     *common.TableDesc
	ir        *common.IntermediateTableRepresentation
	classes   []streamreadaccess.ColumnClass
	conv      common.NamingConventionFwdI
	rowConfig common.TableRowConfigE
	driver    *streamreadaccess.Driver
}

// discoverCardRecipe reconstructs schema's leeway table from its column
// names and builds a Driver over it. Pure, so a caller off the render
// goroutine (get_detail) builds its own rather than share the pane's. ok is
// false for a schema that is not leeway-shaped or whose Driver did not build.
func discoverCardRecipe(schema *arrow.Schema) (r cardRecipe, ok bool) {
	nFields := schema.NumFields()
	colNames := make([]string, 0, nFields)
	for i := range nFields {
		colNames = append(colNames, schema.Field(i).Name)
	}
	// Probe the schema with the shared classifier (ADR-0170 §SD1): sniff the
	// naming-convention separator, attempt discovery, treat failure as
	// "opaque, expected". The data catalog runs the same function over
	// system.columns, so "is this leeway-shaped" has one answer in the
	// codebase rather than two that drift apart.
	cl := datacatalog.Classify(colNames)
	if cl.Kind != datacatalog.KindLeeway {
		// A non-leeway result (aggregation, join, arbitrary SQL) is an
		// expected, fully-supported case — the caller renders the ad-hoc
		// detail view. Debug, not Warn: a normal fallback, not a fault.
		log.Debug().Err(cl.Err).Msg("play: result not leeway-shaped — using ad-hoc view")
		return r, false
	}
	r.table = cl.Table
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	if err := ir.LoadFromTable(cl.Table, tech); err != nil {
		log.Warn().Err(err).Msg("play: ir load failed — falling back")
		return r, false
	}
	r.classes = streamreadaccess.ClassifyArrowColumns(ir, cl.Convention, schema, cl.RowConfig)
	driver, err := streamreadaccess.NewDriverFromSchema(
		cl.Table, ir,
		streamreadaccess.DefaultFormatters(),
		schema, cl.Convention, cl.RowConfig)
	if err != nil {
		log.Warn().Err(err).Msg("play: driver construction failed — falling back")
		return r, false
	}
	r.ir, r.conv, r.rowConfig, r.driver = ir, cl.Convention, cl.RowConfig, driver
	return r, true
}
