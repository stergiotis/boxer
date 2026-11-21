package tiff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// geoKeyTag is a GeoKeyDirectory of inline SHORT keys, given as id, value
// pairs in ascending id order.
func geoKeyTag(pairs ...uint64) wtag {
	v := []uint64{1, 1, 0, uint64(len(pairs) / 2)}
	for i := 0; i < len(pairs); i += 2 {
		v = append(v, pairs[i], 0, 1, pairs[i+1])
	}
	return shortTag(tagGeoKeyDirectory, v...)
}

func placed(t *testing.T, tags ...wtag) (imaging.Placement, []string) {
	t.Helper()
	m := grayU8()
	m.tags = tags
	st, err := decodeBytes(t, wfile{images: []wimage{m}}.bytes(t), Options{})
	require.NoError(t, err)
	return st.Placement(), st.Provenance().Notes
}

// TestPlacement_ClosedForm holds the GeoTIFF affine to its closed form:
// x = X − I·Sx + col·Sx, y = Y + J·Sy − row·Sy for PixelIsArea, and the same
// at (col − ½, row − ½) for PixelIsPoint.
func TestPlacement_ClosedForm(t *testing.T) {
	const sx, sy, ti, tj, tx, ty = 2.0, 3.0, 10.0, 20.0, 1000.0, 5000.0
	scale := doubleTag(tagModelPixelScale, sx, sy, 0)
	tie := doubleTag(tagModelTiepoint, ti, tj, 0, tx, ty, 0)
	area, _ := placed(t, scale, tie, geoKeyTag(keyModelType, 1, keyRasterType, 1, keyProjectedCRS, 32632, keyProjLinearUnits, 9001))
	assert.Equal(t, [6]float64{tx - ti*sx, sx, 0, ty + tj*sy, 0, -sy}, area.Affine)
	assert.Equal(t, "EPSG:32632", area.Frame.Code)
	assert.Equal(t, [2]string{"m", "m"}, area.Frame.Units)
	assert.Equal(t, "GeoTIFF ModelPixelScale, ModelTiepoint, PixelIsArea, ProjectedCRS", area.Source)
	assert.Empty(t, area.Unapplied)
	// The tiepoint's raster coordinate is the corner of pixel (10, 20).
	x, y, ok := area.World(ti, tj)
	require.True(t, ok)
	assert.Equal(t, tx, x)
	assert.Equal(t, ty, y)

	point, _ := placed(t, scale, tie, geoKeyTag(keyModelType, 1, keyRasterType, 2, keyProjectedCRS, 32632))
	assert.Equal(t, [6]float64{tx - ti*sx - sx/2, sx, 0, ty + tj*sy + sy/2, 0, -sy}, point.Affine)
	assert.Contains(t, point.Source, "PixelIsPoint")
	assert.Equal(t, [2]string{}, point.Frame.Units)
	// For PixelIsPoint the tiepoint's raster coordinate is the centre of
	// pixel (10, 20).
	x, y, ok = point.World(ti+0.5, tj+0.5)
	require.True(t, ok)
	assert.Equal(t, tx, x)
	assert.Equal(t, ty, y)

	// The twin: a point-registered tiepoint at the centre of the area
	// raster's pixel places every pixel where the area raster does.
	twin, _ := placed(t, scale, doubleTag(tagModelTiepoint, ti, tj, 0, tx+sx/2, ty-sy/2, 0),
		geoKeyTag(keyModelType, 1, keyRasterType, 2, keyProjectedCRS, 32632, keyProjLinearUnits, 9001))
	assert.Equal(t, area.Affine, twin.Affine)

	absent, _ := placed(t, scale, tie, geoKeyTag(keyModelType, 1, keyProjectedCRS, 2056))
	assert.Equal(t, area.Affine, absent.Affine)
	assert.Contains(t, absent.Source, "GTRasterType absent (read as PixelIsArea)")
}

func TestPlacement_Transformation(t *testing.T) {
	m := []float64{0.5, 0.25, 0, 100, -0.125, -0.5, 0, 200, 0, 0, 0, 0, 0, 0, 0, 1}
	p, notes := placed(t, doubleTag(tagModelTransform, m...), geoKeyTag(keyModelType, 2, keyRasterType, 1, keyGeodeticCRS, 4326, keyGeogAngularUnits, 9102))
	assert.Equal(t, [6]float64{100, 0.5, 0.25, 200, -0.125, -0.5}, p.Affine)
	assert.Equal(t, "EPSG:4326", p.Frame.Code)
	assert.Equal(t, [2]string{"lon", "lat"}, p.Frame.AxisNames)
	assert.Equal(t, [2]string{"deg", "deg"}, p.Frame.Units)
	assert.Empty(t, notes)

	pp, _ := placed(t, doubleTag(tagModelTransform, m...), geoKeyTag(keyModelType, 2, keyRasterType, 2, keyGeodeticCRS, 4326))
	// Half a column and half a row back along the rotated axes.
	assert.Equal(t, [6]float64{100 - (0.5+0.25)/2, 0.5, 0.25, 200 - (-0.125-0.5)/2, -0.125, -0.5}, pp.Affine)

	both, notes := placed(t, doubleTag(tagModelTransform, m...), doubleTag(tagModelPixelScale, 1, 1, 0),
		geoKeyTag(keyModelType, 2, keyGeodeticCRS, 4326))
	assert.Equal(t, p.Affine, both.Affine)
	assert.Contains(t, strings.Join(notes, " "), "were not used")
}

