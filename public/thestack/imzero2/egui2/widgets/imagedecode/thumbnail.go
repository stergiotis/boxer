package imagedecode

import (
	"bytes"
	"image"
	"math"

	"golang.org/x/image/draw"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Thumbnail is a decoded image reduced to a bound, with the size it had.
type Thumbnail struct {
	// Pixels are row-major 0xRRGGBBAA, WidthPx × HeightPx.
	Pixels            []uint32
	WidthPx, HeightPx uint32
	// SrcWidthPx and SrcHeightPx are the source's own dimensions, which is
	// what a caller lays the thumbnail out by: an icon is shown at its
	// native size, not at the thumbnail bound.
	SrcWidthPx, SrcHeightPx uint32
}

// DecodeThumbnailRGBA8 is [DecodeRGBA8] for a caller that shows many images
// small: the image is decoded under the same header-first pixel budget and
// then reduced so neither side exceeds maxSide, aspect kept, and only the
// reduction is returned. A source already within the bound is returned as
// it is — never enlarged. An animated GIF yields its first frame, as
// [image.Decode] does.
//
// The reduction is an area-weighted bilinear kernel, so a photograph reduced
// twentyfold does not alias the way nearest or plain bilinear sampling would.
// The full decode still happens and still costs what it costs; what this
// bounds is what is retained and shipped afterwards.
func DecodeThumbnailRGBA8(data []byte, maxPixels int, maxSide int) (t Thumbnail, err error) {
	if maxSide <= 0 {
		err = eh.New("thumbnail bound must be positive")
		return
	}
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
		err = eb.Build().Str("format", format).Int("width", cfg.Width).Int("height", cfg.Height).Errorf("image has empty bounds")
		return
	}
	if maxPixels > 0 && int64(cfg.Width)*int64(cfg.Height) > int64(maxPixels) {
		err = eb.Build().Str("format", format).Int("width", cfg.Width).Int("height", cfg.Height).
			Int64("pixels", int64(cfg.Width)*int64(cfg.Height)).Int("budget", maxPixels).
			Errorf("image is over the pixel budget")
		return
	}
	img, _, decErr := image.Decode(bytes.NewReader(data))
	if decErr != nil {
		err = eb.Build().Str("format", format).Errorf("unable to decode image: %w", decErr)
		return
	}
	b := img.Bounds()
	t.SrcWidthPx, t.SrcHeightPx = uint32(b.Dx()), uint32(b.Dy())
	w, h := ThumbnailSize(b.Dx(), b.Dy(), maxSide)
	if w != b.Dx() || h != b.Dy() {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	t.Pixels, t.WidthPx, t.HeightPx = Pack(img)
	if t.WidthPx == 0 || t.HeightPx == 0 {
		t = Thumbnail{}
		err = eb.Build().Str("format", format).Errorf("image decoded to empty bounds")
	}
	return
}

// ThumbnailSize is the size (w, h) reduces to under maxSide: aspect kept,
// never enlarged, never under one pixel on a side.
func ThumbnailSize(w, h int, maxSide int) (tw, th int) {
	if w <= maxSide && h <= maxSide {
		return w, h
	}
	scale := float64(maxSide) / float64(max(w, h))
	return max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale)))
}
