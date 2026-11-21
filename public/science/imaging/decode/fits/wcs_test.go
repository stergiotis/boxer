package fits

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// Closed-form expectations for each way the header can state the linear
// transformation (Standard §8.1, eq. (11)): at FITS pixel (p1, p2), the
// intermediate world coordinate is x_i = Σ_j CD_ij (p_j − r_j); the
// engine's pixel is (p1 − 0.5, NAXIS2 + 0.5 − p2).

const n1, n2 = 20, 10

func celestial(cards ...kv) testHDU {
	return rampHDU(-32, []int64{n1, n2}, cards...)
}

func placementOf(t *testing.T, h testHDU) (imaging.Placement, []string) {
	t.Helper()
	s, err := decodeBytes(writeFITS(h), Options{})
	require.NoError(t, err)
	return s.Placement(), s.Provenance().Notes
}

// holdsCD checks the placement's affine against a CD matrix about crpix,
// with rows (lon, lat) taken from world axes rows[0], rows[1] and scaled
// by f, plus offs.
func holdsCD(t *testing.T, p imaging.Placement, cd [2][2]float64, crpix [2]float64, rows [2]int, f float64, offs [2]float64, name string) {
	t.Helper()
	for _, pix := range [][2]float64{{1, 1}, {n1, 1}, {1, n2}, {3.25, 7.5}, {0.5, 0.5}, {n1 + 0.5, n2 + 0.5}} {
		x, y := p.Intermediate(pix[0]-0.5, n2+0.5-pix[1])
		var want [2]float64
		for r, i := range rows {
			want[r] = offs[r] + f*(cd[i][0]*(pix[0]-crpix[0])+cd[i][1]*(pix[1]-crpix[1]))
		}
		assert.InDelta(t, want[0], x, 1e-12, "%s x at %v", name, pix)
		assert.InDelta(t, want[1], y, 1e-12, "%s y at %v", name, pix)
	}
}

