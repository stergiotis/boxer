package mappingplan

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// This file holds the two derived-view helpers the Plan IR's own methods
// depend on — DeriveGoShape (the single canonical→Go rule behind
// PlanCol.GoType / TaggedField.GoType) and UpperFirst (the PascalCase
// helper behind TaggedField.KindVar). They live with the IR rather than
// in the goplan construction machinery so the IR package depends on
// neither goplan nor any front-end; goplan and the marshallers reuse them
// via mappingplan.DeriveGoShape / mappingplan.UpperFirst.

// DeriveGoShape maps a field's canonical value type back to the Go-facing
// fields the rest of the pipeline reads. It is the single canonical→Go
// rule, applied once per field in goplan.PlanBuilder.AddField /
// addCarrierField and by the IR's GoType methods:
//
//   - The scalar modifier picks the multiplicity: HomogenousArray ⟺ a
//     top-level `[]T` element-slice (IsSlice), Set ⟺ `*roaring.Bitmap`
//     (IsRoaring), None ⟺ a scalar T.
//   - goType is the element type's Go spelling, produced by the canonical
//     code generator from the demoted (scalar) canonical — e.g. "uint64",
//     "string", "[]byte", "[16]byte", "time.Time". The Set case is special:
//     a roaring bitmap, not the generator's []uint32.
//
// This is the inverse of the front-end classifiers, so the derived
// goType / isSlice / isRoaring equal what the old Go-type front-ends
// produced for every supported type (keeping emitted codecs byte-identical).
func DeriveGoShape(canonical canonicaltypes.PrimitiveAstNodeI) (goType string, isSlice bool, isRoaring bool, err error) {
	if canonical == nil {
		err = eb.Build().Errorf("field has no canonical type — the front-end must classify the field's value type into a canonical (or set CarrierType)")
		return
	}
	mod, _ := canonicaltypes.GetScalarModifier(canonical)
	isSlice = mod == canonicaltypes.ScalarModifierHomogenousArray
	isRoaring = mod == canonicaltypes.ScalarModifierSet
	if isRoaring {
		// A Set canonical's element generator would say []uint32; the codec
		// carries it as a roaring bitmap instead.
		goType = "*roaring.Bitmap"
		return
	}
	goType, _, _, err = codegen.GenerateGoCode(canonicaltypes.DemoteToScalarPrim(canonical), encodingaspects.EmptyAspectSet)
	return
}

// IsIdentifierLike reports whether s is non-empty and composed only of ASCII
// letters, digits, and underscores — the characters permitted in the Go
// identifiers the codec builds from a ref-channel membership name
// (kind<UpperFirst(memb)> in the marshallgen core, vdd.Memb<UpperFirst(memb)>
// in the facts target). The name is always used as an identifier *suffix*
// after a "kind"/"Memb" prefix, so a leading digit is fine; only out-of-set
// characters (hyphen, dot, space, colon, …) break the generated code.
// ASCII-only matches UpperFirst's scope (it capitalises ASCII only).
func IsIdentifierLike(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// UpperFirst upper-cases the first byte of s when it is an ASCII
// lower-case letter; every other input is returned unchanged. Used to
// derive PascalCase section / sub-column method names from the lw: tag
// strings (e.g. "u32Array" → "U32Array").
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
