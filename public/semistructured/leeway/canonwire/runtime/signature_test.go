package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

// cts is shorthand for a section's declared value-column types.
func cts(in ...canonicaltypes.PrimitiveAstNodeI) []canonicaltypes.PrimitiveAstNodeI {
	return in
}

// A group is the columns' CT strings sorted bytewise and joined by "-", and
// order says which declared column landed where.
func TestGroupOf(t *testing.T) {
	group, order := GroupOf(cts(ctabb.S, ctabb.U32h, ctabb.Sh))
	require.Equal(t, "s-sh-u32h", group)
	require.Equal(t, []int{0, 2, 1}, order)

	// One column is its own group.
	group, order = GroupOf(cts(ctabb.F64))
	require.Equal(t, "f64", group)
	require.Equal(t, []int{0}, order)

	// A value-less section: the empty group, never "<invalid:empty>".
	group, order = GroupOf(nil)
	require.Equal(t, "", group)
	require.Empty(t, order)
}

// Columns of equal CT are told apart only by declaration order, so the sort
// must be stable — the lat/lng case the QOC calls out (criterion C3).
func TestGroupOfKeepsEqualColumnsInDeclarationOrder(t *testing.T) {
	group, order := GroupOf(cts(ctabb.F32, ctabb.F32, ctabb.U64, ctabb.F32))
	require.Equal(t, "f32-f32-f32-u64", group)
	require.Equal(t, []int{0, 1, 3, 2}, order)
}

// The group string must be the one canonicaltypes itself would render for the
// sorted columns, so the Parser can read the key back.
func TestGroupMatchesAstProducer(t *testing.T) {
	in := cts(ctabb.S, ctabb.U32h, ctabb.Sh, ctabb.F32)
	group, order := GroupOf(in)
	sorted := make([]canonicaltypes.PrimitiveAstNodeI, 0, len(in))
	for _, i := range order {
		sorted = append(sorted, in[i])
	}
	require.Equal(t, canonicaltypes.NewGroupAstNode(sorted).String(), group)

	p := canonicaltypes.NewParser()
	back, err := p.ParsePrimitiveTypeOrGroupAst(group)
	require.NoError(t, err)
	require.Equal(t, group, back.String())
}

// A signature is the member groups sorted bytewise and joined by "_"; a lone
// group is its own signature.
func TestSignatureOf(t *testing.T) {
	sig, order := SignatureOf([]string{"u64", "f32-f32"})
	require.Equal(t, "f32-f32_u64", sig)
	require.Equal(t, []int{1, 0}, order)

	sig, order = SignatureOf([]string{"s"})
	require.Equal(t, "s", sig)
	require.Equal(t, []int{0}, order)

	// A standalone value-less section: the empty signature is a valid key.
	sig, order = SignatureOf([]string{""})
	require.Equal(t, "", sig)
	require.Equal(t, []int{0}, order)

	sig, order = SignatureOf(nil)
	require.Equal(t, "", sig)
	require.Empty(t, order)

	// Equal groups keep declaration order, so two `s` sections in one
	// co-section group stay addressable by position.
	sig, order = SignatureOf([]string{"s", "b", "s"})
	require.Equal(t, "b_s_s", sig)
	require.Equal(t, []int{1, 0, 2}, order)
}
