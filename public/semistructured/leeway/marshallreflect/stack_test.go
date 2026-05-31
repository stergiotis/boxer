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
// recordingDML satisfies the reflective method-set RowComposer /
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

// BeginAttribute is variadic so the same mock handles both scalar
// emits (`BeginAttribute(value)`) and container emits
// (`BeginAttribute()` followed by AddToContainerP calls).
func (s *recordingSec) BeginAttribute(values ...string) *recordingAttr {
	if len(values) == 0 {
		s.root.record(fmt.Sprintf("%s.BeginAttribute()", s.name))
	} else {
		s.root.record(fmt.Sprintf("%s.BeginAttribute(%q)", s.name, values[0]))
	}
	return &recordingAttr{root: s.root}
}
func (s *recordingSec) EndSection() {
	s.root.record(fmt.Sprintf("%s.EndSection", s.name))
}

type recordingAttr struct {
	root *recordingDML
}

func (a *recordingAttr) AddToContainerP(value string) {
	a.root.record(fmt.Sprintf("AddToContainerP(%q)", value))
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

// --- DTOs for per-row composer tests. ---

type stackedA struct {
	_          struct{} `kind:"a"`
	Id         uint64   `lw:",id"`
	NaturalKey []byte   `lw:",naturalKey"`
	Color      string   `lw:"color,symbol"`
}

type stackedB struct {
	_          struct{} `kind:"b"`
	Id         uint64   `lw:",id"`
	NaturalKey []byte   `lw:",naturalKey"`
	Label      string   `lw:"label,symbol"`
}

// stackedMixed packs both a scalar and a container field into one
// section so the per-attribute cardinality filter is observable in
// test wire output.
type stackedMixed struct {
	_          struct{} `kind:"mixed"`
	Id         uint64   `lw:",id"`
	NaturalKey []byte   `lw:",naturalKey"`
	Color      string   `lw:"color,symbol"` // scalar → always size-1 attr
	Brand      []string `lw:"brand,symbol"` // container → runtime size
}

// TestRowComposer_SingleRow_PlainPlusSections confirms BeginRow opens
// an entity, emits plain columns from the plainOwner DTO, and emits
// its sections; CommitRow closes the entity.
func TestRowComposer_SingleRow_PlainPlusSections(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 1, strings.Count(joined, "BeginEntity"))
	require.Equal(t, 1, strings.Count(joined, "CommitEntity"))
	require.Contains(t, joined, `SetId(1, "")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
}

// TestRowComposer_Stacked_TwoDTOsOneRow confirms multiple DTOs can
// contribute to one entity: BeginRow with DTO-A's row owns plains and
// emits A's sections, AddSections with DTO-B's row adds B's sections,
// then CommitRow closes. Order of section emit follows call order.
func TestRowComposer_Stacked_TwoDTOsOneRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.AddSections(stackedB{Id: 99, Label: "alpha"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	// Exactly one entity frame.
	require.Equal(t, 1, strings.Count(joined, "BeginEntity"))
	require.Equal(t, 1, strings.Count(joined, "CommitEntity"))
	// Both DTOs' section values appear.
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	// Plains owned by A (Id=1), not B (Id=99).
	require.Contains(t, joined, `SetId(1, "")`)
	require.NotContains(t, joined, `SetId(99, "")`)
	// A's section emit precedes B's.
	redIdx := strings.Index(joined, `Symbol.BeginAttribute("red")`)
	alphaIdx := strings.Index(joined, `Symbol.BeginAttribute("alpha")`)
	require.Less(t, redIdx, alphaIdx, "BeginRow's DTO emits before AddSections's DTO")
	// BeginEntity precedes both; CommitEntity follows.
	beginIdx := strings.Index(joined, "BeginEntity")
	commitIdx := strings.Index(joined, "CommitEntity")
	require.Less(t, beginIdx, redIdx)
	require.Less(t, alphaIdx, commitIdx)
}

// TestRowComposer_MultipleRows_VaryingDTOMix confirms the composer
// can produce different entity shapes across rows — row 0 stacks
// (A, B), row 1 has just A. Each row gets exactly one entity frame.
func TestRowComposer_MultipleRows_VaryingDTOMix(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.AddSections(stackedB{Id: 99, Label: "alpha"}))
	require.NoError(t, m.CommitRow())

	require.NoError(t, m.BeginRow(stackedA{Id: 2, Color: "blue"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 2, strings.Count(joined, "BeginEntity"))
	require.Equal(t, 2, strings.Count(joined, "CommitEntity"))
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("blue")`)
}

