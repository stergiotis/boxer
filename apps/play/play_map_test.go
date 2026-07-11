package play

import (
	"errors"
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

// requestRefresh must clear the request-dedup key AND forget the lane memo:
// without the forget, an unchanged viewport re-demands the identical SQL and
// memo-hits — the Refresh button was a no-op (review finding).
func TestMapDriverRequestRefreshForcesRefetch(t *testing.T) {
	exec := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch {
		return rgbaRec([]uint8{1}, []uint8{2}, []uint8{3}, []uint8{4})
	}}
	close(exec.gate) // never block
	d := NewMapDriver(nil, nil)
	d.lane.close()
	d.lane = newNodeLane(exec, memory.NewGoAllocator(), 0)
	defer d.lane.close()
	d.demandedSQL = "SELECT raster"
	d.lastRequestedKey = mapFetchKey{viewHash: 42}

	d.lane.demand(compiledNode{SQL: d.demandedSQL})
	waitLaneReady(t, d.lane, d.demandedSQL)
	require.Equal(t, 1, exec.callCount())

	v := d.lane.demand(compiledNode{SQL: d.demandedSQL}) // unchanged view: memo hit
	if v.rec != nil {
		v.rec.Release()
	}
	require.Equal(t, 1, exec.callCount())

	d.requestRefresh()
	require.True(t, d.forceRefresh)
	require.Equal(t, mapFetchKey{}, d.lastRequestedKey, "dedup key cleared so updateDemand re-fires")

	v = d.lane.demand(compiledNode{SQL: d.demandedSQL}) // the per-frame demand after Refresh
	if v.rec != nil {
		v.rec.Release()
	}
	waitLaneReady(t, d.lane, d.demandedSQL)
	require.Equal(t, 2, exec.callCount(), "Refresh must re-execute the unchanged SQL")
}

// The lane error is mirrored every demand and pack errors are owned by
// repack, so neither can latch a stale message (review finding).
func TestMapStatusLineDoesNotLatchErrors(t *testing.T) {
	d := NewMapDriver(nil, nil)
	defer d.lane.close()

	d.laneErr = errors.New("boom")
	require.Contains(t, d.statusLine(), "query error")

	d.laneErr = nil // the next demand mirrored a healthy lane
	d.packErr = errors.New("bad shape")
	require.Contains(t, d.statusLine(), "raster error")

	d.packErr = nil
	d.packW, d.packH = 2, 2
	require.Equal(t, "2×2 raster · Altitude & Velocity", d.statusLine())
}

// repack pins the packed state to the served fingerprint (the observers'
// early-cutoff key) and keeps the served SQL for sqlMeta bounds pinning.
func TestMapDriverRepackRecordsFingerprint(t *testing.T) {
	d := NewMapDriver(nil, nil)
	defer d.lane.close()
	rec := rgbaRec([]uint8{1}, []uint8{2}, []uint8{3}, []uint8{4})
	defer rec.Release()
	d.sqlMeta["q"] = rasterMeta{bounds: [4]float64{1, 2, 3, 4}, w: 1, h: 1}

	d.repack(rec, "q", 0xfeed)
	require.Equal(t, uint64(0xfeed), d.lastPackedFP)
	require.Equal(t, "q", d.lastPackedSQL)
	require.Equal(t, uint64(1), d.version)
	require.Equal(t, uint32(1), d.packW)
}

// buildRasterSQL splices each render's colour block into the shared geometry
// header; the result must be well-formed for every builtin mode (ADR-0096 §SD6).
func TestBuildRasterSQLRendersWellFormed(t *testing.T) {
	b := mercBox{minX: 100, maxX: 200, minY: 100, maxY: 200}
	for _, r := range builtinRenders {
		colorSQL := r.colorSQL
		if r.custom {
			colorSQL = "transparency * 255 AS red, transparency AS green, 0 AS blue"
		}
		sql := buildRasterSQL(b, 256, 256, "planes_mercator", 100, colorSQL, r.where)
		for _, want := range []string{"255 AS alpha", "AS red", "AS green", "AS blue", "GROUP BY pos", "WITH FILL FROM 0 TO"} {
			require.Contains(t, sql, want, "render %q missing %q", r.name, want)
		}
	}
}

// An extra WHERE is ANDed with in_view; an empty one leaves the filter bare.
func TestBuildRasterSQLExtraWhere(t *testing.T) {
	b := mercBox{minX: 1, maxX: 2, minY: 1, maxY: 2}
	rgb := "0 AS red, 0 AS green, 0 AS blue"
	require.Contains(t, buildRasterSQL(b, 4, 4, "t", 100, rgb, "t = 'A320'"), "WHERE in_view AND (t = 'A320')")
	require.Contains(t, buildRasterSQL(b, 4, 4, "t", 100, rgb, ""), "WHERE in_view\n")
}
