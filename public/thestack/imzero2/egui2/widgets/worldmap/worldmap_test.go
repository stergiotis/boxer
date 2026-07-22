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
		if len(ct.rings) == 0 {
			t.Errorf("%s: no rings", ct.Admin)
		}
		for _, r := range ct.rings {
			if len(r) < 4 {
				t.Errorf("%s: degenerate ring (%d pts)", ct.Admin, len(r))
			}
			for _, p := range r {
				if p.X < 0 || p.X > 1 || p.Y < 0 || p.Y > 1 {
					t.Fatalf("%s: vertex outside normalized space: (%v, %v)", ct.Admin, p.X, p.Y)
				}
			}
		}
		if !(ct.bbox[0] <= ct.bbox[2] && ct.bbox[1] <= ct.bbox[3]) {
			t.Errorf("%s: inverted bbox %v", ct.Admin, ct.bbox)
		}
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

func TestFitBox(t *testing.T) {
	var w Widget // fitBox reads only the display knobs, not the atlas.

	// Default: fill the available width, no height cap.
	if fw, fh := w.fitBox(); fw != 0 || fh != 0 {
		t.Fatalf("default fitBox = (%d, %d), want (0, 0)", fw, fh)
	}

	// A height cap fills the width but bounds the height.
	w.SetDisplayHeight(340)
	if fw, fh := w.fitBox(); fw != 0 || fh != 340 {
		t.Fatalf("height-capped fitBox = (%d, %d), want (0, 340)", fw, fh)
	}

	// An explicit display width wins over the height cap (the demo's "Width:"
	// slider must resize the map, not be shadowed by a fill-available fit): the
	// box carries the map's own aspect, so the width is exact and the height is
	// aspect-derived.
	w.SetDisplayWidth(900)
	fw, fh := w.fitBox()
	if fw != 900 {
		t.Fatalf("display-width fitBox width = %d, want 900 (height cap must not shadow it)", fw)
	}
	wantH := uint32(max(int(float64(900)/ProjectionAspect()), 1))
	if fh != wantH {
		t.Fatalf("display-width fitBox height = %d, want %d (aspect-derived)", fh, wantH)
	}
	if fw <= fh {
		t.Fatalf("world map should be wider than tall, got %dx%d", fw, fh)
	}

	// Clearing the width falls back to the height-cap path.
	w.SetDisplayWidth(0)
	if fw, fh := w.fitBox(); fw != 0 || fh != 340 {
		t.Fatalf("after clearing width, fitBox = (%d, %d), want (0, 340)", fw, fh)
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
	// Aspect of the Natural Earth projection's world extent (Šavrič et al.):
	// ~1.923 wide:high.
	if a := ProjectionAspect(); math.Abs(a-1.923) > 0.02 {
		t.Fatalf("ProjectionAspect = %v, want ≈1.923", a)
	}
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
	for _, ck := range checks {
		x, y := projectNorm(ck.lon, ck.lat)
		if math.Abs(x-ck.wantX) > ck.tol || math.Abs(y-ck.wantY) > ck.tol {
			t.Errorf("projectNorm(%v, %v) = (%v, %v), want (%v, %v)",
				ck.lon, ck.lat, x, y, ck.wantX, ck.wantY)
		}
	}
	// Monotonic in lon along a fixed latitude; monotonic (decreasing y) in lat.
	prevX := -1.0
	for lon := -180.0; lon <= 180; lon += 15 {
		x, _ := projectNorm(lon, 30)
		if x <= prevX {
			t.Fatalf("x not monotonic in lon at lat 30 (lon %v)", lon)
		}
		prevX = x
	}
	prevY := 2.0
	for lat := -90.0; lat <= 90; lat += 15 {
		_, y := projectNorm(0, lat)
		if y >= prevY {
			t.Fatalf("y not decreasing in lat (lat %v)", lat)
		}
		prevY = y
	}
}

// pixelAt maps a lon/lat to the (row, col) of a w×h raster.
func pixelAt(lon, lat float64, w, h int) (row, col int) {
	x, y := projectNorm(lon, lat)
	return int(y * float64(h)), int(x * float64(w))
}

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
	h := int(float64(w) / ProjectionAspect())
	rgba, index := rasterize(a, w, h, testStyle(a))
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
		row, col := pixelAt(s.lon, s.lat, w, h)
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
	row, col := pixelAt(-40, -40, w, h) // South Atlantic
	if got := index[row*w+col]; got != NoCountry {
		t.Errorf("mid-Atlantic: index = %d (%s), want sea", got, a.Countries[got].Admin)
	}
}

func TestRasterizeDeterministic(t *testing.T) {
	a := mustAtlas(t)
	const w = 256
	h := int(float64(w) / ProjectionAspect())
	sum := func() (uint64, uint64) {
		rgba, index := rasterize(a, w, h, testStyle(a))
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

func TestRasterizeFillMatchesIndex(t *testing.T) {
	a := mustAtlas(t)
	style := testStyle(a)
	const w = 512
	h := int(float64(w) / ProjectionAspect())
	rgba, index := rasterize(a, w, h, style)
	// An interior Brazil pixel carries Brazil's exact fill (all four
	// subsamples agree and no border coverage blends over it).
	bra, _ := a.Resolve("BRA")
	row, col := pixelAt(-53, -11, w, h)
	o := row*w + col
	if index[o] != bra {
		t.Fatalf("interior pixel not Brazil (index %d)", index[o])
	}
	if rgba[o] != style.fills[bra] {
		t.Errorf("interior fill %08x, want %08x", rgba[o], style.fills[bra])
	}
}
