package canonicaltypeedit

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDraftRoundTrip is the core bidirectional contract: a parsed primitive
// loaded into the draft (nodeToDraft) and rebuilt (draftToNode) must produce
// the exact canonical string back, across every family and modifier. This is
// what guarantees the formula bar ⇄ form sync converges.
func TestDraftRoundTrip(t *testing.T) {
	cases := []string{
		"u32", "u32l", "i64n", "f64", "i8", "u8m",
		"z64", "d32", "t64",
		"s", "y", "b", "sx128", "sh", "ym",
		"v", "vc", "w", "wcm", "wch",
	}
	m := NewModel()
	for _, s := range cases {
		n, err := parsePrimitive(s)
		require.NoError(t, err, "parse %q", s)
		m.nodeToDraft(n)
		m.rebuildFromDraft()
		assert.Equal(t, s, m.Canonical(), "round-trip %q", s)
	}
}

// TestNewModelDefault pins the friendly default and that the bar mirrors it.
func TestNewModelDefault(t *testing.T) {
	m := NewModel()
	assert.Equal(t, "u32", m.Canonical())
	assert.True(t, m.Valid())
	var st State
	st.syncBar(m)
	assert.Equal(t, "u32", st.barBuf, "the bar follows the model on first sync")
}

// TestSetCanonical confirms seeding from a string and the no-op-on-garbage
// contract.
func TestSetCanonical(t *testing.T) {
	m := NewModel()
	var st State
	st.syncBar(m)
	m.SetCanonical("i64n")
	st.syncBar(m)
	assert.Equal(t, "i64n", m.Canonical())
	assert.Equal(t, "i64n", st.barBuf, "a seed moves the bar")
	assert.True(t, m.Valid())
	// Invalid input leaves the editor unchanged.
	m.SetCanonical("@@@")
	assert.Equal(t, "i64n", m.Canonical())
}

// TestDraftValidity locks the invalid-but-constructible case: a fixed-width
// string with width 0 canonicalises to "sx0" and reports invalid.
func TestDraftValidity(t *testing.T) {
	m := &Model{base: byte(canonicaltypes.BaseTypeStringUtf8), fixedWidth: true, width: 0}
	m.rebuildFromDraft()
	assert.Equal(t, "sx0", m.Canonical())
	assert.False(t, m.Valid())
}

// TestFamilyOf pins the base-rune → family derivation across all families.
func TestFamilyOf(t *testing.T) {
	assert.Equal(t, familyString, familyOf(byte(canonicaltypes.BaseTypeStringUtf8)))
	assert.Equal(t, familyString, familyOf(byte(canonicaltypes.BaseTypeStringBool)))
	assert.Equal(t, familyNumeric, familyOf(byte(canonicaltypes.BaseTypeMachineNumericFloat)))
	assert.Equal(t, familyTemporal, familyOf(byte(canonicaltypes.BaseTypeTemporalZonedTime)))
	assert.Equal(t, familyNetwork, familyOf(byte(canonicaltypes.BaseTypeNetworkIPv6)))
}

// TestFamilyDefaultBase confirms each family's default base belongs to it.
func TestFamilyDefaultBase(t *testing.T) {
	for _, f := range []familyE{familyString, familyNumeric, familyTemporal, familyNetwork} {
		assert.Equal(t, f, familyOf(familyDefaultBase(f)), "family %d", f)
	}
}

// TestClampWidth pins the form width guard.
func TestClampWidth(t *testing.T) {
	assert.Equal(t, uint16(1), clampWidth(0))
	assert.Equal(t, uint16(32), clampWidth(32))
	assert.Equal(t, uint16(4096), clampWidth(99999))
}

// TestFirstLine covers the parse-error headline helper.
func TestFirstLine(t *testing.T) {
	assert.Equal(t, "headline", firstLine("headline\ndetail"))
	assert.Equal(t, "solo", firstLine("solo"))
}

// TestRenderHeadless renders the primitive editor for one frame under the
// discard channel from a zero State, with the form disclosed, and pins the
// W17 nil-input path (ADR-0267 W19).
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	m := NewModel()
	st := &State{}
	st.SetFormOpen(true)
	res := Render(Input{Ids: ids, ScopeKey: "t", Model: m, State: st})
	require.NoError(t, res.Err)
	assert.False(t, res.Changed)
	assert.Equal(t, "u32", st.barBuf)
	assert.ErrorIs(t, Render(Input{Ids: ids, Model: m}).Err, ErrNeedsIdsModelAndState)
	assert.Equal(t, 0, ids.Depth(), "the id stack is left balanced")
}
