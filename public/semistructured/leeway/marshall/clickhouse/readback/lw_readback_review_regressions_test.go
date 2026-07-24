package readback

// Regression tests for the 2026-06-13 hostile review of the readback
// generator. Each pins a confirmed defect or a previously-untested contract.

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
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

// rbTaggedText is a dynamic-membership tuple element (ADR-0103): its
// membership is per-element data, so the TaggedFields it produces carry an
// EMPTY LWMembership.
type rbTaggedText struct {
	Label string `lw:"@membership,verbatim"`
	Text  string `lw:"symbol:value"`
}

type rbTupleDoc struct {
	_ struct{} `kind:"rbTupleDoc"`

	ID       uint64         `lw:",id"`
	Tracking []byte         `lw:",naturalKey"`
	Texts    []rbTaggedText `lw:"symbol"`
}

// rbNestedNote is a nested attribute struct with a STATIC membership on the
// section field and Many cardinality — N attributes per row through one field.
type rbNestedNote struct {
	Text string
}

type rbNestedDoc struct {
	_ struct{} `kind:"rbNestedDoc"`

	ID       uint64         `lw:",id"`
	Tracking []byte         `lw:",naturalKey"`
	Notes    []rbNestedNote `lw:"droneStatus,symbol"`
}

// Tuple / nested attribute sections are rejected at generation AND validation
// time. Every artefact assumes one attribute per membership per row, so
// emitting for these shapes is silently wrong, not merely incomplete: a
// dynamic tuple carries no LWMembership (a verbatim channel resolved to the
// empty literal and matched nothing), and a static nested `[]S` pinned
// `countEqual(...) = 1` on an N-attribute section. In-tree the hole was masked
// by recordstore/gen's ReadRowSupported gate — a guard in the caller, not here.
func TestRegressionTupleAndNestedRejected(t *testing.T) {
	ir := buildAnchorIR(t)
	res := NewLookupResolver(marshallreflect.MapLookup{"droneStatus": 1})

	for _, tc := range []struct {
		name string
		plan func() (*mappingplan.Plan, error)
	}{
		{"dynamic-membership tuple", func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[rbTupleDoc]() }},
		{"static nested Many", func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[rbNestedDoc]() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := tc.plan()
			if err != nil {
				t.Fatalf("PlanFor: %v", err)
			}
			_, err = NewGenerator(ir, res).Generate(plan)
			if err == nil {
				t.Fatal("Generate must reject a tuple / nested section")
			}
			if !strings.Contains(err.Error(), "not supported by the read-back generator") {
				t.Errorf("unexpected Generate error: %v", err)
			}
			if err = ValidatePlanAgainstIR(plan, ir); err == nil {
				t.Fatal("ValidatePlanAgainstIR must reject a tuple / nested section")
			}
		})
	}
}

type rbUnit struct {
	_ struct{} `kind:"rbUnitDrone"`

	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Battery  uint64                `lw:"battery,u64Array,unit"`
	Spare    option.Option[uint64] `lw:"spare,u64Array,unit"`
}

// A `,unit` field is a scalar T on a container value column — the
// BeginAttributeSingle shape. Its projected slot must be that scalar, not the
// column's Array(T): the slot is named after the Go field, so an Array(T) slot
// does not round-trip the field it names. The Option sibling additionally gets
// the Nullable treatment a scalar slot allows.
func TestRegressionUnitProjectsScalar(t *testing.T) {
	rows := []rbUnit{
		{ID: 1, Tracking: []byte("A"), Battery: 88, Spare: option.Option[uint64]{Val: 7, Has: true}},
		{ID: 2, Tracking: []byte("B"), Battery: 0},    // Battery present-but-zero, Spare absent
		{ID: 3, Tracking: []byte("C"), Battery: 4095}, // Spare absent
	}
	lookup := marshallreflect.MapLookup{"battery": 11, "spare": 12}
	plan, err := marshallreflect.PlanFor[rbUnit]()
	if err != nil {
		t.Fatalf("PlanFor: %v", err)
	}
	a, err := NewGenerator(buildAnchorIR(t), NewLookupResolver(lookup)).Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(a.Projection, "Battery UInt64") {
		t.Errorf("`,unit` must project the scalar element type, not the column's array:\n%s", a.Projection)
	}
	if !strings.Contains(a.Projection, "Spare Nullable(UInt64)") {
		t.Errorf("a `,unit` Option projects a scalar slot, so it takes Nullable:\n%s", a.Projection)
	}

	// The validator names raw physical columns, so it is evaluated in the inner
	// SELECT where they are in scope — the projection tuple is all the outer
	// query sees.
	arrowPath := rbMarshalArrow(t, rows, lookup)
	script := HelperUDFsSQL() + "\nSELECT p.ID, p.Battery, p.Spare IS NULL, ifNull(p.Spare, 0), val FROM (SELECT " +
		a.Projection + " AS p, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := strings.TrimSpace(runClickHouseLocal(t, script))
	want := []string{"1\t88\t0\t7\t1", "2\t0\t1\t0\t1", "3\t4095\t1\t0\t1"}
	got := strings.Split(out, "\n")
	if len(got) != len(want) {
		t.Fatalf("want %d rows, got:\n%s\nscript:\n%s", len(want), out, script)
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] {
			t.Errorf("row %d = %q, want %q", i+1, got[i], want[i])
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
