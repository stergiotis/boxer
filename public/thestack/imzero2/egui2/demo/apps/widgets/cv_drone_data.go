package widgets

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview"
)

// cvDroneRow is the flat leeway drone DTO shared by the componentview demos.
// Status + Tags ride the symbol section (two memberships); battery the
// u64Array; the delivery window the timeRange section. componentview
// recognises identity (status), battery and tasked (tags); the window is
// unrecognised, so it surfaces only in the generic card and the schema — the
// typed/generic complement. Every lw: shape here is a marshallreflect
// round-trip-proven one (scalar+slice into symbol, u64Array unit, timeRange
// sub-columns).
type cvDroneRow struct {
	_ struct{} `kind:"droneMission"`

	ID       uint64    `lw:",id"`
	Tracking []byte    `lw:",naturalKey"`
	Status   string    `lw:"droneStatus,symbol"`
	Tags     []string  `lw:"droneTags,symbolArray"`
	Battery  uint64    `lw:"battery,u64Array,unit"`
	WinBegin time.Time `lw:"window,timeRange:beginIncl"`
	WinEnd   time.Time `lw:"window,timeRange:endExcl"`
}

// cvDroneData is the shared, immutable demo dataset: a small drone batch
// marshalled into anchor's table plus the TableDesc discovered from its
// physical column names. Built once, reused by both componentview demos.
type cvDroneData struct {
	rows           []cvDroneRow
	names          []string
	rec            arrow.RecordBatch
	tblDesc        common.TableDesc
	tableRowConfig common.TableRowConfigE
	err            string
}

var cvDroneDataCache *cvDroneData

func cvDroneLookup() marshallreflect.MapLookup {
	return marshallreflect.MapLookup{"droneStatus": 1, "droneTags": 2, "battery": 3, "window": 4}
}

// ensureCvDroneData builds (once) and returns the shared drone dataset. The
// tour is single-threaded, so a plain nil-guard suffices.
func ensureCvDroneData() *cvDroneData {
	if cvDroneDataCache != nil {
		return cvDroneDataCache
	}
	d := &cvDroneData{}
	cvDroneDataCache = d

	base := time.Unix(1_710_000_000, 0).UTC()
	d.rows = []cvDroneRow{
		{ID: 7, Tracking: []byte("TRK-7"), Status: "IN_TRANSIT", Tags: []string{"survey", "urgent"}, Battery: 8500, WinBegin: base, WinEnd: base.Add(time.Hour)},
		{ID: 3, Tracking: []byte("TRK-3"), Status: "CHARGING", Tags: nil, Battery: 900, WinBegin: base, WinEnd: base.Add(2 * time.Hour)},
	}
	d.names = []string{"drone 7 · operating", "drone 3 · charging"}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(d.rows))
	if err := marshallreflect.Marshal(table, d.rows, cvDroneLookup()); err != nil {
		d.err = "marshal: " + err.Error()
		return d
	}
	recs, err := table.TransferRecords(nil)
	if err != nil || len(recs) == 0 {
		d.err = "transfer records failed"
		return d
	}
	d.rec = recs[0]

	schema := d.rec.Schema()
	colNames := make([]string, schema.NumFields())
	for i := range colNames {
		colNames[i] = schema.Field(i).Name
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		d.err = "naming convention: " + err.Error()
		return d
	}
	td, trc, err := conv.DiscoverTableFromColumnNames(colNames)
	if err != nil {
		d.err = "discover table: " + err.Error()
		return d
	}
	d.tblDesc = td
	d.tableRowConfig = trc
	return d
}

// newCvCardDriver builds a streamreadaccess.Driver for the shared drone record,
// mirroring play's CardDriver wiring. The Driver feeds a Table2CardEmitter the
// same Begin*/End* stream the HTML/JSON card emitters consume.
func newCvCardDriver(d *cvDroneData) (driver *streamreadaccess.Driver, err error) {
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&d.tblDesc, tech); err != nil {
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		return
	}
	driver, err = streamreadaccess.NewDriverFromSchema(
		&d.tblDesc, ir, streamreadaccess.DefaultFormatters(),
		d.rec.Schema(), conv, d.tableRowConfig)
	return
}

