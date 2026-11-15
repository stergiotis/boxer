package imageview

import (
	"context"
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// colourImage is w × h of 8-bit RGBA whose samples say where they are: red
// is the column, green the row, blue their sum, alpha opaque except where
// the test asks otherwise.
func colourImage(t *testing.T, w, h int, alpha func(col, row int) uint8) imaging.Image {
	t.Helper()
	var s [4][]uint8
	for i := range s {
		s[i] = make([]uint8, w*h)
	}
	for row := range h {
		for col := range w {
			i := row*w + col
			s[0][i], s[1][i], s[2][i], s[3][i] = uint8(col), uint8(row), uint8(col+row), 255
			if alpha != nil {
				s[3][i] = alpha(col, row)
			}
		}
	}
	planes := make([]imaging.Plane, 4)
	for i := range planes {
		var err error
		planes[i], err = imaging.NewPlaneU8(w, h, s[i])
		require.NoError(t, err)
	}
	img, err := imaging.NewImage(imaging.ColorModelRGBA, planes, imaging.Provenance{})
	require.NoError(t, err)
	return img
}

func grayImage(t *testing.T, w, h int) imaging.Image {
	t.Helper()
	s := make([]uint16, w*h)
	for i := range s {
		s[i] = uint16(i * 37)
	}
	p, err := imaging.NewPlaneU16(w, h, s)
	require.NoError(t, err)
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	require.NoError(t, err)
	return img
}

func tile(t *testing.T, v *Viewer, x, y int) []uint32 {
	t.Helper()
	px, w, h, err := v.src.TilePixels(context.Background(), portolan.TileCoords{X: x, Y: y})
	require.NoError(t, err)
	require.Equal(t, tileSize, w)
	require.Equal(t, tileSize, h)
	return px
}

func TestRendersWithoutAHostFromAFreshViewer(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()

	ev := v.Render(400, 300)
	assert.False(t, ev.HoverOk)
	assert.NoError(t, ev.Err)
	sm.ScriptReset()

	v.SetImage(colourImage(t, 100, 60, nil))
	for range 3 {
		ev = v.Render(400, 300)
		require.False(t, typed.HasErrors(), typed.GetError())
		assert.False(t, ev.HoverOk, "no pointer was scripted")
		sm.ScriptReset()
	}
	assert.InDelta(t, 4, v.Magnification(), 1e-9, "100 × 60 fits a 400 × 270 canvas four times over, bounded by its width")

	v.SetImage(imaging.Image{})
	v.Render(400, 300)
	require.False(t, typed.HasErrors(), typed.GetError())
}

func TestThePointerReadsTheStoredSamplesWhateverTheRecipe(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	v := New(ids, "probe", Options{HideReadout: true})
	defer v.Close()
	v.SetImage(colourImage(t, 100, 60, func(col, row int) uint8 { return uint8(200 - col) }))
	v.Render(400, 300) // the fit
	sm.ScriptReset()

	// The map's handles, derived under the viewer's own scope as its Render
	// derives them.
	var canvas, area widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("probe")) {
		canvas, area = v.m.Handles()
	}
	pointAt := func(col, row int, flags c.ResponseFlagsE) Events {
		at := v.m.View().LatLngToContainerPoint(portolan.LL(-(float64(row) + 0.5), float64(col)+0.5))
		nan := float32(math.NaN())
		sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: nan, PosY: nan})
		sm.ScriptPointer(c.PointerValue{X: float32(at.X), Y: float32(at.Y), Valid: true})
		sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
		sm.ScriptResponse(area, flags)
		ev := v.Render(400, 300)
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
		return ev
	}

	ev := pointAt(37, 11, c.ContainsPointerResponseFlags)
	require.True(t, ev.HoverOk)
	assert.Equal(t, Readout{Col: 37, Row: 11, Planes: 4, Stored: [4]float64{37, 11, 48, 163}, Valid: [4]bool{true, true, true, true}}, ev.Hover)
	assert.False(t, ev.ClickedOk)

	// A recipe that crushes everything to black changes what is drawn and
	// nothing about what is read.
	require.NoError(t, v.SetRecipe(display.Recipe{Limits: display.Limits{Lo: 250, Hi: 255}}))
	ev = pointAt(99, 59, c.ContainsPointerResponseFlags|c.PrimaryClickedResponseFlags)
	require.True(t, ev.HoverOk)
	require.True(t, ev.ClickedOk)
	assert.Equal(t, [4]float64{99, 59, 158, 101}, ev.Clicked.Stored)
	assert.Equal(t, ev.Hover, ev.Clicked)

	// One pixel past the last column is outside the image.
	ev = pointAt(100, 30, c.ContainsPointerResponseFlags)
	assert.False(t, ev.HoverOk)
}

