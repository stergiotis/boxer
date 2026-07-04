package readback

// End-to-end coverage for shapes the round-trip oracle did not exercise:
// verbatim arrays, set sections, multi-field hasAll presence grouping, the
// Filter artefact embedded in WHERE, and the optional-array sentinel. Each
// marshals real DTOs through anchor's write path and runs the generated
// artefacts in clickhouse-local.

import (
	"strings"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// genArtefacts plans T, builds the anchor IR, and generates the artefacts.
func genArtefacts[T any](t *testing.T, lookup marshallreflect.MapLookup) (Artefacts, *marshallreflect.MapLookup) {
	t.Helper()
	plan, err := marshallreflect.PlanFor[T]()
	if err != nil {
		t.Skipf("PlanFor[%T]: %v", *new(T), err)
	}
	a, err := NewGenerator(buildAnchorIR(t), NewLookupResolver(lookup)).Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return a, &lookup
}

// queryProjection binds the projection tuple as p in an inner SELECT, then
// selects outerCols (referencing p.<slot>) plus presence/validator/sentinel
// over the marshalled batch, returning the TSV lines.
func queryProjection(t *testing.T, arrowPath string, a Artefacts, outerCols string) []string {
	t.Helper()
	script := HelperUDFsSQL() + "\nSELECT " + outerCols + ", pres, val, 'X' FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator +
		" AS val FROM file('" + arrowPath + "', 'Arrow')) ORDER BY 1"
	out := strings.TrimRight(runClickHouseLocal(t, script), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// --- Verbatim homogeneous array ---

type cvVerbArr struct {
	_ struct{} `kind:"cvVerbArr"`

	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Tags     []string `lw:"tags,symbolArray,verbatim"`
}

func TestCoverage_VerbatimArray(t *testing.T) {
	rows := []cvVerbArr{
		{ID: 1, Tracking: []byte("A"), Tags: []string{"red", "green"}},
		{ID: 2, Tracking: []byte("B"), Tags: []string{"blue"}},
	}
	a, _ := genArtefacts[cvVerbArr](t, marshallreflect.MapLookup{})
	arrowPath := rbMarshalArrow(t, rows, marshallreflect.MapLookup{})
	lines := queryProjection(t, arrowPath, a, "p.ID, arrayStringConcat(p.Tags, ',')")
	want := []string{"1\tred,green", "2\tblue"}
	if len(lines) != 2 {
		t.Fatalf("got %d rows: %v", len(lines), lines)
	}
	for i := range want {
		f := strings.SplitN(lines[i], "\t", 3)
		if f[0]+"\t"+f[1] != want[i] {
			t.Errorf("row %d = %q, want %q", i, f[0]+"\t"+f[1], want[i])
		}
		if f[2] != "1\t1\tX" {
			t.Errorf("row %d presence/validator = %q, want 1\\t1\\tX", i, f[2])
		}
	}
}

// --- Set section (roaring bitmap) ---

type cvSet struct {
	_ struct{} `kind:"cvSet"`

	ID       uint64          `lw:",id"`
	Tracking []byte          `lw:",naturalKey"`
	Flags    *roaring.Bitmap `lw:"flags,u32Set"`
}

func TestCoverage_Set(t *testing.T) {
	rows := []cvSet{
		{ID: 1, Tracking: []byte("A"), Flags: roaring.BitmapOf(3, 7, 9)},
		{ID: 2, Tracking: []byte("B"), Flags: roaring.BitmapOf(1)},
	}
	lookup := marshallreflect.MapLookup{"flags": 5}
	a, _ := genArtefacts[cvSet](t, lookup)
	arrowPath := rbMarshalArrow(t, rows, lookup)
	// Set elements come back as an array; compare as a sorted set.
	lines := queryProjection(t, arrowPath, a, "p.ID, arraySort(p.Flags)")
	if len(lines) != 2 {
		t.Fatalf("got %d rows: %v", len(lines), lines)
	}
	want := map[string]string{"1": "[3,7,9]", "2": "[1]"}
	for _, ln := range lines {
		f := strings.SplitN(ln, "\t", 3)
		if got := f[1]; got != want[f[0]] {
			t.Errorf("id %s flags = %q, want %q", f[0], got, want[f[0]])
		}
		if f[2] != "1\t1\tX" {
			t.Errorf("id %s presence/validator = %q, want 1\\t1\\tX", f[0], f[2])
		}
	}
}

// --- Two ref fields in one section → hasAll presence + Filter end-to-end ---

type cvTwoRef struct {
	_ struct{} `kind:"cvTwoRef"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`
	Alpha    string `lw:"alpha,symbol"`
	Beta     string `lw:"beta,symbol"`
}

func TestCoverage_HasAllAndFilter(t *testing.T) {
	rows := []cvTwoRef{
		{ID: 1, Tracking: []byte("A"), Alpha: "a1", Beta: "b1"},
		{ID: 2, Tracking: []byte("B"), Alpha: "a2", Beta: "b2"},
	}
	lookup := marshallreflect.MapLookup{"alpha": 10, "beta": 20}
	a, _ := genArtefacts[cvTwoRef](t, lookup)
	// Two memberships on the same lr column must group into one hasAll term.
	if !strings.Contains(a.Presence, "hasAll(") {
		t.Errorf("two same-column memberships should group into hasAll: %s", a.Presence)
	}
	arrowPath := rbMarshalArrow(t, rows, lookup)

	// Filter (Presence AND Validator) embedded in WHERE selects exactly the kind.
	script := HelperUDFsSQL() + "\nSELECT count() FROM file('" + arrowPath + "', 'Arrow') WHERE " + a.Filter
	got := strings.TrimSpace(runClickHouseLocal(t, script))
	if got != "2" {
		t.Errorf("Filter selected %q rows, want 2", got)
	}

	// A phantom kind's Filter selects none (no false positives via Filter).
	pa, _ := genArtefacts[cvTwoRef](t, marshallreflect.MapLookup{"alpha": 111, "beta": 222})
	script = HelperUDFsSQL() + "\nSELECT count() FROM file('" + arrowPath + "', 'Arrow') WHERE " + pa.Filter
	got = strings.TrimSpace(runClickHouseLocal(t, script))
	if got != "0" {
		t.Errorf("phantom Filter selected %q rows, want 0", got)
	}
}

// --- Mixed-shape multi-sub-column section (scalar + co-containers, ADR-0101) ---

type cvMixedText struct {
	_ struct{} `kind:"cvMixedText"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Text       string   `lw:"prose,text:text"`
	WordLength []uint32 `lw:"prose,text:wordLength"`
	WordBag    []string `lw:"prose,text:wordBag"`
}

// TestCoverage_MixedScalarContainerSection drives anchor's `text` section
// (scalar `text` + co-containers `wordLength`/`wordBag`) end-to-end: the
// per-sub-column subtype dispatch pairs the VALUE_BY_TAG scalar read with
// two LIST_BY_TAG array reads sharing the section's LEN support column.
// Row 3 pins the N = 0 shape: the attribute emits (scalar present),
// arrays come back empty.
func TestCoverage_MixedScalarContainerSection(t *testing.T) {
	rows := []cvMixedText{
		{ID: 1, Tracking: []byte("A"), Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
		{ID: 2, Tracking: []byte("B"), Text: "one", WordLength: []uint32{3}, WordBag: []string{"one"}},
		{ID: 3, Tracking: []byte("C"), Text: ""},
	}
	lookup := marshallreflect.MapLookup{"prose": 9}
	a, _ := genArtefacts[cvMixedText](t, lookup)
	arrowPath := rbMarshalArrow(t, rows, lookup)
	lines := queryProjection(t, arrowPath, a, "p.ID, p.Text, toString(p.WordLength), toString(p.WordBag)")
	if len(lines) != 3 {
		t.Fatalf("got %d rows: %v", len(lines), lines)
	}
	// Single quotes inside array literals arrive TSV-escaped (\').
	want := []string{
		"1\thello world\t[5,5]\t[\\'hello\\',\\'world\\']\t1\t1\tX",
		"2\tone\t[3]\t[\\'one\\']\t1\t1\tX",
		"3\t\t[]\t[]\t1\t1\tX",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, lines[i], want[i])
		}
	}
}