func TestEachLinearFormBuildsTheAffine(t *testing.T) {
	tan := []kv{{"CTYPE1", quote("RA---TAN")}, {"CTYPE2", quote("DEC--TAN")}, {"CRVAL1", "150.25"}, {"CRVAL2", "-2.5"},
		{"CRPIX1", "10.5"}, {"CRPIX2", "4.25"}}
	crpix := [2]float64{10.5, 4.25}
	cd := [2][2]float64{{-2.8e-4, 1.1e-4}, {0.9e-4, 3.0e-4}}

	p, notes := placementOf(t, celestial(append(tan, kv{"CD1_1", num(cd[0][0])}, kv{"CD1_2", num(cd[0][1])},
		kv{"CD2_1", num(cd[1][0])}, kv{"CD2_2", num(cd[1][1])}, kv{"CDELT1", "5"}, kv{"CROTA2", "45"})...))
	require.False(t, p.IsZero(), "%v", notes)
	assert.Empty(t, notes, "CDELT and CROTA2 beside CD are ignored silently, as the standard says")
	assert.True(t, strings.HasPrefix(p.Source, "CDi_j"))
	holdsCD(t, p, cd, crpix, [2]int{0, 1}, 1, [2]float64{}, "CD")
	f := p.Frame
	assert.Equal(t, imaging.FrameKindCelestialGnomonic, f.Kind)
	assert.Equal(t, 150.25, f.RefLon)
	assert.Equal(t, -2.5, f.RefLat)
	assert.Equal(t, 180.0, f.LonPole)
	assert.Equal(t, "ICRS", f.Code)
	assert.Equal(t, [2]string{"RA", "Dec"}, f.AxisNames)
	assert.Equal(t, [2]string{"deg", "deg"}, f.Units)
	lon, lat, ok := p.World(10.5-0.5, n2+0.5-4.25)
	require.True(t, ok)
	assert.InDelta(t, 150.25, lon, 1e-12, "the reference pixel is the reference point")
	assert.InDelta(t, -2.5, lat, 1e-12)

	pc := [2][2]float64{{0.9, 0.3}, {-0.2, 1.1}}
	cdelt := [2]float64{-1e-3, 2e-3}
	p, notes = placementOf(t, celestial(append(tan, kv{"CDELT1", num(cdelt[0])}, kv{"CDELT2", num(cdelt[1])},
		kv{"PC1_1", num(pc[0][0])}, kv{"PC1_2", num(pc[0][1])}, kv{"PC2_1", num(pc[1][0])}, kv{"PC2_2", num(pc[1][1])},
		kv{"RADESYS", quote("FK5")}, kv{"EQUINOX", "2000."})...))
	require.False(t, p.IsZero(), "%v", notes)
	assert.True(t, strings.HasPrefix(p.Source, "PCi_j with CDELTi"))
	holdsCD(t, p, [2][2]float64{{cdelt[0] * pc[0][0], cdelt[0] * pc[0][1]}, {cdelt[1] * pc[1][0], cdelt[1] * pc[1][1]}}, crpix, [2]int{0, 1}, 1, [2]float64{}, "PC")
	assert.Equal(t, "FK5 J2000", p.Frame.Code)

	rho := 30.0
	s, c := math.Sincos(rho * math.Pi / 180)
	p, notes = placementOf(t, celestial(append(tan, kv{"CDELT1", num(cdelt[0])}, kv{"CDELT2", num(cdelt[1])}, kv{"CROTA2", num(rho)}, kv{"EQUINOX", "1950"})...))
	require.False(t, p.IsZero(), "%v", notes)
	assert.True(t, strings.HasPrefix(p.Source, "CROTA2 with CDELTi"))
	holdsCD(t, p, [2][2]float64{{cdelt[0] * c, -cdelt[1] * s}, {cdelt[0] * s, cdelt[1] * c}}, crpix, [2]int{0, 1}, 1, [2]float64{}, "CROTA2")
	assert.Equal(t, "FK4 B1950", p.Frame.Code)

	p, _ = placementOf(t, celestial(append(tan, kv{"CDELT1", num(cdelt[0])}, kv{"CDELT2", num(cdelt[1])})...))
	assert.True(t, strings.HasPrefix(p.Source, "CDELTi"))
	holdsCD(t, p, [2][2]float64{{cdelt[0], 0}, {0, cdelt[1]}}, crpix, [2]int{0, 1}, 1, [2]float64{}, "CDELT")
}

func TestLatitudeFirstPutsLongitudeInX(t *testing.T) {
	cd := [2][2]float64{{1e-3, 2e-4}, {-3e-4, -1e-3}}
	p, notes := placementOf(t, celestial(kv{"CTYPE1", quote("DEC--TAN")}, kv{"CTYPE2", quote("RA---TAN")},
		kv{"CRVAL1", "-35.5"}, kv{"CRVAL2", "280.25"}, kv{"CRPIX1", "3"}, kv{"CRPIX2", "12"},
		kv{"CD1_1", num(cd[0][0])}, kv{"CD1_2", num(cd[0][1])}, kv{"CD2_1", num(cd[1][0])}, kv{"CD2_2", num(cd[1][1])}))
	require.False(t, p.IsZero(), "%v", notes)
	holdsCD(t, p, cd, [2]float64{3, 12}, [2]int{1, 0}, 1, [2]float64{}, "latitude first")
	assert.Equal(t, 280.25, p.Frame.RefLon)
	assert.Equal(t, -35.5, p.Frame.RefLat)
}

