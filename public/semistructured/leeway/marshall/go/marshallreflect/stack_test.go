package marshallreflect_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
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
func (a *recordingAttr) AddMembershipMixedLowCardVerbatimP(name []byte, params []byte) {
	a.root.record(fmt.Sprintf("AddMembershipMixedLowCardVerbatimP(%q, %q)", name, params))
}
func (a *recordingAttr) AddMembershipLowCardRefParametrizedP(params []byte) {
	a.root.record(fmt.Sprintf("AddMembershipLowCardRefParametrizedP(%q)", params))
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

// TestRowComposer_Stacked_TwoDTOsOneRow confirms multiple DTOs can contribute
// to one entity (ADR-0070 D1): BeginRow with DTO-A's row owns plains and emits
// A's sections, AddSections with DTO-B's row adds B's sections, then CommitRow
// closes. Order of section emit follows call order.
//
// The two DTOs here happen to target disjoint sections; they would not have
// to. Sharing a section is supported — contributions merge into one buffered
// frame per section (ADR-0146 D6, which retracted ADR-0070 D3's two-visit
// shape) — see TestRowComposer_SharedSectionSharesOneFrame.
func TestRowComposer_Stacked_TwoDTOsOneRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.AddSections(stackedFoo{Id: 99, Note: "alpha"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	// Exactly one entity frame.
	require.Equal(t, 1, strings.Count(joined, "BeginEntity"))
	require.Equal(t, 1, strings.Count(joined, "CommitEntity"))
	// Both DTOs' section values appear.
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Foo.BeginAttribute("alpha")`)
	// Plains owned by A (Id=1), not B (Id=99).
	require.Contains(t, joined, `SetId(1, "")`)
	require.NotContains(t, joined, `SetId(99, "")`)
	// A's section emit precedes B's.
	redIdx := strings.Index(joined, `Symbol.BeginAttribute("red")`)
	alphaIdx := strings.Index(joined, `Foo.BeginAttribute("alpha")`)
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
	require.NoError(t, m.AddSections(stackedFoo{Id: 99, Note: "alpha"}))
	require.NoError(t, m.CommitRow())

	require.NoError(t, m.BeginRow(stackedA{Id: 2, Color: "blue"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 2, strings.Count(joined, "BeginEntity"))
	require.Equal(t, 2, strings.Count(joined, "CommitEntity"))
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Foo.BeginAttribute("alpha")`)
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

// --- ADR-0146 D6: one section FRAME per entity, shared by every contributor ---
//
// ADR-0070 D3 claimed two DTOs could both emit into section Foo, producing two
// BeginSectionFoo…EndSection cycles. No generated DML supports that: BeginEntity
// opens each section once, EndSection returns it to Initial, and nothing
// reopens it. The recordingDML used here has no state machine, which is why the
// two-pass RowComposer API appeared to work in these tests while failing
// against a real DML.
//
// Two DTOs on one section is nevertheless normal — facts get fused and enriched
// by stages that do not know every component — so the composer buffers each
// contribution and writes them into ONE shared frame at CommitRow, rather than
// refusing the overlap.

// stackedFoo targets the mock's `foo` section, so stacking it beside a DTO on
// `symbol` exercises the disjoint case.
type stackedFoo struct {
	_    struct{} `kind:"foo"`
	Id   uint64   `lw:",id"`
	Note string   `lw:"note,foo"`
}

// TestRowComposer_SharedSectionSharesOneFrame confirms two DTOs on one section
// produce ONE section cycle carrying both their attributes.
func TestRowComposer_SharedSectionSharesOneFrame(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.AddSections(stackedB{Id: 99, Label: "alpha"}),
		"stackedA and stackedB both emit into `symbol`; that is allowed")
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 1, strings.Count(joined, "GetSectionSymbol"),
		"one frame, not one per contributor")
	require.Equal(t, 1, strings.Count(joined, "Symbol.EndSection"))
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	// Call order is preserved inside the shared frame.
	require.Less(t,
		strings.Index(joined, `Symbol.BeginAttribute("red")`),
		strings.Index(joined, `Symbol.BeginAttribute("alpha")`))
}

