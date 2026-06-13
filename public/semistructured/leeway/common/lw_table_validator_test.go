package common

import (
	"testing"

	canonicaltypes "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

func validTaggedSection() TaggedValuesSection {
	ctp := canonicaltypes.NewParser()
	return TaggedValuesSection{
		Name:               "mysection",
		UseAspects:         useaspects.EmptyAspectSet,
		ValueColumnNames:   []naming.StylableName{"col"},
		ValueColumnTypes:   []canonicaltypes.PrimitiveAstNodeI{ctp.MustParsePrimitiveTypeAst("s")},
		ValueEncodingHints: []encodingaspects.AspectSet{encodingaspects.EmptyAspectSet},
		ValueSemantics:     []valueaspects.AspectSet{valueaspects.EmptyAspectSet},
		MembershipSpec:     MembershipSpecNone,
	}
}

// Regression for review A-2/C-7: ValidateSection built its result from a stale
// len-0 snapshot of inst.errors captured before validateSection populated it,
// so it always returned nil — the first adopter would get a validator that
// passes everything.
func TestValidateSectionReportsErrors(t *testing.T) {
	v := NewTableValidator()

	// A well-formed section must pass.
	require.NoError(t, v.ValidateSection(validTaggedSection()))

	// An empty section name must now be reported (previously: silent nil).
	bad := validTaggedSection()
	bad.Name = ""
	require.Error(t, v.ValidateSection(bad), "ValidateSection must surface an invalid section name")

	// The validator must remain usable across calls (errors reset each call).
	require.NoError(t, v.ValidateSection(validTaggedSection()))
}

// Regression for review A-8: a short companion co-slice (here ValueEncodingHints)
// slipped past validateNamesTypes (which only compares names vs types) and then
// panicked inside Normalize's co-sort swaps. The validator now reports the skew.
func TestValidateSectionCoSliceLengthSkew(t *testing.T) {
	v := NewTableValidator()

	badHints := validTaggedSection()
	badHints.ValueEncodingHints = nil
	require.Error(t, v.ValidateSection(badHints), "short ValueEncodingHints must be rejected")

	badSemantics := validTaggedSection()
	badSemantics.ValueSemantics = nil
	require.Error(t, v.ValidateSection(badSemantics), "short ValueSemantics must be rejected")
}
