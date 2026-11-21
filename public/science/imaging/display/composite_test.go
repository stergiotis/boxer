package display

import (
	"fmt"
	"image"
	"math"
	"sync"
	"testing"

	"encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// drawPlane is a random plane of a kind: samples spread over a range of the
// kind, some NaN in a float plane, and when declare is set a declared
// invalid value that occurs at some pixels.
func drawPlane(t *rapid.T, label string, kind imaging.SampleKindE, w, h int, declare bool) imaging.Plane {
	n := w * h
	u := func(i int) float64 { return rapid.Float64Range(0, 1).Draw(t, fmt.Sprintf("%s-%d", label, i)) }
	invAt := map[int]bool{}
	if declare {
		for range 1 + n/8 {
			invAt[rapid.IntRange(0, n-1).Draw(t, label+"-inv")] = true
		}
	}
	var p imaging.Plane
	var err error
	var inv float64
	switch kind {
	case imaging.SampleKindU8:
		s := make([]uint8, n)
		inv = 7
		for i := range s {
			s[i] = uint8(u(i) * 255)
			if invAt[i] {
				s[i] = uint8(inv)
			}
		}
		p, err = imaging.NewPlaneU8(w, h, s)
	case imaging.SampleKindU16:
		s := make([]uint16, n)
		inv = 65535
		for i := range s {
			s[i] = uint16(u(i) * 60000)
			if invAt[i] {
				s[i] = uint16(inv)
			}
		}
		p, err = imaging.NewPlaneU16(w, h, s)
	case imaging.SampleKindI16:
		s := make([]int16, n)
		inv = -32768
		for i := range s {
			s[i] = int16(u(i)*60000 - 30000)
			if invAt[i] {
				s[i] = int16(inv)
			}
		}
		p, err = imaging.NewPlaneI16(w, h, s)
	case imaging.SampleKindI32:
		s := make([]int32, n)
		inv = -1
		for i := range s {
			s[i] = int32(u(i) * 2e6)
			if invAt[i] {
				s[i] = int32(inv)
			}
		}
		p, err = imaging.NewPlaneI32(w, h, s)
	case imaging.SampleKindF32:
		s := make([]float32, n)
		inv = -9999
		for i := range s {
			x := u(i)
			s[i] = float32(x*2000 - 1000)
			if x < 0.05 {
				s[i] = float32(math.NaN())
			}
			if invAt[i] {
				s[i] = float32(inv)
			}
		}
		p, err = imaging.NewPlaneF32(w, h, s)
	case imaging.SampleKindF64:
		s := make([]float64, n)
		inv = 1e300
		for i := range s {
			x := u(i)
			s[i] = x*2e-3 - 1e-3
			if x < 0.05 {
				s[i] = math.NaN()
			}
			if invAt[i] {
				s[i] = inv
			}
		}
		p, err = imaging.NewPlaneF64(w, h, s)
	}
	require.NoError(t, err)
	if rapid.Bool().Draw(t, label+"-transform") {
		p, err = p.WithTransform(imaging.ValueTransform{Scale: -0.5, Offset: 100})
		require.NoError(t, err)
	}
	if declare {
		p, err = p.WithInvalid(inv)
		require.NoError(t, err)
	}
	return p
}

// drawRecipe is a layer recipe whose limits lie somewhere about the plane's
// data, wider or narrower than it.
func drawRecipe(t *rapid.T, label string, p imaging.Plane) Recipe {
	lo, hi := -1.0, 1.0
	if l, ok := DataLimits(p); ok {
		lo, hi = l.Lo, l.Hi
	}
	span := hi - lo
	a := lo + span*rapid.Float64Range(-0.3, 0.8).Draw(t, label+"-lo")
	return Recipe{
		Limits:   Limits{Lo: a, Hi: a + span*rapid.Float64Range(0.05, 1.5).Draw(t, label+"-span")},
		Transfer: rapid.SampledFrom(AllTransfers).Draw(t, label+"-transfer"),
		Param:    rapid.SampledFrom([]float64{0, 0.02, 3}).Draw(t, label+"-param"),
		Invert:   rapid.Bool().Draw(t, label+"-invert"),
	}
}

// planeLevels is each pixel's 8-bit level through the single-plane mapper
// with the grey palette — the established kernel — and whether the pixel
// is valid under the plane's own declaration. For a kind with codes the
// level is also held to the direct formula exactly, for a binned one to
// within one level.
func planeLevels(t require.TestingT, r Recipe, p imaging.Plane) (levels []int, valid []bool) {
	m, err := CompilePlaneFor(r, PlaneSpecOf(p), nil)
	require.NoError(t, err)
	dst := make([]uint32, p.Width()*p.Height())
	require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)))
	_, _, exact := p.Kind().Codes()
	levels, valid = make([]int, len(dst)), make([]bool, len(dst))
	for i, c := range dst {
		v, ok := p.ValueAt(i%p.Width(), i/p.Width())
		valid[i] = ok
		levels[i] = level(c)
		if !ok {
			continue
		}
		want := level(direct(r, GrayPalette(), v))
		if exact {
			require.Equal(t, want, levels[i], "%s level of %v", p.Kind(), v)
		} else if d := levels[i] - want; d < -1 || d > 1 {
			require.Failf(t, "level off", "%s level of %v: %d, direct %d", p.Kind(), v, levels[i], want)
		}
	}
	return levels, valid
}

