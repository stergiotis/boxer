package imagedecode

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// solid builds a w×h image filled with one colour.
func solid(w, h int, col color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, col)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// The pack order and channel layout are the widget's contract: row-major,
// 0xRRGGBBAA.
func TestDecodeRGBA8Packing(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	img.Set(1, 0, color.RGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})

	pixels, w, h, err := DecodeRGBA8(encodePNG(t, img), DefaultMaxPixels)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), w)
	assert.Equal(t, uint32(1), h)
	require.Len(t, pixels, 2)
	assert.Equal(t, uint32(0x123456ff), pixels[0])
	assert.Equal(t, uint32(0xabcdefff), pixels[1])
}

// Row-major: pixel (x=0,y=1) is at index width.
func TestDecodeRGBA8RowMajor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	img.Set(1, 0, color.RGBA{G: 0xff, A: 0xff})
	img.Set(0, 1, color.RGBA{B: 0xff, A: 0xff})
	img.Set(1, 1, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})

	pixels, w, _, err := DecodeRGBA8(encodePNG(t, img), DefaultMaxPixels)
	require.NoError(t, err)
	require.Len(t, pixels, 4)
	assert.Equal(t, uint32(0xff0000ff), pixels[0])
	assert.Equal(t, uint32(0x00ff00ff), pixels[1])
	assert.Equal(t, uint32(0x0000ffff), pixels[int(w)], "second row starts at index width")
	assert.Equal(t, uint32(0xffffffff), pixels[int(w)+1])
}

// All three registered formats decode; the caller declares which one it has,
// but image.Decode identifies it from the magic prefix either way.
func TestDecodeRGBA8Formats(t *testing.T) {
	img := solid(4, 3, color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff})

	t.Run("png", func(t *testing.T) {
		_, w, h, err := DecodeRGBA8(encodePNG(t, img), DefaultMaxPixels)
		require.NoError(t, err)
		assert.Equal(t, [2]uint32{4, 3}, [2]uint32{w, h})
	})

	t.Run("jpeg", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, jpeg.Encode(&buf, img, nil))
		_, w, h, err := DecodeRGBA8(buf.Bytes(), DefaultMaxPixels)
		require.NoError(t, err)
		assert.Equal(t, [2]uint32{4, 3}, [2]uint32{w, h})
	})

	t.Run("gif", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, gif.Encode(&buf, img, nil))
		_, w, h, err := DecodeRGBA8(buf.Bytes(), DefaultMaxPixels)
		require.NoError(t, err)
		assert.Equal(t, [2]uint32{4, 3}, [2]uint32{w, h})
	})
}

// The bound is checked against the header, before the decode allocates. The
// fixture is the reason the check exists: a uniform 4000×4000 PNG is a few KB
// on the wire and 64 MB decoded.
func TestDecodeRGBA8RejectsOversized(t *testing.T) {
	data := encodePNG(t, solid(4000, 4000, color.RGBA{A: 0xff}))
	assert.Less(t, len(data), 1<<20, "the fixture is small encoded — that is the whole problem")

	pixels, w, h, err := DecodeRGBA8(data, 1<<20) // 1 Mpix budget vs 16 Mpix image
	require.Error(t, err)
	assert.Contains(t, err.Error(), "over the")
	assert.Contains(t, err.Error(), "4000x4000", "the reason names the dimensions")
	assert.Nil(t, pixels)
	assert.Zero(t, w)
	assert.Zero(t, h)

	// The same bytes decode when the budget allows them.
	_, w, h, err = DecodeRGBA8(data, DefaultMaxPixels)
	require.NoError(t, err)
	assert.Equal(t, [2]uint32{4000, 4000}, [2]uint32{w, h})
}

// A zero budget means unbounded — only for bytes the caller produced itself.
func TestDecodeRGBA8ZeroBudgetIsUnbounded(t *testing.T) {
	_, w, h, err := DecodeRGBA8(encodePNG(t, solid(64, 64, color.RGBA{A: 0xff})), 0)
	require.NoError(t, err)
	assert.Equal(t, [2]uint32{64, 64}, [2]uint32{w, h})
}

// Failures name what went wrong, so a UI can show the reason rather than an
// empty box.
func TestDecodeRGBA8Errors(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, _, _, err := DecodeRGBA8(nil, DefaultMaxPixels)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("not an image", func(t *testing.T) {
		_, _, _, err := DecodeRGBA8([]byte("this is just some text"), DefaultMaxPixels)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "header")
	})

	t.Run("truncated png", func(t *testing.T) {
		full := encodePNG(t, solid(8, 8, color.RGBA{A: 0xff}))
		// Keep the header (so DecodeConfig succeeds) but lose the pixel data,
		// which sends the failure to image.Decode rather than DecodeConfig.
		_, _, _, err := DecodeRGBA8(full[:len(full)-16], DefaultMaxPixels)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode")
	})
}

// Pack is the packing alone, for callers that already hold a decoded image.
func TestPack(t *testing.T) {
	pixels, w, h := Pack(solid(3, 2, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}))
	assert.Equal(t, [2]uint32{3, 2}, [2]uint32{w, h})
	require.Len(t, pixels, 6)
	for i, p := range pixels {
		assert.Equal(t, uint32(0x112233ff), p, "pixel %d", i)
	}
}

// An image with empty bounds yields nothing rather than panicking on a
// zero-length make.
func TestPackEmptyBounds(t *testing.T) {
	pixels, w, h := Pack(image.NewRGBA(image.Rect(0, 0, 0, 0)))
	assert.Nil(t, pixels)
	assert.Zero(t, w)
	assert.Zero(t, h)
}

// Pack honours a non-zero origin: sub-images are rebased to their own bounds
// rather than indexed from (0,0).
func TestPackNonZeroOrigin(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			base.Set(x, y, color.RGBA{B: 0x10, A: 0xff})
		}
	}
	base.Set(2, 2, color.RGBA{R: 0xff, A: 0xff})
	sub := base.SubImage(image.Rect(2, 2, 4, 4))

	pixels, w, h := Pack(sub)
	assert.Equal(t, [2]uint32{2, 2}, [2]uint32{w, h})
	require.Len(t, pixels, 4)
	assert.Equal(t, uint32(0xff0000ff), pixels[0], "the sub-image's first pixel is base (2,2)")
}
