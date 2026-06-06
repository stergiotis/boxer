package canonicaltypeedit

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stretchr/testify/assert"
)

// TestNewSignatureModel pins the default: one `u32` element, valid, not yet a
// signature (a single primitive).
func TestNewSignatureModel(t *testing.T) {
	sm := NewSignatureModel()
	assert.Equal(t, "u32", sm.Canonical())
	assert.True(t, sm.Valid())
	assert.Len(t, sm.elems, 1)
	assert.False(t, sm.Node().IsSignature())
}

// TestSignatureRoundTrip is the composition contract: a signature string seeded
// in must assemble back to the exact same canonical string across groups and
// signature boundaries.
func TestSignatureRoundTrip(t *testing.T) {
	cases := []string{
		"u32",
		"u32-s",
		"u32-s_vc",
		"u8-i16-f64",
		"s_y_b",
		"vc_wcm",
		"u32l-s_z64_w",
	}
	sm := NewSignatureModel()
	for _, s := range cases {
		sm.SetCanonical(s)
		assert.Equal(t, s, sm.Canonical(), "round-trip %q", s)
	}
}

// TestSignatureShape locks the assembled-node shape: one scalar element is a
// primitive, a '-'-run is a group, and a '_' makes it a signature.
func TestSignatureShape(t *testing.T) {
	sm := NewSignatureModel()

	sm.SetCanonical("u32")
	assert.True(t, sm.Node().IsPrimitive())
	assert.False(t, sm.Node().IsSignature())

	sm.SetCanonical("u32-s")
	assert.False(t, sm.Node().IsPrimitive()) // a group
	assert.False(t, sm.Node().IsSignature())

	sm.SetCanonical("u32-s_vc")
	assert.True(t, sm.Node().IsSignature())
	assert.True(t, sm.Valid())
}

// TestSignatureRemoveAt confirms removal reassembles correctly: dropping the
// middle element of u32-s_vc leaves u32 and vc joined by u32's '-' separator.
func TestSignatureRemoveAt(t *testing.T) {
	sm := NewSignatureModel()
	sm.SetCanonical("u32-s_vc")
	assert.Len(t, sm.elems, 3)
	sm.removeAt(1)
	sm.rebuild()
	assert.Equal(t, "u32-vc", sm.Canonical())
	assert.Len(t, sm.elems, 2)
}

// TestSignatureRemoveAtGuards the never-empty contract.
func TestSignatureRemoveAtGuard(t *testing.T) {
	sm := NewSignatureModel()
	sm.removeAt(0) // only one element — must be a no-op
	assert.Len(t, sm.elems, 1)
}

// TestSignatureInvalidElement propagates element invalidity to the whole
// signature. A fixed-width string with width 0 (sx0) is invalid AND
// unparseable, so the element is built directly rather than via SetCanonical.
func TestSignatureInvalidElement(t *testing.T) {
	good := &Model{base: byte(canonicaltypes.BaseTypeMachineNumericUnsigned), width: 32}
	good.rebuildFromDraft()
	bad := &Model{base: byte(canonicaltypes.BaseTypeStringUtf8), fixedWidth: true, width: 0}
	bad.rebuildFromDraft()
	sm := &SignatureModel{
		elems: []*sigElem{{prim: good, sep: grpSepByte}, {prim: bad, sep: sigSepByte}},
		sel:   0,
	}
	sm.rebuild()
	assert.Equal(t, "u32-sx0", sm.Canonical())
	assert.False(t, sm.Valid())
}
