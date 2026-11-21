package imageview

import (
	"context"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// bandsImage is a w × h RGB image of a sample kind whose three planes are
// bands of unlike range: plane k holds (col + 2·row) · 10^k / 7, plane 1
// a NaN or the declared value 0 at every 97th pixel.
func bandsImage(t *testing.T, w, h int, kind imaging.SampleKindE) imaging.Image {
	t.Helper()
	planes := make([]imaging.Plane, 3)
	for k := range planes {
		v := make([]float64, w*h)
		for i := range v {
			col, row := i%w, i/w
			v[i] = float64(col+2*row) * math.Pow(10, float64(k)) / 7
			if k == 1 && i%97 == 5 {
				v[i] = math.NaN()
			}
		}
		var err error
		switch kind {
		case imaging.SampleKindF32:
			s := make([]float32, len(v))
			for i, x := range v {
				s[i] = float32(x)
			}
			planes[k], err = imaging.NewPlaneF32(w, h, s)
		case imaging.SampleKindU16:
			s := make([]uint16, len(v))
			for i, x := range v {
				if !math.IsNaN(x) {
					s[i] = uint16(min(x, 65535))
				}
			}
			planes[k], err = imaging.NewPlaneU16(w, h, s)
			if err == nil {
				planes[k], err = planes[k].WithInvalid(0)
			}
		}
		require.NoError(t, err)
	}
	img, err := imaging.NewImage(imaging.ColorModelRGB, planes, imaging.Provenance{})
	require.NoError(t, err)
	return img
}

// layersOf makes composite layers of planes under recipes and colours.
func layersOf(planes []int, recipes []display.Recipe, colors []display.LayerColorE) []CompositeLayer {
	out := make([]CompositeLayer, len(planes))
	for i, p := range planes {
		out[i] = CompositeLayer{Plane: p, Layer: display.Layer{Recipe: recipes[i], Color: colors[i]}}
	}
	return out
}

func manual(lo, hi float64) display.Recipe {
	return display.Recipe{Limits: display.Limits{Lo: lo, Hi: hi, Source: display.LimitsSourceManual}}
}

// directComposite maps a whole image through CompositeMapper as the viewer
// would, then lays it over the checkerboard as a tile is.
func directComposite(t *testing.T, img imaging.Image, layers []CompositeLayer, rect image.Rectangle) []uint32 {
	t.Helper()
	dl := make([]display.Layer, len(layers))
	specs := make([]display.PlaneSpec, len(layers))
	planes := make([]imaging.Plane, len(layers))
	for i, l := range layers {
		dl[i], specs[i], planes[i] = l.Layer, display.PlaneSpecOf(img.Plane(l.Plane)), img.Plane(l.Plane)
	}
	m, err := display.CompileComposite(dl, specs)
	require.NoError(t, err)
	px := make([]uint32, rect.Dx()*rect.Dy())
	require.NoError(t, m.Map(px, planes, rect))
	overChecker(px, rect, checkerColours())
	return px
}

// A composite's tiles are CompositeMapper over the same planes, for a kind
// with codes and a binned one, invalid samples included; a plane may be the
// plane of two layers.
func TestACompositesTilesAreTheCompositeMapping(t *testing.T) {
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindU16, imaging.SampleKindF32} {
		img := bandsImage(t, 300, 200, kind)
		v := New(c.NewWidgetIdStack(), "", Options{})
		v.SetImage(img)
		_, single := v.Plane()
		assert.Equal(t, kind == imaging.SampleKindF32, single, "%s: a float colour image opens on its first plane", kind)
		layers := layersOf([]int{2, 0, 1, 0},
			[]display.Recipe{manual(0, 30000), manual(0, 100), manual(10, 900), {Limits: display.Limits{Lo: 5, Hi: 50}, Transfer: display.TransferLog}},
			[]display.LayerColorE{display.LayerColorRed, display.LayerColorGreen, display.LayerColorBlue, display.LayerColorWhite})
		require.NoError(t, v.SetComposite(layers, false), kind)
		assert.True(t, v.IsComposite())
		_, single = v.Plane()
		assert.False(t, single)
		assert.False(t, v.IsSinglePlane())
		got := tile(t, v, 0, 0)
		want := directComposite(t, img, layers, image.Rect(0, 0, 300, 200))
		for row := range 200 {
			require.Equal(t, want[row*300:(row+1)*300], got[row*tileSize:row*tileSize+300], "%s: row %d", kind, row)
		}
		v.Close()
	}
}

