package runtime

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The message has to carry the diagnosis, not only the structured fields: read
// access is surfaced through GUIs and CLIs that render Error(), and eb offers no
// way to read the fields back.
func TestUnexpectedDataTypeMessageNamesTheColumn(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "country", Type: arrow.BinaryTypes.String},
		{Name: "id:kid:u64:g:1hW82H8FG:0:", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)

	err := unexpectedDataTypeE(schema, 0, arrow.BinaryTypes.String, arrow.UINT64)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnexpectedArrowDataType, "callers match on the sentinel")

	msg := err.Error()
	// The name is what makes a shifted projection obvious — seeing "country"
	// in slot 0 says the query prepended a column to SELECT *.
	assert.Contains(t, msg, `"country"`)
	assert.Contains(t, msg, "column 0")
	// Both sides are named by their arrow.Type so they read as comparable, with
	// the full type alongside for list element detail.
	assert.Contains(t, msg, "got STRING (utf8)", "the type actually found")
	assert.Contains(t, msg, "want UINT64", "the type expected at that position")
}

// A record whose schema does not describe the index must still produce a usable
// message rather than panicking on the lookup.
func TestUnexpectedDataTypeToleratesAnUndescribedColumn(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "only", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)

	for _, tc := range []struct {
		name   string
		schema *arrow.Schema
		idx    uint32
	}{
		{"index past the schema", schema, 7},
		{"no schema at all", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := unexpectedDataTypeE(tc.schema, tc.idx, arrow.BinaryTypes.String, arrow.UINT64)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUnexpectedArrowDataType)
			assert.Contains(t, err.Error(), "<unknown>")
			assert.Contains(t, err.Error(), "want UINT64")
		})
	}
}
