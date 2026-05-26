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

// --- DTOs for per-row composer tests. ---

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
