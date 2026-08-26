//go:build !leeway_generic

package runtime

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oneColumnRecord is what a person's own query hands read access — here the
// shape of `SELECT count() FROM facts11`, one column wide where the generated
// read access expects the whole table.
func oneColumnRecord(t *testing.T) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "count()", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.Uint64Builder).Append(42)
	rec := b.NewRecord()
	t.Cleanup(rec.Release)
	return rec
}

// Read access binds by position, and arrow's Record.Column panics on an index
// past the record's width. Every loader has to report that as an error instead:
// callers such as the facts viewer's detail pane already fall back to a generic
// renderer when read access rejects a record, and a query typed by a person is
// not a reason to take the process down.
func TestLoadFromRecordRejectsATooNarrowRecord(t *testing.T) {
	rec := oneColumnRecord(t)

	t.Run("scalar value field", func(t *testing.T) {
		var dest *array.Uint64
		err := LoadScalarValueFieldFromRecord(1, arrow.UINT64, rec, &dest, array.NewUint64Data)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrColumnIndexOutOfRange, "callers match on the sentinel")
		assert.Nil(t, dest, "a rejected load leaves the destination untouched")

		msg := err.Error()
		assert.Contains(t, msg, "column 1")
		assert.Contains(t, msg, "only 1 column")
		// The record's own columns name what actually arrived — seeing
		// "count()" is what makes the mismatch obvious.
		assert.Contains(t, msg, "count()")
	})

	t.Run("non-scalar value field", func(t *testing.T) {
		var list *array.List
		var elems *array.Uint64
		err := LoadNonScalarValueFieldFromRecord(3, arrow.UINT64, rec, &list, &elems, array.NewUint64Data)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrColumnIndexOutOfRange)
		assert.Nil(t, list)
	})

	t.Run("accel field", func(t *testing.T) {
		accel := NewRandomAccessTwoLevelLookupAccel[int, int, int, int64](1)
		err := LoadAccelFieldFromRecord(7, rec, accel)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrColumnIndexOutOfRange)
	})
}

// The last column is in range: the guard must not be off by one.
func TestLoadFromRecordAcceptsTheLastColumn(t *testing.T) {
	rec := oneColumnRecord(t)

	var dest *array.Uint64
	err := LoadScalarValueFieldFromRecord(0, arrow.UINT64, rec, &dest, array.NewUint64Data)
	require.NoError(t, err)
	require.NotNil(t, dest)
	defer dest.Release()
	assert.EqualValues(t, 42, dest.Value(0))
}
