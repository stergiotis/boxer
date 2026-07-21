package play

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
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

// requestRefresh must forget the lane memo: without the forget, an unchanged
// viewport re-demands the identical (SQL, params) and memo-hits — the Refresh
// button was a no-op (review finding). Since 5c the request-dedup key is gone
// (the store dedups the vp_* emits); the force flag re-emits the viewport.
func TestMapDriverRequestRefreshForcesRefetch(t *testing.T) {
	exec := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch {
		return rgbaRec([]uint8{1}, []uint8{2}, []uint8{3}, []uint8{4})
	}}
	close(exec.gate) // never block
	d := NewMapDriver(nil, nil)
	d.lane.close()
	d.lane = newNodeLane(exec, memory.NewGoAllocator(), 0)
	defer d.lane.close()
	node := compiledNode{SQL: "SELECT raster", Params: map[string]string{"param_vp_w": "4"}}

	d.lane.demand(node)
	require.Eventually(t, func() bool {
		v := d.lane.demand(node)
		if v.rec != nil {
			v.rec.Release()
		}
		return !v.loading
	}, 2*time.Second, time.Millisecond)
	require.Equal(t, 1, exec.callCount())

	v := d.lane.demand(node) // unchanged view: memo hit
	if v.rec != nil {
		v.rec.Release()
	}
	require.Equal(t, 1, exec.callCount())

	d.requestRefresh()
	require.True(t, d.forceRefresh)

	d.lane.demand(node) // the per-frame demand after Refresh
	require.Eventually(t, func() bool {
		v := d.lane.demand(node)
		if v.rec != nil {
			v.rec.Release()
		}
		return !v.loading && exec.callCount() == 2
	}, 2*time.Second, time.Millisecond, "Refresh must re-execute the unchanged pair")
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
// early-cutoff key) and recovers dims + lat/lon bounds from the SERVED vp_*
// params (inverse mercator, slice 5c) — the overlay is self-describing.
func TestMapDriverRepackFromServedParams(t *testing.T) {
	d := NewMapDriver(nil, nil)
	defer d.lane.close()
	rec := rgbaRec([]uint8{1}, []uint8{2}, []uint8{3}, []uint8{4})
	defer rec.Release()

	// A real viewport: Greater London, so the inverse-mercator pin can be
	// compared against the forward inputs.
	minLat, maxLat, minLon, maxLon := 51.3, 51.7, -0.6, 0.3
	b, ok := bboxFromLatLon(minLat, maxLat, minLon, maxLon)
	require.True(t, ok)
	served := map[string]string{
		"param_vp_min_x": strconv.FormatUint(uint64(b.minX), 10),
		"param_vp_max_x": strconv.FormatUint(uint64(b.maxX), 10),
		"param_vp_min_y": strconv.FormatUint(uint64(b.minY), 10),
		"param_vp_max_y": strconv.FormatUint(uint64(b.maxY), 10),
		"param_vp_w":     "1",
		"param_vp_h":     "1",
	}

	d.repack(rec, served, 0xfeed)
	require.NoError(t, d.packErr)
	require.Equal(t, uint64(0xfeed), d.lastPackedFP)
	require.Equal(t, uint64(1), d.version)
	require.Equal(t, uint32(1), d.packW)
	require.InDelta(t, minLat, d.packBounds[0], 1e-6, "min lat from inverse mercator")
	require.InDelta(t, minLon, d.packBounds[1], 1e-6)
	require.InDelta(t, maxLat, d.packBounds[2], 1e-6)
	require.InDelta(t, maxLon, d.packBounds[3], 1e-6)

	// A served result missing a vp_* param is a pack error, never a mis-pin.
	d.repack(rec, map[string]string{"param_vp_w": "1"}, 0xbeef)
	require.Error(t, d.packErr)
	require.Equal(t, uint64(0xfeed), d.lastPackedFP, "the prior pack stays")
}

// The raster template splices each render's colour block into the shared
// geometry header, references all six reserved {vp_*:UInt32} slots (ADR-0096
// §SD6), and parses in Grammar1 so its Reads can be derived.
func TestRasterTemplateRendersWellFormed(t *testing.T) {
	for _, r := range builtinRenders {
		colorSQL := r.colorSQL
		if r.custom {
			colorSQL = "transparency * 255 AS red, transparency AS green, 0 AS blue"
		}
		sql := rasterTemplateSQL("planes_mercator", 100, colorSQL, r.where)
		for _, want := range []string{"255 AS alpha", "AS red", "AS green", "AS blue", "GROUP BY pos", "WITH FILL FROM 0 TO"} {
			require.Contains(t, sql, want, "render %q missing %q", r.name, want)
		}
		slots, _, err := extractSlotsAndParams(sql)
		require.NoError(t, err, "the template must parse for Reads derivation: %q", r.name)
		names := make(map[string]bool, len(slots))
		for _, s := range slots {
			names[s.Name] = true
		}
		for _, vp := range mapViewportSignals {
			require.True(t, names[string(vp)], "render %q template must read %s", r.name, vp)
		}
	}
}

