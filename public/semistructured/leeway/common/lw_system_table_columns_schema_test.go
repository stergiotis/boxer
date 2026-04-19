//go:build llm_generated_opus46

package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemTableColumnsSchemaValidates(t *testing.T) {
	manip, err := GetSystemTableColumnsManipulator()
	require.NoError(t, err)

	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	validator := NewTableValidator()
	err = validator.ValidateTable(&tbl)
	require.NoError(t, err)
}

func TestSystemTableColumnsSchemaCborRoundTrip(t *testing.T) {
	manip, err := GetSystemTableColumnsManipulator()
	require.NoError(t, err)

	dto1, err := manip.BuildTableDescDto()
	require.NoError(t, err)

	// Rebuild from DTO to verify round-trip
	manip2, err := NewTableManipulator()
	require.NoError(t, err)

	tbl1, err := manip.BuildTableDesc()
	require.NoError(t, err)

	err = manip2.MergeTable(&tbl1)
	require.NoError(t, err)

	dto2, err := manip2.BuildTableDescDto()
	require.NoError(t, err)

	require.Equal(t, len(dto1.TaggedValuesSections), len(dto2.TaggedValuesSections))
	for i, sec1 := range dto1.TaggedValuesSections {
		sec2 := dto2.TaggedValuesSections[i]
		require.Equal(t, sec1.Name, sec2.Name)
		require.Equal(t, len(sec1.ValueColumnNames), len(sec2.ValueColumnNames))
		require.Equal(t, sec1.MembershipSpec, sec2.MembershipSpec)
	}
}
