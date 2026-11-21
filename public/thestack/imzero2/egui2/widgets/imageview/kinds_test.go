package imageview

import (
	"context"
	"image"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The kinds ADR-0398 adds, end to end through the viewer: a declared invalid
// sample is the checkerboard at full resolution and at a reduced level,
// reads as invalid, is left out of the histogram and of the limits; and a
// percentile stretch is the nearest rank of the valid samples, exactly for a
// kind with codes and to a bin for the others.
func TestTheNewKindsEndToEnd(t *testing.T) {
	checker := checkerColours()
	isChecker := func(px uint32) bool { return px == checker[0] || px == checker[1] }
	for _, tc := range []struct {
		name         string
		invalidAt    image.Point
		validAt      image.Point
		exact        bool
		declaredOnly bool
	}{
		{samples.NameElevation, image.Pt(2, 100), image.Pt(200, 150), true, true},
		{samples.NameCounts, image.Pt(3, 3), image.Pt(200, 150), false, true},
		{samples.NameDouble, image.Pt(7, 7), image.Pt(200, 150), false, false},
	} {
		v := New(c.NewWidgetIdStack(), "", Options{})
		img := sampleImage(t, tc.name)
		v.SetImage(img)
		info := v.SourceInfo()
		inv, declared := img.Plane(0).Invalid()
		assert.Equal(t, declared, info.HasInvalid, tc.name)
		assert.Equal(t, inv, info.Invalid)

		l := v.Recipe().Limits
		slo, shi, _ := img.Plane(0).StoredRange()
		assert.Equal(t, display.Limits{Lo: slo, Hi: shi, Source: display.LimitsSourceData}, l, "%s: the data range leaves the invalid samples out", tc.name)

		px := tile(t, v, 0, 0)
		assert.True(t, isChecker(px[tc.invalidAt.Y*tileSize+tc.invalidAt.X]), "%s: an invalid sample is the checkerboard", tc.name)
		assert.False(t, isChecker(px[tc.validAt.Y*tileSize+tc.validAt.X]), "%s: a valid one is not", tc.name)
		if tc.declaredOnly {
			// The sample is too small to have levels; a tile below full
			// resolution is reduced on the spot, as before a build lands.
			d, err := (&imageSource{img: img}).Tile(context.Background(), -1, 0, 0)
			require.NoError(t, err)
			_, ok := d.Samples.Plane(0).StoredAt(tc.invalidAt.X/2, tc.invalidAt.Y/2)
			assert.False(t, ok, "%s: a reduced cell of invalid samples is invalid", tc.name)
		}

		r, ok := v.readoutAt(portolan.LL(-float64(tc.invalidAt.Y)-0.5, float64(tc.invalidAt.X)+0.5))
		require.True(t, ok)
		assert.False(t, r.Valid[0], tc.name)
		v.hover, v.hoverOk = r, true
		assert.Contains(t, v.readoutText(), "gray invalid", tc.name)
		r, _ = v.readoutAt(portolan.LL(-float64(tc.validAt.Y)-0.5, float64(tc.validAt.X)+0.5))
		v.hover = r
		want, _ := img.Plane(0).StoredAt(tc.validAt.X, tc.validAt.Y)
		assert.Contains(t, v.readoutText(), "gray "+formatSample(want, img.Kind()), tc.name)

		waitForHistogram(t, v)
		h, _ := v.Histogram()
		var vals []float64
		bad := 0
		for y := range img.Height() {
			for x := range img.Width() {
				if s, ok := img.Plane(0).StoredAt(x, y); ok {
					vals = append(vals, s)
				} else {
					bad++
				}
			}
		}
		assert.EqualValues(t, bad, h.Invalid(), tc.name)
		assert.EqualValues(t, len(vals), h.Valid())
		assert.Equal(t, tc.exact, h.IsExact())
		slices.Sort(vals)
		require.NoError(t, v.SetRecipe(percentile(v, 1, 99)))
		lim := v.Recipe().Limits
		wantLo, wantHi := vals[int(math.Ceil(0.01*float64(len(vals))))-1], vals[int(math.Ceil(0.99*float64(len(vals))))-1]
		if tc.exact {
			assert.Equal(t, [2]float64{wantLo, wantHi}, [2]float64{lim.Lo, lim.Hi}, "%s: nearest rank", tc.name)
		} else {
			assert.InDelta(t, wantLo, lim.Lo, h.BinWidth(), tc.name)
			assert.InDelta(t, wantHi, lim.Hi, h.BinWidth(), tc.name)
		}
		_ = tile(t, v, 0, 0)
		v.Close()
	}
}

// A stack's image shows its band labels in the readout and keeps its
// placement and invalid value through a store; a signed 16-bit colour
// composite is drawn as colour, its invalid pixels as the checkerboard.
func TestAStackSelectionThroughAStore(t *testing.T) {
	st, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	img, err := st.Image([]int{1, 0}, []int{3, 2, 1})
	require.NoError(t, err)
	s, err := tiles.FromImage(context.Background(), img, tiles.Options{})
	require.NoError(t, err)
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	require.NoError(t, v.SetSource(StoreSource(s), false))
	info := v.SourceInfo()
	assert.Equal(t, []string{"B8", "B4", "B3"}, info.PlaneLabels)
	assert.Equal(t, "B4", info.PlaneLabel(1))
	assert.Equal(t, samples.BandsPlacement, info.Placement)
	assert.True(t, info.HasInvalid)

	r, ok := v.readoutAt(portolan.LL(-60.5, 150.5))
	require.True(t, ok)
	v.hover, v.hoverOk = r, true
	text := v.readoutText()
	assert.Contains(t, text, "B8 ")
	assert.Contains(t, text, "B3 ")
	assert.NotContains(t, text, "red")
	r, _ = v.readoutAt(portolan.LL(-60.5, 0.5))
	v.hover = r
	assert.Contains(t, v.readoutText(), "B8 invalid  B4 invalid  B3 invalid")

	px := tile(t, v, 0, 0)
	checker := checkerColours()
	assert.Contains(t, checker, px[60*tileSize], "the swath edge is the checkerboard")
	assert.NotContains(t, checker, px[60*tileSize+150])
	_, single := v.Plane()
	assert.False(t, single, "a u16 stack image opens as a colour composite")

	e := sampleImage(t, samples.NameElevation)
	mk := func(i int) imaging.Plane { return e.Plane(0) }
	rgb, err := imaging.NewImage(imaging.ColorModelRGB, []imaging.Plane{mk(0), mk(1), mk(2)}, imaging.Provenance{})
	require.NoError(t, err)
	v.SetImage(rgb)
	_, single = v.Plane()
	assert.False(t, single, "a signed 16-bit colour image is mapped as colour")
	px = tile(t, v, 0, 0)
	assert.Contains(t, checker, px[100*tileSize+2])
	assert.NotContains(t, checker, px[150*tileSize+200])
}

func TestTileBytesCountTheKindsSize(t *testing.T) {
	for _, kind := range imaging.AllSampleKinds {
		var p imaging.Plane
		var err error
		switch kind {
		case imaging.SampleKindU8:
			p, err = imaging.NewPlaneU8(3, 2, make([]uint8, 6))
		case imaging.SampleKindU16:
			p, err = imaging.NewPlaneU16(3, 2, make([]uint16, 6))
		case imaging.SampleKindI16:
			p, err = imaging.NewPlaneI16(3, 2, make([]int16, 6))
		case imaging.SampleKindI32:
			p, err = imaging.NewPlaneI32(3, 2, make([]int32, 6))
		case imaging.SampleKindF32:
			p, err = imaging.NewPlaneF32(3, 2, make([]float32, 6))
		case imaging.SampleKindF64:
			p, err = imaging.NewPlaneF64(3, 2, make([]float64, 6))
		}
		require.NoError(t, err)
		img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
		require.NoError(t, err)
		assert.EqualValues(t, 6*kind.Bytes(), tileBytes(TileData{Samples: img, Region: image.Rect(0, 0, 3, 2)}), kind.String())
	}
}

func TestASourceDescribingWhatItCannotBeIsRefused(t *testing.T) {
	ok := SourceInfo{Width: 4, Height: 4, Model: imaging.ColorModelGray, Kind: imaging.SampleKindI16}
	require.NoError(t, ok.Validate())
	for _, bad := range []SourceInfo{
		func() SourceInfo { b := ok; b.PlaneLabels = []string{"a", "b"}; return b }(),
		func() SourceInfo { b := ok; b.HasInvalid, b.Invalid = true, 40000; return b }(),
		func() SourceInfo {
			b := ok
			b.Placement = imaging.Placement{Frame: imaging.Frame{Kind: imaging.FrameKindPhysical}}
			return b
		}(),
	} {
		assert.Error(t, bad.Validate(), "%+v", bad)
	}
	assert.Equal(t, "gray", ok.PlaneLabel(0))
	assert.Empty(t, ok.PlaneLabel(1))
}

// A source that describes its invalid value without its tiles declaring it
// still has that value drawn and read as invalid.
func TestASourcesDescribedInvalidValueApplies(t *testing.T) {
	src := &i16Source{}
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	require.NoError(t, v.SetSource(src, false))
	r, ok := v.readoutAt(portolan.LL(-0.5, 0.5))
	require.True(t, ok)
	assert.False(t, r.Valid[0], "the described value")
	r, _ = v.readoutAt(portolan.LL(-0.5, 1.5))
	assert.True(t, r.Valid[0])
	px := tile(t, v, 0, 0)
	assert.Contains(t, checkerColours(), px[0])
	assert.NotContains(t, checkerColours(), px[1])
}

type i16Source struct{}

func (inst *i16Source) Info() SourceInfo {
	return SourceInfo{Width: 2, Height: 1, Model: imaging.ColorModelGray, Kind: imaging.SampleKindI16, Invalid: -1, HasInvalid: true, Resident: true}
}

func (inst *i16Source) Tile(context.Context, int, int, int) (d TileData, err error) {
	p, err := imaging.NewPlaneI16(2, 1, []int16{-1, -200})
	if err != nil {
		return
	}
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	return TileData{Samples: img, Region: image.Rect(0, 0, 2, 1)}, err
}

// The histogram strip draws for a kind with negative codes and for a binned
// kind, frame after frame, as the counted histogram lands.
func TestTheHistogramStripDrawsForTheNewKinds(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	for _, name := range []string{samples.NameElevation, samples.NameCounts, samples.NameDouble} {
		v := New(c.NewWidgetIdStack(), "", Options{})
		v.SetImage(sampleImage(t, name))
		for range 2 {
			v.Render(500, 400)
			require.False(t, typed.HasErrors(), typed.GetError())
			sm.ScriptReset()
		}
		waitForHistogram(t, v)
		v.Render(500, 400)
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
		assert.NotZero(t, v.histTop, "%s: the strip has bars", name)
		v.Close()
	}
}
