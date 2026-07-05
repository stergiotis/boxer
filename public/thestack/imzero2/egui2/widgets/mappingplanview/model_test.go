package mappingplanview

import (
	"errors"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypeedit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkRow builds a FieldRow directly (no Model / pager / egui host) with a real
// canonical type editor seeded to uint64 and a fresh per-field FSM — the
// runtime-free fixture the white-box derivation tests reconcile.
func mkRow() *FieldRow {
	r := &FieldRow{typeModel: canonicaltypeedit.NewModel(), fsm: newFieldFSM()}
	r.SetGoType("uint64")
	return r
}

func TestRowIsEmpty(t *testing.T) {
	r := mkRow()
	assert.True(t, rowIsEmpty(r), "a fresh row (default type only) is empty")
	r.GoField = "V"
	assert.False(t, rowIsEmpty(r), "any authored content makes it non-empty")
	r.GoField = ""
	r.Section = "id"
	assert.False(t, rowIsEmpty(r), "a section alone (plain column) counts as content")
}

func TestRowIncompleteReason(t *testing.T) {
	// tagged value field missing its section
	r := mkRow()
	r.GoField, r.Membership = "V", "m"
	assert.Equal(t, "tagged field needs a section", rowIncompleteReason(r))

	// tagged value field with a section but no Go field name
	r2 := mkRow()
	r2.Membership, r2.Section = "m", "sec"
	assert.Equal(t, "value field needs a Go field name", rowIncompleteReason(r2))

	// fully specified tagged value field → ready
	r3 := mkRow()
	r3.GoField, r3.Membership, r3.Section = "V", "m", "sec"
	assert.Equal(t, "", rowIncompleteReason(r3))

	// a plain column with a name → ready
	r4 := mkRow()
	r4.Section = "id"
	assert.Equal(t, "", rowIncompleteReason(r4))

	// an under-specified const is incomplete (exact message is secondary)
	r5 := mkRow()
	r5.IsConst, r5.ConstValue = true, "x"
	assert.NotEqual(t, "", rowIncompleteReason(r5))
}

func TestDeriveState(t *testing.T) {
	// local readiness wins regardless of the build report
	empty := mkRow()
	st, _ := deriveState(empty, 0, BuildResult{FirstFailIdx: -1})
	assert.Equal(t, StateEmpty, st)

	incomplete := mkRow()
	incomplete.Membership = "m" // tagged, no section
	st, reason := deriveState(incomplete, 0, BuildResult{FirstFailIdx: -1})
	assert.Equal(t, StateIncomplete, st)
	assert.NotEqual(t, "", reason)

	ready := func() *FieldRow {
		r := mkRow()
		r.GoField, r.Membership, r.Section = "V", "m", "sec"
		require.Equal(t, "", rowIncompleteReason(r), "fixture must be locally ready")
		return r
	}

	// every AddField passed → Valid
	st, _ = deriveState(ready(), 2, BuildResult{FirstFailIdx: -1})
	assert.Equal(t, StateValid, st)

	// a Finish error stays plan-level — accepted fields are still Valid
	st, _ = deriveState(ready(), 0, BuildResult{FirstFailIdx: -1, FinishErr: errors.New("plan-level")})
	assert.Equal(t, StateValid, st)

	// before the failing field → Valid
	st, _ = deriveState(ready(), 0, BuildResult{FirstFailIdx: 2, FirstFailErr: errors.New("boom")})
	assert.Equal(t, StateValid, st)

	// the failing field, local rejection → Rejected (reason = first line)
	st, reason = deriveState(ready(), 2, BuildResult{FirstFailIdx: 2, FirstFailErr: errors.New("explode requires a multi-element shape")})
	assert.Equal(t, StateRejected, st)
	assert.Equal(t, "explode requires a multi-element shape", reason)

	// the failing field, cross-field rejection → Conflicting
	st, _ = deriveState(ready(), 2, BuildResult{FirstFailIdx: 2, FirstFailErr: errors.New("plain column declared on two DTO fields")})
	assert.Equal(t, StateConflicting, st)

	// after the failing field → Blocked (reason names the 1-based culprit index)
	st, reason = deriveState(ready(), 5, BuildResult{FirstFailIdx: 2, FirstFailErr: errors.New("boom")})
	assert.Equal(t, StateBlocked, st)
	assert.Contains(t, reason, "#3")
}

// TestClassifyConflict_phrases pins that every curated cross-field phrase is
// matched, and that a local-shape message and nil are not — guarding the
// classifier's own list against an accidental deletion.
func TestClassifyConflict_phrases(t *testing.T) {
	for _, p := range crossFieldPhrases {
		assert.Truef(t, classifyConflict(errors.New("prefix "+p+" suffix")), "phrase %q must classify as conflict", p)
	}
	assert.False(t, classifyConflict(errors.New("explode requires a multi-element shape")), "a local shape error is not a conflict")
	assert.False(t, classifyConflict(nil))
}

// shp builds a scalar FieldShape from a Go type, mirroring mappingplan's own
// build_test idiom.
func shp(t *testing.T, goType string) goplan.FieldShape {
	t.Helper()
	cn, err := goplan.ScalarCanonicalForGoType(goType)
	require.NoError(t, err)
	return goplan.FieldShape{Canonical: cn}
}

// TestClassifyConflict_realErrors is the drift guard: it builds plans that trip
// mappingplan's *actual* cross-field and local checks and asserts the classifier
// sorts them correctly. If mappingplan rewords one of these messages out of
// crossFieldPhrases, this fails rather than silently misclassifying.
func TestClassifyConflict_realErrors(t *testing.T) {
	// Duplicate plain column — a real AddField-time cross-field rejection.
	b := goplan.NewPlanBuilder("t", "p", "T")
	require.NoError(t, b.AddUnderscoreField("k", "", ""))
	require.NoError(t, b.AddField("Id", ",id", shp(t, "uint64")))
	dupErr := b.AddField("Id2", ",id", shp(t, "uint64"))
	require.Error(t, dupErr)
	assert.Truef(t, classifyConflict(dupErr), "duplicate plain column must classify as conflict: %v", dupErr)

	// explode without a multi shape — a real local (own-fault) rejection.
	b2 := goplan.NewPlanBuilder("t", "p", "T")
	require.NoError(t, b2.AddUnderscoreField("k", "", ""))
	localErr := b2.AddField("V", "m,sec,explode", shp(t, "uint64"))
	require.Error(t, localErr)
	assert.Falsef(t, classifyConflict(localErr), "explode-without-multi must stay a local reject: %v", localErr)
}

func TestStateRollup(t *testing.T) {
	assert.Equal(t, "", (&Model{}).stateRollup(), "no fields → empty rollup")
	m := &Model{Fields: []*FieldRow{
		{state: StateValid}, {state: StateValid}, {state: StateConflicting}, {state: StateBlocked},
	}}
	// Order follows fieldStateOrder (valid before conflict before blocked).
	assert.Equal(t, "2 valid · 1 conflict · 1 blocked", m.stateRollup())
}

// mkTupleElem builds a TupleElemRow fixture directly (no Model), defaulting to
// the verbatim channel and a string value type.
func mkTupleElem(goField string, memb bool) *TupleElemRow {
	e := &TupleElemRow{
		GoField:      goField,
		IsMembership: memb,
		Channel:      mappingplan.MembershipChannelLowCardVerbatim,
		typeModel:    canonicaltypeedit.NewModel(),
	}
	e.SetGoType("string")
	return e
}

// mkTupleRow builds a ready dynamic-membership tuple fixture (ADR-0103) over
// an anchor-style mixed `text` section: one @membership element + the scalar
// text sub-column + a container sub-column.
func mkTupleRow() *FieldRow {
	r := &FieldRow{IsTuple: true, fsm: newFieldFSM(), typeModel: canonicaltypeedit.NewModel()}
	r.GoField, r.Section, r.TupleStructType = "Texts", "text", "LabeledText"
	label := mkTupleElem("Label", true)
	text := mkTupleElem("Text", false)
	text.Column = "text"
	bag := mkTupleElem("WordBag", false)
	bag.Column = "wordBag"
	bag.typeModel.SetCanonical("sh")
	r.TupleElems = []*TupleElemRow{label, text, bag}
	return r
}

func TestTupleRowReadiness(t *testing.T) {
	fresh := &FieldRow{IsTuple: true, fsm: newFieldFSM(), typeModel: canonicaltypeedit.NewModel()}
	assert.True(t, rowIsEmpty(fresh), "a bare tuple row is empty")

	fresh.GoField = "Texts"
	assert.Equal(t, "tuple needs a section", rowIncompleteReason(fresh))
	fresh.Section = "text"
	assert.Equal(t, "tuple needs an element struct name", rowIncompleteReason(fresh))
	fresh.TupleStructType = "LabeledText"
	assert.Contains(t, rowIncompleteReason(fresh), "tuple needs elements")

	memb := mkTupleElem("", true)
	fresh.TupleElems = []*TupleElemRow{memb}
	assert.Equal(t, "@membership element needs a Go field name", rowIncompleteReason(fresh))
	memb.GoField = "Label"
	assert.Equal(t, "", rowIncompleteReason(fresh), "structural rules (value elements, single membership) stay with the builder")

	ready := mkTupleRow()
	assert.Equal(t, "", rowIncompleteReason(ready))
}

func TestTupleElemLWTag(t *testing.T) {
	memb := mkTupleElem("Label", true)
	assert.Equal(t, "@membership,lowCardVerbatim", memb.ElemLWTag("text"))
	memb.Channel = mappingplan.MembershipChannelHighCardVerbatim
	assert.Equal(t, "@membership,highCardVerbatim", memb.ElemLWTag("text"))

	val := mkTupleElem("WordBag", false)
	val.Column = "wordBag"
	assert.Equal(t, "text:wordBag", val.ElemLWTag("text"))
	val.Column = ""
	assert.Equal(t, "text", val.ElemLWTag("text"))

	r := mkTupleRow()
	assert.Equal(t, "text", r.LWTag(), "the tuple's outer tag is the bare section name")
}

// TestTupleRow_realBuild is the drift guard for the tuple path: the row's
// LWTag / TupleElemSpecs hand a REAL goplan.PlanBuilder a plan it accepts. A
// tuple may now carry MORE THAN ONE `@membership` (ADR-0109) — the second is
// accepted, not rejected — while a structurally bad tuple (a duplicated
// sub-column) is still rejected with the builder's own error, a local
// rejection rather than a cross-field conflict.
func TestTupleRow_realBuild(t *testing.T) {
	r := mkTupleRow()
	r.TupleElems = append(r.TupleElems, mkTupleElem("Label2", true)) // second membership — now valid
	b := goplan.NewPlanBuilder("t", "p", "T")
	require.NoError(t, b.AddUnderscoreField("k", "", ""))
	require.NoError(t, b.AddField("Id", ",id", shp(t, "uint64")))
	require.NoError(t, b.AddTupleSliceField(r.GoField, r.LWTag(), r.TupleStructType, r.TupleElemSpecs()))
	plan, err := b.Finish()
	require.NoError(t, err)
	for _, f := range plan.Fields {
		assert.Equal(t, "Texts", f.TupleField)
		require.Len(t, f.TupleMemberships, 2)
		assert.Equal(t, "Label", f.TupleMemberships[0].GoField)
		assert.Equal(t, "Label2", f.TupleMemberships[1].GoField)
	}

	bad := mkTupleRow()
	dup := mkTupleElem("WordBag2", false)
	dup.Column = "wordBag" // duplicate of the existing WordBag sub-column
	bad.TupleElems = append(bad.TupleElems, dup)
	b2 := goplan.NewPlanBuilder("t", "p", "T")
	require.NoError(t, b2.AddUnderscoreField("k", "", ""))
	rejErr := b2.AddTupleSliceField(bad.GoField, bad.LWTag(), bad.TupleStructType, bad.TupleElemSpecs())
	require.Error(t, rejErr)
	assert.Contains(t, rejErr.Error(), "sub-column appears on two tuple element fields")
	assert.False(t, classifyConflict(rejErr), "a tuple's own structural fault is a local reject")
}
