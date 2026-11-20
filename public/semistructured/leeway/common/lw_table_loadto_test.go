package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// An invalid type among the later plain items fails LoadTo before the
// earlier items are written: the dto's co-arrays stay in step.
func TestLoadToLeavesTheDtoAloneOnAnInvalidType(t *testing.T) {
	valid := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}
	tbl := TableDesc{
		PlainValuesNames:          []naming.StylableName{"id", "ts"},
		PlainValuesTypes:          []canonicaltypes.PrimitiveAstNodeI{valid, canonicaltypes.StringAstNode{}},
		PlainValuesEncodingHints:  []encodingaspects.AspectSet{encodingaspects.EmptyAspectSet, encodingaspects.EmptyAspectSet},
		PlainValuesItemTypes:      []PlainItemTypeE{PlainItemTypeEntityId, PlainItemTypeEntityTimestamp},
		PlainValuesValueSemantics: []valueaspects.AspectSet{valueaspects.EmptyAspectSet, valueaspects.EmptyAspectSet},
	}
	var dto TableDescDto
	err := tbl.LoadTo(&dto)
	require.ErrorIs(t, err, ErrInvalidType)
	assert.Empty(t, dto.EntityIdNames, "nothing was written before the type check")
	assert.Empty(t, dto.EntityIdTypes)
}