// A change of a layer's recipe, colour or visibility, of the layer in hand
// and of the link maps the cached samples again and asks the source
// nothing.
func TestALayerChangeAsksTheSourceNothing(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	src := newPatternSource(1000, 700, 1, 1)
	require.NoError(t, v.SetSource(src, false))
	layers := layersOf([]int{0, 1}, []display.Recipe{manual(0, 255), manual(0, 255)},
		[]display.LayerColorE{display.LayerColorGreen, display.LayerColorMagenta})
	require.NoError(t, v.SetComposite(layers, true))
	first := sourceTile(t, v, 0, 1, 0)
	require.EqualValues(t, 1, src.calls.Load())
	// Cell (512 + 5, 7): red (517 + 21) mod 256 in green, green 100 in magenta.
	red := uint32((512 + 5 + 21) % 256)
	assert.Equal(t, uint32(100)<<24|red<<16|100<<8|0xff, first[7*tileSize+5])

	r := manual(0, 127)
	require.NoError(t, v.SetRecipe(r))
	require.NoError(t, v.SetLayer(1, CompositeLayer{Plane: 1, Layer: display.Layer{Recipe: r, Color: display.LayerColorBlue}}))
	after := sourceTile(t, v, 0, 1, 0)
	assert.Equal(t, uint32(0)<<24|min(255, uint32(math.Round(float64(red)*255/127)))<<16|uint32(math.Round(100*255.0/127))<<8|0xff, after[7*tileSize+5])
	hidden, _ := v.Composite()
	hidden[0].Hidden = true
	require.NoError(t, v.SetLayer(0, hidden[0]))
	assert.Equal(t, uint32(math.Round(100*255.0/127))<<8|0xff, sourceTile(t, v, 0, 1, 0)[7*tileSize+5], "a hidden layer adds nothing")
	require.NoError(t, v.SetCompositeLinked(false))
	require.NoError(t, v.SetLayerInHand(1))
	sourceTile(t, v, 0, 1, 0)
	assert.EqualValues(t, 1, src.calls.Load(), "every change was mapped from the cached samples")
}

// A linked composite sets a recipe on every layer; an unlinked one on the
// layer in hand alone, and says so beside the image while two layers are
// shown. Linking again gives every layer the recipe in hand.
func TestTheLinkDecidesWhichLayersARecipeReaches(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	img := bandsImage(t, 120, 80, imaging.SampleKindF32)
	v.SetImage(img)
	layers := layersOf([]int{0, 1, 2}, []display.Recipe{manual(0, 10), manual(0, 10), manual(0, 10)},
		display.DefaultLayerColors(3))
	assert.Error(t, v.SetComposite(layersOf([]int{0, 1}, []display.Recipe{manual(0, 10), manual(0, 20)}, display.DefaultLayerColors(2)), true),
		"a linked composite with two recipes is refused")
	require.NoError(t, v.SetComposite(layers, true))
	assert.True(t, v.CompositeLinked())
	_, stated := v.CompositeStatement()
	assert.False(t, stated, "linked: nothing to state")

	require.NoError(t, v.SetLayerInHand(2))
	require.NoError(t, v.SetRecipe(manual(1, 5)))
	got, _ := v.Composite()
	for i := range got {
		assert.Equal(t, manual(1, 5), got[i].Recipe, "layer %d: linked, every layer", i)
	}
	assert.Error(t, v.SetLayer(0, CompositeLayer{Plane: 0, Layer: display.Layer{Recipe: manual(0, 3), Color: display.LayerColorRed}}),
		"a linked layer's recipe is every layer's")

	require.NoError(t, v.SetCompositeLinked(false))
	text, stated := v.CompositeStatement()
	assert.True(t, stated, "unlinked, with three layers shown")
	assert.Equal(t, CompositeUnlinkedText, text)
	require.NoError(t, v.SetRecipe(manual(2, 4)))
	got, _ = v.Composite()
	assert.Equal(t, manual(1, 5), got[0].Recipe)
	assert.Equal(t, manual(1, 5), got[1].Recipe)
	assert.Equal(t, manual(2, 4), got[2].Recipe, "the layer in hand alone")
	assert.Equal(t, manual(2, 4), v.Recipe(), "the recipe reported is the layer in hand's")

	for i := range 2 {
		got[i].Hidden = true
		require.NoError(t, v.SetLayer(i, got[i]))
	}
	_, stated = v.CompositeStatement()
	assert.False(t, stated, "one layer shown has no other to compare with")
	got[1].Hidden = false
	require.NoError(t, v.SetLayer(1, got[1]))
	_, stated = v.CompositeStatement()
	assert.True(t, stated)

	require.NoError(t, v.SetCompositeLinked(true))
	got, _ = v.Composite()
	for i := range got {
		assert.Equal(t, manual(2, 4), got[i].Recipe, "layer %d takes the recipe in hand", i)
	}
	_, stated = v.CompositeStatement()
	assert.False(t, stated)

	require.NoError(t, v.SetPlane(1))
	assert.False(t, v.IsComposite())
	_, ok := v.Composite()
	assert.False(t, ok)
	assert.Error(t, v.SetCompositeLinked(true), "no composite")
}