// directComposite is the composite of the layers over the planes, pixel by
// pixel, from each layer's levels: the reference the kernel is held to.
func directComposite(t require.TestingT, layers []Layer, planes []imaging.Plane, rect image.Rectangle) []uint32 {
	w := 0
	type lv struct {
		levels []int
		valid  []bool
	}
	per := make([]lv, len(layers))
	for i, l := range layers {
		if l.Hidden {
			continue
		}
		w = planes[i].Width()
		per[i].levels, per[i].valid = planeLevels(t, l.Recipe, planes[i])
	}
	out := make([]uint32, 0, rect.Dx()*rect.Dy())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			var r, g, b int
			invalid := false
			for i, l := range layers {
				if l.Hidden {
					continue
				}
				k := y*w + x
				if !per[i].valid[k] {
					invalid = true
				}
				c := l.Color.Pixel()
				lvl := per[i].levels[k]
				if c>>24 != 0 {
					r += lvl
				}
				if c>>16&0xff != 0 {
					g += lvl
				}
				if c>>8&0xff != 0 {
					b += lvl
				}
			}
			if invalid {
				out = append(out, 0)
				continue
			}
			out = append(out, uint32(min(r, 255))<<24|uint32(min(g, 255))<<16|uint32(min(b, 255))<<8|0xff)
		}
	}
	return out
}

func specsOf(planes []imaging.Plane) []PlaneSpec {
	specs := make([]PlaneSpec, len(planes))
	for i, p := range planes {
		specs[i] = PlaneSpecOf(p)
	}
	return specs
}

