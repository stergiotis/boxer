package decode

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// A PNG written sample by sample, so a test knows exactly what the file
// stores: any colour type and bit depth the format defines, not interlaced,
// with whatever extra chunks the case needs before the pixel data.
type pngChunk struct {
	kind string
	body []byte
}

func writeChunk(buf *bytes.Buffer, kind string, body []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(body)))
	buf.Write(n[:])
	buf.WriteString(kind)
	buf.Write(body)
	crc := crc32.NewIEEE()
	crc.Write([]byte(kind))
	crc.Write(body)
	binary.BigEndian.PutUint32(n[:], crc.Sum32())
	buf.Write(n[:])
}

// rawPNG writes a file whose pixel (x, y) has, in each of its channels, the
// sample sample(x, y, channel).
func rawPNG(t *testing.T, w, h int, colorType, bitDepth uint8, extra []pngChunk, sample func(x, y, c int) uint16) []byte {
	t.Helper()
	channels := map[uint8]int{pngColorGray: 1, pngColorRGB: 3, pngColorPalette: 1, pngColorGrayAlpha: 2, pngColorRGBA: 4}[colorType]
	var out bytes.Buffer
	out.Write(pngSignature)
	head := make([]byte, 13)
	binary.BigEndian.PutUint32(head[0:], uint32(w))
	binary.BigEndian.PutUint32(head[4:], uint32(h))
	head[8], head[9] = bitDepth, colorType
	writeChunk(&out, "IHDR", head)
	for _, c := range extra {
		writeChunk(&out, c.kind, c.body)
	}
	var raw bytes.Buffer
	for y := range h {
		raw.WriteByte(0) // filter: none
		var acc, bits uint
		for x := range w {
			for c := range channels {
				v := sample(x, y, c)
				switch {
				case bitDepth == 16:
					raw.WriteByte(byte(v >> 8))
					raw.WriteByte(byte(v))
				case bitDepth == 8:
					raw.WriteByte(byte(v))
				default:
					acc = acc<<bitDepth | uint(v)
					bits += uint(bitDepth)
					if bits == 8 {
						raw.WriteByte(byte(acc))
						acc, bits = 0, 0
					}
				}
			}
		}
		if bits > 0 {
			raw.WriteByte(byte(acc << (8 - bits)))
		}
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, err := zw.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	writeChunk(&out, "IDAT", z.Bytes())
	writeChunk(&out, "IEND", nil)
	return out.Bytes()
}

// stored returns plane c's sample at (x, y) whatever the plane's kind.
func stored(t *testing.T, img imaging.Image, c, x, y int) uint16 {
	t.Helper()
	v, ok := img.Plane(c).StoredAt(x, y)
	require.True(t, ok)
	return uint16(v)
}

const tw, th = 5, 3

func TestPNGEveryColourTypeAndDepthKeepsItsSamples(t *testing.T) {
	cases := []struct {
		name      string
		colorType uint8
		depth     uint8
		model     imaging.ColorModelE
		kind      imaging.SampleKindE
	}{
		{"gray8", pngColorGray, 8, imaging.ColorModelGray, imaging.SampleKindU8},
		{"gray16", pngColorGray, 16, imaging.ColorModelGray, imaging.SampleKindU16},
		{"grayalpha8", pngColorGrayAlpha, 8, imaging.ColorModelGrayAlpha, imaging.SampleKindU8},
		{"grayalpha16", pngColorGrayAlpha, 16, imaging.ColorModelGrayAlpha, imaging.SampleKindU16},
		{"rgb8", pngColorRGB, 8, imaging.ColorModelRGB, imaging.SampleKindU8},
		{"rgb16", pngColorRGB, 16, imaging.ColorModelRGB, imaging.SampleKindU16},
		{"rgba8", pngColorRGBA, 8, imaging.ColorModelRGBA, imaging.SampleKindU8},
		{"rgba16", pngColorRGBA, 16, imaging.ColorModelRGBA, imaging.SampleKindU16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Samples that differ per pixel and per channel, use the whole
			// range, and in the alpha channel are mostly neither 0 nor full,
			// so a multiplication by alpha anywhere would show.
			sample := func(x, y, c int) uint16 {
				v := uint32(x*7919+y*104729+c*1299709) * 2654435761
				if tc.depth == 8 {
					return uint16(v >> 24)
				}
				return uint16(v >> 16)
			}
			img, err := Decode(rawPNG(t, tw, th, tc.colorType, tc.depth, nil, sample), tc.name+".png", Options{})
			require.NoError(t, err)
			assert.Equal(t, tc.model, img.Model())
			assert.Equal(t, tc.kind, img.Kind())
			assert.Equal(t, tw, img.Width())
			assert.Equal(t, th, img.Height())
			for c := range img.PlaneCount() {
				for y := range th {
					for x := range tw {
						assert.Equal(t, sample(x, y, c), stored(t, img, c, x, y), "plane %d at (%d,%d)", c, x, y)
					}
				}
			}
			p := img.Provenance()
			assert.Equal(t, "png", p.Format)
			assert.Equal(t, tc.depth, p.StoredBitDepth)
			assert.False(t, p.Lossy)
			assert.Zero(t, p.Derivations)
			assert.Empty(t, p.ColorDeclaration)
			assert.Equal(t, tc.name+".png", p.SourceName)
		})
	}
}

