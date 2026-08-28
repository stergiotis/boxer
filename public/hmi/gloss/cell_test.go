package gloss

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArrowCellRawStringAndBinary(t *testing.T) {
	mem := memory.NewGoAllocator()
	payload := "# Title\n\nbody"

	t.Run("String", func(t *testing.T) {
		b := array.NewStringBuilder(mem)
		defer b.Release()
		b.Append(payload)
		arr := b.NewArray()
		defer arr.Release()
		c := ArrowCell{Arr: arr, Row: 0}
		raw, ok := c.Raw()
		require.True(t, ok)
		assert.Equal(t, payload, raw)
		assert.Equal(t, ValueKindText, c.Kind())
		assert.Equal(t, payload, c.Text())
	})
	t.Run("Binary keeps bytes and is not hex", func(t *testing.T) {
		b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
		defer b.Release()
		blob := "\x89PNG\r\n\x1a\n\xff\xfe"
		b.Append([]byte(blob))
		arr := b.NewArray()
		defer arr.Release()
		c := ArrowCell{Arr: arr, Row: 0}
		raw, ok := c.Raw()
		require.True(t, ok)
		assert.Equal(t, blob, raw, "the decoder needs the bytes, not a sanitised rendering")
		assert.NotEqual(t, c.Text(), raw, "Text hex-encodes binary")
		assert.Equal(t, ValueKindBytes, c.Kind())
	})
	t.Run("Dictionary of strings", func(t *testing.T) {
		dt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
		b := array.NewDictionaryBuilder(mem, dt).(*array.BinaryDictionaryBuilder)
		defer b.Release()
		require.NoError(t, b.AppendString("low"))
		arr := b.NewArray()
		defer arr.Release()
		c := ArrowCell{Arr: arr, Row: 0}
		raw, ok := c.Raw()
		require.True(t, ok)
		assert.Equal(t, "low", raw)
		assert.Equal(t, ValueKindText, c.Kind(), "a dictionary reads as its value type")
	})
}

func TestArrowCellNumeric(t *testing.T) {
	mem := memory.NewGoAllocator()
	fb := array.NewFloat64Builder(mem)
	defer fb.Release()
	fb.Append(21.5)
	fb.AppendNull()
	farr := fb.NewArray()
	defer farr.Release()

	c := ArrowCell{Arr: farr, Row: 0}
	v, ok := c.Float64()
	require.True(t, ok)
	assert.Equal(t, 21.5, v)
	assert.Equal(t, ValueKindNumeric, c.Kind())
	_, ok = c.Raw()
	assert.False(t, ok, "a float has no raw bytes; callers fall back to Text")
	assert.Equal(t, "21.5", c.Text())

	null := ArrowCell{Arr: farr, Row: 1}
	assert.True(t, null.IsNull())
	_, ok = null.Float64()
	assert.False(t, ok)
	assert.Equal(t, "", null.Text())

	oob := ArrowCell{Arr: farr, Row: 7}
	assert.True(t, oob.IsNull(), "out of range reads as null")

	ub := array.NewUint64Builder(mem)
	defer ub.Release()
	ub.Append(42)
	ub.Append(1 << 63)
	uarr := ub.NewArray()
	defer uarr.Release()
	i, ok := ArrowCell{Arr: uarr, Row: 0}.Int64()
	require.True(t, ok)
	assert.Equal(t, int64(42), i)
	_, ok = ArrowCell{Arr: uarr, Row: 1}.Int64()
	assert.False(t, ok, "a uint64 past int64 does not wrap")
	f, ok := ArrowCell{Arr: uarr, Row: 1}.Float64()
	require.True(t, ok)
	assert.Equal(t, float64(1<<63), f)
}

func TestTextCell(t *testing.T) {
	c := TextCell{S: "21.5", K: ValueKindNumeric}
	v, ok := c.Float64()
	require.True(t, ok)
	assert.Equal(t, 21.5, v)
	_, ok = c.Int64()
	assert.False(t, ok)
	raw, ok := c.Raw()
	require.True(t, ok)
	assert.Equal(t, "21.5", raw)
	assert.False(t, c.IsNull())

	_, ok = TextCell{S: "abc"}.Float64()
	assert.False(t, ok)
}

func TestKindOfArrow(t *testing.T) {
	assert.Equal(t, ValueKindNumeric, KindOfArrow(arrow.PrimitiveTypes.Float32))
	assert.Equal(t, ValueKindNumeric, KindOfArrow(arrow.PrimitiveTypes.Uint8))
	assert.Equal(t, ValueKindText, KindOfArrow(arrow.BinaryTypes.LargeString))
	assert.Equal(t, ValueKindBytes, KindOfArrow(&arrow.FixedSizeBinaryType{ByteWidth: 16}))
	assert.Equal(t, ValueKindTemporal, KindOfArrow(&arrow.TimestampType{Unit: arrow.Millisecond}))
	assert.Equal(t, ValueKindTemporal, KindOfArrow(arrow.FixedWidthTypes.Date32))
	assert.Equal(t, ValueKindBool, KindOfArrow(arrow.FixedWidthTypes.Boolean))
	assert.Equal(t, ValueKindOther, KindOfArrow(arrow.ListOf(arrow.PrimitiveTypes.Float64)), "a list is applied to its items by the host, not glossed whole")
	assert.Equal(t, ValueKindOther, KindOfArrow(nil))
	assert.Equal(t, ValueKindText, KindOfArrow(&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}))
}

// byteCell is a cell of raw bytes, the shape an Arrow binary column hands a
// face. TextCell.Raw returns S as-is, so it carries bytes as well as text.
func byteCell(s string) TextCell { return TextCell{S: s, K: ValueKindBytes} }
