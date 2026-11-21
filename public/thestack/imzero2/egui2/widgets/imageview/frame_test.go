package imageview

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// skyPlacement is a gnomonic placement of 1″ pixels about RA 150°, Dec 2°.
func skyPlacement() imaging.Placement {
	const s = 1.0 / 3600
	return imaging.Placement{
		Affine: [6]float64{50 * s, -s, 0, 40 * s, 0, -s},
		Frame: imaging.Frame{Kind: imaging.FrameKindCelestialGnomonic, Code: "ICRS",
			AxisNames: [2]string{"RA", "Dec"}, Units: [2]string{"deg", "deg"},
			RefLon: 150, RefLat: 2, LonPole: imaging.DefaultLonPole(2)},
		Source: "a test",
	}
}

// A position is the placement's World at the continuous pixel coordinates,
// written to a tenth of a pixel and never fewer than two decimals.
func TestAFramePositionIsThePlacementsWorld(t *testing.T) {
	p := samples.BandsPlacement
	pos, ok := FramePositionAt(p, 80.5, 60.25)
	require.True(t, ok)
	a, b, _ := p.World(80.5, 60.25)
	assert.Equal(t, [2]float64{a, b}, [2]float64{pos.A, pos.B})
	assert.Equal(t, "E 500805.00 m  N 5199397.50 m  (EPSG:32632)", pos.Text(), "10 m pixels: two decimals at least")

	sky := skyPlacement()
	pos, ok = FramePositionAt(sky, 50, 40)
	require.True(t, ok)
	assert.InDelta(t, 150, pos.A, 1e-9, "the reference pixel is the reference point")
	assert.InDelta(t, 2, pos.B, 1e-9)
	assert.Equal(t, "RA 150.00000°  Dec 2.00000°  (ICRS)", pos.Text(), "1″ pixels: a tenth of a pixel is five decimals of a degree")
	a, b, _ = sky.World(12.25, 70.5)
	pos, _ = FramePositionAt(sky, 12.25, 70.5)
	assert.Equal(t, [2]float64{a, b}, [2]float64{pos.A, pos.B})

	_, ok = FramePositionAt(imaging.Placement{}, 1, 1)
	assert.False(t, ok, "nothing is placed")

	phys := imaging.Placement{Affine: [6]float64{0, 0.65, 0, 0, 0, 0.65},
		Frame: imaging.Frame{Kind: imaging.FrameKindPhysical, Units: [2]string{"um", "um"}}}
	pos, _ = FramePositionAt(phys, 10, 20)
	assert.Equal(t, "x 6.50 um  y 13.00 um", pos.Text(), "a frame that names no axes calls them x and y")
}

func TestDescribePlacementNamesTheFrameThePixelAndTheSource(t *testing.T) {
	assert.Equal(t, "placed in EPSG:32632 (E, N in m; coordinates as the system defines them, not converted) · pixel 10 × 10 m · from synthetic",
		DescribePlacement(samples.BandsPlacement))
	sky := DescribePlacement(skyPlacement())
	assert.Contains(t, sky, "gnomonic (TAN)")
	assert.Contains(t, sky, "RA 150.000000°, Dec 2.000000° (ICRS)")
	assert.Contains(t, sky, "decimal degrees")
	assert.Contains(t, sky, "pixel 1 × 1″")
	assert.Contains(t, sky, "from a test")
	assert.Equal(t, "not placed", DescribePlacement(imaging.Placement{}))
}