// TestRowComposer_SectionsWrittenAtCommit records the timing consequence of
// buffering: nothing reaches the DML's section frames until CommitRow.
func TestRowComposer_SectionsWrittenAtCommit(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NotContains(t, strings.Join(dml.log, "\n"), "GetSectionSymbol",
		"sections are buffered until CommitRow")
	// The entity header is not buffered — it must precede any section.
	require.Contains(t, strings.Join(dml.log, "\n"), `SetId(1, "")`)

	require.NoError(t, m.CommitRow())
	require.Contains(t, strings.Join(dml.log, "\n"), "GetSectionSymbol")
}

// TestRowComposer_AllowsDisjointSections covers the plain stacking case:
// DTOs that touch different sections (the ADR-0070 D1 case) each get their
// own frame.
func TestRowComposer_AllowsDisjointSections(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.AddSections(stackedFoo{Id: 1, Note: "n"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
	require.Contains(t, joined, "GetSectionFoo")
}

// TestRowComposer_SectionBufferResetsPerRow confirms the buffer is per entity:
// row 2 must not replay row 1's contributions.
func TestRowComposer_SectionBufferResetsPerRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	require.NoError(t, m.CommitRow())
	require.NoError(t, m.BeginRow(stackedA{Id: 2, Color: "blue"}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 2, strings.Count(joined, "BeginEntity"))
	require.Contains(t, joined, `Symbol.BeginAttribute("blue")`)
	require.Equal(t, 1, strings.Count(joined, `Symbol.BeginAttribute("red")`),
		"row 1's contribution must not be replayed into row 2")
}

// TestRowComposer_AddSectionsRequiresBeginRow keeps the state-machine guard
// that the removed filtered methods also carried.
func TestRowComposer_AddSectionsRequiresBeginRow(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	err := m.AddSections(stackedA{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside of a row")
}

// TestRowComposer_ScalarFirstOrderingSurvives pins the ordering that IS
// decided — ADR-0071 C1's static scalar-before-container partition within a
// section. The runtime-cardinality refinement the two-pass API added on top was
// never an accepted decision, and removing it leaves C1 untouched.
func TestRowComposer_ScalarFirstOrderingSurvives(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedMixed{Id: 1, Color: "red", Brand: []string{"x", "y", "z"}}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	redIdx := strings.Index(joined, `Symbol.BeginAttribute("red")`)
	xIdx := strings.Index(joined, `AddToContainerP("x")`)
	require.NotEqual(t, -1, redIdx)
	require.NotEqual(t, -1, xIdx)
	require.Less(t, redIdx, xIdx, "the scalar field emits before the container field")
}

// TestRowComposer_EmptyContainerIsSpliced confirms splice semantics survive the
// removal: an empty container emits no attribute, the scalar still does.
func TestRowComposer_EmptyContainerIsSpliced(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedMixed{Id: 1, Color: "red", Brand: nil}))
	require.NoError(t, m.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.NotContains(t, joined, "AddToContainerP", "an empty container emits nothing")
	require.Contains(t, joined, `Symbol.BeginAttribute("red")`)
}

// The composer now holds the frame invariant the generated builders hold
// (ADR-0183 D4): one contribution per kind per entity. Two DTOs of one kind on
// one row would claim the same slots twice, which the read side reports as an
// arity error rather than as two components — so the refusal belongs where the
// second contribution is made, not three layers later.
func TestRowComposer_RefusesASecondContributionFromOneKind(t *testing.T) {
	dml := &recordingDML{}
	m := marshallreflect.NewRowComposer(dml, fakeLookup{})

	require.NoError(t, m.BeginRow(stackedA{Id: 1, Color: "red"}))
	err := m.AddSections(stackedA{Id: 1, Color: "blue"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "one contribution per kind")

	// Different kinds on the same row stay legal, which is the whole point of
	// the composer.
	require.NoError(t, m.AddSections(stackedB{Id: 99, Label: "alpha"}))
	require.NoError(t, m.CommitRow())

	// The next row starts clean.
	require.NoError(t, m.BeginRow(stackedA{Id: 2, Color: "green"}))
	require.NoError(t, m.CommitRow())
}
