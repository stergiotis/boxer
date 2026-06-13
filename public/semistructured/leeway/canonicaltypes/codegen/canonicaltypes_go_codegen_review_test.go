package codegen

import (
	"testing"

	canonicaltypes "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stretchr/testify/require"
)

// Regression for review B-2/D-5: the Go codegen emitted nonexistent
// uint128/uint256/int128/int256 with a nil error; a u128 column passed every
// gate and broke only at `go build` of the generated code. It must now return
// an error.
func TestGoCodegenRejectsUnrepresentableTypes(t *testing.T) {
	for _, ct := range []canonicaltypes.PrimitiveAstNodeI{
		canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 128},
		canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 256},
	} {
		_, _, _, err := GenerateGoCode(ct, encodingaspects.EmptyAspectSet)
		require.Errorf(t, err, "Go codegen must reject %s (no Go builtin)", ct.String())
	}

	// Fixed-width UTF-8 (sxN) deliberately maps to plain `string` (Go has no
	// fixed-width string); this is a documented divergence, not an error (B-5).
	code, _, _, err := GenerateGoCode(
		canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 8},
		encodingaspects.EmptyAspectSet)
	require.NoError(t, err)
	require.Equal(t, "string", code)

	// Sanity: representable widths still succeed.
	_, _, _, err = GenerateGoCode(
		canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64},
		encodingaspects.EmptyAspectSet)
	require.NoError(t, err)
}
