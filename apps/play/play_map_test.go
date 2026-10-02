package play

import (
	"context"
	"errors"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
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

// Cancel stops the in-flight raster fetch and says so: the panel's per-frame
// demand does not restart the pair it was cancelled on (that is what would make
// Cancel a no-op with a still camera), and the notice survives until a run
// actually starts — silence after a click reads as the button having missed.
func TestMapCancelFetchStopsAndAnnounces(t *testing.T) {
	exec := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch {
		return rgbaRec([]uint8{1}, []uint8{2}, []uint8{3}, []uint8{4})
	}}
	d := NewMapDriver(nil, nil)
	d.lane.close()
	d.lane = newNodeLane(exec, memory.NewGoAllocator(), 0)
	defer d.lane.close()
	node := compiledNode{SQL: "SELECT raster", Params: map[string]string{"param_vp_w": "4"}}

	d.noteLane(d.lane.demand(node))
	require.True(t, d.loading)
	require.Eventually(t, func() bool { return exec.callCount() == 1 },
		2*time.Second, time.Millisecond)

	d.cancelFetch()
	close(exec.gate) // release the cancelled run; its completion is discarded
	require.False(t, d.loading)
	require.Contains(t, d.statusLine(), "cancelled")

	// The frames that follow re-demand the same pair — the still-camera case.
	require.Never(t, func() bool {
		v := d.lane.demand(node)
		if v.rec != nil {
			v.rec.Release()
		}
		return v.loading
	}, 150*time.Millisecond, 5*time.Millisecond, "Cancel must not be a re-run")
	require.Equal(t, 1, exec.callCount())

	// A pan (new params) starts a run, which clears the notice — the same
	// mirroring the lane error gets, so neither can latch.
	panned := compiledNode{SQL: "SELECT raster", Params: map[string]string{"param_vp_w": "8"}}
	d.noteLane(d.lane.demand(panned))
	require.True(t, d.loading)
	require.NotContains(t, d.statusLine(), "cancelled")
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
	require.Equal(t, "2×2 raster · Altitude & Speed", d.statusLine())
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
		for _, want := range []string{"255 AS alpha", "AS red", "AS green", "AS blue", "GROUP BY pos", "SELECT toUInt32(pos), "} {
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

// The default render, run through clickhouse-local on a synthetic
// planes_mercator-shaped table (one point per 1000×1000 mercator cell, one
// cell per pixel): every channel stays in 0..255 for negative, absurd and
// non-finite inputs, empty pixels are black, distinct altitude and speed bands
// get distinct colours, and a dense pixel is brighter than a lone sample. The
// template's SELECT is swapped for the pre-cast floats so an out-of-range
// value cannot hide behind the UInt8 cast.
func TestAltitudeSpeedRenderOnClickHouse(t *testing.T) {
	bin, err := chlocalpool.LookupBinary()
	if err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	const grid, cell = 8, 1000
	pt := func(px, py int, alt, speed string) string {
		return "(" + strconv.Itoa(px*cell+cell/2) + "," + strconv.Itoa(py*cell+cell/2) + "," + alt + "," + speed + ")"
	}
	var rows []string
	// Row 0: altitude bands at 250 kt — ground, approach, the 10,000 ft
	// terminal ceiling, climb, cruise.
	altBands := []string{"0", "3000", "10000", "20000", "37000"}
	for i, a := range altBands {
		rows = append(rows, pt(i, 0, a, "250"))
	}
	// Row 1: speed bands at 10,000 ft — stationary, light aircraft, a jet
	// under the 250 kt terminal limit, a jet at cruise.
	speedBands := []string{"0", "100", "250", "450"}
	for i, s := range speedBands {
		rows = append(rows, pt(i, 1, "10000", s))
	}
	// Row 2: inputs no aircraft reports.
	for i, as := range [][2]string{
		{"-1200", "-50"}, {"-2147483648", "0"}, {"2147483647", "1e9"},
		{"60000", "inf"}, {"45000", "nan"}, {"-500", "-inf"},
	} {
		rows = append(rows, pt(i, 2, as[0], as[1]))
	}
	// Row 3: a dense pixel beside a lone sample with the same altitude and speed.
	for range 500 {
		rows = append(rows, pt(0, 3, "10000", "250"))
	}
	rows = append(rows, pt(1, 3, "10000", "250"))

	insert := "CREATE TABLE planes_mercator (mercator_x UInt32, mercator_y UInt32, altitude Int32, ground_speed Float32) ENGINE = Memory;\n" +
		"INSERT INTO planes_mercator VALUES " + strings.Join(rows, ",") + ";\n"
	span := strconv.Itoa(grid * cell)
	const castSelect = "SELECT toUInt32(pos), round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8"
	render := func(sampling uint32) map[int][3]float64 {
		r := builtinRenders[0]
		tmpl := rasterTemplateSQL("planes_mercator", sampling, r.colorSQL, r.where)
		require.Contains(t, tmpl, castSelect)
		tmpl = strings.Replace(tmpl, castSelect, "SELECT pos, red, green, blue", 1)
		tmpl, err := passes.CanonicalizeFull(100).Run(tmpl)
		require.NoError(t, err)
		out, err := exec.Command(bin, "local", "--output-format", "TSV",
			"--param_vp_min_x=0", "--param_vp_max_x="+span, "--param_vp_min_y=0", "--param_vp_max_y="+span,
			"--param_vp_w="+strconv.Itoa(grid), "--param_vp_h="+strconv.Itoa(grid),
			"--query", insert+tmpl).CombinedOutput()
		require.NoError(t, err, string(out))
		px := make(map[int][3]float64, grid*grid)
		for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			f := strings.Split(line, "\t")
			require.Len(t, f, 4, line)
			pos, err := strconv.Atoi(f[0])
			require.NoError(t, err, line)
			var c [3]float64
			for i := range c {
				c[i], err = strconv.ParseFloat(f[i+1], 64)
				require.NoError(t, err, line)
				require.False(t, math.IsNaN(c[i]), "pixel %d: %s", pos, line)
				require.GreaterOrEqual(t, c[i], 0.0, "pixel %d: %s", pos, line)
				require.LessOrEqual(t, c[i], 255.0, "pixel %d: %s", pos, line)
			}
			px[pos] = c
		}
		require.NotContains(t, px, grid*grid-1, "an empty pixel has no row (sparse result)")
		return px
	}
	// Distinct: some channel differs by at least 12/255 between every pair.
	distinct := func(px map[int][3]float64, row int, names []string) {
		for i := range names {
			for j := i + 1; j < len(names); j++ {
				a, b := px[row*grid+i], px[row*grid+j]
				require.True(t, max(math.Abs(a[0]-b[0]), math.Abs(a[1]-b[1]), math.Abs(a[2]-b[2])) >= 12,
					"row %d: %s vs %s: %v vs %v", row, names[i], names[j], a, b)
			}
		}
	}
	sum := func(c [3]float64) float64 { return c[0] + c[1] + c[2] }

	// Sampling 100 (the panel default): a lone sample here has transparency
	// ≈0.84, so both altitude and speed must separate.
	px := render(100)
	distinct(px, 0, altBands)
	distinct(px, 1, speedBands)
	require.Greater(t, sum(px[3*grid]), sum(px[3*grid+1]), "density brightens a pixel")

	// Sampling 1: a lone sample is faint (transparency ≈0.33), where the sRGB
	// gamut leaves chroma — and so speed — little room; altitude, carried by
	// hue, must still separate.
	px = render(1)
	distinct(px, 0, altBands)
	require.Greater(t, sum(px[3*grid]), sum(px[3*grid+1]), "density brightens a pixel")
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

func TestSanitizeTableFoldsLinesAndBlocksStatementBreakers(t *testing.T) {
	// The Map's source editor is multi-line on demand, so a break is folded
	// rather than refused; the three statement-breakers stay refusals, since
	// they are what could carry the splice out of `FROM <src> WHERE …`.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain identifier", "planes_mercator_sample100", "planes_mercator_sample100"},
		{"qualified", "default.planes_mercator_sample100", "default.planes_mercator_sample100"},
		{"surrounding space is trimmed", "  planes  ", "planes"},
		{"empty", "", ""},
		{"whitespace only", " \n\t ", ""},
		{
			"a table function written over several lines folds to one",
			"merge(default,\n       '^planes_mercator_sample100$')",
			"merge(default,        '^planes_mercator_sample100$')",
		},
		{"CRLF folds to ONE space", "merge(a,\r\n'b')", "merge(a, 'b')"},
		{"leading break is folded then trimmed", "\nplanes\n", "planes"},
		// The refusals. Each is checked AFTER the fold, so hiding one behind a
		// line break does not get it through.
		{"terminator", "planes; DROP TABLE x", ""},
		{"terminator behind a break", "planes\n; DROP TABLE x", ""},
		{"line comment", "planes -- rest of the line", ""},
		{"line comment behind a break", "planes\n-- rest of the line", ""},
		{"block comment", "planes /* x */", ""},
		{"block comment behind a break", "planes\n/* x */", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sanitizeTable(tc.in))
		})
	}
}

