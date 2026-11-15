package imageview

import (
	"context"
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
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

// waitForLevels waits for the viewer's level build and adopts it, as the
// Render after its completion would.
func waitForLevels(t *testing.T, v *Viewer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for v.built.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	v.adoptLevels()
	require.True(t, v.LevelsReady(), "the levels were built")
}

func tile(t *testing.T, v *Viewer, x, y int) []uint32 {
	t.Helper()
	return tileAt(t, v, x, y, 0)
}

// tileAt is the tile at (x, y) of level k.
func tileAt(t *testing.T, v *Viewer, x, y, k int) []uint32 {
	t.Helper()
	px, w, h, err := v.src.TilePixels(context.Background(), portolan.TileCoords{X: x, Y: y, Z: -k})
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
	assert.Equal(t, Readout{Col: 37, Row: 11, Planes: 4, Stored: [4]float64{37, 11, 48, 163},
		Values: [4]float64{37, 11, 48, 163}, Valid: [4]bool{true, true, true, true}}, ev.Hover, "without a transform a value is its sample")
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

func TestATileBelowFullResolutionIsTheRecipeAppliedToTheLevelsMeans(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	img := grayImage(t, 2300, 1100)
	v.SetImage(img)
	p := img.Plane(0)
	require.Equal(t, 3, levels.CountFor(2300, 1100, levels.Options{}), "2300 → 1150 → 575 → 288")

	// What a tile of level k must be: the recipe applied to the means the
	// definition gives, transparent outside the level.
	want := func(k, x, y int) []uint32 {
		lw, lh := levels.Size(2300, 1100, k)
		rect := image.Rect(x*tileSize, y*tileSize, (x+1)*tileSize, (y+1)*tileSize).Intersect(image.Rect(0, 0, lw, lh))
		out := make([]uint32, tileSize*tileSize)
		if rect.Empty() {
			return out
		}
		mean, _, _, err := levels.Reduce(p, k, rect)
		require.NoError(t, err)
		m, err := display.CompilePlane(v.Recipe(), p.Kind(), p.Transform(), nil)
		require.NoError(t, err)
		dst := make([]uint32, rect.Dx()*rect.Dy())
		require.NoError(t, m.Map(dst, mean, imaging.Plane{}, image.Rect(0, 0, rect.Dx(), rect.Dy())))
		for row := range rect.Dy() {
			copy(out[row*tileSize:], dst[row*rect.Dx():(row+1)*rect.Dx()])
		}
		return out
	}
	check := func(when string) {
		for k := 1; k <= 3; k++ {
			lw, lh := levels.Size(2300, 1100, k)
			for y := 0; y*tileSize < lh; y++ {
				for x := 0; x*tileSize < lw; x++ {
					require.Equal(t, want(k, x, y), tileAt(t, v, x, y, k), "%s: level %d tile (%d,%d)", when, k, x, y)
				}
			}
		}
	}
	// Whether or not the build has landed, the tiles are the same cells.
	check("before or during the build")
	waitForLevels(t, v)
	require.NotNil(t, v.pyramid)
	check("from the built levels")

	_, _, _, err := v.src.TilePixels(context.Background(), portolan.TileCoords{Z: -4})
	assert.Error(t, err, "there is no fourth level")
	assert.Equal(t, -3, sourceFor(img).MinNativeZoom)
	assert.Zero(t, sourceFor(colourImage(t, 40, 30, nil)).MinNativeZoom, "a small image has no reduced levels")
}

func TestTheViewSaysWhichLevelItDraws(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	assert.Zero(t, v.Level())
	assert.True(t, v.LevelsReady(), "no image, nothing to build")
	v.SetImage(grayImage(t, 2300, 1100))
	v.Render(600, 400) // the fit: 2300 wide into about 520
	sm.ScriptReset()
	assert.InDelta(t, 520.0/2300, v.Magnification(), 0.01)
	assert.Equal(t, 2, v.Level(), "magnification 0.226 lies between 1/4 and 1/8: level 2 is the finer")
	assert.Contains(t, v.readoutText(), "drawn from means of 4 × 4")

	v.ActualSize()
	v.Render(600, 400)
	sm.ScriptReset()
	assert.Zero(t, v.Level())
	assert.NotContains(t, v.readoutText(), "means")

	v.m.View().SetZoom(-20)
	assert.Equal(t, 3, v.Level(), "never past the last level the image has")
}

func TestSettingAnotherImageStopsTheBuildOfTheLast(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	big := grayImage(t, 3000, 3000)
	v.SetImage(big)
	small := colourImage(t, 40, 30, nil)
	v.SetImage(small)
	assert.Nil(t, v.built.Load(), "nothing of the first build is left to adopt")
	assert.True(t, v.LevelsReady())
	v.SetImage(big)
	waitForLevels(t, v)
	v.Close() // waits; a second Close of the build side must not hang
	v.stopBuild()
}

// rampImage is a w × 1-row-repeated 16-bit plane whose samples run from 0 to
// top across the columns, with a few far outliers when asked.
func rampImage(t *testing.T, w, h int, top uint16, outliers bool) imaging.Image {
	t.Helper()
	s := make([]uint16, w*h)
	for row := range h {
		for col := range w {
			s[row*w+col] = uint16(col * int(top) / (w - 1))
		}
	}
	if outliers {
		s[0], s[1] = 60000, 65535
	}
	p, err := imaging.NewPlaneU16(w, h, s)
	require.NoError(t, err)
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	require.NoError(t, err)
	return img
}

// waitForHistogram waits for the shown plane's histogram and adopts it.
func waitForHistogram(t *testing.T, v *Viewer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v.adoptHistograms()
		if _, ok := v.Histogram(); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the histogram was not counted")
}

func TestDraggingALimitHandleChangesTheRecipeAndNothingElse(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	v := New(ids, "h", Options{})
	defer v.Close()
	v.SetImage(rampImage(t, 201, 50, 1000, false))
	require.Equal(t, display.Limits{Lo: 0, Hi: 1000, Source: display.LimitsSourceData}, v.Recipe().Limits)
	v.Render(400, 300) // asks for the histogram
	sm.ScriptReset()
	waitForHistogram(t, v)

	var canvas, lo, hi widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("h")) {
		canvas = widgethandle.Make(ids.PrepareStr("hist").Derive())
		lo = widgethandle.Make(ids.PrepareStr("hist-lo").Derive())
		hi = widgethandle.Make(ids.PrepareStr("hist-hi").Derive())
	}
	// The strip is as wide as the image canvas: 400 less the colour bar and
	// its gap. Its axis runs 0 … 1000, so a pixel is 3.125 values.
	const stripW = 400 - colorBarWidth - layoutGap
	frame := func(pointerX float32, loFlags, hiFlags c.ResponseFlagsE) Events {
		nan := float32(math.NaN())
		sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{OriginX: 50, OriginY: 400, PosX: nan, PosY: nan})
		sm.ScriptPointer(c.PointerValue{X: 50 + pointerX, Y: 420, Valid: true})
		sm.ScriptResponse(lo, loFlags)
		sm.ScriptResponse(hi, hiFlags)
		ev := v.Render(400, 300)
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
		return ev
	}
	down := c.IsPointerButtonDownResponseFlags

	// Press the upper handle two pixels left of where it stands: nothing
	// moves, and the limits stay the data's.
	ev := frame(stripW-2, 0, c.DragStartedResponseFlags|down)
	assert.False(t, ev.LimitsChanged)
	require.True(t, v.drag.active)
	assert.Equal(t, display.LimitsSourceData, v.Recipe().Limits.Source)
	// Drag it to the middle of the strip.
	ev = frame(stripW/2-2, 0, c.DraggedResponseFlags|down)
	assert.True(t, ev.LimitsChanged)
	l := v.Recipe().Limits
	assert.InDelta(t, 500, l.Hi, 1e-9)
	assert.Equal(t, 0.0, l.Lo)
	assert.Equal(t, display.LimitsSourceManual, l.Source, "a dragged limit is one a person set")
	// The axis holds still while the handle is held, so the same pointer
	// position is the same value.
	ev = frame(stripW/2-2, 0, c.DraggedResponseFlags|down)
	assert.False(t, ev.LimitsChanged)
	// Release.
	frame(stripW/4, 0, c.DragStoppedResponseFlags)
	assert.False(t, v.drag.active)
	assert.InDelta(t, 500, v.Recipe().Limits.Hi, 1e-9, "the release frame moves nothing")

	// The lower handle cannot cross the upper one.
	frame(0, c.DragStartedResponseFlags|down, 0)
	frame(stripW, c.DraggedResponseFlags|down, 0)
	l = v.Recipe().Limits
	assert.Less(t, l.Lo, l.Hi)
	assert.InDelta(t, 500, l.Lo, 1, "it stops just short of the upper limit")
	frame(stripW, 0, 0)
	assert.False(t, v.drag.active, "a drag ends when the button is no longer down")

	// The samples were never touched: the readout of a pixel is its sample.
	r, ok := v.readoutAt(portolan.LL(-0.5, 100.5))
	require.True(t, ok)
	assert.Equal(t, 500.0, r.Stored[0])
}

