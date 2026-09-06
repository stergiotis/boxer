package worldmap

import (
	"hash/fnv"
	"math"
	"testing"
)

func mustAtlas(t *testing.T) *Atlas {
	t.Helper()
	a, err := LoadAtlas()
	if err != nil {
		t.Fatalf("LoadAtlas: %v", err)
	}
	return a
}

func TestAtlasLoads(t *testing.T) {
	a := mustAtlas(t)
	// The vendored NE 110m admin-0 file carries 177 features (assets/README.md).
	if got := len(a.Countries); got != 177 {
		t.Fatalf("countries = %d, want 177", got)
	}
	for i := range a.Countries {
		ct := &a.Countries[i]
		if len(ct.geo) == 0 {
			t.Errorf("%s: no rings", ct.Admin)
		}
		for _, r := range ct.geo {
			if len(r) < 4 {
				t.Errorf("%s: degenerate ring (%d pts)", ct.Admin, len(r))
			}
			for _, g := range r {
				if g.Lon < -180 || g.Lon > 180 || g.Lat < -90 || g.Lat > 90 {
					t.Fatalf("%s: vertex outside lon/lat: (%v, %v)", ct.Admin, g.Lon, g.Lat)
				}
			}
		}
	}
	// Every projection covers the same countries in the same order (CountryIdx
	// indexes both) with the same rings (Country.ringHole stays parallel), and
	// lands inside the normalized box the rasterizer indexes with.
	for _, p := range Projections {
		pa := a.Projected(p)
		if len(pa.countries) != len(a.Countries) {
			t.Fatalf("%s: %d projected countries, want %d", p, len(pa.countries), len(a.Countries))
		}
		for i := range pa.countries {
			rings := pa.countries[i].rings
			if len(rings) != len(a.Countries[i].geo) {
				t.Fatalf("%s/%s: %d projected rings, want %d",
					p, a.Countries[i].Admin, len(rings), len(a.Countries[i].geo))
			}
			for _, r := range rings {
				for _, q := range r {
					if q.X < 0 || q.X > 1 || q.Y < 0 || q.Y > 1 {
						t.Fatalf("%s/%s: vertex outside normalized space: (%v, %v)",
							p, a.Countries[i].Admin, q.X, q.Y)
					}
				}
			}
			if bb := pa.countries[i].bbox; !(bb[0] <= bb[2] && bb[1] <= bb[3]) {
				t.Errorf("%s/%s: inverted bbox %v", p, a.Countries[i].Admin, bb)
			}
		}
	}
}

// The projected geometry is memoized per projection on the process-wide atlas:
// flipping the picker back and forth must not re-project 10k points, and two
// projections must not share a buffer.
func TestProjectedCaching(t *testing.T) {
	a := mustAtlas(t)
	first := a.Projected(ProjectionEqualEarth)
	if first != a.Projected(ProjectionEqualEarth) {
		t.Error("Atlas.Projected re-projected instead of returning the cache")
	}
	if first == a.Projected(ProjectionNaturalEarth) {
		t.Error("two projections share one Projected")
	}
	// An out-of-enum value falls back to the default rather than panicking on
	// the array index — a stale persisted setting still draws a map.
	if a.Projected(Projection(200)) != a.Projected(ProjectionNaturalEarth) {
		t.Error("an unknown projection did not fall back to the default")
	}
	if got := first.Rings(NoCountry); got != nil {
		t.Errorf("Rings(NoCountry) = %v, want nil", got)
	}
	if got := first.Rings(CountryIdx(len(a.Countries))); got != nil {
		t.Errorf("Rings(out of range) = %v, want nil", got)
	}
}