func TestSanitizeTableFoldCannotForgeAStatementBreaker(t *testing.T) {
	// The fold inserts a SPACE, so it can only separate characters. Two dashes
	// or a slash-star split by a line break must stay split, not become the
	// comment opener the checks look for -- which is what makes folding before
	// the checks safe rather than a hole in them.
	require.Equal(t, "a- -b", sanitizeTable("a-\n-b"))
	require.Equal(t, "a/ *b", sanitizeTable("a/\n*b"))
}

// The raster is sized in logical points (View.Size), clamped to [16,
// mapMaxDim]; no device-pixel factor applies (ADR-0096 2026-10-01 Update).
func TestClampDimPinsLogicalPointsAndCap(t *testing.T) {
	require.EqualValues(t, 16, clampDim(5))
	require.EqualValues(t, 800, clampDim(800.4))
	require.EqualValues(t, mapMaxDim, clampDim(4000))
}

// foldViewLon folds a view onto the one world copy mercator_x covers: a view
// panned whole turns asks for the same span, a world-wide view asks for the
// whole world, and a view past ±180 keeps the side its midpoint is on.
func TestFoldViewLon(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		west, east             float64
		wantW, wantE, wantFrac float64
	}{
		{"inside", -10, 20, -10, 20, 1},
		{"one turn east", 350, 380, -10, 20, 1},
		{"two turns west", -730, -700, -10, 20, 1},
		{"wider than a world", -200, 200, -180, 180, 0.9},
		{"past +180, midpoint west of it", 150, 190, 150, 180, 0.75},
		{"past +180, midpoint east of it", 170, 200, -180, -160, 20.0 / 30},
	} {
		w, e, frac := foldViewLon(tc.west, tc.east)
		require.InDelta(t, tc.wantW, w, 1e-9, tc.name)
		require.InDelta(t, tc.wantE, e, 1e-9, tc.name)
		require.InDelta(t, tc.wantFrac, frac, 1e-9, tc.name)
	}
}