// The composite of one to eight layers of any kinds, mixed, with NaN and
// declared invalid samples, hidden layers, every colour and saturation, is
// the per-pixel sum of what each layer's plane mapper gives.
func TestCompositeMatchesTheDirectEvaluation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		w, h := rapid.IntRange(1, 9).Draw(t, "w"), rapid.IntRange(1, 7).Draw(t, "h")
		n := rapid.IntRange(1, MaxLayers).Draw(t, "layers")
		layers := make([]Layer, n)
		planes := make([]imaging.Plane, n)
		for i := range n {
			label := fmt.Sprintf("layer%d", i)
			kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, label+"-kind")
			planes[i] = drawPlane(t, label, kind, w, h, rapid.Bool().Draw(t, label+"-declare"))
			layers[i] = Layer{
				Recipe: drawRecipe(t, label, planes[i]),
				Color:  rapid.SampledFrom(AllLayerColors).Draw(t, label+"-color"),
				Hidden: rapid.Float64Range(0, 1).Draw(t, label+"-hidden") < 0.2,
			}
		}
		m, err := CompileComposite(layers, specsOf(planes))
		require.NoError(t, err)
		x0, y0 := rapid.IntRange(0, w-1).Draw(t, "x0"), rapid.IntRange(0, h-1).Draw(t, "y0")
		rect := image.Rect(x0, y0, rapid.IntRange(x0+1, w).Draw(t, "x1"), rapid.IntRange(y0+1, h).Draw(t, "y1"))
		mapped := make([]uint32, rect.Dx()*rect.Dy())
		require.NoError(t, m.Map(mapped, planes, rect))
		want := directComposite(t, layers, planes, rect)
		for i := range want {
			if want[i] != mapped[i] {
				t.Fatalf("pixel %d of %v: %08x, direct %08x", i, rect, mapped[i], want[i])
			}
		}
	})
}

// Every kind, alone and in white, is the plane in grey: the composite's
// level is the plane mapper's.
func TestAWhiteLayerOfEveryKindIsThePlaneInGrey(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		p := drawPlane(t, "p", kind, 16, 4, rapid.Bool().Draw(t, "declare"))
		r := drawRecipe(t, "r", p)
		m, err := CompileComposite([]Layer{{Recipe: r, Color: LayerColorWhite}}, []PlaneSpec{PlaneSpecOf(p)})
		require.NoError(t, err)
		got := make([]uint32, 64)
		require.NoError(t, m.Map(got, []imaging.Plane{p}, whole(p)))
		grey, err := CompilePlaneFor(r, PlaneSpecOf(p), nil)
		require.NoError(t, err)
		want := make([]uint32, 64)
		require.NoError(t, grey.Map(want, p, imaging.Plane{}, whole(p)))
		require.Equal(t, want, got)
	})
}

// Three layers red, green and blue under one recipe are, bit for bit, the
// colour mapping of the same planes as an RGB image (ADR-0401 §SD1), for
// every kind the colour mapper takes, with declared invalid samples.
func TestARedGreenBlueCompositeIsTheColourMapping(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom([]imaging.SampleKindE{imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16}).Draw(t, "kind")
		declare := rapid.Bool().Draw(t, "declare")
		transform := rapid.Bool().Draw(t, "transform")
		w, h := rapid.IntRange(1, 12).Draw(t, "w"), rapid.IntRange(1, 6).Draw(t, "h")
		planes := make([]imaging.Plane, 3)
		for i := range planes {
			label := fmt.Sprintf("plane%d", i)
			// One transform for all three: the colour mapper has one.
			p := drawPlane(t, label, kind, w, h, declare)
			var err error
			if transform {
				p, err = p.WithTransform(imaging.ValueTransform{Scale: 0.25, Offset: -7})
			} else {
				p, err = p.WithTransform(imaging.IdentityTransform)
			}
			require.NoError(t, err)
			planes[i] = p
		}
		img, err := imaging.NewImage(imaging.ColorModelRGB, planes, imaging.Provenance{})
		require.NoError(t, err)
		r := drawRecipe(t, "r", planes[0])
		cm, err := CompileColorFor(r, PlaneSpecOf(planes[0]))
		require.NoError(t, err)
		want := make([]uint32, w*h)
		require.NoError(t, cm.Map(want, img, whole(planes[0])))

		c := NewComposite(3, r)
		require.Equal(t, []LayerColorE{LayerColorRed, LayerColorGreen, LayerColorBlue}, []LayerColorE{c[0].Color, c[1].Color, c[2].Color})
		m, err := CompileComposite(c, specsOf(planes))
		require.NoError(t, err)
		got := make([]uint32, w*h)
		require.NoError(t, m.Map(got, planes, whole(planes[0])))
		require.Equal(t, want, got)
	})
}

