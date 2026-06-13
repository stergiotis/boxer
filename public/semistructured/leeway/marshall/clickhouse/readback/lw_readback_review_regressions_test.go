package readback

// Regression tests for the 2026-06-13 hostile review of the readback
// generator. Each pins a confirmed defect or a previously-untested contract.

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

func rbMarshalArrow[T any](t *testing.T, rows []T, lookup marshallreflect.MapLookup) string {
	t.Helper()
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	if err := marshallreflect.Marshal(table, rows, lookup); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	recs, err := table.TransferRecords(nil)
	if err != nil {
		t.Fatalf("TransferRecords: %v", err)
	}
	t.Cleanup(func() {
		for _, r := range recs {
			r.Release()
		}
	})
	if len(recs) == 0 {
		t.Fatalf("no records")
	}
	return writeArrowFile(t, recs[0])
}

// Discrimination: the presence/validator artefacts must REJECT a row that is
// not the kind. The round-trip tests only feed matching rows, so a validator
// that always returns 1 would pass them; this feeds real rows to the
// artefacts of a phantom kind whose membership ids are absent from the data.
func TestRegressionDiscrimination(t *testing.T) {
	rows := []rtDrone{{ID: 1, Tracking: []byte("A"), Status: "X", Path: []uint64{9}}}
	arrowPath := rbMarshalArrow(t, rows, marshallreflect.MapLookup{"droneStatus": 1, "flightPath": 2})

	plan, err := marshallreflect.PlanFor[rtDrone]()
	if err != nil {
		t.Fatal(err)
	}
	// Phantom kind: same shape, different (absent) membership ids.
	g := NewGenerator(buildAnchorIR(t), NewLookupResolver(marshallreflect.MapLookup{"droneStatus": 777, "flightPath": 888}))
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	script := HelperUDFsSQL() + "\nSELECT " + a.Presence + " AS pres, " + a.Validator +
		" AS val FROM file('" + arrowPath + "', 'Arrow')"
	out := strings.TrimSpace(runClickHouseLocal(t, script))
	if out != "0\t0" {
		t.Errorf("non-matching row admitted: presence,validator = %q, want 0\\t0", out)
	}
}

type rbOpt struct {
	_ struct{} `kind:"rbDroneOpt"`

	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Note     option.Option[string] `lw:"note,symbol,verbatim"`
}

// Scalar optional fields project as Nullable(T): an absent optional reads back
// as NULL, distinguishable from one present with the type default (ADR-0066
// decision 4 — "nullable slots in (b)").
func TestRegressionOptionalNullableScalar(t *testing.T) {
	rows := []rbOpt{
		{ID: 1, Tracking: []byte("A"), Note: option.Option[string]{Val: "hi", Has: true}},
		{ID: 2, Tracking: []byte("B")}, // absent
		{ID: 3, Tracking: []byte("C"), Note: option.Option[string]{Val: "", Has: true}}, // present, empty
	}
	plan, err := marshallreflect.PlanFor[rbOpt]()
	if err != nil {
		t.Fatal(err)
	}
	arrowPath := rbMarshalArrow(t, rows, marshallreflect.MapLookup{})
	a, err := NewGenerator(buildAnchorIR(t), NewLookupResolver(marshallreflect.MapLookup{})).Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.Projection, "Note Nullable(String)") {
		t.Errorf("scalar optional must project as Nullable: %s", a.Projection)
	}
	// A trailing sentinel column keeps the variable-width value off the line
	// end, so a present-but-empty value's TSV field is not stripped.
	script := HelperUDFsSQL() + "\nSELECT p.ID, p.Note IS NULL, ifNull(p.Note, '<null>'), 'X' FROM (SELECT " +
		a.Projection + " AS p FROM file('" + arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := strings.TrimSpace(runClickHouseLocal(t, script))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 rows, got:\n%s", out)
	}
	// Row 1: present "hi" → not null. Row 2: absent → NULL. Row 3: present "" → not null.
	wants := []struct{ isNull, val string }{{"0", "hi"}, {"1", "<null>"}, {"0", ""}}
	for i, w := range wants {
		f := strings.Split(lines[i], "\t")
		if len(f) != 4 || f[1] != w.isNull || f[2] != w.val {
			t.Errorf("row %d = %q, want IS NULL=%s val=%q", i+1, f, w.isNull, w.val)
		}
	}
}

// Const on a non-scalar value section is rejected at generation time rather
// than emitting `array = 'const'` SQL that errors at query time.
func TestRegressionConstOnArrayRejected(t *testing.T) {
	type rbConstArr struct {
		_ struct{} `kind:"rbConstArr"`
		_ struct{} `lw:"tagset,symbolArray,verbatim,const=production"`

		ID       uint64 `lw:",id"`
		Tracking []byte `lw:",naturalKey"`
	}
	plan, err := marshallreflect.PlanFor[rbConstArr]()
	if err != nil {
		t.Skipf("write side does not plan const-on-array: %v", err)
	}
	_, err = NewGenerator(buildAnchorIR(t), NewLookupResolver(marshallreflect.MapLookup{})).Generate(plan)
	if err == nil {
		t.Fatal("const on a non-scalar section must be a generation error")
	}
	if !strings.Contains(err.Error(), "scalar value sections") {
		t.Errorf("unexpected error: %v", err)
	}
}