// The raster is drawn on the world copy nearest the view, and on its
// neighbours when the view is wide enough to show them.
func TestRasterCopies(t *testing.T) {
	require.Equal(t, []float64{0}, rasterCopies(-10, 20, -30, 30))
	require.Equal(t, []float64{360}, rasterCopies(-10, 20, 330, 390), "follows the reader a turn east")
	require.Equal(t, []float64{0, -360, 360}, rasterCopies(-180, 180, -200, 200), "a world view shows the edges of both neighbours")
	require.Empty(t, rasterCopies(-10, 20, 40, 60), "a raster outside the view is not drawn")
}

// latCoverage is below 1 only when the view reaches past the mercator clamp.
func TestLatCoverage(t *testing.T) {
	require.InDelta(t, 1, latCoverage(40, 60), 1e-12)
	c := latCoverage(-89.9, 89.9)
	require.Greater(t, c, 0.0)
	require.Less(t, c, 1.0)
}

// Regression: after a pan of one whole world, both edges used to clamp to the
// world's edge, the viewport was dropped as degenerate, and the map went dark.
// The folded request is the one the unpanned view makes.
func TestUpdateViewportAfterFullWorldPan(t *testing.T) {
	read := func(west, east float64, screenW float32) map[SignalID]string {
		g := newQueryGraph(nil, nil)
		d := NewMapDriver(nil, nil)
		defer d.lane.close()
		d.updateViewport(30, 60, west, east, screenW, 600, graphEmitter{graph: g})
		sig := g.signals()
		out := map[SignalID]string{}
		for _, s := range mapViewportSignals {
			p, found := sig.Get(s)
			require.True(t, found, "signal %s must be emitted for view %v..%v", s, west, east)
			out[s] = p.Raw
		}
		return out
	}
	require.Equal(t, read(-30, 30, 800), read(330, 390, 800))

	world := read(-200, 200, 1000)
	require.Equal(t, "0", world["vp_min_x"])
	require.Equal(t, strconv.FormatUint(mercUnitMax, 10), world["vp_max_x"])
	require.Equal(t, "900", world["vp_w"], "the raster spans the 90% of the view one world covers")
}

