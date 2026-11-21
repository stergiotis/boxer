package imaging

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const deg = math.Pi / 180

// affineAbout is an affine that takes pixel coordinates (c0, r0) to (0, 0)
// through the 2 × 2 matrix m, as a FITS CD matrix about CRPIX does.
func affineAbout(c0, r0 float64, m [4]float64) [6]float64 {
	return [6]float64{-(m[0]*c0 + m[1]*r0), m[0], m[1], -(m[2]*c0 + m[3]*r0), m[2], m[3]}
}

func gnomonic(refLon, refLat, lonPole float64, affine [6]float64) Placement {
	return Placement{Affine: affine, Frame: Frame{Kind: FrameKindCelestialGnomonic, Code: "ICRS",
		AxisNames: [2]string{"RA", "Dec"}, Units: [2]string{"deg", "deg"}, RefLon: refLon, RefLat: refLat, LonPole: lonPole}}
}

// paperWorld evaluates Calabretta & Greisen 2002 literally — TAN's
// φ = arg(−y, x), θ = atan(180/(π R_θ)) and eq. (2) — as the reference the
// direction-cosine form is held to.
func paperWorld(f Frame, x, y float64) (lon, lat float64) {
	r := math.Hypot(x, y)
	phi := math.Atan2(x, -y)
	theta := math.Pi / 2
	if r > 0 {
		theta = math.Atan(180 / (math.Pi * r))
	}
	dp, pp := f.RefLat*deg, f.LonPole*deg
	lat = math.Asin(math.Sin(theta)*math.Sin(dp) + math.Cos(theta)*math.Cos(dp)*math.Cos(phi-pp))
	lon = f.RefLon*deg + math.Atan2(-math.Cos(theta)*math.Sin(phi-pp),
		math.Sin(theta)*math.Cos(dp)-math.Cos(theta)*math.Sin(dp)*math.Cos(phi-pp))
	lon = math.Mod(lon/deg, 360)
	if lon < 0 {
		lon += 360
	}
	return lon, lat / deg
}

func lonDiff(a, b float64) float64 {
	d := math.Mod(a-b, 360)
	if d > 180 {
		d -= 360
	}
	if d < -180 {
		d += 360
	}
	return d
}

func TestAnAffinePlacementMapsBothWays(t *testing.T) {
	cases := []struct {
		name   string
		affine [6]float64
	}{
		{"north up", [6]float64{2_600_000, 0.5, 0, 1_200_000, 0, -0.5}},
		{"rotated", affineAbout(10, 20, [4]float64{math.Cos(0.3), -math.Sin(0.3), math.Sin(0.3), math.Cos(0.3)})},
		{"sheared", [6]float64{-5, 2, 0.7, 3, 0.1, -1.5}},
	}
	for _, c := range cases {
		for _, kind := range []FrameKindE{FrameKindPhysical, FrameKindReferenceSystem} {
			p := Placement{Affine: c.affine, Frame: Frame{Kind: kind}}
			require.NoError(t, p.Validate(), c.name)
			for _, at := range [][2]float64{{0, 0}, {0.5, 0.5}, {123.25, -7}, {4096, 3000}} {
				x, y := p.Intermediate(at[0], at[1])
				a, b, ok := p.World(at[0], at[1])
				require.True(t, ok)
				assert.Equal(t, x, a)
				assert.Equal(t, y, b)
				assert.InDelta(t, c.affine[0]+c.affine[1]*at[0]+c.affine[2]*at[1], a, 1e-9)
				col, row, ok := p.Pixel(a, b)
				require.True(t, ok)
				assert.InDelta(t, at[0], col, 1e-9, "%s col", c.name)
				assert.InDelta(t, at[1], row, 1e-9, "%s row", c.name)
			}
		}
	}
	sx, sy := Placement{Affine: [6]float64{0, 3, 0, 0, 4, 2}}.PixelSize()
	assert.Equal(t, 5.0, sx)
	assert.Equal(t, 2.0, sy)

	_, _, ok := Placement{}.World(1, 1)
	assert.False(t, ok, "the zero placement places nothing")
	_, _, ok = Placement{}.Pixel(1, 1)
	assert.False(t, ok)
}