func TestPNGDepthsBelowEightArriveScaledAndSaySo(t *testing.T) {
	for _, depth := range []uint8{1, 2, 4} {
		top := uint16(1)<<depth - 1
		sample := func(x, y, c int) uint16 { return uint16(x+y) % (top + 1) }
		img, err := Decode(rawPNG(t, tw, th, pngColorGray, depth, nil, sample), "low.png", Options{})
		require.NoError(t, err)
		assert.Equal(t, imaging.ColorModelGray, img.Model())
		assert.Equal(t, imaging.SampleKindU8, img.Kind())
		for y := range th {
			for x := range tw {
				assert.Equal(t, sample(x, y, 0)*255/top, stored(t, img, 0, x, y), "depth %d at (%d,%d)", depth, x, y)
			}
		}
		p := img.Provenance()
		assert.Equal(t, depth, p.StoredBitDepth)
		assert.True(t, p.Derivations.Has(imaging.DerivationDepthExpanded))
	}
}

func TestPNGPaletteIsExpandedAndSaysSo(t *testing.T) {
	palette := []byte{10, 20, 30, 200, 100, 50, 0, 0, 0, 255, 255, 255}
	sample := func(x, y, c int) uint16 { return uint16(x+y) % 4 }
	for _, depth := range []uint8{2, 4, 8} {
		img, err := Decode(rawPNG(t, tw, th, pngColorPalette, depth, []pngChunk{{"PLTE", palette}}, sample), "p.png", Options{})
		require.NoError(t, err)
		assert.Equal(t, imaging.ColorModelRGB, img.Model())
		for y := range th {
			for x := range tw {
				i := int(sample(x, y, 0))
				for c := range 3 {
					assert.Equal(t, uint16(palette[3*i+c]), stored(t, img, c, x, y))
				}
			}
		}
		p := img.Provenance()
		assert.Equal(t, "palette", p.StoredColorType)
		assert.True(t, p.Derivations.Has(imaging.DerivationPaletteExpanded))
		assert.False(t, p.Derivations.Has(imaging.DerivationDepthExpanded), "indices are not samples")
	}

	// Per-entry alpha makes it four planes, alpha straight.
	trns := []byte{0, 128, 255}
	img, err := Decode(rawPNG(t, tw, th, pngColorPalette, 8, []pngChunk{{"PLTE", palette}, {"tRNS", trns}}, sample), "pa.png", Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelRGBA, img.Model())
	assert.Equal(t, uint16(200), stored(t, img, 0, 1, 0), "entry 1's red is not multiplied by its alpha")
	assert.Equal(t, uint16(128), stored(t, img, 3, 1, 0))
	assert.Equal(t, uint16(0), stored(t, img, 3, 0, 0))
	assert.Equal(t, uint16(255), stored(t, img, 3, 3, 0), "an entry past the table is opaque")
	assert.False(t, img.Provenance().Derivations.Has(imaging.DerivationColorKeyAlpha))
}

