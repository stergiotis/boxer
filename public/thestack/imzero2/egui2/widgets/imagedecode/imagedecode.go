// Package imagedecode decodes encoded image bytes into the RGBA8 pixel
// layout the egui2 image widget consumes: row-major, one uint32 per
// pixel, packed 0xRRGGBBAA.
//
// The bindings carry no encoded-bytes path — [bindings.Image] takes
// decoded pixels — so every caller that wants to show a PNG owns this
// conversion. This package is that conversion, and nothing else: it
// imports no bindings and draws nothing.
//
// # Formats
//
// PNG, JPEG and GIF, blank-imported so [image.Decode] auto-detects from
// the magic prefix. WebP / AVIF / BMP are deliberately absent — they add
// roughly a megabyte to every binary that links them, and the images
// that reach this package in practice are PNG screenshots and JPEG
// photos. A caller needing an exotic format can blank-import the decoder
// itself; [image.Decode] consults a process-wide registry, so the extra
// format becomes available here without a change to this file.
//
// # Bounds
//
// [DecodeRGBA8] resolves the header with [image.DecodeConfig] and
// rejects an oversized image *before* the full decode allocates. This is
// the difference between reading 40 KB and allocating 3.6 GB: PNG
// compresses uniform data so well that a 30000×30000 image is a small
// file. Any caller decoding bytes it did not itself produce should pass
// a real bound.
package imagedecode

import (
	"bytes"
	"image"

	// Format decoders: blank-imported so [image.Decode] and
	// [image.DecodeConfig] auto-detect from the magic prefix.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// DefaultMaxPixels bounds a decode at 64 megapixels — comfortably past
// any screenshot or photo, and far short of a decompression bomb. It is
// a pixel count, not a byte count, because the allocation this guards is
// 4 bytes per pixel regardless of how small the encoded form was.
const DefaultMaxPixels = 64 << 20

// DecodeRGBA8 decodes PNG / JPEG / GIF bytes into row-major 0xRRGGBBAA
// pixels sized widthPx × heightPx.
//
// maxPixels bounds widthPx*heightPx, checked against the header before
// the pixel buffer is allocated; pass [DefaultMaxPixels] unless the
// caller has a reason of its own, or zero to decode unbounded (only safe
// for bytes the caller produced).
//
// The error names what failed — unknown format, oversized, undecodable —
// so a UI can show it rather than an empty box.
func DecodeRGBA8(data []byte, maxPixels int) (pixels []uint32, widthPx uint32, heightPx uint32, err error) {
	if len(data) == 0 {
		err = eh.New("empty image data")
		return
	}
	cfg, format, cfgErr := image.DecodeConfig(bytes.NewReader(data))
	if cfgErr != nil {
		err = eh.Errorf("unable to read image header: %w", cfgErr)
		return
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		err = eh.Errorf("%s image has empty bounds (%dx%d)", format, cfg.Width, cfg.Height)
		return
	}
	// int64 throughout: on a 32-bit build the product of two plausible
	// int32 dimensions overflows, and an overflowed product compares
	// happily against any bound.
	if maxPixels > 0 && int64(cfg.Width)*int64(cfg.Height) > int64(maxPixels) {
		err = eh.Errorf("%s image is %dx%d (%d pixels), over the %d-pixel budget",
			format, cfg.Width, cfg.Height, int64(cfg.Width)*int64(cfg.Height), maxPixels)
		return
	}
	img, _, decErr := image.Decode(bytes.NewReader(data))
	if decErr != nil {
		err = eh.Errorf("unable to decode %s image: %w", format, decErr)
		return
	}
	pixels, widthPx, heightPx = Pack(img)
	if widthPx == 0 || heightPx == 0 {
		pixels = nil
		err = eh.Errorf("%s image decoded to empty bounds", format)
		return
	}
	return
}

// Pack converts a decoded [image.Image] into row-major 0xRRGGBBAA
// pixels. Exported for callers that already hold a decoded image (a
// rendered chart, a synthesised bitmap) and only want the packing.
//
// An image with empty bounds yields nil pixels and zero dimensions.
func Pack(img image.Image) (pixels []uint32, widthPx uint32, heightPx uint32) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	pixels = make([]uint32, w*h)
	i := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// RGBA() returns alpha-premultiplied 16-bit components
			// (0..65535); the widget wants 8-bit 0xRRGGBBAA.
			r, g, b, a := img.At(x, y).RGBA()
			pixels[i] = uint32(r>>8)<<24 | uint32(g>>8)<<16 | uint32(b>>8)<<8 | uint32(a>>8)
			i++
		}
	}
	widthPx = uint32(w)
	heightPx = uint32(h)
	return
}