// --- Per-component DTOs (ADR-0075 / ADR-0146 D2). ---
//
// cvDroneRow above is a WRITER: one fat DTO carrying every component, which is
// how the demo dataset is produced. It is deliberately not what the components
// are read through. A fat DTO can only answer "is all of this here?"; a
// component DTO declares just the slots its own component owns, so Detect can
// answer per component — which is what makes the archetype legible per entity
// rather than assumed.

// cvIdentity reads the identity component: the drone's status symbol.
type cvIdentity struct {
	_        struct{} `kind:"cvIdentity"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Status   string   `lw:"droneStatus,symbol"`
}

// cvBattery reads the battery component.
type cvBattery struct {
	_        struct{} `kind:"cvBattery"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Charge   uint64   `lw:"battery,u64Array,unit"`
}

// cvTasked reads the tasked component. Its tags are a container, so the slot is
// [0..1]: a drone with no tags writes no attribute and reads back as absent —
// which is exactly what the report should show.
type cvTasked struct {
	_        struct{} `kind:"cvTasked"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Tags     []string `lw:"droneTags,symbolArray"`
}

// cvComponentBinder binds the three recognised components to the DTOs that read
// them. The `window` (timeRange) section is deliberately unbound: no renderer
// claims it, so it surfaces only through the generic card — the typed/generic
// complement ADR-0075 is about — and Detect ignores it, exactly as it ignores
// components this process has never heard of.
func cvComponentBinder() (*componentview.Binder, error) {
	b := componentview.NewBinder()
	b.Lookup = cvDroneLookup()

	identity, err := componentview.Bind(componentview.KindIdentity, func(r cvIdentity) any {
		return componentview.IdentityVal{Status: r.Status}
	})
	if err != nil {
		return nil, err
	}
	battery, err := componentview.Bind(componentview.KindBattery, func(r cvBattery) any {
		return componentview.BatteryVal{Charge: r.Charge}
	})
	if err != nil {
		return nil, err
	}
	tasked, err := componentview.Bind(componentview.KindTasked, func(r cvTasked) any {
		return componentview.TaskedVal{Tags: r.Tags}
	})
	if err != nil {
		return nil, err
	}
	for _, bind := range []componentview.Binding{identity, battery, tasked} {
		if err = b.Add(bind); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// cvSectionReaders binds the record's per-section RA readers for the sections
// the bound components claim. Released by the caller.
func cvSectionReaders(rec arrow.RecordBatch) (readers *marshallreflect.SectionReaders, release func(), err error) {
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	if err = idR.LoadFromRecord(rec); err != nil {
		return
	}
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	if err = symR.LoadFromRecord(rec); err != nil {
		return
	}
	symArrR := anchor.NewReadAccessTestTableTaggedSymbolArray()
	symArrR.SetColumnIndices(symArrR.GetColumnIndices())
	if err = symArrR.LoadFromRecord(rec); err != nil {
		return
	}
	u64R := anchor.NewReadAccessTestTableTaggedU64Array()
	u64R.SetColumnIndices(u64R.GetColumnIndices())
	if err = u64R.LoadFromRecord(rec); err != nil {
		return
	}
	readers = marshallreflect.NewSectionReaders(idR.Len()).
		PlainColumn("id", idR.ValueId).
		PlainColumn("naturalKey", idR.ValueNaturalKey).
		Section("symbol", symR.GetAttributes(), symR.GetMemberships()).
		Section("symbolArray", symArrR.GetAttributes(), symArrR.GetMemberships()).
		Section("u64Array", u64R.GetAttributes(), u64R.GetMemberships())
	release = func() {
		idR.Release()
		symR.Release()
		symArrR.Release()
		u64R.Release()
	}
	return
}
