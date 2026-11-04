package geodesy

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func TestWgs84Constants(t *testing.T) {
	assert.InDelta(t, 6_356_752.314_245, Wgs84SemiMinorM, 1e-6)
	assert.InDelta(t, 0.006_694_379_990_14, Wgs84EccentricitySq, 1e-14)
	assert.Less(t, Wgs84MinCurvatureRadiusM, Wgs84SemiMinorM)
	assert.Greater(t, Wgs84MaxCurvatureRadiusM, Wgs84SemiMajorM)
	// a degree of latitude at the equator is the meridian radius there
	// times π/180, and the bound sits just under it.
	assert.Less(t, MinMetresPerDegreeLat, Wgs84MinCurvatureRadiusM*degToRad)
	assert.Greater(t, MinMetresPerDegreeLat, 0.999*Wgs84MinCurvatureRadiusM*degToRad)
}

func TestGeodeticToEcefKnownPoints(t *testing.T) {
	const b = Wgs84SemiMinorM
	cases := []struct {
		name string
		g    Geodetic
		want Vec3
	}{
		{"equator prime meridian", Geodetic{}, Vec3{X: Wgs84SemiMajorM}},
		{"equator 90E", Geodetic{LonDeg: 90}, Vec3{Y: Wgs84SemiMajorM}},
		{"north pole", Geodetic{LatDeg: 90}, Vec3{Z: b}},
		{"south pole 1 km up", Geodetic{LatDeg: -90, HeightM: 1000}, Vec3{Z: -b - 1000}},
		{"equator 180 at 100 m", Geodetic{LonDeg: 180, HeightM: 100}, Vec3{X: -Wgs84SemiMajorM - 100}},
		// N(45°) = 6 388 838.290 m; X = N cos 45°, Z = N (1 − e²) sin 45°.
		{"45N on the prime meridian", Geodetic{LatDeg: 45}, Vec3{X: 4_517_590.879, Z: 4_487_348.409}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := GeodeticToEcef(c.g)
			assert.InDelta(t, 0, p.Sub(c.want).Norm(), 1e-2)
		})
	}
}

func TestEcefGeodeticRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := Geodetic{
			LatDeg:  rapid.Float64Range(-90, 90).Draw(t, "lat"),
			LonDeg:  rapid.Float64Range(-180, 180).Draw(t, "lon"),
			HeightM: rapid.Float64Range(-11_000, 2_000_000).Draw(t, "h"),
		}
		p := GeodeticToEcef(g)
		back := EcefToGeodetic(p)
		if d := GeodeticToEcef(back).Sub(p).Norm(); d > 1e-3 {
			t.Fatalf("round trip off by %g m", d)
		}
		if math.Abs(back.HeightM-g.HeightM) > 1e-3 {
			t.Fatalf("height %g, want %g", back.HeightM, g.HeightM)
		}
		if math.Abs(back.LatDeg-g.LatDeg) > 1e-9 {
			t.Fatalf("latitude %g, want %g", back.LatDeg, g.LatDeg)
		}
	})
}

func TestEcefToGeodeticAtThePoles(t *testing.T) {
	for _, h := range []float64{-100, 0, 12_000} {
		g := EcefToGeodetic(Vec3{Z: Wgs84SemiMinorM + h})
		assert.InDelta(t, 90, g.LatDeg, 1e-12)
		assert.InDelta(t, h, g.HeightM, 1e-6)
		g = EcefToGeodetic(Vec3{Z: -Wgs84SemiMinorM - h})
		assert.InDelta(t, -90, g.LatDeg, 1e-12)
		assert.InDelta(t, h, g.HeightM, 1e-6)
	}
}

func TestVec3Cross(t *testing.T) {
	x := Vec3{X: 1}
	y := Vec3{Y: 1}
	assert.Equal(t, Vec3{Z: 1}, x.Cross(y))
	assert.Equal(t, Vec3{Z: -1}, y.Cross(x))
	assert.Equal(t, Vec3{}, x.Cross(x))
}