func TestPNGTransparentColourBecomesAnAlphaPlane(t *testing.T) {
	sample := func(x, y, c int) uint16 { return uint16(x * 10) }
	key := []byte{0, 20} // the grey level 20 is transparent
	img, err := Decode(rawPNG(t, tw, th, pngColorGray, 8, []pngChunk{{"tRNS", key}}, sample), "k.png", Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelGrayAlpha, img.Model())
	assert.Equal(t, uint16(0), stored(t, img, 1, 2, 0))
	assert.Equal(t, uint16(255), stored(t, img, 1, 3, 0))
	assert.Equal(t, uint16(30), stored(t, img, 0, 3, 0))
	assert.True(t, img.Provenance().Derivations.Has(imaging.DerivationColorKeyAlpha))

	key16 := []byte{0, 7, 0, 7, 0, 7}
	rgb := func(x, y, c int) uint16 { return uint16(x) * 7 }
	img, err = Decode(rawPNG(t, tw, th, pngColorRGB, 16, []pngChunk{{"tRNS", key16}}, rgb), "k16.png", Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelRGBA, img.Model())
	assert.Equal(t, imaging.SampleKindU16, img.Kind())
	assert.Equal(t, uint16(0), stored(t, img, 3, 1, 1))
	assert.Equal(t, uint16(65535), stored(t, img, 3, 2, 1))
	assert.Equal(t, uint16(14), stored(t, img, 1, 2, 1))
}

func TestPNGColourStatementsAndAnimationAreRecorded(t *testing.T) {
	gamma := make([]byte, 4)
	binary.BigEndian.PutUint32(gamma, 45455)
	extra := []pngChunk{
		{"sRGB", []byte{0}},
		{"gAMA", gamma},
		{"iCCP", append([]byte("Display P3\x00\x00"), 1, 2, 3)},
		{"acTL", make([]byte, 8)},
	}
	img, err := Decode(rawPNG(t, tw, th, pngColorGray, 8, extra, func(x, y, c int) uint16 { return 1 }), "c.png", Options{})
	require.NoError(t, err)
	p := img.Provenance()
	assert.Equal(t, `sRGB; gamma 0.45455; ICC profile "Display P3"`, p.ColorDeclaration)
	assert.True(t, p.Derivations.Has(imaging.DerivationDefaultFrameOnly))
}

func TestGoEncodedPNGRoundTripsWithStraightAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	src.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 0})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, src))
	img, err := Decode(buf.Bytes(), "a.png", Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelRGBA, img.Model())
	assert.Equal(t, []uint16{200, 100, 50, 128}, []uint16{stored(t, img, 0, 0, 0), stored(t, img, 1, 0, 0), stored(t, img, 2, 0, 0), stored(t, img, 3, 0, 0)})
	assert.Equal(t, uint16(255), stored(t, img, 0, 1, 0), "colour under a fully transparent pixel is kept")
}

func TestAPremultipliedPictureIsUnmultipliedOnTheSlowPath(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 100, G: 50, B: 25, A: 128})
	planes, err := planesOf(src, imaging.ColorModelRGBA, imaging.SampleKindU8)
	require.NoError(t, err)
	want := color.NRGBAModel.Convert(src.At(0, 0)).(color.NRGBA)
	got := func(c int) uint8 { v, _ := planes[c].StoredAt(0, 0); return uint8(v) }
	assert.Equal(t, [4]uint8{want.R, want.G, want.B, want.A}, [4]uint8{got(0), got(1), got(2), got(3)})
	assert.Greater(t, got(0), uint8(190), "red is about 100·255/128, not 100")
}

func jpegOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