func TestCelestialUnitsAreConvertedToDegrees(t *testing.T) {
	p, notes := placementOf(t, celestial(kv{"CTYPE1", quote("GLON-TAN")}, kv{"CTYPE2", quote("GLAT-TAN")},
		kv{"CUNIT1", quote("arcsec")}, kv{"CUNIT2", quote("arcmin")}, kv{"CRVAL1", "36000"}, kv{"CRVAL2", "-60"},
		kv{"CRPIX1", "1"}, kv{"CRPIX2", "1"}, kv{"CDELT1", "-3.6"}, kv{"CDELT2", "0.06"}))
	require.False(t, p.IsZero(), "%v", notes)
	assert.Equal(t, 10.0, p.Frame.RefLon)
	assert.Equal(t, -1.0, p.Frame.RefLat)
	assert.Equal(t, "GALACTIC", p.Frame.Code)
	assert.Equal(t, [2]string{"GLON", "GLAT"}, p.Frame.AxisNames)
	x, y := p.Intermediate(1.5, n2+0.5-2)
	assert.InDelta(t, -1e-3, x, 1e-15)
	assert.InDelta(t, 1e-3, y, 1e-15)
	assert.Contains(t, notes, "CUNIT1 'arcsec' was converted to degrees")
	assert.Contains(t, notes, "CUNIT2 'arcmin' was converted to degrees")
}

func TestPoleAndCodes(t *testing.T) {
	base := func(extra ...kv) testHDU {
		return celestial(append([]kv{{"CTYPE1", quote("RA---TAN")}, {"CTYPE2", quote("DEC--TAN")}, {"CDELT1", "-0.01"}, {"CDELT2", "0.01"}}, extra...)...)
	}
	p, _ := placementOf(t, base(kv{"CRVAL2", "90"}))
	assert.Equal(t, 0.0, p.Frame.LonPole, "the default at the pole")
	p, _ = placementOf(t, base(kv{"LONPOLE", "170"}))
	assert.Equal(t, 170.0, p.Frame.LonPole)
	p, notes := placementOf(t, base(kv{"PV1_1", "0"}, kv{"PV1_2", "90"}, kv{"PV1_3", "165"}))
	require.False(t, p.IsZero(), "%v", notes)
	assert.Equal(t, 165.0, p.Frame.LonPole, "PV1_3 is LONPOLE")
	p, _ = placementOf(t, base(kv{"RADESYS", quote("ICRS")}, kv{"EQUINOX", "2000"}))
	assert.Equal(t, "ICRS", p.Frame.Code)
	p, _ = placementOf(t, celestial(kv{"CTYPE1", quote("ELON-TAN")}, kv{"CTYPE2", quote("ELAT-TAN")}, kv{"CDELT1", "1E-3"}, kv{"CDELT2", "1E-3"}))
	assert.Equal(t, "ECLIPTIC ICRS", p.Frame.Code)
}

func TestLinearAxesArePhysical(t *testing.T) {
	p, notes := placementOf(t, celestial(kv{"CTYPE1", quote("X")}, kv{"CTYPE2", quote("Y")}, kv{"CUNIT1", quote("mm")}, kv{"CUNIT2", quote("mm")},
		kv{"CRVAL1", "100"}, kv{"CRVAL2", "-40"}, kv{"CRPIX1", "1"}, kv{"CRPIX2", "1"}, kv{"CDELT1", "0.5"}, kv{"CDELT2", "0.25"}))
	require.False(t, p.IsZero(), "%v", notes)
	assert.Equal(t, imaging.FrameKindPhysical, p.Frame.Kind)
	assert.Equal(t, [2]string{"mm", "mm"}, p.Frame.Units)
	assert.Equal(t, [2]string{"X", "Y"}, p.Frame.AxisNames)
	holdsCD(t, p, [2][2]float64{{0.5, 0}, {0, 0.25}}, [2]float64{1, 1}, [2]int{0, 1}, 1, [2]float64{100, -40}, "linear")
	a, b, ok := p.World(0.5, n2-0.5)
	require.True(t, ok)
	assert.Equal(t, 100.0, a, "FITS pixel (1, 1) is the reference")
	assert.Equal(t, -40.0, b)

	p, notes = placementOf(t, celestial(kv{"CDELT1", "2"}, kv{"CDELT2", "3"}))
	require.False(t, p.IsZero(), "%v", notes)
	assert.Equal(t, [2]string{"axis1", "axis2"}, p.Frame.AxisNames)

	p, notes = placementOf(t, celestial())
	assert.True(t, p.IsZero())
	assert.Empty(t, notes, "no world coordinate keywords: nothing to say")
}