func TestPercentileLimitsWaitForTheHistogramAndIgnoreOutliers(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(rampImage(t, 1001, 20, 1000, true))
	assert.Equal(t, 65535.0, v.Recipe().Limits.Hi, "the data range is the outliers'")
	_, ok := v.Histogram()
	assert.False(t, ok, "counted off the frame, so not there at the first asking")

	require.NoError(t, v.SetRecipe(percentile(v, 1, 99)))
	waitForHistogram(t, v)
	l := v.Recipe().Limits
	assert.Equal(t, display.LimitsSourcePercentile, l.Source)
	assert.Equal(t, 1.0, l.PercentLo)
	assert.Equal(t, 99.0, l.PercentHi)
	assert.InDelta(t, 10, l.Lo, 2)
	assert.InDelta(t, 990, l.Hi, 2)
	h, ok := v.Histogram()
	require.True(t, ok)
	assert.EqualValues(t, 1001*20, h.Valid(), "over the whole plane")

	// With the histogram already there the stretch applies at once.
	require.NoError(t, v.SetRecipe(percentile(v, 10, 90)))
	assert.InDelta(t, 100, v.Recipe().Limits.Lo, 2)

	assert.Error(t, v.SetRecipe(percentile(v, 90, 10)))
	assert.Error(t, v.SetRecipe(percentile(v, -1, 50)))
	v.SetImage(colourImage(t, 40, 30, nil))
	assert.Error(t, v.SetRecipe(percentile(v, 1, 99)), "a colour composite has no single plane to take percentiles of")
	_, ok = v.Histogram()
	assert.False(t, ok)
	require.NoError(t, v.SetPlane(0))
	require.NoError(t, v.SetRecipe(percentile(v, 0, 50)))
	require.NoError(t, v.SetPlane(1))
	waitForHistogram(t, v)
	assert.Equal(t, display.LimitsSourceData, v.Recipe().Limits.Source, "a stretch asked for on one plane is not applied to another")
}