func TestPlacementValidate(t *testing.T) {
	good := gnomonic(10, 20, 180, affineAbout(0, 0, [4]float64{-1e-3, 0, 0, 1e-3}))
	require.NoError(t, good.Validate())
	assert.NoError(t, Placement{}.Validate(), "the zero placement places nothing and is valid")
	bad := map[string]func(p *Placement){
		"unknown frame":        func(p *Placement) { p.Frame.Kind = 9 },
		"singular affine":      func(p *Placement) { p.Affine = [6]float64{0, 1, 2, 0, 2, 4} },
		"zero affine":          func(p *Placement) { p.Affine = [6]float64{} },
		"NaN in the affine":    func(p *Placement) { p.Affine[0] = math.NaN() },
		"Inf in the affine":    func(p *Placement) { p.Affine[5] = math.Inf(-1) },
		"latitude above 90":    func(p *Placement) { p.Frame.RefLat = 90.5 },
		"latitude below -90":   func(p *Placement) { p.Frame.RefLat = -91 },
		"longitude not finite": func(p *Placement) { p.Frame.RefLon = math.NaN() },
		"pole not finite":      func(p *Placement) { p.Frame.LonPole = math.Inf(1) },
	}
	for name, spoil := range bad {
		p := good
		spoil(&p)
		assert.Error(t, p.Validate(), name)
	}
	// A physical frame does not read the sky fields.
	phys := Placement{Affine: [6]float64{0, 1, 0, 0, 0, 1}, Frame: Frame{Kind: FrameKindPhysical, RefLat: 400}}
	assert.NoError(t, phys.Validate())
	assert.Equal(t, 180.0, DefaultLonPole(-30))
	assert.Equal(t, 180.0, DefaultLonPole(89.999))
	assert.Equal(t, 0.0, DefaultLonPole(90))
}

func TestGnomonicClosedForms(t *testing.T) {
	for _, ref := range [][2]float64{{0, 0}, {150, -30}, {359.5, 45}, {12, -89}} {
		lon0, lat0 := ref[0], ref[1]
		// Pixel (100, 200) is the reference pixel; one pixel is 0.01° in the
		// projection plane, x east and y north.
		p := gnomonic(lon0, lat0, DefaultLonPole(lat0), affineAbout(100, 200, [4]float64{0.01, 0, 0, -0.01}))
		require.NoError(t, p.Validate())
		a, b, ok := p.World(100, 200)
		require.True(t, ok)
		assert.InDelta(t, 0, lonDiff(a, lon0), 1e-12, "the reference pixel is the reference point")
		assert.InDelta(t, lat0, b, 1e-12)

		// d degrees due north in the projection plane is at latitude
		// lat0 + atan(d·π/180), on the reference meridian.
		for _, d := range []float64{0.01, 0.5, 0.75} {
			row := 200 - d/0.01
			a, b, ok = p.World(100, row)
			require.True(t, ok)
			want := lat0 + math.Atan(d*deg)/deg
			if want <= 90 {
				assert.InDelta(t, want, b, 1e-12, "north by %v from %v", d, ref)
				assert.InDelta(t, 0, lonDiff(a, lon0), 1e-9)
			}
		}
		// On the equator, d degrees east is at longitude lon0 + atan(d·π/180).
		if lat0 == 0 {
			a, b, ok = p.World(100+50, 200)
			require.True(t, ok)
			assert.InDelta(t, math.Atan(0.5*deg)/deg, lonDiff(a, lon0), 1e-12)
			assert.InDelta(t, 0, b, 1e-12)
		}
		// The point opposite the reference point is not on the projection.
		_, _, ok = p.Pixel(lon0+180, -lat0)
		assert.False(t, ok)
		_, _, ok = p.Pixel(lon0+90, 0.5*(-lat0))
		if lat0 == 0 {
			assert.False(t, ok, "90° away is the horizon, θ = 0")
		}
	}
}

func TestGnomonicIsThePapersFormulas(t *testing.T) {
	frames := []Frame{
		{RefLon: 83.6, RefLat: 22.0, LonPole: 180},
		{RefLon: 0.2, RefLat: -60, LonPole: 180},
		{RefLon: 270, RefLat: 90, LonPole: 0},
		{RefLon: 10, RefLat: 30, LonPole: 150},
	}
	for _, f := range frames {
		for _, xy := range [][2]float64{{0.3, 0.1}, {-2, 5}, {12, -7}, {0, 0.001}, {30, 30}} {
			lon, lat := f.deproject(xy[0], xy[1])
			wl, wb := paperWorld(f, xy[0], xy[1])
			assert.InDelta(t, 0, lonDiff(lon, wl), 1e-10, "lon of %v in %+v", xy, f)
			assert.InDelta(t, wb, lat, 1e-10, "lat of %v in %+v", xy, f)
			assert.True(t, lon >= 0 && lon < 360)
		}
	}
}