// TestRowComposer_RejectsBeginRow_WhileInRow confirms the state
// machine enforces close-before-reopen.
func TestRowComposer_RejectsBeginRow_WhileInRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	err := m.BeginRow(stackedA{Id: 2, Color: "blue"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already inside a row")
}

// TestRowComposer_RejectsAddSections_WithoutBeginRow confirms calling
// AddSections before BeginRow fails without any DML side effects.
func TestRowComposer_RejectsAddSections_WithoutBeginRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	err := m.AddSections(stackedB{Id: 1, Label: "alpha"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside of a row")
	require.Empty(t, dml.log)
}

// TestRowComposer_RejectsCommitRow_WithoutBeginRow confirms calling
// CommitRow before BeginRow fails without any DML side effects.
func TestRowComposer_RejectsCommitRow_WithoutBeginRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	err := m.CommitRow()
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside of a row")
	require.Empty(t, dml.log)
}

// TestRowComposer_AcceptsPointerRow confirms passing *T also works
// (the composer dereferences before plan resolution).
func TestRowComposer_AcceptsPointerRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	row := stackedA{Id: 7, Color: "green"}
	require.NoError(t, m.BeginRow(&row))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Contains(t, joined, `SetId(7, "")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("green")`)
}

// TestRowComposer_RejectsNonStructRow confirms a non-struct argument
// is rejected with a clear message.
func TestRowComposer_RejectsNonStructRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	err := m.BeginRow(42)
	require.Error(t, err)
	require.Contains(t, err.Error(), "row must be a struct")
}