func TestARegionIsPixelsClippedToTheImageAndGoesWithIt(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	_, ok := v.Region()
	assert.False(t, ok)
	v.SetRegion(image.Rect(1, 1, 5, 5))
	_, ok = v.Region()
	assert.False(t, ok, "no image, no region")

	v.SetImage(grayImage(t, 100, 60))
	v.SetRegion(image.Rect(90, 50, 140, 80))
	r, ok := v.Region()
	require.True(t, ok)
	assert.Equal(t, image.Rect(90, 50, 100, 60), r, "clipped to the image")
	v.SetRegion(image.Rect(200, 200, 210, 210))
	_, ok = v.Region()
	assert.False(t, ok, "a rectangle that misses the image clears the mark")

	// A box on the view marks every pixel it touches: pixel (col, row)
	// covers [col, col+1) × [row, row+1), and latitude is the negated row.
	box := portolan.LatLngBoundsOf(portolan.LL(-20.2, 10.5), portolan.LL(-4.9, 30.01))
	assert.Equal(t, image.Rect(10, 4, 31, 21), v.pixelsIn(box))
	off := portolan.LatLngBoundsOf(portolan.LL(-500, 300), portolan.LL(-400, 400))
	assert.True(t, v.pixelsIn(off).Empty())

	assert.False(t, v.Selecting())
	v.SetSelecting(true)
	assert.True(t, v.Selecting())
	v.SetRegion(image.Rect(0, 0, 10, 10))
	v.SetImage(grayImage(t, 20, 20))
	_, ok = v.Region()
	assert.False(t, ok, "another image has no region")
	assert.True(t, v.Selecting(), "the mode is the viewer's, not the image's")
}

