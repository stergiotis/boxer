package chstore

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The round trip writes one row per kind of the runtime vocabulary, in the
// facts columns, and leaves no Arrow memory behind once released.
func TestRoundTripRows_OneRowPerKind(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	records, kinds, err := roundTripRows(context.Background(), alloc)
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	require.NotEmpty(t, kinds)
	assert.Contains(t, kinds, "runtime-kind-grant")
	assert.Contains(t, kinds, "runtime-kind-llm-call")
	rows := 0
	for _, r := range records {
		rows += int(r.NumRows())
		assert.True(t, r.Schema().Equal(records[0].Schema()), "every batch is in the facts columns")
	}
	assert.Equal(t, len(kinds), rows)
	c, err := SchemaContract(Config{Database: "boxer", Table: "facts"})
	require.NoError(t, err)
	assert.Equal(t, len(c.Columns), int(records[0].NumCols()), "the batches carry every facts column")
}
