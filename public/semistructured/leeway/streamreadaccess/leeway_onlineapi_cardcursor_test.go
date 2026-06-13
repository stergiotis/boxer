package streamreadaccess

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// Regression for review F-4: newCardCursor silently fell back to "card=1 per
// attribute" when the cardinality column was present but not a Uint64 list,
// mis-slicing every multi-element attribute into silently wrong output. It must
// now surface an error instead.
func TestNewCardCursorRejectsNonUint64Cardinality(t *testing.T) {
	pool := memory.NewGoAllocator()

	// A List<String> standing in for the expected List<Uint64> cardinality col.
	lb := array.NewListBuilder(pool, arrow.BinaryTypes.String)
	lb.Append(true)
	lb.ValueBuilder().(*array.StringBuilder).Append("not-a-count")
	arr := lb.NewArray()
	defer arr.Release()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "card", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: false},
	}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, 1)
	defer rec.Release()

	d := &Driver{errs: make([]error, 0, 8)}
	cur := d.newCardCursor(rec, 0, 0)

	require.Nil(t, cur.inner, "a malformed cardinality column must not yield a usable cursor")
	require.True(t, d.hasError(), "a non-Uint64 cardinality column must surface a driver error, not a silent card=1 fallback")
}

// A genuinely absent cardinality column (sentinel index < 0) is the legitimate
// scalar-section case and must remain a clean card=1 cursor, not an error.
func TestNewCardCursorAbsentColumnIsNotAnError(t *testing.T) {
	d := &Driver{errs: make([]error, 0, 8)}
	cur := d.newCardCursor(nil, -1, 0)
	require.Nil(t, cur.inner)
	require.False(t, d.hasError(), "an absent cardinality column is a legitimate card=1 case")

	relOff, card := cur.step(0)
	require.Equal(t, 0, relOff)
	require.Equal(t, 1, card)
}
