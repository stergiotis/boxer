package canonicaltypes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression for review B-4: IsValid() returned unconditionally true for
// machine-numeric / temporal nodes (and StringAstNode omitted the base-type
// check), so zero-value / hand-built nodes passed validity yet String() emitted
// unparseable text ("u0", ""). Validity must imply "String() reparses".
func TestIsValidRejectsUnparseableNodes(t *testing.T) {
	require.False(t, MachineNumericTypeAstNode{}.IsValid(), "zero-value machine numeric must be invalid")
	require.False(t, TemporalTypeAstNode{}.IsValid(), "zero-value temporal must be invalid")
	require.False(t, StringAstNode{}.IsValid(), "zero-value string must be invalid")

	// u0 — zero width is unparseable (NUMBER forbids 0).
	require.False(t, MachineNumericTypeAstNode{BaseType: BaseTypeMachineNumericUnsigned, Width: 0}.IsValid())
	// z0 — zero-width temporal likewise.
	require.False(t, TemporalTypeAstNode{BaseType: BaseTypeTemporalUtcDatetime, Width: 0}.IsValid())

	// Well-formed nodes must remain valid and round-trip through the parser.
	p := NewParser()
	for _, valid := range []PrimitiveAstNodeI{
		MachineNumericTypeAstNode{BaseType: BaseTypeMachineNumericUnsigned, Width: 32},
		TemporalTypeAstNode{BaseType: BaseTypeTemporalUtcDatetime, Width: 64},
		StringAstNode{BaseType: BaseTypeStringUtf8},
	} {
		require.Truef(t, valid.IsValid(), "%s must be valid", valid.String())
		reparsed, err := p.ParsePrimitiveTypeAst(valid.String())
		require.NoErrorf(t, err, "IsValid node %q must reparse", valid.String())
		require.Equal(t, valid.String(), reparsed.String())
	}
}

// Regression for review B-3: the group-conversion loop neither checked nor
// accumulated the member parse error, so a member overflow was overwritten and
// a nil node appended — the caller got err==nil and a GroupAstNode that
// panicked on the next String()/IsValid(). The error must propagate.
func TestParseGroupPropagatesMemberError(t *testing.T) {
	p := NewParser()
	// Second member width overflows uint32 (ParseUint(..,32)).
	_, err := p.ParsePrimitiveTypeOrGroupAst("s-u99999999999")
	require.Error(t, err, "a group member parse error must propagate")
}
