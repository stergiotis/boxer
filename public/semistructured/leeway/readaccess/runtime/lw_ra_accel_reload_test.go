//go:build !leeway_generic

package runtime

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func cardsRecord(t *testing.T, mem memory.Allocator, cards ...uint64) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "card", Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	lb := b.Field(0).(*array.ListBuilder)
	lb.Append(true)
	lb.ValueBuilder().(*array.Uint64Builder).AppendValues(cards, nil)
	return b.NewRecord()
}

// Regression: an accel reused across records kept the previous record's
// per-entity cache (SetCurrentEntityIdx returned early on the same entity
// index), and replacing the releaser leaked the previous record's list data.
func TestAccelReloadedFromANewRecordForgetsTheOldOne(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	accel := NewRandomAccessTwoLevelLookupAccel[int, int, int, int64](4)

	rec1 := cardsRecord(t, mem, 1, 1)
	require.NoError(t, LoadAccelFieldFromRecord(0, rec1, accel))
	rec1.Release()
	accel.SetCurrentEntityIdx(0)
	require.Equal(t, Range[int]{BeginIncl: 1, EndExcl: 2}, accel.LookupForwardRange(1))

	rec2 := cardsRecord(t, mem, 3, 2)
	require.NoError(t, LoadAccelFieldFromRecord(0, rec2, accel))
	rec2.Release()
	accel.SetCurrentEntityIdx(0)
	require.Equal(t, Range[int]{BeginIncl: 3, EndExcl: 5}, accel.LookupForwardRange(1))

	accel.Release()
	// A second Release must not release the data again.
	accel.Release()
}
