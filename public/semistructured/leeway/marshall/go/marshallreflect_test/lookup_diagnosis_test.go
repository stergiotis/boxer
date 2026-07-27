package marshallreflect_test

// ADR-0146 §Scope: diagnosing a membership lookup that disagrees with the
// writer's registry.
//
// A ref-channel membership rides the wire as a bare uint64, so a reader asking
// for the wrong id observes exactly what an absent component observes. For a
// mandatory scalar the arity gate catches it; for an Option or a container,
// zero attributes is legal and the field simply reads empty. That is not
// checkable — these tests pin the diagnosis instead.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

type ldWriter struct {
	_        struct{} `kind:"ldWriter"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	State    string   `lw:"ldHealth,symbol"`
	Tags     []string `lw:"ldTags,symbolArray"`
}

type ldScalar struct {
	_        struct{} `kind:"ldScalar"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	State    string   `lw:"ldHealth,symbol"`
}

type ldOptional struct {
	_        struct{}              `kind:"ldOptional"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	State    option.Option[string] `lw:"ldHealth,symbol"`
}

type ldContainer struct {
	_        struct{} `kind:"ldContainer"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Tags     []string `lw:"ldTags,symbolArray"`
}

func ldRight() marshallreflect.MapLookup {
	return marshallreflect.MapLookup{"ldHealth": 1, "ldTags": 2}
}

// The reader's registry disagrees with the writer's: same names, different ids.
func ldWrong() marshallreflect.MapLookup {
	return marshallreflect.MapLookup{"ldHealth": 91, "ldTags": 92}
}

func ldWrite(t *testing.T) (arrow.RecordBatch, func()) {
	t.Helper()
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(tbl, []ldWriter{
		{ID: 1, Tracking: []byte("R"), State: "green", Tags: []string{"a", "b"}},
	}, ldRight()))
	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

func ldReaders(t *testing.T, rec arrow.RecordBatch) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(rec))
	saR := anchor.NewReadAccessTestTableTaggedSymbolArray()
	saR.SetColumnIndices(saR.GetColumnIndices())
	require.NoError(t, saR.LoadFromRecord(rec))
	return marshallreflect.NewSectionReaders(idR.Len()).
			PlainColumn("id", idR.ValueId).
			PlainColumn("naturalKey", idR.ValueNaturalKey).
			Section("symbol", symR.GetAttributes(), symR.GetMemberships()).
			Section("symbolArray", saR.GetAttributes(), saR.GetMemberships()),
		func() { idR.Release(); symR.Release(); saR.Release() }
}

// A mandatory scalar was already an error; the message now names what the
// section actually carried, which is what identifies the mismatch.
func TestLookupDiagnosis_ScalarErrorNamesWhatTheSectionCarries(t *testing.T) {
	rec, release := ldWrite(t)
	defer release()
	readers, rel := ldReaders(t, rec)
	defer rel()

	var got []ldScalar
	err := marshallreflect.Unmarshal(readers, &got, ldWrong())
	require.Error(t, err)
	require.ErrorContains(t, err, "ldHealth")
	require.ErrorContains(t, err, "section carries [1]",
		"the observed id is what tells you the lookup resolved 91 against a wire holding 1")
	require.ErrorContains(t, err, "membership lookup")
}

// Option and container fields stay silent, because zero attributes is a legal
// state for them. This pins that — it is the behaviour InspectLookup exists to
// explain, not a defect to be fixed by erroring.
func TestLookupDiagnosis_OptionAndContainerStaySilent(t *testing.T) {
	rec, release := ldWrite(t)
	defer release()
	readers, rel := ldReaders(t, rec)
	defer rel()

	var opt []ldOptional
	require.NoError(t, marshallreflect.Unmarshal(readers, &opt, ldWrong()))
	require.False(t, opt[0].State.Has, "reads as absent, indistinguishable from a true absence")

	var cont []ldContainer
	require.NoError(t, marshallreflect.Unmarshal(readers, &cont, ldWrong()))
	require.Empty(t, cont[0].Tags)
}

func TestLookupDiagnosis_InspectReportsTheMismatch(t *testing.T) {
	rec, release := ldWrite(t)
	defer release()
	readers, rel := ldReaders(t, rec)
	defer rel()

	rep, err := marshallreflect.InspectLookup[ldContainer](readers, ldWrong())
	require.NoError(t, err)
	require.Len(t, rep.Slots, 1)
	require.True(t, rep.Slots[0].Resolved)
	require.Equal(t, uint64(92), rep.Slots[0].ResolvedID)
	require.Zero(t, rep.Slots[0].Attributes, "nothing matched")
	require.Equal(t, []uint64{2}, rep.SectionRefIDs["symbolArray"],
		"the section does carry a membership — id 2, not the 92 asked for")

	suspect := rep.Suspect()
	require.Len(t, suspect, 1)
	require.Equal(t, "ldTags", suspect[0].Membership)
	require.Contains(t, rep.String(), "id=92")
}

// With the right lookup the same report shows the slot matching, and flags
// nothing.
func TestLookupDiagnosis_CorrectLookupIsClean(t *testing.T) {
	rec, release := ldWrite(t)
	defer release()
	readers, rel := ldReaders(t, rec)
	defer rel()

	rep, err := marshallreflect.InspectLookup[ldContainer](readers, ldRight())
	require.NoError(t, err)
	require.Equal(t, uint64(2), rep.Slots[0].ResolvedID)
	require.Equal(t, 1, rep.Slots[0].Rows)
	require.Equal(t, 1, rep.Slots[0].Attributes)
	require.Empty(t, rep.Suspect())
}

// Suspect is a heuristic, and this is the false positive it admits to: a
// component genuinely absent from a section other components populate looks
// exactly like a wrong lookup.
func TestLookupDiagnosis_SuspectAdmitsAFalsePositive(t *testing.T) {
	// `symbol` carries only ldHealth; a kind claiming a DIFFERENT membership in
	// that section is honestly absent, yet Suspect flags it.
	type ldOther struct {
		_        struct{}              `kind:"ldOther"`
		ID       uint64                `lw:",id"`
		Tracking []byte                `lw:",naturalKey"`
		Other    option.Option[string] `lw:"ldOther,symbol"`
	}
	rec, release := ldWrite(t)
	defer release()
	readers, rel := ldReaders(t, rec)
	defer rel()

	rep, err := marshallreflect.InspectLookup[ldOther](readers,
		marshallreflect.MapLookup{"ldOther": 55})
	require.NoError(t, err)
	require.Len(t, rep.Suspect(), 1,
		"honest absence in a populated section is indistinguishable from a wrong id")
}