// The readout line carries the frame coordinates of the pointer for an
// image with a placement, and nothing more for one without; a host can
// leave them out.
func TestTheReadoutCarriesTheFrameCoordinates(t *testing.T) {
	st, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	img, err := st.Image([]int{0, 0}, []int{3})
	require.NoError(t, err)
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(img)

	ll := portolan.LL(-60.25, 80.5)
	v.hover, v.hoverOk = v.readoutAt(ll)
	v.hoverFrame, v.hoverFrameOk = FramePositionAt(v.SourceInfo().Placement, ll.Lng, -ll.Lat)
	text := v.readoutText()
	assert.Contains(t, text, "x 80  y 60   E 500805.00 m  N 5199397.50 m  (EPSG:32632)   B8 ")
	pos, ok := v.HoverFrame()
	require.True(t, ok)
	assert.Equal(t, 80.5, pos.X)
	v.Opts.HideFrameCoordinates = true
	assert.NotContains(t, v.readoutText(), "EPSG")

	v.Opts.HideFrameCoordinates = false

	v.SetImage(grayImage(t, 200, 100))
	v.hover, v.hoverOk = v.readoutAt(ll)
	v.hoverFrame, v.hoverFrameOk = FramePositionAt(v.SourceInfo().Placement, ll.Lng, -ll.Lat)
	assert.False(t, v.hoverFrameOk, "an image without a placement has no frame")
	assert.Contains(t, v.readoutText(), "x 80  y 60   gray ")
}

// An image given as an ImageSource replaces the one shown and keeps the
// view, and is still an image in memory: levels, histogram and all.
func TestAnImageSourceKeepsTheView(t *testing.T) {
	v, frame := viewerWith(t, Options{}, 1000, 600)
	frame()
	v.SetView(100, 50, 4)
	frame()
	second := grayImage(t, 1000, 600)
	require.NoError(t, v.SetSource(ImageSource(second), true))
	frame()
	x, y, mag, ok := v.View()
	require.True(t, ok)
	assert.InDelta(t, 100, x, 1e-6)
	assert.InDelta(t, 50, y, 1e-6)
	assert.InDelta(t, 4, mag, 1e-9)
	assert.Equal(t, second.Hash(), v.Image().Hash(), "shown as an image in memory")
	assert.True(t, v.histogramAvailable())

	require.NoError(t, v.SetSource(ImageSource(grayImage(t, 1000, 600)), false))
	frame()
	_, _, mag, _ = v.View()
	assert.InDelta(t, 0.4, mag, 1e-9, "without keepView it fits")
	assert.Error(t, v.SetSource(ImageSource(imaging.Image{}), true), "the zero image has no size")
}

// The readout gives a GeoTIFF's and a FITS file's frame coordinates as their
// placements' World does, read through the readers.
func TestTheReadoutOfAReadersPlacement(t *testing.T) {
	for _, tc := range []struct{ reader, name, code string }{
		{"tiff", "geo_area.tif", "EPSG:2056"},
		{"fits", "tan_cd.fits", ""},
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "science", "imaging", "decode", tc.reader, "testdata", tc.name))
		require.NoError(t, err)
		st, err := decode.DecodeStack(context.Background(), bytes.NewReader(b), int64(len(b)), tc.name, decode.Options{})
		require.NoError(t, err)
		var img imaging.Image
		if st.Model() != 0 {
			img, _, err = st.AsStored(make([]int, len(st.Axes())))
		} else {
			img, err = st.Image(make([]int, len(st.Axes())), nil)
		}
		require.NoError(t, err)
		v := New(c.NewWidgetIdStack(), "", Options{})
		v.SetImage(img)
		ll := portolan.LL(-2.75, 3.25)
		v.hover, v.hoverOk = v.readoutAt(ll)
		v.hoverFrame, v.hoverFrameOk = FramePositionAt(v.SourceInfo().Placement, ll.Lng, -ll.Lat)
		require.True(t, v.hoverFrameOk, tc.name)
		p := st.Placement()
		a, bb, ok := p.World(3.25, 2.75)
		require.True(t, ok, tc.name)
		assert.Equal(t, [2]float64{a, bb}, [2]float64{v.hoverFrame.A, v.hoverFrame.B}, tc.name)
		assert.Contains(t, v.readoutText(), v.hoverFrame.Text(), tc.name)
		if tc.code != "" {
			assert.Contains(t, v.readoutText(), "("+tc.code+")", tc.name)
		}
		if p.Frame.Kind == imaging.FrameKindCelestialGnomonic {
			assert.Contains(t, v.hoverFrame.Text(), "°", tc.name)
		}
		v.Close()
	}
}