func TestJPEGGrayAndColourMatchTheStandardDecoder(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 16, 16))
	col := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			gray.SetGray(x, y, color.Gray{Y: uint8(x*16 + y)})
			col.SetRGBA(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: uint8(x*8 + y*8), A: 255})
		}
	}

	data := jpegOf(t, gray)
	img, err := Decode(data, "g.jpg", Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelGray, img.Model())
	ref, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	for y := range 16 {
		for x := range 16 {
			assert.Equal(t, uint16(ref.(*image.Gray).GrayAt(x, y).Y), stored(t, img, 0, x, y))
		}
	}
	p := img.Provenance()
	assert.True(t, p.Lossy)
	assert.Equal(t, "gray", p.StoredColorType)
	assert.Equal(t, uint8(8), p.StoredBitDepth)
	assert.Zero(t, p.Derivations)

	data = jpegOf(t, col)
	img, err = Decode(data, "c.jpg", Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelRGB, img.Model())
	ref, err = jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	for y := range 16 {
		for x := range 16 {
			want := color.NRGBAModel.Convert(ref.At(x, y)).(color.NRGBA)
			assert.Equal(t, [3]uint16{uint16(want.R), uint16(want.G), uint16(want.B)},
				[3]uint16{stored(t, img, 0, x, y), stored(t, img, 1, x, y), stored(t, img, 2, x, y)}, "(%d,%d)", x, y)
		}
	}
	p = img.Provenance()
	assert.Equal(t, "ycbcr", p.StoredColorType)
	assert.True(t, p.Derivations.Has(imaging.DerivationYCbCrConverted))
}

func TestCMYKIsConvertedWithTheStandardFormula(t *testing.T) {
	src := image.NewCMYK(image.Rect(0, 0, 2, 1))
	src.SetCMYK(0, 0, color.CMYK{C: 255, M: 0, Y: 0, K: 0})
	src.SetCMYK(1, 0, color.CMYK{C: 10, M: 200, Y: 30, K: 40})
	planes, err := planesOf(rgbOfCMYK(src), imaging.ColorModelRGB, imaging.SampleKindU8)
	require.NoError(t, err)
	for x := range 2 {
		c := src.CMYKAt(x, 0)
		r, g, b := color.CMYKToRGB(c.C, c.M, c.Y, c.K)
		got := func(i int) uint8 { v, _ := planes[i].StoredAt(x, 0); return uint8(v) }
		assert.Equal(t, [3]uint8{r, g, b}, [3]uint8{got(0), got(1), got(2)})
	}
}

func TestWhatTheDecodersDoNotReadIsRefused(t *testing.T) {
	_, err := Decode([]byte("GIF89a not an image this package reads"), "x.gif", Options{})
	assert.ErrorContains(t, err, "not a PNG or a JPEG")

	// A 12-bit JPEG: the frame header of a real file with its precision
	// byte changed.
	data := jpegOf(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	i := bytes.Index(data, []byte{0xff, 0xc0})
	require.Positive(t, i)
	data[i+4] = 12
	_, err = Decode(data, "deep.jpg", Options{})
	assert.Error(t, err)

	whole := rawPNG(t, tw, th, pngColorGray, 8, nil, func(x, y, c int) uint16 { return 1 })
	_, err = Decode(whole[:len(whole)-20], "cut.png", Options{})
	assert.ErrorContains(t, err, "does not read")

	_, err = Decode(whole, "big.png", Options{MaxPixels: tw*th - 1})
	assert.ErrorContains(t, err, "more pixels than the decode limit")
	_, err = Decode(whole, "fits.png", Options{MaxPixels: tw * th})
	assert.NoError(t, err)
}

func TestSniff(t *testing.T) {
	assert.Equal(t, FormatPNG, Sniff(pngSignature))
	assert.Equal(t, FormatJPEG, Sniff([]byte{0xff, 0xd8, 0xff, 0xe0}))
	assert.Zero(t, Sniff([]byte{0xff, 0xd8}))
	assert.Zero(t, Sniff(nil))
}

func TestJPEGFrameNames(t *testing.T) {
	data := jpegOf(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	info := scanJPEG(data)
	assert.Equal(t, "baseline", info.frameName())
	assert.Equal(t, uint8(8), info.precision)
	assert.Equal(t, uint8(1), info.components)
	assert.Equal(t, "lossless", jpegInfo{frame: 0xc3}.frameName())
	assert.Equal(t, "progressive", jpegInfo{frame: 0xc2}.frameName())
	assert.Equal(t, "unknown", jpegInfo{}.frameName())
}
