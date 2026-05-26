//go:build llm_generated_opus47

package marshallreflect_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallreflect"
)

// --- Recording mock DML. ---
//
// recordingDML satisfies the reflective method-set MarshalStack /
// Marshal dispatch against: BeginEntity / SetId / SetTimestamp /
// GetSection<Sym|Foo|Bar> / CommitEntity. Section and attribute
// recorders chain back through pointers so the call sequence is
// observable as a flat string slice on the root recorder.

type recordingDML struct {
	log []string
}

func (r *recordingDML) record(s string) { r.log = append(r.log, s) }

func (r *recordingDML) BeginEntity() { r.record("BeginEntity") }
func (r *recordingDML) SetId(id uint64, nk []byte) {
	r.record(fmt.Sprintf("SetId(%d, %q)", id, nk))
}
func (r *recordingDML) SetTimestamp(ts time.Time) {
	r.record(fmt.Sprintf("SetTimestamp(%d)", ts.UnixNano()))
}
func (r *recordingDML) CommitEntity() error {
	r.record("CommitEntity")
	return nil
}

// One section method per section the test DTOs declare. The reflective
// dispatch in marshallreflect.marshalSection looks up
// `GetSection<PascalCase(section)>` by name.
func (r *recordingDML) GetSectionSymbol() *recordingSec {
	r.record("GetSectionSymbol")
	return &recordingSec{root: r, name: "Symbol"}
}
func (r *recordingDML) GetSectionFoo() *recordingSec {
	r.record("GetSectionFoo")
	return &recordingSec{root: r, name: "Foo"}
}
func (r *recordingDML) GetSectionBar() *recordingSec {
	r.record("GetSectionBar")
	return &recordingSec{root: r, name: "Bar"}
}

type recordingSec struct {
	root *recordingDML
	name string
}

func (s *recordingSec) BeginAttribute(value string) *recordingAttr {
	s.root.record(fmt.Sprintf("%s.BeginAttribute(%q)", s.name, value))
	return &recordingAttr{root: s.root}
}
func (s *recordingSec) EndSection() {
	s.root.record(fmt.Sprintf("%s.EndSection", s.name))
}

type recordingAttr struct {
	root *recordingDML
}

func (a *recordingAttr) AddMembershipLowCardRefP(id uint64) {
	a.root.record(fmt.Sprintf("AddMembershipLowCardRefP(%d)", id))
}
func (a *recordingAttr) EndAttributeP() {
	a.root.record("EndAttributeP")
}

// --- Lookup that maps lw: tag name to a stable uint64. ---

type fakeLookup struct{}