// TestPlacement_NotAnAffine leaves the placement empty, with a note, where
// the file's statement is not an affine or cannot be evaluated.
func TestPlacement_NotAnAffine(t *testing.T) {
	keys := geoKeyTag(keyModelType, 1, keyProjectedCRS, 2056)
	scale := doubleTag(tagModelPixelScale, 1, 1, 0)
	cases := []struct {
		name string
		tags []wtag
		note string
	}{
		{"several tiepoints", []wtag{doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0, 5, 5, 0, 6, 7, 0), keys},
			"2 tiepoints without ModelTransformation describe a warp"},
		{"several tiepoints with a scale", []wtag{scale, doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0, 5, 5, 0, 6, 7, 0), keys},
			"2 tiepoints"},
		{"tiepoint without scale", []wtag{doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0), keys}, "needs ModelTiepoint and ModelPixelScale together"},
		{"tilted", []wtag{doubleTag(tagModelTransform, 1, 0, 0, 0, 0, -1, 0, 0, 0.1, 0, 1, 0, 0, 0, 0, 1), keys},
			"tilts the raster"},
		{"projective", []wtag{doubleTag(tagModelTransform, 1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1, 0, 0.01, 0, 0, 1), keys},
			"projective"},
		{"short transformation", []wtag{doubleTag(tagModelTransform, 1, 0, 0, 0), keys}, "does not hold 16 values"},
		{"zero scale", []wtag{doubleTag(tagModelPixelScale, 0, 1, 0), doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0), keys},
			"cannot be evaluated"},
		{"raster type 3", []wtag{scale, doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0), geoKeyTag(keyModelType, 1, keyRasterType, 3)},
			"GTRasterType 3"},
		{"geocentric", []wtag{scale, doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0), geoKeyTag(keyModelType, 3)}, "geocentric"},
		{"bad geokeys", []wtag{scale, doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0), shortTag(tagGeoKeyDirectory, 1, 1, 0, 9)},
			"GeoKeyDirectory cannot be read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, notes := placed(t, c.tags...)
			assert.True(t, p.IsZero())
			assert.Contains(t, strings.Join(notes, " "), c.note)
		})
	}
}

// TestPlacement_Declared records what the placement does not evaluate.
func TestPlacement_Declared(t *testing.T) {
	scale := doubleTag(tagModelPixelScale, 1, 1, 0)
	tie := doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0)
	p, _ := placed(t, scale, tie, geoKeyTag(keyModelType, 1, keyProjectedCRS, 32767, keyProjLinearUnits, 9001, keyVerticalCRS, 5728))
	assert.Equal(t, "user-defined", p.Frame.Code)
	assert.Len(t, p.Unapplied, 2)
	assert.Contains(t, p.Unapplied[0], "user-defined (32767)")
	assert.Contains(t, p.Unapplied[1], "VerticalCRS 5728")

	p, _ = placed(t, scale, tie)
	assert.Equal(t, imaging.FrameKindReferenceSystem, p.Frame.Kind)
	assert.Equal(t, "", p.Frame.Code)
	assert.Equal(t, []string{"no GeoKeyDirectory: the reference system is not stated"}, p.Unapplied)

	p, _ = placed(t, scale, tie, geoKeyTag(keyModelType, 1))
	assert.Equal(t, []string{"ProjectedCRS is not stated as an EPSG code"}, p.Unapplied)
}

// TestPlacement_Resolution is a physical scale for a plain TIFF.
func TestPlacement_Resolution(t *testing.T) {
	p, _ := placed(t, rationalTag(tagXResolution, 300), rationalTag(tagYResolution, 150), shortTag(tagResolutionUnit, 2))
	assert.Equal(t, imaging.FrameKindPhysical, p.Frame.Kind)
	assert.Equal(t, [2]string{"in", "in"}, p.Frame.Units)
	assert.Equal(t, [6]float64{0, 1.0 / 300, 0, 0, 0, 1.0 / 150}, p.Affine)

	p, _ = placed(t, rationalTag(tagXResolution, 300), rationalTag(tagYResolution, 300))
	assert.Equal(t, [2]string{"in", "in"}, p.Frame.Units)
	assert.Contains(t, p.Source, "inch by TIFF 6.0")

	p, _ = placed(t, rationalTag(tagXResolution, 300), rationalTag(tagYResolution, 300), shortTag(tagResolutionUnit, 1))
	assert.True(t, p.IsZero())

	p, notes := placed(t, rationalTag(tagXResolution, 0), rationalTag(tagYResolution, 300), shortTag(tagResolutionUnit, 3))
	assert.True(t, p.IsZero())
	assert.Contains(t, strings.Join(notes, " "), "not a positive number")

	// GeoTIFF wins over a resolution.
	p, _ = placed(t, rationalTag(tagXResolution, 300), rationalTag(tagYResolution, 300),
		doubleTag(tagModelPixelScale, 1, 1, 0), doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0), geoKeyTag(keyModelType, 1, keyProjectedCRS, 2056))
	assert.Equal(t, imaging.FrameKindReferenceSystem, p.Frame.Kind)
}
