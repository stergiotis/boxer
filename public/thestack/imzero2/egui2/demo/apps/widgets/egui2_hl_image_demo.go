package widgets

// =============================================================================
// DEMO: image — RGBA8 widget, four fit modes, embedded + dynamic sources
// =============================================================================
//
// Exercises the Image widget end-to-end:
//   - One "embedded" asset (procedural 64×64 checker — stands in for a PNG
//     decoded from go:embed; static, contentVersion=1 forever).
//   - One "dynamic" bitmap (procedural 96×64 gradient that shifts each
//     frame so we exercise the contentVersion bump path through
//     ImageVersionTracker).
//   - All four fit modes side by side on the embedded asset.
//   - Hover position read-back via SendRespHoverPx + UnpackHoverRc.
//
// =============================================================================

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

const (
	imageEmbeddedW uint32 = 64
	imageEmbeddedH uint32 = 64
	imageDynamicW  uint32 = 96
	imageDynamicH  uint32 = 64
)

var (
	imageEmbeddedPixels []uint32
	imageDynamicPixels         = make([]uint32, imageDynamicW*imageDynamicH)
	imageDynamicVersion uint64 = 1
	imageDynamicFrame   uint32

	imageHoverRc uint64

	// imageVersionTracker is used only for the dynamic gradient — see
	// drawImageRow's comment. Key string must equal the PrepareStr key
	// passed into Image(), 1:1 with the widget id.
	imageVersionTracker = c.NewImageVersionTracker[string]()
)

// buildImageEmbedded fills a 64×64 RGBA buffer with an 8×8 checkerboard in
// teal/yellow plus a thin red border. Stands in for a decoded asset; built
// once and never mutated, so contentVersion stays at 1 forever.
func buildImageEmbedded() {
	if imageEmbeddedPixels != nil {
		return
	}
	w := int(imageEmbeddedW)
	h := int(imageEmbeddedH)
	imageEmbeddedPixels = make([]uint32, w*h)
	const teal = uint32(0x21918cff)
	const yellow = uint32(0xfde725ff)
	const red = uint32(0xff0000ff)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var px uint32
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				px = red
			} else if ((x>>3)+(y>>3))%2 == 0 {
				px = teal
			} else {
				px = yellow
			}
			imageEmbeddedPixels[y*w+x] = px
		}
	}
}

// rebuildImageDynamic refreshes the dynamic buffer with a horizontal
// gradient whose hue rotates with `imageDynamicFrame`. Bumps the content
// version so the tracker re-ships pixels.
func rebuildImageDynamic() {
	w := int(imageDynamicW)
	h := int(imageDynamicH)
	phase := imageDynamicFrame
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := uint32((x*255/w + int(phase)) & 0xff)
			g := uint32((y * 255 / h) & 0xff)
			b := uint32((255 - x*255/w + int(phase)*2) & 0xff)
			imageDynamicPixels[y*w+x] = (r << 24) | (g << 16) | (b << 8) | 0xff
		}
	}
	imageDynamicVersion++
	imageDynamicFrame = (imageDynamicFrame + 4) & 0xff
}

// drawImageRow draws one fit mode on the embedded asset and shows the
// fit's name above it. No version tracker here — each row is a distinct
// widget id with its own Rust-side texture cache entry; the tracker only
// pays off when the *same widget id* is shown repeatedly with unchanging
// content. For four sibling widgets sharing one logical asset, four
// first-frame uploads (16 KB each) is cheaper than reasoning about
// per-id keying.
func drawImageRow(ids *c.WidgetIdStack, idSuffix string, label string, fit c.FitE, fixedW uint32, fixedH uint32) {
	c.Label(label).Send()
	c.Image(
		ids.PrepareStr("img-embedded-"+idSuffix),
		imageEmbeddedW, imageEmbeddedH,
		1, // contentVersion — embedded asset, never changes
		uint8(fit),
		fixedW, fixedH,
		uint8(c.FilterNearestE),
		c.TintNoneRgba,
		imageEmbeddedPixels,
	).SendResp()
}

// demoImage is the body of the "image" demo registered in
// egui2_hl_image_demo.go's init(). Renders the four fit modes plus a
// dynamic frame and reports the hover (row, col).
func demoImage(ids *c.WidgetIdStack) {
	buildImageEmbedded()
	rebuildImageDynamic()

	c.Label("Embedded asset (64×64 checker), four fit modes:").Send()
	drawImageRow(ids, "native", "  Native (64×64 native pixels):", c.FitNativeE, 0, 0)
	drawImageRow(ids, "fixed", "  Fixed (160×80, distorts aspect):", c.FitFixedE, 160, 80)
	drawImageRow(ids, "aspect", "  AspectMax (fits inside 200×120):", c.FitAspectMaxE, 200, 120)
	drawImageRow(ids, "fill", "  FillRect (fills available width):", c.FitFillRectE, 0, 0)

	c.Separator().Send()
	c.Label("Dynamic bitmap (96×64, hue rotating each frame):").Send()
	dynPixels := imageVersionTracker.PixelsToSend("dynamic", imageDynamicVersion, imageDynamicPixels)
	flags := c.Image(
		ids.PrepareStr("img-dynamic"),
		imageDynamicW, imageDynamicH,
		imageDynamicVersion,
		uint8(c.FitAspectMaxE),
		300, 200,
		uint8(c.FilterLinearE),
		c.TintNoneRgba,
		dynPixels,
	).SendRespHoverPx(&imageHoverRc)

	row, col, hovered := c.UnpackHoverRc(imageHoverRc)
	if hovered {
		c.Label(fmt.Sprintf("  hover: row=%d col=%d  (%s)",
			row, col, clickedSummary(flags))).Send()
	} else {
		c.Label(fmt.Sprintf("  hover: (none)  (%s)", clickedSummary(flags))).Send()
	}
}

func clickedSummary(f c.ResponseFlagsE) (out string) {
	switch {
	case f.HasDoubleClicked():
		out = "double-clicked"
	case f.HasPrimaryClicked():
		out = "primary-clicked"
	case f.HasSecondaryClicked():
		out = "secondary-clicked"
	default:
		out = "no click"
	}
	return
}

func init() {
	registry.Register(registry.Demo{
		Name: "image", Category: "Graphics & canvas", Title: icons.IconFileImage + " image (RGBA8)",
		Stage:       [2]float32{780, 820},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Image widget showcase: embedded checker asset shown in all four Fit modes, plus a dynamic gradient driven by ImageVersionTracker. Hover position is read back in image-pixel space via SendRespHoverPx.",
		Render:      func(ids *c.WidgetIdStack) { demoImage(ids) },
	})
}
