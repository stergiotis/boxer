package mappingplanview

import (
	"errors"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
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
func shp(t *testing.T, goType string) mappingplan.FieldShape {
	t.Helper()
	cn, err := mappingplan.ScalarCanonicalForGoType(goType)
	require.NoError(t, err)
	return mappingplan.FieldShape{Canonical: cn}
}

// TestClassifyConflict_realErrors is the drift guard: it builds plans that trip
// mappingplan's *actual* cross-field and local checks and asserts the classifier
// sorts them correctly. If mappingplan rewords one of these messages out of
// crossFieldPhrases, this fails rather than silently misclassifying.
func TestClassifyConflict_realErrors(t *testing.T) {
	// Duplicate plain column — a real AddField-time cross-field rejection.
	b := mappingplan.NewPlanBuilder("t", "p", "T")
	require.NoError(t, b.AddUnderscoreField("k", "", ""))
	require.NoError(t, b.AddField("Id", ",id", shp(t, "uint64")))
	dupErr := b.AddField("Id2", ",id", shp(t, "uint64"))
	require.Error(t, dupErr)
	assert.Truef(t, classifyConflict(dupErr), "duplicate plain column must classify as conflict: %v", dupErr)

	// explode without a multi shape — a real local (own-fault) rejection.
	b2 := mappingplan.NewPlanBuilder("t", "p", "T")
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
