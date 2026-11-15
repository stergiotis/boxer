package decode

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// sameAsWhole holds the row decoder to the whole decode: the same planes,
// sample for sample, the same provenance but for the decoder's name, and so
// the same digest.
func sameAsWhole(t *testing.T, data []byte, what string) *tiles.Store {
	t.Helper()
	img, err := Decode(data, "x.png", Options{})
	require.NoError(t, err, what)
	var early *tiles.Store
	s, err := DecodeTiled(context.Background(), data, "x.png", Options{}, tiles.Options{}, func(st *tiles.Store) { early = st })
	require.NoError(t, err, what)
	require.Same(t, s, early, "%s: the store is handed out as soon as it exists", what)
	require.True(t, s.Finished())
	got, err := s.Read(context.Background(), image.Rect(0, 0, img.Width(), img.Height()))
	require.NoError(t, err)
	require.Equal(t, img.Model(), got.Model(), what)
	require.Equal(t, img.Kind(), got.Kind(), what)
	for i := range img.PlaneCount() {
		require.Equal(t, img.Plane(i).U8(), got.Plane(i).U8(), "%s: plane %d", what, i)
		require.Equal(t, img.Plane(i).U16(), got.Plane(i).U16(), "%s: plane %d", what, i)
	}
	wp, gp := img.Provenance(), s.Provenance()
	assert.Equal(t, "boxer PNG row decoder", gp.Decoder, what)
	gp.Decoder = wp.Decoder
	assert.Equal(t, wp, gp, "%s: the same provenance, digest included", what)
	return s
}

// Every colour type and bit depth the format defines, written sample by
// sample with no filter, colour keys and palettes with and without
// transparency.
func TestTheRowDecoderGivesTheWholeDecodesPlanes(t *testing.T) {
	sample := func(x, y, c int) uint16 { return uint16(x*131 + y*17 + c*29) }
	for _, tc := range []struct {
		name      string
		colorType uint8
		depths    []uint8
	}{
		{"gray", pngColorGray, []uint8{1, 2, 4, 8, 16}},
		{"rgb", pngColorRGB, []uint8{8, 16}},
		{"gray+alpha", pngColorGrayAlpha, []uint8{8, 16}},
		{"rgba", pngColorRGBA, []uint8{8, 16}},
	} {
		for _, d := range tc.depths {
			mask := uint16(1)<<d - 1
			if d == 16 {
				mask = 0xffff
			}
			data := rawPNG(t, 37, 21, tc.colorType, d, nil, func(x, y, c int) uint16 { return sample(x, y, c) & mask })
			sameAsWhole(t, data, tc.name)
		}
	}
	palette := make([]byte, 3*256)
	for i := range palette {
		palette[i] = byte(i * 7)
	}
	for _, d := range []uint8{1, 2, 4, 8} {
		mask := uint16(1)<<d - 1
		data := rawPNG(t, 33, 9, pngColorPalette, d, []pngChunk{{"PLTE", palette[:3*(int(mask)+1)]}}, func(x, y, c int) uint16 { return sample(x, y, c) & mask })
		sameAsWhole(t, data, "palette")
	}
	trns := []byte{0, 128, 255, 7}
	sameAsWhole(t, rawPNG(t, 20, 6, pngColorPalette, 8, []pngChunk{{"PLTE", palette[:3*16]}, {"tRNS", trns}},
		func(x, y, c int) uint16 { return uint16((x + y) % 16) }), "palette with transparency")
	// A colour key: the colour planes under transparent pixels are compared
	// too, which the standard decoder keeps as stored.
	sameAsWhole(t, rawPNG(t, 9, 5, pngColorGray, 8, []pngChunk{{"tRNS", []byte{0, 3}}},
		func(x, y, c int) uint16 { return uint16((x + y) % 5) }), "grey key")
	sameAsWhole(t, rawPNG(t, 9, 5, pngColorRGB, 16, []pngChunk{{"tRNS", []byte{0, 1, 0, 2, 0, 3}}},
		func(x, y, c int) uint16 { return uint16(x%3 + c) }), "rgb key")
}

// Files the standard encoder writes, which choose a filter per row, large
// enough to cross the store's tile and band edges.
func TestTheRowDecoderUndoesEveryFilterAcrossBands(t *testing.T) {
	encode := func(img image.Image) []byte {
		var buf bytes.Buffer
		require.NoError(t, png.Encode(&buf, img))
		return buf.Bytes()
	}
	g := image.NewGray(image.Rect(0, 0, 700, 1100))
	g16 := image.NewGray16(image.Rect(0, 0, 530, 600))
	n := image.NewNRGBA(image.Rect(0, 0, 600, 530))
	n64 := image.NewNRGBA64(image.Rect(0, 0, 300, 520))
	pal := make(color.Palette, 200)
	for i := range pal {
		pal[i] = color.NRGBA{uint8(i), uint8(255 - i), uint8(i * 3), 255}
	}
	p := image.NewPaletted(image.Rect(0, 0, 300, 1030), pal)
	for y := range 1100 {
		for x := range 700 {
			v := (x*x + y*3) ^ (y * 7)
			g.Pix[y*g.Stride+x] = uint8(v)
			if y < 600 && x < 530 {
				g16.SetGray16(x, y, color.Gray16{Y: uint16(v * 31)})
			}
			if y < 530 && x < 600 {
				n.SetNRGBA(x, y, color.NRGBA{uint8(v), uint8(x), uint8(y), uint8(v >> 3)})
			}
			if y < 520 && x < 300 {
				n64.SetNRGBA64(x, y, color.NRGBA64{uint16(v * 13), uint16(x * 200), uint16(y * 100), uint16(v*7) | 1})
			}
			if y < 1030 && x < 300 {
				p.SetColorIndex(x, y, uint8(v%200))
			}
		}
	}
	for name, img := range map[string]image.Image{"gray": g, "gray16": g16, "nrgba": n, "nrgba64": n64, "paletted": p} {
		s := sameAsWhole(t, encode(img), name)
		assert.Positive(t, s.Reduced()+1, name)
	}
}

// A damaged file is refused, and a reader waiting on the store hears why.
func TestADamagedFileIsRefusedAndTheStoreSaysWhy(t *testing.T) {
	good := rawPNG(t, 40, 30, pngColorGray, 8, nil, func(x, y, c int) uint16 { return uint16(x + y) })
	bad := bytes.Clone(good)
	bad[len(bad)-20] ^= 0xff // inside the image data, past its header
	var early *tiles.Store
	_, err := DecodeTiled(context.Background(), bad, "bad.png", Options{}, tiles.Options{}, func(st *tiles.Store) { early = st })
	require.Error(t, err)
	_, wholeErr := Decode(bad, "bad.png", Options{})
	assert.Error(t, wholeErr, "the whole decode refuses it too")
	if early != nil {
		assert.Error(t, early.Wait(context.Background()))
	}
	short := good[:len(good)-30]
	_, err = DecodeTiled(context.Background(), short, "short.png", Options{}, tiles.Options{}, nil)
	assert.Error(t, err)
	_, err = DecodeTiled(context.Background(), good, "big.png", Options{MaxPixels: 100}, tiles.Options{}, nil)
	assert.ErrorContains(t, err, "decode limit")

	// A JPEG goes through the whole decode into the same kind of store.
	j := image.NewGray(image.Rect(0, 0, 40, 30))
	s, err := DecodeTiled(context.Background(), jpegOf(t, j), "j.jpg", Options{}, tiles.Options{}, nil)
	require.NoError(t, err)
	assert.True(t, s.Provenance().Lossy)
	assert.Equal(t, imaging.ColorModelGray, s.Model())
}