// uniformU16 is a w × h unsigned 16-bit plane holding v everywhere.
func uniformU16(t require.TestingT, w, h int, v uint16) imaging.Plane {
	s := make([]uint16, w*h)
	for i := range s {
		s[i] = v
	}
	p, err := imaging.NewPlaneU16(w, h, s)
	require.NoError(t, err)
	return p
}

func TestCompositeColoursSaturationAndHiddenLayers(t *testing.T) {
	r := Recipe{Limits: Limits{Lo: 0, Hi: 255}}
	half := uniformU16(t, 2, 2, 128)
	full := uniformU16(t, 2, 2, 255)
	for _, c := range AllLayerColors {
		m, err := CompileComposite([]Layer{{Recipe: r, Color: c}}, []PlaneSpec{PlaneSpecOf(half)})
		require.NoError(t, err)
		dst := make([]uint32, 4)
		require.NoError(t, m.Map(dst, []imaging.Plane{half}, whole(half)))
		want := c.Pixel()&0x80808000 | 0xff
		assert.Equal(t, want, dst[0], "%s at level 128", c)
	}

	// Eight full-scale white layers stay at full scale; yellow and red at 128
	// each give red 255, green 128.
	eight := NewComposite(8, r).WithRecipe(r)
	planes := make([]imaging.Plane, 8)
	for i := range eight {
		eight[i].Color = LayerColorWhite
		planes[i] = full
	}
	m, err := CompileComposite(eight, specsOf(planes))
	require.NoError(t, err)
	dst := make([]uint32, 4)
	require.NoError(t, m.Map(dst, planes, whole(full)))
	assert.Equal(t, uint32(0xffffffff), dst[0])

	m, err = CompileComposite([]Layer{{Recipe: r, Color: LayerColorYellow}, {Recipe: r, Color: LayerColorRed}}, specsOf([]imaging.Plane{half, half}))
	require.NoError(t, err)
	require.NoError(t, m.Map(dst, []imaging.Plane{half, half}, whole(half)))
	assert.Equal(t, uint32(0xff8000ff), dst[0])

	// Five to eight layers each at 100 in red: saturation across the cut
	// that happens after the fourth.
	hundred := uniformU16(t, 2, 2, 100)
	for n := 1; n <= MaxLayers; n++ {
		layers := make([]Layer, n)
		ps := make([]imaging.Plane, n)
		for i := range layers {
			layers[i], ps[i] = Layer{Recipe: r, Color: LayerColorMagenta}, hundred
		}
		m, err = CompileComposite(layers, specsOf(ps))
		require.NoError(t, err)
		require.NoError(t, m.Map(dst, ps, whole(hundred)))
		v := uint32(min(100*n, 255))
		assert.Equal(t, v<<24|v<<8|0xff, dst[0], "%d layers", n)
	}

	// A hidden layer adds nothing and its invalid samples do not count; all
	// hidden is opaque black.
	invalid, err := half.WithInvalid(128)
	require.NoError(t, err)
	layers := []Layer{{Recipe: r, Color: LayerColorGreen}, {Recipe: r, Color: LayerColorRed, Hidden: true}}
	m, err = CompileComposite(layers, specsOf([]imaging.Plane{half, invalid}))
	require.NoError(t, err)
	require.NoError(t, m.Map(dst, []imaging.Plane{half, invalid}, whole(half)))
	assert.Equal(t, uint32(0x008000ff), dst[0])
	require.NoError(t, m.Map(dst, []imaging.Plane{half, {}}, whole(half)), "a hidden layer's plane is not read")
	assert.Equal(t, uint32(0x008000ff), dst[0])
	layers[1].Hidden = false
	m, err = CompileComposite(layers, specsOf([]imaging.Plane{half, invalid}))
	require.NoError(t, err)
	require.NoError(t, m.Map(dst, []imaging.Plane{half, invalid}, whole(half)))
	assert.Equal(t, uint32(0), dst[0], "a shown invalid layer makes the pixel invalid")

	layers[0].Hidden, layers[1].Hidden = true, true
	m, err = CompileComposite(layers, specsOf([]imaging.Plane{half, invalid}))
	require.NoError(t, err)
	require.NoError(t, m.Map(dst, []imaging.Plane{half, invalid}, whole(half)))
	assert.Equal(t, []uint32{0xff, 0xff, 0xff, 0xff}, dst)
	require.NoError(t, m.Map(dst, []imaging.Plane{{}, {}}, image.Rect(0, 0, 2, 2)))
	assert.Equal(t, []uint32{0xff, 0xff, 0xff, 0xff}, dst)
}

