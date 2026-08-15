package marshallreflect_test

// ADR-0146 D3 / M4: only PRIMARY memberships discriminate on read, realizing
// ADR-0073 E1. A secondary membership annotates the attribute a primary one
// located; it must not pull that attribute's value into a field of its own.
//
// Role filtering is inert unless a caller supplies a classifier, so the first
// test here is that nothing changed by default.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
)

// secondaryNames classifies the listed memberships as annotations and
// everything else as identity-bearing — the inverse of PathPrefixClassifier's
// path-prefix convention, and the shape an application with a small annotation
// vocabulary would write.
type secondaryNames map[string]bool

func (s secondaryNames) Classify(_ membershiprole.SectionContext, mv membership.MembershipValue) (membershiprole.MembershipRoleE, membershiprole.ParamTreatmentE) {
	if s[mv.Verbatim] {
		return membershiprole.MembershipRoleSecondary, membershiprole.ParamTreatmentNone
	}
	return membershiprole.MembershipRolePrimary, membershiprole.ParamTreatmentNone
}

// rfElem carries TWO verbatim memberships on one attribute — one identity-
// bearing, one an annotation. ADR-0109 D3 allows any number of fixed
// membership fields on a channel.
type rfElem struct {
	Primary   string `lw:"@membership,verbatim"`
	Annotated string `lw:"@membership,verbatim"`
	Value     string `lw:"symbol:value"`
}

type rfWriter struct {
	_        struct{} `kind:"rfWriter"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Elems    []rfElem `lw:"symbol"`
}

// The component that legitimately owns the attribute.
type rfHealth struct {
	_        struct{} `kind:"rfHealth"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Health   string   `lw:"health,symbol,verbatim"`
}

// A DTO whose field names the ANNOTATION. Without role filtering it steals the
// annotated attribute's value; with it, the slot reads as unpopulated.
type rfAudited struct {
	_        struct{} `kind:"rfAudited"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Audited  string   `lw:"audited,symbol,verbatim"`
}

func rfWrite(t *testing.T) (arrow.RecordBatch, func()) {
	t.Helper()
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(tbl, []rfWriter{{
		ID: 1, Tracking: []byte("R"),
		Elems: []rfElem{{Primary: "health", Annotated: "audited", Value: "green"}},
	}}, nil))
	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

func rfReaders(t *testing.T, rec arrow.RecordBatch) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(rec))
	return marshallreflect.NewSectionReaders(idR.Len()).
			PlainColumn("id", idR.ValueId).
			PlainColumn("naturalKey", idR.ValueNaturalKey).
			Section("symbol", symR.GetAttributes(), symR.GetMemberships()),
		func() { idR.Release(); symR.Release() }
}

// Without a classifier both memberships discriminate — the pre-M4 behaviour,
// which must not change for callers that pass no option.
func TestRole_NoClassifierIsUnchanged(t *testing.T) {
	rec, release := rfWrite(t)
	defer release()
	readers, rel := rfReaders(t, rec)
	defer rel()

	var health []rfHealth
	require.NoError(t, marshallreflect.Unmarshal(readers, &health, nil))
	require.Equal(t, "green", health[0].Health)

	// The annotation-named DTO also reads the value, because nothing tells the
	// codec that `audited` merely annotates.
	var audited []rfAudited
	require.NoError(t, marshallreflect.Unmarshal(readers, &audited, nil))
	require.Equal(t, "green", audited[0].Audited,
		"without a classifier every membership discriminates")
}

func TestRole_SecondaryMembershipDoesNotSelect(t *testing.T) {
	rec, release := rfWrite(t)
	defer release()
	readers, rel := rfReaders(t, rec)
	defer rel()
	classifier := marshallreflect.WithRoleClassifier(secondaryNames{"audited": true})

	// The primary membership still locates the attribute.
	var health []rfHealth
	require.NoError(t, marshallreflect.Unmarshal(readers, &health, nil, classifier))
	require.Equal(t, "green", health[0].Health)

	// The annotation no longer does: its slot is unpopulated, and a mandatory
	// scalar over an unpopulated slot is refused by the arity gate.
	var audited []rfAudited
	err := marshallreflect.Unmarshal(readers, &audited, nil, classifier)
	require.Error(t, err)
	require.ErrorContains(t, err, "at least 1")
}

// Detect must reach the same conclusion as the decode — otherwise a row could
// report Exact and then fail to decode.
func TestRole_DetectAgreesWithDecode(t *testing.T) {
	rec, release := rfWrite(t)
	defer release()
	readers, rel := rfReaders(t, rec)
	defer rel()
	classifier := marshallreflect.WithRoleClassifier(secondaryNames{"audited": true})

	p, err := marshallreflect.Detect[rfHealth](readers, 0, nil, classifier)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceExact, p)

	p, err = marshallreflect.Detect[rfAudited](readers, 0, nil, classifier)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceAbsent, p,
		"a kind whose only slot is a secondary membership is not carried")

	// Unfiltered, the same row detects the annotation kind as present.
	p, err = marshallreflect.Detect[rfAudited](readers, 0, nil)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceExact, p)
}

// The trap ADR-0146 D3 records, as an executable statement: PathPrefixClassifier
// marks primary by a "/" prefix, so ordinary DTO memberships classify as
// SECONDARY and every field reads back unpopulated. It is the right policy for
// the card / widget paths, whose memberships are `/`-prefixed paths, and the
// wrong one for the codec — which is why the codec's default is nil, not this.
func TestRole_PathPrefixClassifierIsWrongForPlainMemberships(t *testing.T) {
	rec, release := rfWrite(t)
	defer release()
	readers, rel := rfReaders(t, rec)
	defer rel()

	var health []rfHealth
	err := marshallreflect.Unmarshal(readers, &health, nil,
		marshallreflect.WithRoleClassifier(membershiprole.PathPrefixClassifier{}))
	require.Error(t, err,
		"`health` has no / prefix, so PathPrefixClassifier calls it secondary and nothing selects")

	// Passing no classifier — the actual default — decodes fine.
	health = nil
	require.NoError(t, marshallreflect.Unmarshal(readers, &health, nil))
	require.Equal(t, "green", health[0].Health)
}

// A nil classifier passed explicitly is the same as passing nothing, so a
// caller threading an optional policy through does not have to branch.
func TestRole_NilClassifierOptionIsInert(t *testing.T) {
	rec, release := rfWrite(t)
	defer release()
	readers, rel := rfReaders(t, rec)
	defer rel()

	var health []rfHealth
	require.NoError(t, marshallreflect.Unmarshal(readers, &health, nil,
		marshallreflect.WithRoleClassifier(nil)))
	require.Equal(t, "green", health[0].Health)
}