// percentile is v's recipe with percentile limits asked for.
func percentile(v *Viewer, lo, hi float64) display.Recipe {
	r := v.Recipe()
	r.Limits = display.Limits{Source: display.LimitsSourcePercentile, PercentLo: lo, PercentHi: hi}
	return r
}

func TestAPercentileRecipeAppliesTheRestAtOnceAndALaterRecipeReplacesIt(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(rampImage(t, 1001, 20, 1000, true))
	before := v.Recipe().Limits

	r := percentile(v, 1, 99)
	r.Transfer, r.Invert = display.TransferSqrt, true
	require.NoError(t, v.SetRecipe(r))
	if v.LimitsPending() {
		got := v.Recipe()
		assert.Equal(t, display.TransferSqrt, got.Transfer, "the rest of the recipe is applied at once")
		assert.True(t, got.Invert)
		assert.Equal(t, before, got.Limits, "the limits in force stay until the histogram is counted")

		// A manual recipe asked for meanwhile wins over the stretch.
		manual := got
		manual.Limits = display.Limits{Lo: 100, Hi: 200, Source: display.LimitsSourceManual}
		require.NoError(t, v.SetRecipe(manual))
		assert.False(t, v.LimitsPending())
		waitForHistogram(t, v)
		assert.Equal(t, manual.Limits, v.Recipe().Limits, "the stretch that was replaced does not land")
	}

	waitForHistogram(t, v)
	require.NoError(t, v.SetRecipe(percentile(v, 1, 99)))
	assert.False(t, v.LimitsPending(), "with the histogram counted the limits are set at once")
	assert.Equal(t, display.LimitsSourcePercentile, v.Recipe().Limits.Source)
	assert.InDelta(t, 990, v.Recipe().Limits.Hi, 2)

	v.SetImage(flatImage(t, 30, 20, 7))
	waitForHistogram(t, v)
	assert.Error(t, v.SetRecipe(percentile(v, 1, 99)), "the two percentiles fall on one value")
}

// flatImage is an 8-bit plane of one value.
func flatImage(t *testing.T, w, h int, value uint8) imaging.Image {
	t.Helper()
	s := make([]uint8, w*h)
	for i := range s {
		s[i] = value
	}
	p, err := imaging.NewPlaneU8(w, h, s)
	require.NoError(t, err)
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	require.NoError(t, err)
	return img
}

// sampleImage is one of the synthetic samples.
func sampleImage(t *testing.T, name string) imaging.Image {
	t.Helper()
	img, err := samples.ByName(name)
	require.NoError(t, err)
	return img
}