// The raster template must survive the host's pre-execute canonicalization
// (ADR-0108 CanonicalizeFull, wired by RegisterPasses) unbroken. Regression:
// integer division was spelled with the `DIV` operator, which grammar1 does
// not model — it mis-parsed `expr DIV span_x` as a chained alias and the
// identifier pass quoted DIV into `"DIV"`, a ClickHouse syntax error that
// silently killed the Map pane. Writing it as intDiv() (canonical function
// form) rides through. Guards every render's spliced template.
func TestRasterTemplateSurvivesCanonicalization(t *testing.T) {
	for _, r := range builtinRenders {
		colorSQL := r.colorSQL
		if r.custom {
			colorSQL = "transparency * 255 AS red, transparency AS green, 0 AS blue"
		}
		sql := rasterTemplateSQL("planes_mercator", 100, colorSQL, r.where)
		out, err := passes.CanonicalizeFull(100).Run(sql)
		require.NoError(t, err, "render %q", r.name)
		require.NotContains(t, out, `"DIV"`,
			"render %q: the DIV operator must not be quoted as an identifier (grammar1 gap)", r.name)
		require.Contains(t, out, "intDiv",
			"render %q: integer division must stay the canonical intDiv() function", r.name)
	}
}

// An extra WHERE is ANDed with in_view; an empty one leaves the filter bare.
func TestRasterTemplateExtraWhere(t *testing.T) {
	rgb := "0 AS red, 0 AS green, 0 AS blue"
	require.Contains(t, rasterTemplateSQL("t", 100, rgb, "t = 'A320'"), "WHERE in_view AND (t = 'A320')")
	require.Contains(t, rasterTemplateSQL("t", 100, rgb, ""), "WHERE in_view\n")
}

// updateViewport publishes the six reserved vp_* signals with the mercator
// values of the forward projection, and builds the template + its Reads.
func TestUpdateViewportEmitsSignalsAndTemplate(t *testing.T) {
	g := newQueryGraph(nil, nil)
	d := NewMapDriver(nil, nil)
	defer d.lane.close()

	d.updateViewport(51.3, 51.7, -0.6, 0.3, 800, 600, graphEmitter{graph: g})

	b, ok := bboxFromLatLon(51.3, 51.7, -0.6, 0.3)
	require.True(t, ok)
	sig := g.signals()
	for name, want := range map[SignalID]uint32{
		"vp_min_x": b.minX, "vp_max_x": b.maxX,
		"vp_min_y": b.minY, "vp_max_y": b.maxY,
		"vp_w": 800, "vp_h": 600,
	} {
		p, found := sig.Get(name)
		require.True(t, found, "signal %s must be emitted", name)
		require.Equal(t, strconv.FormatUint(uint64(want), 10), p.Raw, "signal %s", name)
	}

	require.NotEmpty(t, d.template)
	params := resolveSignalNames(d.templateReads, nil, sig)
	require.True(t, hasViewportParams(params), "the compile resolves the full vp_* set")

	// The template is viewport-free: a pan changes only the params.
	d.updateViewport(48.0, 48.4, 2.0, 2.9, 800, 600, graphEmitter{graph: g})
	require.Equal(t, params["param_vp_w"], "800")
	tmplAfterPan := d.template
	d.updateViewport(51.3, 51.7, -0.6, 0.3, 800, 600, graphEmitter{graph: g})
	require.Equal(t, tmplAfterPan, d.template, "pan/zoom never changes the SQL text")
}

// The inverse mercator round-trips the forward projection within float64
// noise (the SD4 contract run backwards).
func TestMercatorInverseRoundTrip(t *testing.T) {
	for _, tc := range []struct{ lat, lon float64 }{
		{0, 0}, {51.5, -0.1}, {-33.9, 151.2}, {84.9, 179.9}, {-84.9, -179.9},
	} {
		x := lonToMercX(tc.lon)
		y := latToMercY(tc.lat)
		require.InDelta(t, tc.lon, mercXToLon(float64(x)), 1e-6, "lon %v", tc.lon)
		require.InDelta(t, tc.lat, mercYToLat(float64(y)), 1e-6, "lat %v", tc.lat)
	}
}