func TestGnomonicRoundTripsToANanoPixel(t *testing.T) {
	cases := []struct {
		name  string
		p     Placement
		w, h  float64
		where func(t *testing.T, lon, lat float64)
	}{
		{"a field at mid latitude, rotated and sheared",
			gnomonic(150, -30, 180, affineAbout(1024, 1024, [4]float64{-2.1e-4, 0.4e-4, 0.5e-4, 2.0e-4})), 2048, 2048, nil},
		{"a field on the pole",
			gnomonic(0, 90, 0, affineAbout(512, 512, [4]float64{-1e-3, 0, 0, 1e-3})), 1024, 1024,
			func(t *testing.T, lon, lat float64) { assert.Greater(t, lat, 89.0) }},
		{"a field near the pole with LONPOLE 180",
			gnomonic(45, 89.7, 180, affineAbout(512, 512, [4]float64{-1e-3, 0, 0, 1e-3})), 1024, 1024, nil},
		{"a field across longitude 0",
			gnomonic(359.9, 5, 180, affineAbout(500, 500, [4]float64{-1e-3, 0, 0, -1e-3})), 1000, 1000,
			func(t *testing.T, lon, lat float64) { assert.True(t, lon < 0.5 || lon > 359.3, "lon %v", lon) }},
		{"a wide field",
			gnomonic(200, 10, 180, affineAbout(500, 500, [4]float64{-0.05, 0.01, 0.0, 0.05})), 1000, 1000, nil},
		{"arcsecond pixels",
			gnomonic(10, -45, 180, affineAbout(2000, 2000, [4]float64{-1.0 / 3600, 0, 0, -1.0 / 3600})), 4000, 4000, nil},
	}
	for _, c := range cases {
		require.NoError(t, c.p.Validate(), c.name)
		const steps = 17
		for i := range steps + 1 {
			for j := range steps + 1 {
				col, row := c.w*float64(i)/steps+0.25, c.h*float64(j)/steps-0.5
				lon, lat, ok := c.p.World(col, row)
				require.True(t, ok, c.name)
				require.True(t, lon >= 0 && lon < 360, "%s: lon %v", c.name, lon)
				if c.where != nil {
					c.where(t, lon, lat)
				}
				gc, gr, ok := c.p.Pixel(lon, lat)
				require.True(t, ok, c.name)
				assert.InDelta(t, col, gc, 1e-9, "%s: col at (%v, %v)", c.name, col, row)
				assert.InDelta(t, row, gr, 1e-9, "%s: row at (%v, %v)", c.name, col, row)
				// A longitude a turn away is the same position.
				gc, gr, ok = c.p.Pixel(lon-360, lat)
				require.True(t, ok)
				assert.InDelta(t, col, gc, 1e-8)
				assert.InDelta(t, row, gr, 1e-8)
			}
		}
	}
}

func TestALevelsPlacementFollowsItsFactor(t *testing.T) {
	places := []Placement{
		{Affine: [6]float64{7, 0.5, 0.1, -3, 0.05, -0.5}, Frame: Frame{Kind: FrameKindPhysical, Units: [2]string{"um", "um"}}, Unapplied: []string{"u"}},
		gnomonic(80, 20, 180, affineAbout(300, 200, [4]float64{-1e-3, 2e-4, 1e-4, 1e-3})),
	}
	for _, p := range places {
		for _, k := range []int{-2, 0, 1, 3, 5} {
			lv := p.Level(k)
			require.NoError(t, lv.Validate())
			assert.Equal(t, p.Frame, lv.Frame)
			f := math.Ldexp(1, k)
			for _, cell := range [][2]float64{{0, 0}, {1, 1}, {3.5, 2.25}, {10, 7}} {
				a, b, ok := lv.World(cell[0], cell[1])
				require.True(t, ok)
				wa, wb, _ := p.World(cell[0]*f, cell[1]*f)
				if p.Frame.Kind == FrameKindCelestialGnomonic {
					assert.InDelta(t, 0, lonDiff(a, wa), 1e-12)
				} else {
					assert.InDelta(t, wa, a, 1e-12)
				}
				assert.InDelta(t, wb, b, 1e-12)
			}
			sx, sy := lv.PixelSize()
			px, py := p.PixelSize()
			assert.InDelta(t, px*f, sx, 1e-15)
			assert.InDelta(t, py*f, sy, 1e-15)
		}
		assert.Equal(t, p.Affine, p.Level(0).Affine)
	}
	lv := places[0].Level(2)
	lv.Unapplied[0] = "changed"
	assert.Equal(t, "u", places[0].Unapplied[0], "a level's placement is a copy")
	assert.True(t, Placement{}.Level(3).IsZero())
}