// The statement of an unlinked composite is drawn with the legend, as a
// label a scene finds by its text; a linked one draws the legend alone.
func TestTheUnlinkedStatementIsDrawn(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(bandsImage(t, 120, 80, imaging.SampleKindU16))
	require.NoError(t, v.SetComposite(layersOf([]int{0, 1}, []display.Recipe{manual(0, 30), manual(0, 300)}, display.DefaultLayerColors(2)), false))
	for range 2 {
		v.Render(500, 400)
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	assert.Contains(t, v.readoutText(), "×", "the readout line is drawn")
	assert.True(t, strings.HasPrefix(v.legendText(CompositeLayer{Plane: 1, Layer: display.Layer{Recipe: manual(0, 300)}}), "green 0–300"))
}

// zscale limits are found at once for an image in memory and off the frame
// for a source, reading its tiles; both equal display.ZScaleLimits over the
// same plane, and the limits in force stand while they are pending.
func TestZScaleLimitsAreFoundAsPercentilesAre(t *testing.T) {
	img := bandsImage(t, 1300, 900, imaging.SampleKindF32)
	zs := display.Recipe{Limits: display.Limits{Source: display.LimitsSourceZScale}}
	want, ok := display.ZScaleLimits(img.Plane(1), 0, 0)
	require.True(t, ok)

	mem := New(c.NewWidgetIdStack(), "", Options{})
	defer mem.Close()
	mem.SetImage(img)
	require.NoError(t, mem.SetPlane(1))
	require.NoError(t, mem.SetRecipe(zs))
	assert.False(t, mem.LimitsPending(), "an image in memory is sampled at once")
	assert.Equal(t, want, mem.Recipe().Limits)
	assert.Equal(t, display.ZScaleContrast, mem.Recipe().Limits.ZScaleContrast, "the contrast used is recorded")
	assert.EqualValues(t, display.ZScaleSamples, mem.Recipe().Limits.ZScaleSamples)

	store, err := tiles.FromImage(context.Background(), img, tiles.Options{})
	require.NoError(t, err)
	st := New(c.NewWidgetIdStack(), "", Options{})
	defer st.Close()
	require.NoError(t, st.SetSource(StoreSource(store), false))
	layers := layersOf([]int{0, 1, 2}, []display.Recipe{manual(0, 1), manual(0, 1), manual(0, 1)}, display.DefaultLayerColors(3))
	require.NoError(t, st.SetComposite(layers, false))
	require.NoError(t, st.SetLayerInHand(1))
	require.NoError(t, st.SetRecipe(zs))
	assert.True(t, st.LimitsPending(), "a source is sampled off the frame")
	assert.Equal(t, manual(0, 1).Limits, st.Recipe().Limits, "the limits in force stand meanwhile")
	deadline := time.Now().Add(10 * time.Second)
	for st.LimitsPending() && time.Now().Before(deadline) {
		st.adoptHistograms()
		time.Sleep(time.Millisecond)
	}
	require.False(t, st.LimitsPending())
	assert.Equal(t, want, st.Recipe().Limits, "the same limits from the store's tiles")
	got, _ := st.Composite()
	assert.Equal(t, manual(0, 1), got[0].Recipe, "unlinked: the layer in hand alone")

	rendered := New(c.NewWidgetIdStack(), "", Options{})
	defer rendered.Close()
	require.NoError(t, rendered.SetSource(newPatternSource(100, 100, 0, 0), false))
	require.NoError(t, rendered.SetPlane(0))
	assert.ErrorContains(t, rendered.SetRecipe(zs), "drawn")
	require.NoError(t, rendered.SetAsStored())
	assert.ErrorContains(t, rendered.SetRecipe(zs), "single plane")
	big := zs
	big.Limits.ZScaleSamples = MaxZScaleSamples + 1
	require.NoError(t, mem.SetPlane(0))
	assert.ErrorContains(t, mem.SetRecipe(big), "at most")
}

// The clipped shares are a count over the samples for a kind with a code
// per bin, and are not stated until the histogram is counted.
func TestClippedSharesAreACountOverTheSamples(t *testing.T) {
	img := rampImage(t, 1000, 30, 999, true)
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	v.SetImage(img)
	require.NoError(t, v.SetRecipe(manual(100, 899.5)))
	_, ok := v.ClippedShares()
	assert.False(t, ok, "nothing before the histogram is counted")
	waitForHistogram(t, v)
	share, ok := v.ClippedShares()
	require.True(t, ok)
	var below, above, n int
	p := img.Plane(0)
	for row := range p.Height() {
		for col := range p.Width() {
			x, _ := p.ValueAt(col, row)
			n++
			switch {
			case x < 100:
				below++
			case x > 899.5:
				above++
			}
		}
	}
	assert.True(t, share.Exact)
	assert.InDelta(t, float64(below)/float64(n), share.Below, 1e-12)
	assert.InDelta(t, float64(above)/float64(n), share.Above, 1e-12)
	assert.Equal(t, "clipped 10.0 % below, 10.0 % above", ClippedText(share))
	assert.Equal(t, "clipped about 0.03 % below, about 0 % above", ClippedText(display.ClippedShare{Below: 0.0003}))
	assert.Equal(t, "0.0004 %", formatShare(0.000004))
	assert.Equal(t, "99.99 %", formatShare(0.9999))
	assert.Equal(t, "100 %", formatShare(1))
}