func (fakeLookup) LookupMembership(name string) (uint64, error) {
	// Deterministic FNV-ish hash so we get distinct ids per name.
	var h uint64 = 14695981039346656037
	for _, c := range []byte(name) {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h, nil
}

// --- DTOs for stacked tests. ---

type stackedA struct {
	_     struct{} `kind:"a"`
	Id    uint64   `lw:",id"`
	Color string   `lw:"color,symbol"`
}

type stackedB struct {
	_     struct{} `kind:"b"`
	Id    uint64   `lw:",id"`
	Label string   `lw:"label,symbol"`
}

// stackedB_mismatch declares a different plain shape (Id is int64,
// not uint64) so the plain-agreement check rejects mixing it with
// stackedA. This is a contrived shape — id MUST be uint64 per the
// plain-column shape validator — so the test instead uses a DTO
// missing a plain column the first DTO has (via different fields).
type stackedNoMatch struct {
	_     struct{} `kind:"c"`
	Id    uint64   `lw:",id"`
	Ts    int64    `lw:",ts"`
	Other string   `lw:"other,symbol"`
}

// TestMarshalStack_SingleBatchEqualsMarshal confirms that
// MarshalStack with one batch produces the same observable call
// sequence as Marshal with that batch. Per ADR-0008 D1 baseline.
func TestMarshalStack_SingleBatchEqualsMarshal(t *testing.T) {
	rows := []stackedA{{Id: 1, Color: "red"}, {Id: 2, Color: "blue"}}

	dmlA := &recordingDML{}
	err := marshallreflect.Marshal(dmlA, rows, fakeLookup{})
	require.NoError(t, err)

	dmlB := &recordingDML{}
	err = marshallreflect.MarshalStack(dmlB, []any{rows}, fakeLookup{})
	require.NoError(t, err)

	require.Equal(t, dmlA.log, dmlB.log,
		"MarshalStack with one batch must produce identical wire calls to Marshal")
}

// TestMarshalStack_TwoBatchesInterleave confirms that two batches
// produce one BeginEntity / CommitEntity frame per row index with
// both DTOs' sections emitted between. The B DTO's section call
// must appear AFTER A's section call within the same entity frame.
func TestMarshalStack_TwoBatchesInterleave(t *testing.T) {
	aRows := []stackedA{{Id: 1, Color: "red"}}
	bRows := []stackedB{{Id: 1, Label: "alpha"}}

	dml := &recordingDML{}
	err := marshallreflect.MarshalStack(dml, []any{aRows, bRows}, fakeLookup{})
	require.NoError(t, err)

	joined := strings.Join(dml.log, "\n")
	// Exactly one entity frame.
	require.Equal(t, 1, strings.Count(joined, "BeginEntity"))
	require.Equal(t, 1, strings.Count(joined, "CommitEntity"))
	// Both DTOs' section values appear.
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	// A's call precedes B's call within the entity.
	redIdx := strings.Index(joined, `Symbol.BeginAttribute("red")`)
	alphaIdx := strings.Index(joined, `Symbol.BeginAttribute("alpha")`)
	require.Less(t, redIdx, alphaIdx, "batch 0 (A) should emit before batch 1 (B)")
	// BeginEntity precedes both section emits; CommitEntity follows.
	beginIdx := strings.Index(joined, "BeginEntity")
	commitIdx := strings.Index(joined, "CommitEntity")
	require.Less(t, beginIdx, redIdx)
	require.Less(t, alphaIdx, commitIdx)
}

// TestMarshalStack_RejectsPlainShapeMismatch confirms cross-DTO plain
// disagreement is rejected at marshal time per ADR-0008 D1.
// stackedA has plain {id}, stackedNoMatch has plain {id, ts} — the
// extra `ts` triggers the agreement check.
func TestMarshalStack_RejectsPlainShapeMismatch(t *testing.T) {
	aRows := []stackedA{{Id: 1, Color: "red"}}
	cRows := []stackedNoMatch{{Id: 1, Ts: 42, Other: "x"}}

	dml := &recordingDML{}
	err := marshallreflect.MarshalStack(dml, []any{aRows, cRows}, fakeLookup{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "plain-column count mismatch")
	require.Empty(t, dml.log, "no DML calls before the agreement check fails")
}

// TestMarshalStack_RejectsRowCountMismatch confirms unequal batch
// lengths trigger an error before any DML method is called.
func TestMarshalStack_RejectsRowCountMismatch(t *testing.T) {
	aRows := []stackedA{{Id: 1, Color: "red"}, {Id: 2, Color: "blue"}}
	bRows := []stackedB{{Id: 1, Label: "alpha"}}

	dml := &recordingDML{}
	err := marshallreflect.MarshalStack(dml, []any{aRows, bRows}, fakeLookup{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "row count disagreement")
	require.Empty(t, dml.log, "no DML calls before the agreement check fails")
}

// TestMarshalStack_EmptyBatchListIsNoOp confirms passing an empty
// batches slice is a no-op rather than an error.
func TestMarshalStack_EmptyBatchListIsNoOp(t *testing.T) {
	dml := &recordingDML{}
	err := marshallreflect.MarshalStack(dml, nil, fakeLookup{})
	require.NoError(t, err)
	require.Empty(t, dml.log)
}

