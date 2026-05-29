//go:build llm_generated_opus47

package widgets

// =============================================================================
// DEMO: scrollingTexture — procedural chirp pattern
// =============================================================================
//
// Milestone 2 end-to-end exercise for the scrollingTexture widget. Generates
// a 512×256 RGBA buffer with a sinusoidal-chirp pattern, maps intensities
// through a 5-stop Viridis-like palette, and pushes the full buffer into the
// widget on the first Render call. Subsequent calls push nothing (newCount=0)
// so the texture is static — TestDriver's 8-frame settle captures a stable
// PNG.
//
// The colormap lives inline here. The generic Go `colormap` package called
// out in ADR-0009 is a follow-up milestone; this demo only exercises the
// Rust widget's texture lifecycle and split-UV draw.
//
// =============================================================================

import (
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

const (
	scrollingTextureDemoWidth  uint32 = 512
	scrollingTextureDemoHeight uint32 = 256
)

// scrollingTextureDemoState carries the per-window chirp buffer plus
// the one-shot flag that gates the initial full push: streaming
// widgets accept new columns only when newCount > 0, so the buffer
// is uploaded on the first frame after Mount and a redraw-only call
// (newCount=0) fires every subsequent frame.
type scrollingTextureDemoState struct {
	built  bool
	buffer []uint32
}

func init() {
	registry.Register(registry.Demo{
		Name:        "scrolling-texture",
		Category:    "Graphics & canvas",
		Title:       "scrolling texture",
		Stage:       [2]float32{600, 320},
		Kind:        registry.DemoKindUX,
		Description: "Streaming-texture demo (ADR-0009): a generated scrolling texture demonstrating the zero-copy upload path.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &scrollingTextureDemoState{
				buffer: make([]uint32, scrollingTextureDemoWidth*scrollingTextureDemoHeight),
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoScrollingTexture(ids, state.(*scrollingTextureDemoState))
		},
		SourceFunc: demoScrollingTexture,
	})
}

// viridisLUT — 5-stop approximation of Matplotlib's viridis (RGBA, big-endian hex).
var viridisLUT = [5]uint32{
	0x440154ff, // 0.00 — dark purple
	0x3b528bff, // 0.25 — deep blue
	0x21918cff, // 0.50 — teal
	0x5ec962ff, // 0.75 — green
	0xfde725ff, // 1.00 — yellow
}

// viridisRGBA maps an intensity in [0, 1] to a packed RGBA u32 via linear
// interpolation between neighbouring LUT stops.
func viridisRGBA(v float64) uint32 {
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	t := v * 4.0
	idx := int(t)
	if idx >= 4 {
		return viridisLUT[4]
	}
	frac := t - float64(idx)
	a := viridisLUT[idx]
	b := viridisLUT[idx+1]
	mix := func(x, y uint32) uint32 {
		return uint32(float64(x)*(1-frac) + float64(y)*frac)
	}
	ar, ag, ab, aa := (a>>24)&0xff, (a>>16)&0xff, (a>>8)&0xff, a&0xff
	br, bg, bb, ba := (b>>24)&0xff, (b>>16)&0xff, (b>>8)&0xff, b&0xff
	return (mix(ar, br) << 24) | (mix(ag, bg) << 16) | (mix(ab, bb) << 8) | mix(aa, ba)
}

// buildScrollingTextureBuffer fills buf with a chirp pattern: spatial
// frequency along the row axis rises from left to right, producing a
// visually-legible "spectrogram-like" texture.
func buildScrollingTextureBuffer(buf []uint32) {
	w := int(scrollingTextureDemoWidth)
	h := int(scrollingTextureDemoHeight)
	for col := 0; col < w; col++ {
		tPhase := float64(col) / float64(w)
		freq := 3.0 + tPhase*8.0
		for row := 0; row < h; row++ {
			norm := float64(row) / float64(h)
			intensity := math.Sin(norm*freq*math.Pi*2+tPhase*4)*0.5 + 0.5
			buf[col*h+row] = viridisRGBA(intensity)
		}
	}
}

// demoScrollingTexture is the per-frame render body. On the first
// call (per window instance) it builds the chirp buffer and pushes
// all columns; afterwards it issues a redraw-only call (newCount=0).
// head stays at 0 because (0 + W) mod W = 0, so the split-UV draw
// degenerates to a single image call that spans the full texture.
// emptyColumns is a shared zero-length buffer used on redraw-only frames.
// A non-nil empty slice is load-bearing: PutUint32SliceArg writes the nil
// sentinel 0xFFFFFFFF for a nil slice, and the Rust read_plain_u32h reader
// treats that as a literal length (4 billion) and tries to allocate.
var emptyColumns = []uint32{}

func demoScrollingTexture(ids *c.WidgetIdStack, st *scrollingTextureDemoState) {
	var newCount uint32
	var payload []uint32
	if !st.built {
		buildScrollingTextureBuffer(st.buffer)
		st.built = true
		newCount = scrollingTextureDemoWidth
		payload = st.buffer
	} else {
		newCount = 0
		payload = emptyColumns
	}
	c.ScrollingTexture(
		ids.PrepareStr("scrolling-texture-demo"),
		scrollingTextureDemoWidth,
		scrollingTextureDemoHeight,
		0, // orientation: ScrollLeft
		0, // filter: Nearest
		0, // head: stays 0 after the one-shot fill
		newCount,
		payload,
		0, // displayWidthPx: 0 = use width_slots (historical default)
		0, // displayHeightPx: 0 = use height_slots
	).Send()
}