// A colour block may define alpha; only a block that does not gets the opaque
// default appended, so the alias is never defined twice.
func TestRasterTemplateAlphaFromColourBlock(t *testing.T) {
	plain := rasterTemplateSQL("planes_mercator", 100, "0 AS red, 0 AS green, 0 AS blue", "")
	require.Equal(t, 1, strings.Count(plain, "255 AS alpha"))

	own := "transparency * 255 AS red, 0 AS green, 0 AS blue, transparency * 255 AS alpha"
	tmpl := rasterTemplateSQL("planes_mercator", 100, own, "")
	require.NotContains(t, tmpl, "255 AS alpha,")
	require.Equal(t, 1, strings.Count(strings.ToLower(tmpl), "as alpha"))
	canon, err := passes.CanonicalizeFull(100).Run(tmpl)
	require.NoError(t, err)

	bin, err := chlocalpool.LookupBinary()
	if err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	out, err := exec.Command(bin, "local", "--output-format", "TSV",
		"--param_vp_min_x=0", "--param_vp_max_x=2000", "--param_vp_min_y=0", "--param_vp_max_y=1000",
		"--param_vp_w=2", "--param_vp_h=1",
		"--query", "CREATE TABLE planes_mercator (mercator_x UInt32, mercator_y UInt32) ENGINE = Memory;\n"+
			"INSERT INTO planes_mercator VALUES (500, 500);\n"+canon).CombinedOutput()
	require.NoError(t, err, string(out))
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, lines, 1, "the empty pixel has no row; packRaster leaves it alpha 0")
	lit := strings.Split(lines[0], "\t")
	require.Len(t, lit, 5)
	require.Equal(t, "0", lit[0], "pos")
	require.NotEqual(t, "255", lit[4], "a lone sample's alpha follows its transparency")
	require.Equal(t, lit[1], lit[4], "alpha is the block's own expression")
}

