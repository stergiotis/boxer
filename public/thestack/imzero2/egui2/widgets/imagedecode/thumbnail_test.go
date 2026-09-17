package imagedecode

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func encodeGradientPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestThumbnailSize(t *testing.T) {
	cases := []struct{ w, h, max, tw, th int }{
		{100, 50, 512, 100, 50},    // within the bound: untouched, never enlarged
		{1024, 512, 512, 512, 256}, // halved
		{8000, 1000, 512, 512, 64}, // panorama
		{10, 9000, 512, 1, 512},    // a strip keeps a pixel of width
		{512, 512, 512, 512, 512},
	}
	for _, tc := range cases {
		tw, th := ThumbnailSize(tc.w, tc.h, tc.max)
		if tw != tc.tw || th != tc.th {
			t.Errorf("ThumbnailSize(%d, %d, %d) = %d × %d, want %d × %d", tc.w, tc.h, tc.max, tw, th, tc.tw, tc.th)
		}
	}
}

func TestDecodeThumbnailBoundsWhatIsRetained(t *testing.T) {
	th, err := DecodeThumbnailRGBA8(encodeGradientPNG(t, 900, 300), DefaultMaxPixels, 128)
	if err != nil {
		t.Fatal(err)
	}
	if th.WidthPx != 128 || th.HeightPx != 43 {
		t.Errorf("thumbnail is %d × %d", th.WidthPx, th.HeightPx)
	}
	if th.SrcWidthPx != 900 || th.SrcHeightPx != 300 {
		t.Errorf("source size reported as %d × %d", th.SrcWidthPx, th.SrcHeightPx)
	}
	if len(th.Pixels) != int(th.WidthPx*th.HeightPx) {
		t.Errorf("%d pixels for %d × %d", len(th.Pixels), th.WidthPx, th.HeightPx)
	}
	if th.Pixels[0]&0xff != 0xff {
		t.Errorf("an opaque source came out with alpha %#x", th.Pixels[0]&0xff)
	}

	small, err := DecodeThumbnailRGBA8(encodeGradientPNG(t, 16, 16), DefaultMaxPixels, 128)
	if err != nil {
		t.Fatal(err)
	}
	if small.WidthPx != 16 || small.HeightPx != 16 {
		t.Errorf("an icon was resized to %d × %d", small.WidthPx, small.HeightPx)
	}
}

func TestDecodeThumbnailRejects(t *testing.T) {
	if _, err := DecodeThumbnailRGBA8(encodeGradientPNG(t, 200, 200), 100, 64); err == nil || !strings.Contains(err.Error(), "pixel budget") {
		t.Errorf("over the budget: %v", err)
	}
	if _, err := DecodeThumbnailRGBA8([]byte("not an image"), DefaultMaxPixels, 64); err == nil {
		t.Errorf("garbage decoded")
	}
	corrupt := encodeGradientPNG(t, 64, 64)
	corrupt = corrupt[:len(corrupt)/2]
	if _, err := DecodeThumbnailRGBA8(corrupt, DefaultMaxPixels, 64); err == nil {
		t.Errorf("a truncated PNG decoded")
	}
	if _, err := DecodeThumbnailRGBA8(nil, DefaultMaxPixels, 64); err == nil {
		t.Errorf("empty data decoded")
	}
}