func TestACompositeThatCannotBeShownIsRefused(t *testing.T) {
	r := Recipe{Limits: Limits{Lo: 0, Hi: 255}}
	p := uniformU16(t, 3, 2, 9)
	spec := PlaneSpecOf(p)
	nine := make([]Layer, MaxLayers+1)
	for i := range nine {
		nine[i] = Layer{Recipe: r, Color: LayerColorWhite}
	}
	specs := func(n int) []PlaneSpec {
		s := make([]PlaneSpec, n)
		for i := range s {
			s[i] = spec
		}
		return s
	}
	_, err := CompileComposite(nine, specs(9))
	assert.Error(t, err, "nine layers")
	_, err = CompileComposite(nil, nil)
	assert.Error(t, err, "no layers")
	_, err = CompileComposite([]Layer{{Recipe: r}}, specs(1))
	assert.Error(t, err, "no colour")
	_, err = CompileComposite([]Layer{{Recipe: Recipe{Limits: Limits{Lo: 0, Hi: 255}, Palette: "batlow"}, Color: LayerColorRed}}, specs(1))
	assert.Error(t, err, "a palette")
	_, err = CompileComposite([]Layer{{Recipe: Recipe{Limits: Limits{Lo: 0, Hi: 255}, FlagClipped: true}, Color: LayerColorRed}}, specs(1))
	assert.Error(t, err, "a clipping flag")
	_, err = CompileComposite([]Layer{{Recipe: Recipe{Limits: Limits{Lo: 1, Hi: 1}}, Color: LayerColorRed}}, specs(1))
	assert.Error(t, err, "no range")
	_, err = CompileComposite([]Layer{{Recipe: Recipe{Limits: Limits{Lo: 1, Hi: 1}}, Color: LayerColorRed, Hidden: true}}, specs(1))
	assert.Error(t, err, "a hidden layer is validated too")
	_, err = CompileComposite([]Layer{{Recipe: r, Color: LayerColorRed}}, specs(2))
	assert.Error(t, err, "spec count")
	_, err = CompileComposite([]Layer{{Recipe: r, Color: LayerColorRed}}, []PlaneSpec{{Kind: imaging.SampleKindU8, Transform: imaging.IdentityTransform, Invalid: 300, HasInvalid: true}})
	assert.Error(t, err, "an invalid value the kind cannot hold")
	_, err = CompileComposite([]Layer{{Recipe: r, Color: LayerColorRed}}, []PlaneSpec{{Transform: imaging.IdentityTransform}})
	assert.Error(t, err, "no kind")

	two := []Layer{{Recipe: r, Color: LayerColorGreen}, {Recipe: r, Color: LayerColorMagenta}}
	m, err := CompileComposite(two, specs(2))
	require.NoError(t, err)
	dst := make([]uint32, 6)
	assert.Error(t, m.Map(dst, []imaging.Plane{p}, whole(p)), "plane count")
	other := uniformU16(t, 2, 3, 9)
	assert.Error(t, m.Map(dst, []imaging.Plane{p, other}, whole(p)), "size")
	u8, err := imaging.NewPlaneU8(3, 2, make([]uint8, 6))
	require.NoError(t, err)
	assert.Error(t, m.Map(dst, []imaging.Plane{p, u8}, whole(p)), "kind")
	declared, err := p.WithInvalid(9)
	require.NoError(t, err)
	assert.Error(t, m.Map(dst, []imaging.Plane{p, declared}, whole(p)), "an invalid declaration the spec does not make")
	assert.Error(t, m.Map(dst, []imaging.Plane{p, {}}, whole(p)), "a shown layer's plane is missing")
	assert.Error(t, m.Map(dst[:5], []imaging.Plane{p, p}, whole(p)), "destination size")
	assert.Error(t, m.Map(dst, []imaging.Plane{p, p}, image.Rect(1, 0, 4, 2)), "region outside")

	dm, err := CompileComposite(two, []PlaneSpec{PlaneSpecOf(declared), PlaneSpecOf(declared)})
	require.NoError(t, err)
	require.NoError(t, dm.Map(dst, []imaging.Plane{declared, p}, whole(p)), "a plane that declares none is drawn under the spec's declaration")
	assert.Equal(t, uint32(0), dst[0])
}