// The ladder derives its levels from the source's name: coarsest first, each
// with its own sampling factor, the reader's table never marked derived.
func TestMapLadderLevels(t *testing.T) {
	lv := func(table string, sampling uint32, derived bool) mapLevel {
		return mapLevel{table: table, sampling: sampling, derived: derived}
	}
	require.Equal(t, []mapLevel{lv("planes_mercator_sample100", 100, false), lv("planes_mercator_sample10", 10, true), lv("planes_mercator", 1, true)},
		mapLadderLevels("planes_mercator_sample100", 7, true, nil))
	require.Equal(t, []mapLevel{lv("planes_mercator_sample100", 100, true), lv("planes_mercator_sample10", 10, true), lv("planes_mercator", 1, false)},
		mapLadderLevels("planes_mercator", 7, true, nil))
	require.Equal(t, []mapLevel{lv("db.t_sample10", 10, false), lv("db.t", 1, true)},
		mapLadderLevels("db.t_sample10", 7, true, nil))
	require.Equal(t, []mapLevel{lv("t_sample100", 100, false), lv("t", 1, true)},
		mapLadderLevels("t_sample100", 7, true, map[string]bool{"t_sample10": true}), "a missing level is left out")
	require.Equal(t, []mapLevel{lv("planes_mercator_sample100", 7, false)},
		mapLadderLevels("planes_mercator_sample100", 7, false, nil), "refine off reads the one table at the manual sampling")
	src := "remoteSecure('h:9440', default.planes_mercator_sample100, 'website', '')"
	require.Equal(t, []mapLevel{lv(src, 7, false)}, mapLadderLevels(src, 7, true, nil), "a table function is not laddered")
}

func TestMapLevelLabel(t *testing.T) {
	require.Equal(t, "1 % sample", mapLevel{sampling: 100}.label())
	require.Equal(t, "10 % sample", mapLevel{sampling: 10}.label())
	require.Equal(t, "full table", mapLevel{sampling: 1}.label())
}

// A level climbs only within budget, Refresh's noBudget climbs past it, and
// the last level stays.
func TestMapLadderServedAndBudget(t *testing.T) {
	levels := mapLadderLevels("t_sample100", 1, true, nil)
	var l mapLadder
	require.True(t, l.reset("a", levels))
	require.False(t, l.reset("a", levels), "the same inputs leave the ladder where it is")
	require.True(t, l.served(time.Second))
	require.Equal(t, 1, l.level)
	require.False(t, l.served(mapLadderBudget+time.Second), "an overrun stops the climb")
	require.True(t, l.stopped)
	require.Contains(t, l.status(l.current(), false), "refinement paused")

	require.True(t, l.reset("b", levels))
	l.noBudget = true
	require.True(t, l.served(mapLadderBudget+time.Second))
	require.True(t, l.served(mapLadderBudget+time.Second))
	require.False(t, l.served(0), "the last level stays")
	require.Equal(t, "full table", l.status(l.current(), false))
}

// ladderExecutor serves a 1×1 raster for any table except the missing ones,
// which fail as ClickHouse does, and records the tables it was asked for.
type ladderExecutor struct {
	mu      sync.Mutex
	missing map[string]bool
	tables  []string
}

func (inst *ladderExecutor) execute(_ context.Context, c compiledNode, _ memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	m := regexp.MustCompile(`(?m)^FROM (\S+)$`).FindStringSubmatch(c.SQL)
	inst.mu.Lock()
	inst.tables = append(inst.tables, m[1])
	inst.mu.Unlock()
	if inst.missing[m[1]] {
		err = errString("clickhouse http 404: Code: 60. DB::Exception: Unknown table expression identifier '" + m[1] + "'. (UNKNOWN_TABLE)")
		return
	}
	rec = rgbaRec([]uint8{1}, []uint8{2}, []uint8{3}, []uint8{4})
	schema = rec.Schema()
	return
}

func (inst *ladderExecutor) asked() []string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]string(nil), inst.tables...)
}