func TestWhatIsNotEvaluatedLeavesThePlacementZeroAndSaysSo(t *testing.T) {
	tan := func(c1, c2 string, extra ...kv) testHDU {
		return celestial(append([]kv{{"CTYPE1", quote(c1)}, {"CTYPE2", quote(c2)}, {"CRVAL1", "10"}, {"CRVAL2", "20"},
			{"CDELT1", "-1E-3"}, {"CDELT2", "1E-3"}}, extra...)...)
	}
	cases := []struct {
		name string
		h    testHDU
		says string
	}{
		{"SIN", tan("RA---SIN", "DEC--SIN"), "the SIN projection"},
		{"ZEA", tan("RA---ZEA", "DEC--ZEA"), "the ZEA projection"},
		{"CAR", tan("GLON-CAR", "GLAT-CAR"), "the CAR projection"},
		{"SIP", tan("RA---TAN-SIP", "DEC--TAN-SIP"), "SIP distortion"},
		{"TPV", tan("RA---TPV", "DEC--TPV"), "TPV distortion convention"},
		{"PV on latitude", tan("RA---TAN", "DEC--TAN", kv{"PV2_1", "0.01"}), "PV2_1 = 0.01 alters the TAN projection"},
		{"PV phi0", tan("RA---TAN", "DEC--TAN", kv{"PV1_1", "5"}), "PV1_1 = 5 alters"},
		{"A_ORDER", tan("RA---TAN", "DEC--TAN", kv{"A_ORDER", "2"}), "A_ORDER declares a distortion"},
		{"no projection code", tan("RA", "DEC"), "declares no projection code"},
		{"unpaired", tan("RA---TAN", "FREQ"), "are not a longitude–latitude pair"},
		{"two longitudes", tan("RA---TAN", "GLON-TAN"), "are not a longitude–latitude pair"},
		{"unknown unit", tan("RA---TAN", "DEC--TAN", kv{"CUNIT1", quote("furlong")}), "not an angle unit"},
		{"non-linear linear axis", tan("FREQ-LOG", "Y"), "non-linear algorithm LOG"},
		{"CROTA on linear", tan("X", "Y", kv{"CROTA2", "10"}), "rotate axes other"},
		{"CROTA latitude first", tan("DEC--TAN", "RA---TAN", kv{"CROTA1", "10"}), "rotate axes other"},
		{"no scale", celestial(kv{"CRPIX1", "3"}, kv{"CRVAL1", "3"}), "no scale"},
		{"PV on linear", tan("X", "Y", kv{"PV1_1", "1"}), "gives a parameter to a linear axis"},
		{"singular", tan("RA---TAN", "DEC--TAN", kv{"CD1_1", "1"}, kv{"CD1_2", "1"}, kv{"CD2_1", "1"}, kv{"CD2_2", "1"}), "cannot be evaluated"},
	}
	for _, c := range cases {
		p, notes := placementOf(t, c.h)
		assert.True(t, p.IsZero(), c.name)
		assert.Equal(t, imaging.Placement{}, p, c.name)
		assert.Contains(t, strings.Join(notes, "\n"), c.says, c.name)
	}

	// A celestial pair coupled to a third axis.
	h := rampHDU(-32, []int64{4, 4, 3}, kv{"CTYPE1", quote("RA---TAN")}, kv{"CTYPE2", quote("DEC--TAN")},
		kv{"PC1_3", "0.5"}, kv{"CDELT1", "-1E-3"}, kv{"CDELT2", "1E-3"})
	p, notes := placementOf(t, h)
	assert.True(t, p.IsZero())
	assert.Contains(t, strings.Join(notes, "\n"), "couples axis 1 with axis 3")
	// A celestial axis beyond the image's two.
	h = rampHDU(-32, []int64{4, 4, 3}, kv{"CTYPE1", quote("X")}, kv{"CTYPE2", quote("Y")}, kv{"CTYPE3", quote("RA---TAN")}, kv{"CDELT1", "1"})
	p, notes = placementOf(t, h)
	assert.True(t, p.IsZero())
	assert.Contains(t, strings.Join(notes, "\n"), "outside the image's two axes")
}

