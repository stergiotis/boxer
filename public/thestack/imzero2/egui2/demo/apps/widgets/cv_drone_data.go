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