func TestResolve(t *testing.T) {
	a := mustAtlas(t)
	cases := []struct {
		in   string
		want string // expected Country.Name; "" = must miss
	}{
		{"DE", "Germany"},
		{"deu", "Germany"},
		{"Germany", "Germany"},
		{"  germany ", "Germany"},
		// The upstream -99 quirks resolve through the _EH fields (SD1/SD4).
		{"FR", "France"},
		{"FRA", "France"},
		{"NO", "Norway"},
		{"XK", "Kosovo"},
		{"Kosovo", "Kosovo"},
		// No ISO codes upstream — name-only entries.
		{"Northern Cyprus", "N. Cyprus"},
		{"N. Cyprus", "N. Cyprus"},
		{"Somaliland", "Somaliland"},
		// ADMIN and NAME are both keys.
		{"United Republic of Tanzania", "Tanzania"},
		{"Dem. Rep. Congo", "Dem. Rep. Congo"},
		{"Democratic Republic of the Congo", "Dem. Rep. Congo"},
		{"Ivory Coast", "Côte d'Ivoire"},
		{"Côte d'Ivoire", "Côte d'Ivoire"},
		// Aliases.
		{"United States", "United States of America"},
		{"USA", "United States of America"},
		{"US", "United States of America"},
		{"UK", "United Kingdom"},
		{"Czech Republic", "Czechia"},
		{"Burma", "Myanmar"},
		// Misses.
		{"XX", ""},
		{"Atlantis", ""},
		{"Cape Verde", ""}, // absent at 110m scale — must miss cleanly
		{"", ""},
	}
	for _, tc := range cases {
		idx, ok := a.Resolve(tc.in)
		if tc.want == "" {
			if ok {
				t.Errorf("Resolve(%q) = %s, want miss", tc.in, a.Countries[idx].Name)
			}
			continue
		}
		if !ok {
			t.Errorf("Resolve(%q) missed, want %s", tc.in, tc.want)
			continue
		}
		if got := a.Countries[idx].Name; got != tc.want {
			t.Errorf("Resolve(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestCanvasBox(t *testing.T) {
	var w Widget // canvasBox reads only the display knobs, not the atlas.

	// Default: span the probed pane width (less the scrollbar margin), height
	// from the projection aspect.
	cw, ch := w.canvasBox(1000)
	if cw != 1000-canvasMargin {
		t.Fatalf("pane-sized width = %d, want %d", cw, 1000-canvasMargin)
	}
	if want := max(int(float64(cw)/ProjectionNaturalEarth.Aspect()), 1); ch != want {
		t.Fatalf("pane-sized height = %d, want %d (aspect-derived)", ch, want)
	}
	if cw <= ch {
		t.Fatalf("world map should be wider than tall, got %dx%d", cw, ch)
	}

	// A height cap binds, and the width follows it back down to keep the
	// aspect rather than stretching the map.
	w.SetDisplayHeight(200)
	cw, ch = w.canvasBox(1000)
	if ch != 200 {
		t.Fatalf("height-capped height = %d, want 200", ch)
	}
	if want := int(float64(200) * ProjectionNaturalEarth.Aspect()); cw != want {
		t.Fatalf("height-capped width = %d, want %d (aspect-derived)", cw, want)
	}

	// An explicit display width wins over the pane probe (the demo's "Width:"
	// slider must resize the map) — the height still follows the aspect, so a
	// height cap under it still binds.
	w.SetDisplayHeight(0)
	w.SetDisplayWidth(900)
	cw, ch = w.canvasBox(1000)
	if cw != 900 {
		t.Fatalf("display-width width = %d, want 900 (the pane probe must not shadow it)", cw)
	}
	if want := max(int(float64(900)/ProjectionNaturalEarth.Aspect()), 1); ch != want {
		t.Fatalf("display-width height = %d, want %d (aspect-derived)", ch, want)
	}

	// Clearing the width falls back to the pane probe.
	w.SetDisplayWidth(0)
	if cw, _ = w.canvasBox(600); cw != 600-canvasMargin {
		t.Fatalf("after clearing width, width = %d, want %d", cw, 600-canvasMargin)
	}

	// A degenerate probe (first frame passes the fallback) still yields a
	// drawable box, and an absurd one is clamped rather than trusted.
	if cw, ch = w.canvasBox(0); cw < minCanvasW || ch < 1 {
		t.Fatalf("zero-pane box = %dx%d, want at least %d wide", cw, ch, minCanvasW)
	}
	if cw, ch = w.canvasBox(1e6); cw > maxCanvasW || ch > maxCanvasH {
		t.Fatalf("huge-pane box = %dx%d, want clamped to %dx%d", cw, ch, maxCanvasW, maxCanvasH)
	}
}

// The painter highlight fills outer rings and only outlines holes, so the
// atlas has to keep the roles parallel to the rings. The asset carries exactly
// one hole (South Africa's Lesotho enclave); that count is a fact about the
// vendored file, so a change to it should be noticed.
func TestRingRoles(t *testing.T) {
	a, err := LoadAtlas()
	if err != nil {
		t.Fatal(err)
	}
	holes := 0
	var holed []string
	for i := range a.Countries {
		ct := &a.Countries[i]
		if len(ct.ringHole) != len(ct.geo) {
			t.Fatalf("%s: %d ring roles for %d rings", ct.Admin, len(ct.ringHole), len(ct.geo))
		}
		if len(ct.geo) > 0 && ct.ringHole[0] {
			t.Fatalf("%s: first ring marked as a hole", ct.Admin)
		}
		for _, h := range ct.ringHole {
			if h {
				holes++
			}
		}
		if len(ct.ringHole) > 0 && ct.ringHole[len(ct.ringHole)-1] {
			holed = append(holed, ct.Admin)
		}
	}
	if holes != 1 {
		t.Fatalf("asset carries %d hole rings, want 1 (South Africa/Lesotho) — holed: %v", holes, holed)
	}
	if len(holed) != 1 || holed[0] != "South Africa" {
		t.Fatalf("hole belongs to %v, want [South Africa]", holed)
	}
}

// widenDegenerate must keep min < max at every magnitude — SetValues feeds the
// result to colormap.NewConfig, which panics on min == max. A fixed ±0.5 pad
// vanishes below the float64 ULP near 2^63 (a uint64 id/hash column where the
// clicked-country drill-down leaves every row equal), which crashed the value
// fill; the pad now scales to the magnitude.
func TestWidenDegenerate(t *testing.T) {
	for _, v := range []float64{
		0, 5, -5, 0.25, 1e6, -1e6, 1e15, 1e18, -1e18,
		float64(uint64(1) << 63),  // ~9.2e18, ULP ~2048 — the panic case
		-float64(uint64(1) << 63), // and negative
	} {
		mn, mx := widenDegenerate(v)
		if !(mn < mx) {
			t.Fatalf("widenDegenerate(%g) = [%g, %g]; NewConfig needs min < max", v, mn, mx)
		}
		if math.IsInf(mn, 0) || math.IsInf(mx, 0) || math.IsNaN(mn) || math.IsNaN(mx) {
			t.Fatalf("widenDegenerate(%g) = [%g, %g]; must stay finite", v, mn, mx)
		}
	}
}

func TestProjection(t *testing.T) {
	// Aspect of each projection's world extent, as published: ~1.923 for
	// Natural Earth (Šavrič et al. 2011), ~2.05 for Equal Earth (Šavrič et al.
	// 2018).
	for _, tc := range []struct {
		p      Projection
		aspect float64
	}{
		{ProjectionNaturalEarth, 1.923},
		{ProjectionEqualEarth, 2.055},
	} {
		if a := tc.p.Aspect(); math.Abs(a-tc.aspect) > 0.02 {
			t.Errorf("%s aspect = %v, want ≈%v", tc.p, a, tc.aspect)
		}
	}
	// Anchors every projection here must hit: both are pseudocylindrical,
	// symmetric in each axis, with x linear in longitude along the equator.
	checks := []struct {
		lon, lat, wantX, wantY, tol float64
	}{
		{0, 0, 0.5, 0.5, 1e-9},   // origin center
		{-180, 0, 0, 0.5, 1e-9},  // west edge at the equator
		{180, 0, 1, 0.5, 1e-9},   // east edge at the equator
		{0, 90, 0.5, 0, 1e-9},    // north pole at the top
		{0, -90, 0.5, 1, 1e-9},   // south pole at the bottom
		{90, 0, 0.75, 0.5, 1e-9}, // linear in lon at the equator
	}
	for _, p := range Projections {
		for _, ck := range checks {
			x, y := projectNorm(p, ck.lon, ck.lat)
			if math.Abs(x-ck.wantX) > ck.tol || math.Abs(y-ck.wantY) > ck.tol {
				t.Errorf("%s: projectNorm(%v, %v) = (%v, %v), want (%v, %v)",
					p, ck.lon, ck.lat, x, y, ck.wantX, ck.wantY)
			}
		}
		// Monotonic in lon along a fixed latitude; monotonic (decreasing y) in lat.
		prevX := -1.0
		for lon := -180.0; lon <= 180; lon += 15 {
			x, _ := projectNorm(p, lon, 30)
			if x <= prevX {
				t.Fatalf("%s: x not monotonic in lon at lat 30 (lon %v)", p, lon)
			}
			prevX = x
		}
		prevY := 2.0
		for lat := -90.0; lat <= 90; lat += 15 {
			_, y := projectNorm(p, 0, lat)
			if y >= prevY {
				t.Fatalf("%s: y not decreasing in lat (lat %v)", p, lat)
			}
			prevY = y
		}
	}
}

// The world extent is taken analytically at the equator and the pole, on the
// claim that no parallel projects wider or taller. That holds for these two
// formulas but not for pseudocylindrical projections in general, so it is
// scanned rather than assumed: a projection added with a bulging parallel
// would otherwise project vertices outside the normalized box the rasterizer
// indexes with.
func TestProjectionExtent(t *testing.T) {
	const steps = 4096
	for _, p := range Projections {
		sp := p.spec()
		for i := 0; i <= steps; i++ {
			phi := -math.Pi/2 + math.Pi*float64(i)/steps
			if x := math.Abs(sp.x(math.Pi, phi)); x > sp.xMax*(1+1e-12) {
				t.Fatalf("%s: |x| = %v at lat %v exceeds xMax %v",
					p, x, phi*180/math.Pi, sp.xMax)
			}
			if y := math.Abs(sp.y(phi)); y > sp.yMax*(1+1e-12) {
				t.Fatalf("%s: |y| = %v at lat %v exceeds yMax %v",
					p, y, phi*180/math.Pi, sp.yMax)
			}
		}
	}
}

// Equal Earth is equal-area — that property is the reason it is offered next
// to Natural Earth, so it is checked rather than trusted to the coefficients:
// the projected area of a 10°×10° cell divided by the cell's spherical area
// must be the same constant everywhere. Natural Earth, a compromise
// projection, must fail the same check by a wide margin; that arm keeps the
// test from passing on a bug that flattens both.
func TestEqualEarthIsEqualArea(t *testing.T) {
	spread := func(p Projection) float64 {
		lo, hi := math.Inf(1), math.Inf(-1)
		for lat := -90.0; lat < 90; lat += 10 {
			sph := (10 * math.Pi / 180) *
				(math.Sin((lat+10)*math.Pi/180) - math.Sin(lat*math.Pi/180))
			r := projCellArea(p, 0, lat, 10, 10, 512) / sph
			lo, hi = math.Min(lo, r), math.Max(hi, r)
		}
		return hi/lo - 1
	}
	if got := spread(ProjectionEqualEarth); got > 1e-3 {
		t.Errorf("Equal Earth cell-area ratio varies by %.3g%%, want equal-area", got*100)
	}
	if got := spread(ProjectionNaturalEarth); got < 0.05 {
		t.Errorf("Natural Earth cell-area ratio varies by only %.3g%% — the "+
			"equal-area check above cannot be distinguishing anything", got*100)
	}
}

// projCellArea integrates the projected area of the spherical cell
// [lon, lon+dLon] × [lat, lat+dLat] in normalized projection units. x is
// linear in longitude and y depends on latitude alone, so the cell's area is
// ∫ width(φ) dy — a strip sum, no polygon.
func projCellArea(p Projection, lon, lat, dLon, dLat float64, steps int) float64 {
	area := 0.0
	for i := range steps {
		phi0 := lat + dLat*float64(i)/float64(steps)
		phi1 := lat + dLat*float64(i+1)/float64(steps)
		x0, _ := projectNorm(p, lon, (phi0+phi1)/2)
		x1, _ := projectNorm(p, lon+dLon, (phi0+phi1)/2)
		_, ya := projectNorm(p, lon, phi0)
		_, yb := projectNorm(p, lon, phi1)
		area += math.Abs(x1-x0) * math.Abs(yb-ya)
	}
	return area
}

// pixelAt maps a lon/lat to the (row, col) of a w×h raster under p.
func pixelAt(p Projection, lon, lat float64, w, h int) (row, col int) {
	x, y := projectNorm(p, lon, lat)
	return int(y * float64(h)), int(x * float64(w))
}

// natEarth is the default projection's geometry — what every raster test that
// is not about projections works from.
func natEarth(a *Atlas) *Projected { return a.Projected(ProjectionNaturalEarth) }

func testStyle(a *Atlas) rasterStyle {
	fills := make([]uint32, len(a.Countries))
	for i := range fills {
		// Distinct opaque fill per country so rgba↔index consistency is checkable.
		fills[i] = uint32(i)<<16 | 0x000000ff | uint32(i)<<25
	}
	return rasterStyle{fills: fills, sea: 0x00000000, stroke: 0x0a0a0a8c}
}

func TestRasterizeHitsInteriors(t *testing.T) {
	a := mustAtlas(t)
	const w = 512
	h := int(float64(w) / ProjectionNaturalEarth.Aspect())
	rgba, index := rasterize(natEarth(a), w, h, testStyle(a))
	if len(rgba) != w*h || len(index) != w*h {
		t.Fatalf("buffer sizes %d/%d, want %d", len(rgba), len(index), w*h)
	}
	// Projection corners are sea.
	for _, o := range []int{0, w - 1, (h - 1) * w, h*w - 1} {
		if index[o] != NoCountry {
			t.Errorf("corner %d: index %d, want sea", o, index[o])
		}
		if rgba[o]&0xff != 0 {
			t.Errorf("corner %d: alpha %d, want transparent sea", o, rgba[o]&0xff)
		}
	}
	// Interior samples far from any border.
	interior := []struct {
		lon, lat float64
		key      string
	}{
		{-53, -11, "BRA"},
		{-100, 40, "USA"},
		{100, 60, "RUS"},
		{134, -25, "AUS"},
		{10, 51, "DEU"},
		{78, 22, "IND"},
	}
	for _, s := range interior {
		want, ok := a.Resolve(s.key)
		if !ok {
			t.Fatalf("resolver missing %s", s.key)
		}
		row, col := pixelAt(ProjectionNaturalEarth, s.lon, s.lat, w, h)
		got := index[row*w+col]
		if got != want {
			gotName := "sea"
			if got != NoCountry {
				gotName = a.Countries[got].Admin
			}
			t.Errorf("(%v,%v): index = %s, want %s", s.lon, s.lat, gotName, s.key)
		}
	}
	// Open ocean is sea.
	row, col := pixelAt(ProjectionNaturalEarth, -40, -40, w, h) // South Atlantic
	if got := index[row*w+col]; got != NoCountry {
		t.Errorf("mid-Atlantic: index = %d (%s), want sea", got, a.Countries[got].Admin)
	}
}

func TestRasterizeDeterministic(t *testing.T) {
	a := mustAtlas(t)
	const w = 256
	h := int(float64(w) / ProjectionNaturalEarth.Aspect())
	sum := func() (uint64, uint64) {
		rgba, index := rasterize(natEarth(a), w, h, testStyle(a))
		hr := fnv.New64a()
		for _, p := range rgba {
			hr.Write([]byte{byte(p >> 24), byte(p >> 16), byte(p >> 8), byte(p)})
		}
		hi := fnv.New64a()
		for _, ci := range index {
			hi.Write([]byte{byte(uint32(ci) >> 24), byte(uint32(ci) >> 16), byte(uint32(ci) >> 8), byte(uint32(ci))})
		}
		return hr.Sum64(), hi.Sum64()
	}
	r1, i1 := sum()
	r2, i2 := sum()
	if r1 != r2 || i1 != i2 {
		t.Fatalf("rasterize not deterministic: rgba %x/%x index %x/%x", r1, r2, i1, i2)
	}
}

// The raster output is pinned by hash. The pass is split into a cached
// size-derived half and a per-data recolour, with a lookup table standing in
// for the four-subsample average wherever the subsamples agree — all of which
// is only sound if it reproduces the straightforward pass exactly. These
// hashes are that pass's output, taken before the split; they are not
// arbitrary goldens to re-bless, so a diff here means the reduction changed,
// not that the test drifted.
func TestRasterizeGolden(t *testing.T) {
	a := mustAtlas(t)
	for _, tc := range []struct {
		w           int
		rgba, index uint64
	}{
		{w: 256, rgba: 0x9ce5bd28b0ae1683, index: 0x25198ccf54eff5a6},
		{w: 512, rgba: 0xcc0be04b485a4a97, index: 0x9843a812853fd285},
	} {
		h := int(float64(tc.w) / ProjectionNaturalEarth.Aspect())
		rgba, index := rasterize(natEarth(a), tc.w, h, testStyle(a))
		if got := hashPixels(rgba); got != tc.rgba {
			t.Errorf("w=%d rgba hash = %#016x, want %#016x", tc.w, got, tc.rgba)
		}
		if got := hashIndex(index); got != tc.index {
			t.Errorf("w=%d index hash = %#016x, want %#016x", tc.w, got, tc.index)
		}
	}
}

// A cached geometry must repaint to exactly what a from-scratch pass produces,
// for any style — otherwise a value change would leave the previous data's
// colours (or its lookup table) showing through.
func TestResolveReusesGeometry(t *testing.T) {
	a := mustAtlas(t)
	const w = 256
	h := int(float64(w) / ProjectionNaturalEarth.Aspect())

	alt := testStyle(a)
	for i := range alt.fills { // a palette with nothing in common with testStyle
		alt.fills[i] = uint32(len(alt.fills)-i)<<8 | 0xff0000ff
	}
	alt.sea = 0x102030ff
	alt.stroke = 0xffffffc0

	g := buildRasterGeometry(natEarth(a), w, h)
	rgba := make([]uint32, w*h)
	// Same geometry, three repaints in a row: each must match the one-shot
	// pass, including the second visit to a style already painted once.
	for _, style := range []rasterStyle{testStyle(a), alt, testStyle(a)} {
		g.resolve(rgba, style)
		want, _ := rasterize(natEarth(a), w, h, style)
		if hashPixels(rgba) != hashPixels(want) {
			t.Fatalf("cached resolve differs from a fresh rasterize")
		}
	}
}

// The widget keeps the geometry across data changes and must drop it when the
// raster size changes — a stale one would repaint into a buffer of the wrong
// shape, or silently keep the old resolution.
func TestWidgetGeometryCacheLifecycle(t *testing.T) {
	a := mustAtlas(t)
	w := &Widget{atlas: a, NoDataRGBA: 0x555555ff, StrokeRGBA: 0x0a0a0a8c}

	h1 := int(float64(256) / ProjectionNaturalEarth.Aspect())
	w.rasterizeNow(256, h1)
	first := w.geom
	if first == nil || w.rw != 256 || len(w.rgba) != 256*h1 {
		t.Fatalf("first raster: geom=%v rw=%d pixels=%d", first != nil, w.rw, len(w.rgba))
	}
	if w.index == nil || len(w.index) != 256*h1 {
		t.Fatalf("hit-test buffer not published (%d entries)", len(w.index))
	}

	// A data change at the same size keeps the geometry and the buffer.
	rgbaBefore := w.rgba
	w.PresenceRGBA = 0x2a788eff
	w.presence, w.haveValues = true, true
	w.values = make([]float64, len(a.Countries))
	w.values[0] = 1
	w.rasterizeNow(256, h1)
	if w.geom != first {
		t.Error("a value change rebuilt the geometry")
	}
	if &w.rgba[0] != &rgbaBefore[0] {
		t.Error("a value change reallocated the pixel buffer")
	}

	// A size change replaces both, and the published hit-test buffer follows.
	h2 := int(float64(320) / ProjectionNaturalEarth.Aspect())
	w.rasterizeNow(320, h2)
	if w.geom == first {
		t.Error("a size change kept the old geometry")
	}
	if len(w.rgba) != 320*h2 || w.rw != 320 || w.rh != h2 {
		t.Errorf("after resize: %dx%d, %d pixels", w.rw, w.rh, len(w.rgba))
	}
	if len(w.index) != 320*h2 {
		t.Errorf("hit-test buffer still %d entries, want %d", len(w.index), 320*h2)
	}
}

// Switching the projection must rebuild the raster geometry — it is keyed on
// (outlines, size) and the outlines move — and re-derive the aspect-following
// height. A kept geometry would leave the previous projection's shapes on
// screen, with the hit-test index buffer to match.
func TestWidgetSetProjection(t *testing.T) {
	a := mustAtlas(t)
	w := &Widget{atlas: a, NoDataRGBA: 0x555555ff, StrokeRGBA: 0x0a0a0a8c, wantW: 256}
	w.wantH = w.heightFor(w.wantW)
	if w.projection != ProjectionNaturalEarth {
		t.Fatalf("the zero Widget draws %s, want the Natural Earth default", w.projection)
	}
	// A fixed height for both arms, so the two index buffers are comparable.
	w.rasterizeNow(256, 100)
	natGeom, natIndex := w.geom, hashIndex(w.index)

	w.SetProjection(ProjectionEqualEarth)
	if !w.dirty {
		t.Error("a projection change did not mark the texture dirty")
	}
	if want := max(int(256/ProjectionEqualEarth.Aspect()), 1); w.wantH != want {
		t.Errorf("raster height = %d after the switch, want %d (the new aspect)", w.wantH, want)
	}
	w.rasterizeNow(256, 100)
	if w.geom == natGeom {
		t.Error("a projection change kept the previous projection's geometry")
	}
	if w.geom.proj != ProjectionEqualEarth {
		t.Errorf("geometry built for %s, want %s", w.geom.proj, ProjectionEqualEarth)
	}
	if hashIndex(w.index) == natIndex {
		t.Error("Equal Earth produced Natural Earth's hit-test buffer")
	}

	// Re-selecting the current projection is a no-op — no dirty flag, so no
	// re-raster on the next frame.
	w.dirty = false
	w.SetProjection(ProjectionEqualEarth)
	if w.dirty {
		t.Error("re-selecting the current projection re-rasterizes")
	}
	// An unknown value falls back to the default rather than to no map.
	w.SetProjection(Projection(200))
	if w.projection != ProjectionNaturalEarth {
		t.Errorf("unknown projection left %s selected, want the default", w.projection)
	}
}

func hashPixels(px []uint32) uint64 {
	h := fnv.New64a()
	for _, p := range px {
		h.Write([]byte{byte(p >> 24), byte(p >> 16), byte(p >> 8), byte(p)})
	}
	return h.Sum64()
}

func hashIndex(index []CountryIdx) uint64 {
	h := fnv.New64a()
	for _, ci := range index {
		h.Write([]byte{byte(uint32(ci) >> 24), byte(uint32(ci) >> 16), byte(uint32(ci) >> 8), byte(uint32(ci))})
	}
	return h.Sum64()
}

// The two halves of the pass, measured apart: BenchmarkRasterGeometry is what a
// size change costs, BenchmarkRasterResolve what a value change costs. The
// second is the one that runs on every query result.
func BenchmarkRasterGeometry(b *testing.B) {
	a, err := LoadAtlas()
	if err != nil {
		b.Fatal(err)
	}
	const w = 1280
	h := int(float64(w) / ProjectionNaturalEarth.Aspect())
	b.ReportAllocs()
	for b.Loop() {
		buildRasterGeometry(natEarth(a), w, h)
	}
}

func BenchmarkRasterResolve(b *testing.B) {
	a, err := LoadAtlas()
	if err != nil {
		b.Fatal(err)
	}
	const w = 1280
	h := int(float64(w) / ProjectionNaturalEarth.Aspect())
	g := buildRasterGeometry(natEarth(a), w, h)
	style := testStyle(a)
	rgba := make([]uint32, w*h)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		g.resolve(rgba, style)
	}
}

func TestRasterizeFillMatchesIndex(t *testing.T) {
	a := mustAtlas(t)
	style := testStyle(a)
	const w = 512
	h := int(float64(w) / ProjectionNaturalEarth.Aspect())
	rgba, index := rasterize(natEarth(a), w, h, style)
	// An interior Brazil pixel carries Brazil's exact fill (all four
	// subsamples agree and no border coverage blends over it).
	bra, _ := a.Resolve("BRA")
	row, col := pixelAt(ProjectionNaturalEarth, -53, -11, w, h)
	o := row*w + col
	if index[o] != bra {
		t.Fatalf("interior pixel not Brazil (index %d)", index[o])
	}
	if rgba[o] != style.fills[bra] {
		t.Errorf("interior fill %08x, want %08x", rgba[o], style.fills[bra])
	}
}
