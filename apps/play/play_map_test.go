package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func rgbaRec(r, g, b, a []uint8) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "r", Type: arrow.PrimitiveTypes.Uint8},
		{Name: "g", Type: arrow.PrimitiveTypes.Uint8},
		{Name: "b", Type: arrow.PrimitiveTypes.Uint8},
		{Name: "a", Type: arrow.PrimitiveTypes.Uint8},
	}, nil)
	cols := make([]arrow.Array, 4)
	for i, vals := range [][]uint8{r, g, b, a} {
		bld := array.NewUint8Builder(mem)
		bld.AppendValues(vals, nil)
		cols[i] = bld.NewArray()
		bld.Release()
	}
	rec := array.NewRecord(schema, cols, int64(len(r)))
	for _, col := range cols {
		col.Release()
	}
	return rec
}

// packRaster packs the 4×UInt8 columns into 0xRRGGBBAA and pads to w*h.
func TestPackRasterPacksAndPads(t *testing.T) {
	rec := rgbaRec([]uint8{0xAA, 0xBB}, []uint8{0x11, 0x22}, []uint8{0x33, 0x44}, []uint8{0xFF, 0x80})
	defer rec.Release()

	pixels, err := packRaster(rec, 2, 2) // 2 rows of data, 4-pixel raster
	require.NoError(t, err)
	require.Len(t, pixels, 4, "padded to w*h")
	require.Equal(t, uint32(0xAA1133FF), pixels[0])
	require.Equal(t, uint32(0xBB224480), pixels[1])
	require.Equal(t, uint32(0), pixels[2], "WITH FILL gap padded transparent")
	require.Equal(t, uint32(0), pixels[3])
}

func TestPackRasterTruncatesOverflow(t *testing.T) {
	rec := rgbaRec([]uint8{1, 2, 3}, []uint8{1, 2, 3}, []uint8{1, 2, 3}, []uint8{1, 2, 3})
	defer rec.Release()
	pixels, err := packRaster(rec, 1, 1) // 3 rows, 1-pixel raster
	require.NoError(t, err)
	require.Len(t, pixels, 1)
}

func TestPackRasterRejectsNonRGBAResult(t *testing.T) {
	rec := int64Rec("n", 1, 2, 3) // one Int64 column — not 4×UInt8
	defer rec.Release()
	_, err := packRaster(rec, 1, 1)
	require.Error(t, err)
}