// The panel climbs the ladder one landed level at a time under one set of
// vp_* params, skips a derived level the server lacks, names the level on
// screen, and a settle with the same view does not start it over.
func TestMapDriverClimbsTheLadder(t *testing.T) {
	exec := &ladderExecutor{missing: map[string]bool{"planes_mercator_sample10": true}}
	d := NewMapDriver(nil, nil)
	d.lane.close()
	d.lane = newNodeLane(exec, memory.NewGoAllocator(), 0)
	defer d.lane.close()
	g := newQueryGraph(nil, nil)

	settle := func() map[string]string {
		d.updateViewport(47, 48, 8, 9, 64, 64, graphEmitter{graph: g})
		return resolveSignalNames(d.templateReads, nil, g.signals())
	}
	params := settle()
	require.Eventually(t, func() bool {
		d.demandRaster(params)
		return d.packLevel.table == "planes_mercator" && !d.loading
	}, 2*time.Second, time.Millisecond)
	require.Equal(t, []string{"planes_mercator_sample100", "planes_mercator_sample10", "planes_mercator"}, exec.asked())
	require.Nil(t, d.laneErr, "the skipped level's error is not the panel's")
	require.True(t, d.ladder.missing["planes_mercator_sample10"])
	require.Contains(t, d.statusLine(), "full table")

	params = settle()
	for range 20 {
		d.demandRaster(params)
	}
	require.Len(t, exec.asked(), 3, "an unchanged settle neither restarts nor re-runs")

	d.updateViewport(47, 48, 9, 10, 64, 64, graphEmitter{graph: g})
	params = resolveSignalNames(d.templateReads, nil, g.signals())
	require.Eventually(t, func() bool {
		d.demandRaster(params)
		return len(exec.asked()) == 5 && !d.loading
	}, 2*time.Second, time.Millisecond)
	require.Equal(t, []string{"planes_mercator_sample100", "planes_mercator"}, exec.asked()[3:], "a pan starts over, without the missing level")
}

// The sparse shape scatters (pos, r, g, b, a) rows into a zeroed buffer in
// any order, and drops a pos past the raster; extra columns are ignored.
func TestPackRasterSparse(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{
		{Name: "pos", Type: arrow.PrimitiveTypes.Uint32},
		{Name: "r", Type: arrow.PrimitiveTypes.Uint8}, {Name: "g", Type: arrow.PrimitiveTypes.Uint8},
		{Name: "b", Type: arrow.PrimitiveTypes.Uint8}, {Name: "a", Type: arrow.PrimitiveTypes.Uint8},
		{Name: "extra", Type: arrow.PrimitiveTypes.Uint32},
	}, nil))
	defer b.Release()
	b.Field(0).(*array.Uint32Builder).AppendValues([]uint32{3, 0, 99}, nil)
	for i := 1; i <= 4; i++ {
		b.Field(i).(*array.Uint8Builder).AppendValues([]uint8{uint8(i), uint8(10 + i), 7}, nil)
	}
	b.Field(5).(*array.Uint32Builder).AppendValues([]uint32{1, 2, 3}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	px, err := packRaster(rec, 2, 2)
	require.NoError(t, err)
	require.Equal(t, []uint32{0x0b0c0d0e, 0, 0, 0x01020304}, px)
}

// The cache toggle reaches the lane's requests, and Refresh's freshness
// lasts until the view changes.
func TestMapCacheFollowsToggleAndRefresh(t *testing.T) {
	exec := &ladderExecutor{}
	d := NewMapDriver(nil, nil)
	d.lane.close()
	d.lane = newNodeLane(exec, memory.NewGoAllocator(), 0)
	defer d.lane.close()
	g := newQueryGraph(nil, nil)
	settle := func(lon float64) {
		d.updateViewport(47, 48, lon, lon+1, 64, 64, graphEmitter{graph: g})
		d.demandRaster(resolveSignalNames(d.templateReads, nil, g.signals()))
	}

	settle(8)
	require.False(t, d.cacheUse.Load(), "off by default")
	d.cache = true
	settle(8)
	require.True(t, d.cacheUse.Load())
	require.False(t, d.cacheFresh.Load())

	d.requestRefresh()
	settle(8)
	require.True(t, d.cacheFresh.Load(), "a Refresh computes afresh")
	settle(8)
	require.True(t, d.cacheFresh.Load(), "for the whole climb it restarted")
	settle(9)
	require.False(t, d.cacheFresh.Load(), "a pan reads the cache again")
}
