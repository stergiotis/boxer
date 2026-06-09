package readback

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// rtDrone is a DTO whose sections (symbol, u64Array) are a subset of the
// anchor schema, so it marshals against anchor's InEntityTestTable. Status is
// a scalar symbol on the low-card-ref channel; Path a homogenous array on
// u64Array (also low-card-ref).
type rtDrone struct {
	_ struct{} `kind:"droneMission"`

	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Status   string   `lw:"droneStatus,symbol"`
	Path     []uint64 `lw:"flightPath,u64Array"`
}

// buildAnchorIR loads the anchor schema into an InformationRetrieval whose
// physical column names match an Arrow batch produced by anchor's
// InEntityTestTable.
func buildAnchorIR(t *testing.T) *InformationRetrieval {
	t.Helper()
	manip, err := anchor.GetSchemaInManipulator()
	if err != nil {
		t.Fatalf("GetSchemaInManipulator: %v", err)
	}
	tblDesc, err := manip.BuildTableDesc()
	if err != nil {
		t.Fatalf("BuildTableDesc: %v", err)
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		t.Fatalf("naming convention: %v", err)
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tblDesc, tech); err != nil {
		t.Fatalf("LoadFromTable: %v", err)
	}
	info := NewInformationRetrieval(conv)
	if err = info.LoadTable(ir, anchor.TableRowConfig); err != nil {
		t.Fatalf("InformationRetrieval.LoadTable: %v", err)
	}
	return info
}

func writeArrowFile(t *testing.T, rec arrow.RecordBatch) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batch.arrow")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create arrow file: %v", err)
	}
	w, err := ipc.NewFileWriter(f, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(memory.NewGoAllocator()))
	if err != nil {
		_ = f.Close()
		t.Fatalf("ipc.NewFileWriter: %v", err)
	}
	if err = w.Write(rec); err != nil {
		_ = w.Close()
		_ = f.Close()
		t.Fatalf("ipc write: %v", err)
	}
	if err = w.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("ipc close: %v", err)
	}
	if err = f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
	return path
}

// TestRoundTrip_AnchorArrow is the real-data oracle (#6): it marshals DTOs to a
// genuine Arrow batch via anchor's write path, writes it to an Arrow file, runs
// the generated projection/presence/validator over it in clickhouse-local, and
// asserts the read-back equals the originals with presence=validator=1.
func TestRoundTrip_AnchorArrow(t *testing.T) {
	original := []rtDrone{
		{ID: 1001, Tracking: []byte("TRK-A"), Status: "IN_TRANSIT", Path: []uint64{10, 20, 30}},
		{ID: 1002, Tracking: []byte("TRK-B"), Status: "DELIVERED", Path: []uint64{40}},
		{ID: 1003, Tracking: []byte("TRK-C"), Status: "RETURNED", Path: []uint64{50, 60}},
	}
	lookup := marshallreflect.MapLookup{"droneStatus": 1, "flightPath": 2}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	if err := marshallreflect.Marshal(table, original, lookup); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	recs, err := table.TransferRecords(nil)
	if err != nil {
		t.Fatalf("TransferRecords: %v", err)
	}
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	if len(recs) == 0 {
		t.Fatalf("no records produced")
	}
	arrowPath := writeArrowFile(t, recs[0])

	plan, err := marshallreflect.PlanFor[rtDrone]()
	if err != nil {
		t.Fatalf("PlanFor: %v", err)
	}
	g := NewGenerator(buildAnchorIR(t), NewLookupResolver(lookup))
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	script := HelperUDFsSQL() + "\nSELECT p.ID, p.Tracking, p.Status, p.Path, pres, val FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := runClickHouseLocal(t, script)

	type row struct{ id, tracking, status, path, pres, val string }
	var got []row
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("expected 6 columns, got %d: %q\nscript:\n%s", len(f), line, script)
		}
		got = append(got, row{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(got) != len(original) {
		t.Fatalf("got %d rows, want %d:\n%s", len(got), len(original), out)
	}

	want := make([]rtDrone, len(original))
	copy(want, original)
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	for i, w := range want {
		r := got[i]
		if r.id != fmt.Sprintf("%d", w.ID) {
			t.Errorf("row %d id = %q, want %d", i, r.id, w.ID)
		}
		if r.tracking != string(w.Tracking) {
			t.Errorf("row %d tracking = %q, want %q", i, r.tracking, string(w.Tracking))
		}
		if r.status != w.Status {
			t.Errorf("row %d status = %q, want %q", i, r.status, w.Status)
		}
		if want := formatUintArray(w.Path); r.path != want {
			t.Errorf("row %d path = %q, want %q", i, r.path, want)
		}
		if r.pres != "1" {
			t.Errorf("row %d presence = %q, want 1", i, r.pres)
		}
		if r.val != "1" {
			t.Errorf("row %d validator = %q, want 1", i, r.val)
		}
	}
}

func formatUintArray(xs []uint64) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// rtConst carries a write-only const field (a fixed verbatim attribute emitted
// on every row) alongside a regular verbatim field in the same section —
// section uniformity holds (both verbatim).
type rtConst struct {
	_ struct{} `kind:"droneMissionExt"`
	_ struct{} `lw:"appKind,symbol,verbatim,const=production"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`
	Tag      string `lw:"feature,symbol,verbatim"`
}

// TestRoundTrip_Const checks the const path on real data: the generated
// validator must require the const membership present once and carrying the
// fixed value, and that holds for every marshalled row. The const has no
// projected slot (write-only); Tag projects normally.
func TestRoundTrip_Const(t *testing.T) {
	original := []rtConst{
		{ID: 3001, Tracking: []byte("CON-A"), Tag: "edge"},
		{ID: 3002, Tracking: []byte("CON-B"), Tag: "stable"},
	}
	lookup := marshallreflect.MapLookup{} // all verbatim; the const needs no id

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	if err := marshallreflect.Marshal(table, original, lookup); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	recs, err := table.TransferRecords(nil)
	if err != nil {
		t.Fatalf("TransferRecords: %v", err)
	}
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	if len(recs) == 0 {
		t.Fatalf("no records produced")
	}
	arrowPath := writeArrowFile(t, recs[0])

	plan, err := marshallreflect.PlanFor[rtConst]()
	if err != nil {
		t.Fatalf("PlanFor: %v", err)
	}
	g := NewGenerator(buildAnchorIR(t), NewLookupResolver(lookup))
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(a.Validator, "'production'") {
		t.Errorf("validator should pin the const value:\n%s", a.Validator)
	}

	script := HelperUDFsSQL() + "\nSELECT p.ID, p.Tag, pres, val FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := runClickHouseLocal(t, script)

	var rows [][]string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	if len(rows) != len(original) {
		t.Fatalf("got %d rows, want %d:\n%s", len(rows), len(original), out)
	}
	sort.Slice(original, func(i, j int) bool { return original[i].ID < original[j].ID })
	for i, w := range original {
		r := rows[i]
		if len(r) != 4 {
			t.Fatalf("row %d: want 4 columns, got %d: %q", i, len(r), r)
		}
		if r[1] != w.Tag {
			t.Errorf("row %d Tag = %q, want %q", i, r[1], w.Tag)
		}
		if r[2] != "1" {
			t.Errorf("row %d presence = %q, want 1", i, r[2])
		}
		if r[3] != "1" {
			t.Errorf("row %d validator = %q, want 1 (const present + value matches)", i, r[3])
		}
	}
}