func TestPrecedenceConflictsAreNoted(t *testing.T) {
	base := []kv{{"CTYPE1", quote("RA---TAN")}, {"CTYPE2", quote("DEC--TAN")}}
	p, notes := placementOf(t, celestial(append(base, kv{"CD1_1", "-1E-3"}, kv{"CD2_2", "1E-3"}, kv{"PC1_1", "1"})...))
	require.False(t, p.IsZero())
	assert.Equal(t, -1e-3, p.Affine[1])
	assert.Contains(t, notes, "the header gives both CDi_j and PCi_j, which the standard forbids; CDi_j was used")
	p, notes = placementOf(t, celestial(append(base, kv{"CDELT1", "-1E-3"}, kv{"CDELT2", "1E-3"}, kv{"PC1_1", "1"}, kv{"CROTA2", "20"})...))
	require.False(t, p.IsZero())
	assert.Equal(t, -1e-3, p.Affine[1])
	assert.Contains(t, notes, "CROTAi was ignored: the header gives PCi_j, with which it must not appear")
}

func TestAlternateDescriptionsAreNotedNotRead(t *testing.T) {
	p, notes := placementOf(t, celestial(kv{"CTYPE1", quote("RA---TAN")}, kv{"CTYPE2", quote("DEC--TAN")}, kv{"CDELT1", "-1E-3"}, kv{"CDELT2", "1E-3"},
		kv{"CTYPE1B", quote("X")}, kv{"CRPIX1A", "1"}, kv{"WCSNAMEA", quote("detector")}, kv{"PC1_2B", "0"}))
	require.False(t, p.IsZero())
	assert.Equal(t, []string{"alternate description A", "alternate description B"}, p.Unapplied)
	assert.Contains(t, notes, "alternate world coordinate descriptions A, B are declared and not read")

	for _, kw := range []string{"PCOUNT", "CTYPE1", "DATE-OBS", "NAXIS1", "CD1_1", "PC01_1A", "CTYPE0A"} {
		_, ok := altLetter(kw)
		assert.False(t, ok, kw)
	}
}

func TestSelectorCoordinatesNeedALinearUncoupledAxis(t *testing.T) {
	axes := []int64{2, 2, 3}
	s, err := decodeBytes(writeFITS(rampHDU(8, axes, kv{"CTYPE3", quote("WAVE")}, kv{"CRVAL3", "5E-7"}, kv{"CRPIX3", "1"},
		kv{"CD1_1", "1"}, kv{"CD2_2", "1"}, kv{"CD3_3", "1E-8"}, kv{"CUNIT3", quote("m")})), Options{})
	require.NoError(t, err)
	require.Len(t, s.Axes(), 1)
	assert.InDeltaSlice(t, []float64{5e-7, 5.1e-7, 5.2e-7}, s.Axes()[0].Coords, 1e-20)
	assert.Equal(t, "m", s.Axes()[0].Unit)

	s, err = decodeBytes(writeFITS(rampHDU(8, axes, kv{"CTYPE3", quote("WAVE-LOG")}, kv{"CRVAL3", "5E-7"}, kv{"CRPIX3", "1"}, kv{"CDELT3", "1E-8"})), Options{})
	require.NoError(t, err)
	assert.Nil(t, s.Axes()[0].Coords)
	assert.Contains(t, strings.Join(s.Provenance().Notes, "\n"), "non-linear algorithm LOG")

	s, err = decodeBytes(writeFITS(rampHDU(8, axes, kv{"CRVAL3", "5"}, kv{"CRPIX3", "1"}, kv{"CDELT3", "1"}, kv{"PC3_1", "0.5"})), Options{})
	require.NoError(t, err)
	assert.Nil(t, s.Axes()[0].Coords)
	assert.Contains(t, strings.Join(s.Provenance().Notes, "\n"), "coupled to another axis")
}