func TestFrameRotationIsOrthonormal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		origin := Geodetic{
			LatDeg:  rapid.Float64Range(-89.9, 89.9).Draw(t, "lat"),
			LonDeg:  rapid.Float64Range(-180, 180).Draw(t, "lon"),
			HeightM: rapid.Float64Range(-500, 5000).Draw(t, "h"),
		}
		f := NewFrame(origin)
		for i := range 3 {
			for j := range 3 {
				d := 0.0
				for k := range 3 {
					d += f.rot[3*i+k] * f.rot[3*j+k]
				}
				want := 0.0
				if i == j {
					want = 1
				}
				if math.Abs(d-want) > 1e-14 {
					t.Fatalf("row %d·row %d = %g", i, j, d)
				}
			}
		}
		// right-handed: east × north = up, and up is the ellipsoid's normal
		if d := f.East().Cross(f.North()).Sub(f.Up()).Norm(); d > 1e-14 {
			t.Fatalf("frame is not right-handed: %g", d)
		}
		if d := LocalUp(origin).Sub(f.Up()).Norm(); d > 1e-15 {
			t.Fatalf("local up differs from the frame's by %g", d)
		}
		if d := f.OriginEcef().Sub(GeodeticToEcef(origin)).Norm(); d > 0 {
			t.Fatalf("origin ECEF differs by %g", d)
		}
		v := Vec3{
			X: rapid.Float64Range(-1e5, 1e5).Draw(t, "e"),
			Y: rapid.Float64Range(-1e5, 1e5).Draw(t, "n"),
			Z: rapid.Float64Range(-1e4, 1e4).Draw(t, "u"),
		}
		if d := f.EcefToEnu(f.EnuToEcef(v)).Sub(v).Norm(); d > 1e-6 {
			t.Fatalf("ENU round trip off by %g m", d)
		}
		if d := f.RotateEcefToEnu(f.RotateEnuToEcef(v)).Sub(v).Norm(); d > 1e-9 {
			t.Fatalf("rotation round trip off by %g", d)
		}
		// a point straight above the origin is straight up in the frame
		above := origin
		above.HeightM += 1000
		if d := f.EcefToEnu(GeodeticToEcef(above)).Sub(Vec3{Z: 1000}).Norm(); d > 1e-6 {
			t.Fatalf("local vertical off by %g m", d)
		}
		// the rotation at a point is the frame's there (ADR-0356 §SD3)
		if d := EnuToEcefAt(origin, v).Sub(f.RotateEnuToEcef(v)).Norm(); d > 0 {
			t.Fatalf("EnuToEcefAt differs from the frame's rotation by %g", d)
		}
		if d := EcefToEnuAt(origin, EnuToEcefAt(origin, v)).Sub(v).Norm(); d > 1e-9 {
			t.Fatalf("rotation at a point does not invert: %g", d)
		}
	})
}

func TestFrameAxesAtTheEquator(t *testing.T) {
	f := NewFrame(Geodetic{})
	assert.InDelta(t, 0, f.East().Sub(Vec3{Y: 1}).Norm(), 1e-15)
	assert.InDelta(t, 0, f.North().Sub(Vec3{Z: 1}).Norm(), 1e-15)
	assert.InDelta(t, 0, f.Up().Sub(Vec3{X: 1}).Norm(), 1e-15)
	assert.Equal(t, Geodetic{}, f.Origin())
}

func TestGroundVelocityEnu(t *testing.T) {
	v := GroundVelocityEnu(100, 90, 2)
	assert.InDelta(t, 100, v.X, 1e-12)
	assert.InDelta(t, 0, v.Y, 1e-12)
	assert.InDelta(t, 2, v.Z, 1e-12)
	v = GroundVelocityEnu(50, 180, 0)
	assert.InDelta(t, 0, v.X, 1e-12)
	assert.InDelta(t, -50, v.Y, 1e-12)
	// a northbound report at the equator is +Z in ECEF
	r := EnuToEcefAt(Geodetic{}, GroundVelocityEnu(10, 0, 0))
	assert.InDelta(t, 0, r.Sub(Vec3{Z: 10}).Norm(), 1e-12)
}