// TestRowComposer_AddSingleValueAttributes_PureScalarRow confirms a
// DTO with only scalar fields emits its single section with one
// size-1 attribute when AddSingleValueAttributes is called.
func TestRowComposer_AddSingleValueAttributes_PureScalarRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.AddSingleValueAttributes(stackedB{Id: 1, Label: "alpha"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	// No AddToContainerP calls — there are no containers.
	require.NotContains(t, joined, "AddToContainerP")
}

// TestRowComposer_AddMultiValueAttributes_SkipsScalars confirms
// AddMultiValueAttributes never emits attributes for purely scalar
// DTOs — pure scalar rows produce no Begin/EndSection frame at all
// (sectionHasMatchingField returns false).
func TestRowComposer_AddMultiValueAttributes_SkipsScalars(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	logBefore := append([]string(nil), dml.log...)
	require.NoError(t, m.AddMultiValueAttributes(stackedB{Id: 1, Label: "alpha"}))
	require.NoError(t, m.CommitRow())

	// Between BeginRow's emit and CommitRow's CommitEntity, the only
	// new entries should be from CommitEntity — AddMultiValueAttributes
	// on a pure-scalar DTO is a no-op.
	tail := dml.log[len(logBefore):]
	for _, line := range tail {
		require.NotContains(t, line, "Symbol", "AddMultiValueAttributes should not open Symbol section for pure-scalar DTO")
	}
	require.Contains(t, dml.log, "CommitEntity")
}

// TestRowComposer_CardinalitySplit_OneOneMany confirms the
// 1,1,…,>1,>1,… per-section attribute ordering when chaining the two
// new methods across multiple DTOs in the same row.
//
// Both DTOs share section "symbol". DTO A's Brand is len=1 (so its
// container attribute is size-1); DTO B's Brand is len=3 (size-3).
// Chaining AddSingleValueAttributes(A) → AddSingleValueAttributes(B)
// → AddMultiValueAttributes(A) → AddMultiValueAttributes(B) should
// produce — within section symbol — attributes in this order:
//
//	A.Color ("red")           [size 1, scalar]
//	A.Brand[0]                 [size 1, container len=1]
//	B.Label ("alpha")          [size 1, scalar]
//	B.Brand{x,y,z}             [size 3, container len=3]
//
// (A's multi-value pass produces nothing because its Brand is len=1
// which is single-value; B's single-value pass produces only its
// scalar Label because Brand is len=3.)
func TestRowComposer_CardinalitySplit_OneOneMany(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	rowA := stackedMixed{Id: 1, Color: "red", Brand: []string{"acme"}}
	rowB := stackedMixed{Id: 2, Color: "blue", Brand: []string{"x", "y", "z"}}

	// BeginRow emits plain + plainOwner's full sections. To exercise
	// only the filtered methods, plainOwner is a scalar-only DTO and
	// we drive the cardinality split via Add* calls on the mixed DTOs.
	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "owner"}))
	require.NoError(t, m.AddSingleValueAttributes(rowA))
	require.NoError(t, m.AddSingleValueAttributes(rowB))
	require.NoError(t, m.AddMultiValueAttributes(rowA))
	require.NoError(t, m.AddMultiValueAttributes(rowB))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")

	// Order assertion: rowA's scalar + len=1 container, then rowB's
	// scalar, then rowB's len=3 container. rowA's MultiValue pass
	// produces no Symbol cycle (len=1 is single-value).
	redIdx := strings.Index(joined, `Symbol.BeginAttribute("red")`)
	acmeIdx := strings.Index(joined, `AddToContainerP("acme")`)
	blueIdx := strings.Index(joined, `Symbol.BeginAttribute("blue")`)
	xIdx := strings.Index(joined, `AddToContainerP("x")`)

	require.NotEqual(t, -1, redIdx)
	require.NotEqual(t, -1, acmeIdx)
	require.NotEqual(t, -1, blueIdx)
	require.NotEqual(t, -1, xIdx)
	require.Less(t, redIdx, acmeIdx, "size-1 scalar precedes size-1 container within rowA's single-value pass")
	require.Less(t, acmeIdx, blueIdx, "rowA's single-value emit precedes rowB's single-value emit")
	require.Less(t, blueIdx, xIdx, "size-1 emits precede size-3 container")

	// rowA's container is len=1 so its MultiValue pass emits nothing.
	require.Equal(t, 1, strings.Count(joined, `AddToContainerP("acme")`),
		"rowA's len=1 container should appear exactly once (single-value pass only)")
	// rowB's container has three elements added.
	require.Equal(t, 1, strings.Count(joined, `AddToContainerP("x")`))
	require.Equal(t, 1, strings.Count(joined, `AddToContainerP("y")`))
	require.Equal(t, 1, strings.Count(joined, `AddToContainerP("z")`))
}

// TestRowComposer_AddSingleValueAttributes_EmptyContainerSkipped
// confirms an empty container produces no attribute at all from
// either method (splice semantics preserved).
func TestRowComposer_AddSingleValueAttributes_EmptyContainerSkipped(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	row := stackedMixed{Id: 1, Color: "red", Brand: nil}
	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "owner"}))
	require.NoError(t, m.AddSingleValueAttributes(row))
	require.NoError(t, m.AddMultiValueAttributes(row))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.NotContains(t, joined, "AddToContainerP",
		"empty Brand should produce no container attribute under either filter")
	// The scalar Color still emits under the single-value pass.
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
}

// TestRowComposer_FilteredMethodsRequireBeginRow confirms the
// state-machine guard applies to the new methods as well.
func TestRowComposer_FilteredMethodsRequireBeginRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	err := m.AddSingleValueAttributes(stackedA{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside of a row")

	err = m.AddMultiValueAttributes(stackedA{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside of a row")
}