// A float plane with invalid samples, through every stage: an invalid
// sample is the checkerboard in a tile at full resolution and at a reduced
// level, says so in the readout, is left out of the histogram, and does not
// move limits taken from the data or from percentiles.
func TestAFloatPlaneWithInvalidSamplesEndToEnd(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	img := sampleImage(t, samples.NameFloat)
	v.SetImage(img)
	checker := checkerColours()
	isChecker := func(px uint32) bool { return px == checker[0] || px == checker[1] }

	l := v.Recipe().Limits
	assert.Equal(t, display.LimitsSourceData, l.Source)
	assert.Less(t, l.Lo, -40.0)
	assert.Greater(t, l.Hi, 9e4)
	assert.False(t, math.IsNaN(l.Lo) || math.IsNaN(l.Hi), "invalid samples are not in the data range")

	col, row := samples.FloatInvalid.Min.X+10, samples.FloatInvalid.Min.Y+10
	px := tile(t, v, 0, 0)
	assert.True(t, isChecker(px[row*tileSize+col]), "an invalid sample is drawn as the checkerboard")
	assert.False(t, isChecker(px[10*tileSize+300]), "a valid one is not")
	lv := tileAt(t, v, 0, 0, 1)
	assert.True(t, isChecker(lv[(row/2)*tileSize+col/2]), "a reduced cell over invalid samples only is invalid too")

	r, ok := v.readoutAt(portolan.LL(-float64(row)-0.5, float64(col)+0.5))
	require.True(t, ok)
	assert.False(t, r.Valid[0])
	v.hover, v.hoverOk = r, true
	assert.Contains(t, v.readoutText(), "gray invalid")

	h, ok := v.Histogram()
	if !ok {
		waitForHistogram(t, v)
		h, _ = v.Histogram()
	}
	invalid := uint64(samples.FloatInvalid.Dx() * samples.FloatInvalid.Dy())
	assert.Equal(t, invalid, h.Invalid())
	assert.Equal(t, uint64(img.Width()*img.Height())-invalid, h.Valid())

	require.NoError(t, v.SetRecipe(percentile(v, 1, 99)))
	// The 99th percentile by nearest rank over the valid samples, which a
	// float histogram locates to within one of its bins.
	var vals []float64
	for _, x := range img.Plane(0).F32() {
		if x == x {
			vals = append(vals, float64(x))
		}
	}
	slices.Sort(vals)
	want := vals[int(math.Ceil(0.99*float64(len(vals))))-1]
	assert.InDelta(t, want, v.Recipe().Limits.Hi, h.BinWidth())
	assert.Less(t, v.Recipe().Limits.Hi, 0.7*l.Hi, "well inside the data range")
	r2 := v.Recipe()
	r2.Transfer = display.TransferAsinh
	require.NoError(t, v.SetRecipe(r2), "a stretch over limits that span zero")
	_ = tile(t, v, 0, 0)
}

// A plane under a value transform: limits, the colour bar, the histogram and
// the readout are in values, and the readout says which stored sample a
// value came from.
func TestAPlaneUnderAValueTransformEndToEnd(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	img := sampleImage(t, samples.NameScaled)
	v.SetImage(img)
	tr := samples.ScaledTransform

	slo, shi, ok := img.Plane(0).StoredRange()
	require.True(t, ok)
	l := v.Recipe().Limits
	assert.InDelta(t, tr.Apply(slo), l.Lo, 1e-9, "the data limits are values")
	assert.InDelta(t, tr.Apply(shi), l.Hi, 1e-9)

	r, ok := v.readoutAt(portolan.LL(-0.5, 0.5))
	require.True(t, ok)
	assert.Equal(t, 4000.0, r.Stored[0])
	assert.InDelta(t, 0.0, r.Values[0], 1e-9)
	v.hover, v.hoverOk = r, true
	assert.Contains(t, v.readoutText(), "gray 0 (stored 4000)")

	waitForHistogram(t, v)
	h, _ := v.Histogram()
	hlo, hhi, _ := h.Range()
	assert.InDelta(t, l.Lo, hlo, 1e-9, "the histogram's range is in values too")
	assert.InDelta(t, l.Hi, hhi, 1e-9)

	// Limits set in values map the stored samples they stand for: the sample
	// at the lower limit is the darkest grey, at the upper the lightest.
	rec := v.Recipe()
	rec.Limits = display.Limits{Lo: 0, Hi: 50, Source: display.LimitsSourceManual}
	require.NoError(t, v.SetRecipe(rec))
	px := tile(t, v, 0, 0)
	assert.Equal(t, uint32(0x000000ff), px[0], "stored 4000 is value 0, the lower limit")
	assert.Equal(t, uint32(0xffffffff), px[0*tileSize+479], "stored 9000 is value 50, the upper limit")
}