func TestATileIsTheRecipeAppliedToTheSamples(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	img := grayImage(t, 600, 520)
	v.SetImage(img)
	p := img.Plane(0)

	direct := func(r display.Recipe, rect image.Rectangle) []uint32 {
		pal, ok := paletteFor(r.Palette)
		require.True(t, ok)
		m, err := display.CompilePlane(r, p.Kind(), p.Transform(), pal)
		require.NoError(t, err)
		dst := make([]uint32, rect.Dx()*rect.Dy())
		require.NoError(t, m.Map(dst, p, imaging.Plane{}, rect))
		return dst
	}
	assert.Equal(t, direct(v.Recipe(), image.Rect(0, 0, tileSize, tileSize)), tile(t, v, 0, 0), "an interior tile")

	// The tile at the corner covers 88 × 8 pixels of the image; the rest of
	// it is transparent.
	edge := tile(t, v, 1, 1)
	part := direct(v.Recipe(), image.Rect(tileSize, tileSize, 600, 520))
	for y := range tileSize {
		for x := range tileSize {
			got := edge[y*tileSize+x]
			if x < 600-tileSize && y < 520-tileSize {
				require.Equal(t, part[y*(600-tileSize)+x], got, "(%d,%d)", x, y)
			} else {
				require.Zero(t, got, "(%d,%d) is outside the image", x, y)
			}
		}
	}
	assert.Zero(t, tile(t, v, 5, 5)[0], "a tile wholly outside the image is transparent")
	_, _, _, err := v.src.TilePixels(context.Background(), portolan.TileCoords{Z: 1})
	assert.Error(t, err, "only the full-resolution level exists")

	// Another recipe, another tile; a recipe that cannot be shown is refused
	// and what is shown stays.
	before := tile(t, v, 0, 0)
	stretched := display.Recipe{Limits: display.Limits{Lo: 100, Hi: 5000}, Transfer: display.TransferAsinh, Palette: "batlow"}
	require.NoError(t, v.SetRecipe(stretched))
	assert.Equal(t, direct(stretched, image.Rect(0, 0, tileSize, tileSize)), tile(t, v, 0, 0))
	assert.NotEqual(t, before, tile(t, v, 0, 0))
	assert.Error(t, v.SetRecipe(display.Recipe{Limits: display.Limits{Lo: 5, Hi: 5}}))
	bad := stretched
	bad.Palette = "rainbow"
	assert.Error(t, v.SetRecipe(bad))
	assert.Equal(t, stretched, v.Recipe())
}

func TestAColourImageOpensAsStoredAndOnePlaneCanBeShownInstead(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(colourImage(t, 40, 30, nil))
	_, single := v.Plane()
	assert.False(t, single)
	assert.False(t, v.singlePlane(), "no colour bar for a colour composite")
	px := tile(t, v, 0, 0)
	assert.Equal(t, uint32(7)<<24|uint32(3)<<16|uint32(10)<<8|0xff, px[3*tileSize+7], "the stored red, green and blue of pixel (7,3)")

	require.NoError(t, v.SetPlane(1)) // green: the row
	idx, single := v.Plane()
	assert.True(t, single)
	assert.Equal(t, 1, idx)
	assert.True(t, v.singlePlane())
	assert.Equal(t, display.LimitsSourceData, v.Recipe().Limits.Source)
	px = tile(t, v, 0, 0)
	top, bottom := px[0*tileSize+5], px[29*tileSize+5]
	assert.Equal(t, uint32(0x000000ff), top, "row 0 is the lowest value of the plane: black")
	assert.Equal(t, uint32(0xffffffff), bottom, "row 29 is the highest: white")
	assert.Error(t, v.SetPlane(9))

	require.NoError(t, v.SetAsStored())
	assert.Equal(t, display.LimitsSourceNominal, v.Recipe().Limits.Source)
	assert.Equal(t, uint32(7)<<24|uint32(3)<<16|uint32(10)<<8|0xff, tile(t, v, 0, 0)[3*tileSize+7])
}

func TestATransparentPixelIsLaidOverTheCheckerboard(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(colourImage(t, 40, 30, func(col, row int) uint8 {
		switch {
		case col < 20:
			return 0
		case col < 30:
			return 255
		}
		return 128
	}))
	px := tile(t, v, 0, 0)
	k := checkerColours()
	assert.Equal(t, k[0], px[0], "a fully transparent pixel in the first square is the first tone")
	assert.Equal(t, k[1], px[checkerSize], "and in the next square the second")
	assert.Equal(t, k[1], px[checkerSize*tileSize], "the squares alternate down as well as across")
	assert.Equal(t, uint32(25)<<24|uint32(2)<<16|uint32(27)<<8|0xff, px[2*tileSize+25], "an opaque pixel is itself")
	half := px[2*tileSize+35]
	assert.Equal(t, uint32(0xff), half&0xff, "nothing translucent leaves the tile")
	want := func(shift uint, sample uint32) uint32 {
		kk := k[(35/checkerSize+2/checkerSize)&1] >> shift & 0xff
		return (sample*128 + kk*127 + 127) / 255
	}
	assert.Equal(t, want(24, 35)<<24|want(16, 2)<<16|want(8, 37)<<8|0xff, half)
	for i, p := range px[:tileSize] {
		if i >= 40 {
			assert.Zero(t, p, "outside the image stays transparent")
		}
	}
}

func TestEveryPaletteNameResolves(t *testing.T) {
	names := PaletteNames()
	assert.Equal(t, PaletteGray, names[0])
	assert.Len(t, names, 15)
	for _, name := range names {
		pal, ok := paletteFor(name)
		require.True(t, ok, name)
		assert.Equal(t, uint32(0xff), pal.Colors[0]&0xff, "%s: opaque", name)
		assert.NotEqual(t, pal.Colors[0], pal.Colors[255], "%s: a ramp, not a flat colour", name)
	}
	gray, ok := paletteFor("")
	require.True(t, ok)
	assert.Equal(t, uint32(0x808080ff), gray.Colors[128], "the grey ramp is the identity")
	_, ok = paletteFor("jet")
	assert.False(t, ok)
}