func TestLinkingAndTheDefaultColours(t *testing.T) {
	assert.Equal(t, []LayerColorE{LayerColorWhite}, DefaultLayerColors(1))
	assert.Equal(t, []LayerColorE{LayerColorGreen, LayerColorMagenta}, DefaultLayerColors(2))
	assert.Equal(t, []LayerColorE{LayerColorRed, LayerColorGreen, LayerColorBlue}, DefaultLayerColors(3))
	assert.Equal(t, AllLayerColors, DefaultLayerColors(7))
	assert.Equal(t, append(append([]LayerColorE{}, AllLayerColors...), LayerColorRed), DefaultLayerColors(8))
	assert.Empty(t, DefaultLayerColors(0))

	r := Recipe{Limits: Limits{Lo: 0, Hi: 100}, Palette: "batlow", FlagClipped: true}
	c := NewComposite(4, r)
	require.NoError(t, c.Validate(), "the palette and the flag are cleared")
	assert.True(t, c.IsLinked())
	c[2].Recipe.Transfer = TransferLog
	assert.False(t, c.IsLinked())
	c[2].Hidden = true
	assert.True(t, c.IsLinked(), "a hidden layer's recipe does not unlink")
	c[2].Hidden = false
	other := Recipe{Limits: Limits{Lo: -5, Hi: 5}, Invert: true}
	linked := c.WithRecipe(other)
	assert.True(t, linked.IsLinked())
	assert.False(t, c.IsLinked(), "WithRecipe leaves the composite it is called on alone")
	for i := range linked {
		assert.Equal(t, other, linked[i].Recipe)
		assert.Equal(t, c[i].Color, linked[i].Color)
	}

	data, err := json.Marshal(linked)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"cyan"`)
	var back Composite
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, linked, back)
	var col LayerColorE
	assert.Error(t, col.UnmarshalText([]byte("orange")))
	assert.Error(t, col.UnmarshalText([]byte("")))
	_, err = LayerColorE(0).MarshalText()
	assert.Error(t, err)
	for _, x := range AllLayerColors {
		text, err := x.MarshalText()
		require.NoError(t, err)
		require.NoError(t, col.UnmarshalText(text))
		assert.Equal(t, x, col)
	}
	assert.Equal(t, uint32(0xff00ffff), LayerColorMagenta.Pixel())
	assert.Equal(t, uint32(0x00ffffff), LayerColorCyan.Pixel())
}

// One compiled mapper maps the tiles of a plane from several goroutines at
// once and gives what one goroutine gives.
func TestACompositeMapsFromSeveralGoroutines(t *testing.T) {
	const w, h, tile = 96, 64, 16
	planes := []imaging.Plane{benchPlane(t, imaging.SampleKindU16, w, h, 1), benchPlane(t, imaging.SampleKindF32, w, h, 2), benchPlane(t, imaging.SampleKindI16, w, h, 3),
		benchPlane(t, imaging.SampleKindF64, w, h, 4), benchPlane(t, imaging.SampleKindU8, w, h, 5)}
	layers := make([]Layer, len(planes))
	for i, p := range planes {
		layers[i] = Layer{Recipe: DefaultPlaneRecipe(p), Color: AllLayerColors[i]}
	}
	m, err := CompileComposite(layers, specsOf(planes))
	require.NoError(t, err)
	want := make([]uint32, w*h)
	require.NoError(t, m.Map(want, planes, image.Rect(0, 0, w, h)))
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for ty := 0; ty < h; ty += tile {
		for tx := 0; tx < w; tx += tile {
			wg.Go(func() {
				rect := image.Rect(tx, ty, tx+tile, ty+tile)
				dst := make([]uint32, tile*tile)
				for range 20 {
					if err := m.Map(dst, planes, rect); err != nil {
						errs <- err
						return
					}
				}
				for y := range tile {
					for x := range tile {
						if dst[y*tile+x] != want[(ty+y)*w+tx+x] {
							errs <- fmt.Errorf("tile %v pixel %d,%d differs", rect, x, y)
							return
						}
					}
				}
			})
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// benchPlane is a deterministic plane of a kind with a structured field and
// a few invalid samples in the float kinds.
func benchPlane(t require.TestingT, kind imaging.SampleKindE, w, h int, seed uint64) imaging.Plane {
	s := zscaleStream{state: seed}
	n := w * h
	var p imaging.Plane
	var err error
	switch kind {
	case imaging.SampleKindU8:
		v := make([]uint8, n)
		for i := range v {
			v[i] = uint8(s.uniform() * 256)
		}
		p, err = imaging.NewPlaneU8(w, h, v)
	case imaging.SampleKindU16:
		v := make([]uint16, n)
		for i := range v {
			v[i] = uint16(s.uniform() * 65536)
		}
		p, err = imaging.NewPlaneU16(w, h, v)
	case imaging.SampleKindI16:
		v := make([]int16, n)
		for i := range v {
			v[i] = int16(s.uniform()*65536 - 32768)
		}
		p, err = imaging.NewPlaneI16(w, h, v)
	case imaging.SampleKindI32:
		v := make([]int32, n)
		for i := range v {
			v[i] = int32(s.uniform() * 1e9)
		}
		p, err = imaging.NewPlaneI32(w, h, v)
	case imaging.SampleKindF32:
		v := make([]float32, n)
		for i := range v {
			v[i] = float32(s.uniform()*1000 - 100)
			if i%997 == 0 {
				v[i] = float32(math.NaN())
			}
		}
		p, err = imaging.NewPlaneF32(w, h, v)
	case imaging.SampleKindF64:
		v := make([]float64, n)
		for i := range v {
			v[i] = s.uniform()
			if i%991 == 0 {
				v[i] = math.NaN()
			}
		}
		p, err = imaging.NewPlaneF64(w, h, v)
	}
	require.NoError(t, err)
	return p
}

// BenchmarkCompositeTile maps one 512 × 512 tile of 1, 3 and 8 layers of
// unsigned 16-bit and of 32-bit float planes, each layer its own plane under
// a log stretch.
func BenchmarkCompositeTile(b *testing.B) {
	const size = 512
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindU16, imaging.SampleKindF32} {
		var planes []imaging.Plane
		for i := range MaxLayers {
			planes = append(planes, benchPlane(b, kind, size, size, uint64(i+1)))
		}
		for _, n := range []int{1, 3, 8} {
			b.Run(fmt.Sprintf("%s/%d", kind, n), func(b *testing.B) {
				layers := NewComposite(n, Recipe{})
				for i := range layers {
					layers[i].Recipe = DefaultPlaneRecipe(planes[i])
					layers[i].Recipe.Transfer = TransferLog
				}
				m, err := CompileComposite(layers, specsOf(planes[:n]))
				require.NoError(b, err)
				dst := make([]uint32, size*size)
				rect := image.Rect(0, 0, size, size)
				b.ReportAllocs()
				b.SetBytes(int64(size * size * n))
				for b.Loop() {
					if err := m.Map(dst, planes[:n], rect); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
