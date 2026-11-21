package tiff

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// grayU8 is a small, valid image a refusal case changes one thing of.
func grayU8() wimage {
	return wimage{w: 6, h: 5, kind: imaging.SampleKindU8, planes: samplePlanes(imaging.SampleKindU8, 6, 5, 1, 3),
		photometric: photoBlackIsZero, compression: compressionNone}
}

func requireRefused(t *testing.T, data []byte, sentinel error, mention string) {
	t.Helper()
	st, err := decodeBytes(t, data, Options{})
	require.Error(t, err)
	require.True(t, st.IsZero())
	require.ErrorIs(t, err, sentinel, "%v", err)
	if mention != "" {
		assert.Contains(t, err.Error(), mention)
	}
}

// TestDecode_RefusesByName refuses each compression, sample format,
// photometric interpretation, orientation and predictor outside ADR-0398
// §SD6, naming it.
func TestDecode_RefusesByName(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*wimage)
		mention string
	}{
		{"jpeg", func(m *wimage) { m.compression = 7 }, "compression 7 (JPEG)"},
		{"old jpeg", func(m *wimage) { m.compression = 6 }, "compression 6 (old-style JPEG)"},
		{"ccitt", func(m *wimage) { m.compression = 4 }, "compression 4 (CCITT T.6)"},
		{"zstd", func(m *wimage) { m.compression = 50000 }, "compression 50000 (Zstandard)"},
		{"lerc", func(m *wimage) { m.compression = 34887 }, "compression 34887 (LERC)"},
		{"jpeg2000", func(m *wimage) { m.compression = 34712 }, "compression 34712 (JPEG 2000)"},
		{"webp", func(m *wimage) { m.compression = 50001 }, "compression 50001 (WebP)"},
		{"lzma", func(m *wimage) { m.compression = 34925 }, "compression 34925 (LZMA)"},
		{"unknown compression", func(m *wimage) { m.compression = 60000 }, "compression 60000 (unknown)"},
		{"1-bit", func(m *wimage) { m.tags = append(m.tags, shortTag(tagBitsPerSample, 1)) }, "unsigned 1-bit samples"},
		{"4-bit", func(m *wimage) { m.tags = append(m.tags, shortTag(tagBitsPerSample, 4)) }, "unsigned 4-bit samples"},
		{"12-bit", func(m *wimage) { m.tags = append(m.tags, shortTag(tagBitsPerSample, 12)) }, "unsigned 12-bit samples"},
		{"signed 8-bit", func(m *wimage) { m.tags = append(m.tags, shortTag(tagSampleFormat, 2)) }, "signed 8-bit samples"},
		{"unsigned 32-bit", func(m *wimage) {
			m.tags = append(m.tags, shortTag(tagBitsPerSample, 32), shortTag(tagSampleFormat, 1))
		}, "unsigned 32-bit samples"},
		{"signed 64-bit", func(m *wimage) {
			m.tags = append(m.tags, shortTag(tagBitsPerSample, 64), shortTag(tagSampleFormat, 2))
		}, "signed 64-bit samples"},
		{"unsigned 64-bit", func(m *wimage) { m.tags = append(m.tags, shortTag(tagBitsPerSample, 64)) }, "unsigned 64-bit samples"},
		{"float 16", func(m *wimage) {
			m.tags = append(m.tags, shortTag(tagBitsPerSample, 16), shortTag(tagSampleFormat, 3))
		}, "16-bit floating-point samples"},
		{"undefined format", func(m *wimage) { m.tags = append(m.tags, shortTag(tagSampleFormat, 4)) }, "sample format 4 (undefined)"},
		{"complex int", func(m *wimage) { m.tags = append(m.tags, shortTag(tagSampleFormat, 5)) }, "sample format 5 (complex integer)"},
		{"complex float", func(m *wimage) {
			m.tags = append(m.tags, shortTag(tagBitsPerSample, 64), shortTag(tagSampleFormat, 6))
		}, "sample format 6 (complex floating point)"},
		{"mixed formats", func(m *wimage) {
			m.planes = samplePlanes(imaging.SampleKindU8, 6, 5, 2, 1)
			m.extras = []uint16{0}
			m.tags = append(m.tags, shortTag(tagSampleFormat, 1, 2))
		}, "samples of mixed sample formats"},
		{"mixed widths", func(m *wimage) {
			m.planes = samplePlanes(imaging.SampleKindU8, 6, 5, 2, 1)
			m.extras = []uint16{0}
			m.tags = append(m.tags, shortTag(tagBitsPerSample, 8, 16))
		}, "samples of mixed bit widths"},
		{"cmyk", func(m *wimage) { m.photometric = 5 }, "photometric interpretation 5 (Separated (CMYK))"},
		{"ycbcr", func(m *wimage) { m.photometric = 6 }, "photometric interpretation 6 (YCbCr)"},
		{"cielab", func(m *wimage) { m.photometric = 8 }, "photometric interpretation 8 (CIELab)"},
		{"logluv", func(m *wimage) { m.photometric = 32845 }, "photometric interpretation 32845 (LogLuv)"},
		{"mask as image", func(m *wimage) { m.photometric = 4 }, "photometric interpretation 4 (transparency mask)"},
		{"orientation 3", func(m *wimage) { m.tags = append(m.tags, shortTag(tagOrientation, 3)) }, "orientation 3 (bottom-right)"},
		{"orientation 6", func(m *wimage) { m.tags = append(m.tags, shortTag(tagOrientation, 6)) }, "orientation 6 (right-top)"},
		{"predictor 3 on integers", func(m *wimage) { m.predictor = 3 }, "predictor 3 (floating point) on integer samples"},
		{"predictor 2 on floats", func(m *wimage) {
			m.kind = imaging.SampleKindF32
			m.planes = samplePlanes(m.kind, 6, 5, 1, 1)
			m.predictor = 2
		}, "predictor 2 (horizontal differencing) on floating-point samples"},
		{"predictor 9", func(m *wimage) { m.predictor = 9 }, "predictor 9"},
		{"palette with alpha", func(m *wimage) {
			m.photometric = photoPalette
			m.planes = samplePlanes(imaging.SampleKindU8, 6, 5, 2, 1)
			m.extras = []uint16{2}
		}, "a palette image with extra samples"},
		{"volumetric", func(m *wimage) { m.tags = append(m.tags, longTag(tagImageDepth, 4)) }, "ImageDepth 4"},
		{"sparse", func(m *wimage) {
			m.tags = append(m.tags, longTag(tagStripOffsets, 0), longTag(tagStripByteCounts, 0))
		}, "a sparse file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := grayU8()
			c.change(&m)
			for _, big := range []bool{false, true} {
				data := wfile{big: big, images: []wimage{m}}.bytes(t)
				requireRefused(t, data, ErrUnsupported, c.mention)
				if c.name == "sparse" {
					// Inspect does not read the chunk table.
					continue
				}
				info, err := Inspect(context.Background(), bytes.NewReader(data), int64(len(data)))
				require.NoError(t, err)
				assert.Equal(t, DirectoryStatusRefused, info.Directories[0].Status)
				assert.Contains(t, info.Directories[0].Reason, c.mention)
			}
		})
	}
}

// readCounter counts the bytes read through it, to show what a refusal
// touched.
type readCounter struct {
	r    io.ReaderAt
	read atomic.Int64
}

func (inst *readCounter) ReadAt(p []byte, off int64) (int, error) {
	n, err := inst.r.ReadAt(p, off)
	inst.read.Add(int64(n))
	return n, err
}

// TestDecode_Limits refuses from the directory: before a sample is read and
// before anything of the size the directory claims is allocated.
func TestDecode_Limits(t *testing.T) {
	huge := grayU8()
	huge.tags = append(huge.tags, longTag(tagImageWidth, 1<<30), longTag(tagImageLength, 1<<30), longTag(tagRowsPerStrip, 1<<30))
	// One strip of the claimed size would need 2^60 bytes; the offsets
	// counted are still one.
	data := wfile{images: []wimage{huge}}.bytes(t)
	rc := &readCounter{r: bytes.NewReader(data)}
	_, err := Decode(context.Background(), rc, int64(len(data)), "huge", Options{})
	require.ErrorIs(t, err, ErrLimit)
	assert.Contains(t, err.Error(), "MaxPixels")

	t.Run("pixels", func(t *testing.T) {
		data := wfile{images: []wimage{grayU8()}}.bytes(t)
		_, err := decodeBytes(t, data, Options{MaxPixels: 29})
		require.ErrorIs(t, err, ErrLimit)
		_, err = decodeBytes(t, data, Options{MaxPixels: 30})
		require.NoError(t, err)
	})
	t.Run("bytes over pages and bands", func(t *testing.T) {
		m := grayU8()
		m.kind, m.photometric = imaging.SampleKindU16, photoRGB
		m.planes = samplePlanes(m.kind, 6, 5, 3, 1)
		data := wfile{images: []wimage{m, m}}.bytes(t)
		_, err := decodeBytes(t, data, Options{MaxBytes: 2*3*30*2 - 1})
		require.ErrorIs(t, err, ErrLimit)
		assert.Contains(t, err.Error(), "MaxBytes")
		_, err = decodeBytes(t, data, Options{MaxBytes: 2 * 3 * 30 * 2})
		require.NoError(t, err)
	})
	t.Run("a tile far larger than the image", func(t *testing.T) {
		m := grayU8()
		m.tiled, m.tw, m.th = true, 16, 16
		m.tags = append(m.tags, longTag(tagTileWidth, 1<<20), longTag(tagTileLength, 1<<20))
		data := wfile{images: []wimage{m}}.bytes(t)
		rc := &readCounter{r: bytes.NewReader(data)}
		_, err := Decode(context.Background(), rc, int64(len(data)), "tile", Options{})
		require.ErrorIs(t, err, ErrLimit)
		assert.Contains(t, err.Error(), "pixels per tile")
	})
	t.Run("no sample is read before the limits", func(t *testing.T) {
		m := grayU8()
		m.w, m.h = 400, 300
		m.planes = samplePlanes(m.kind, 400, 300, 1, 1)
		data := wfile{images: []wimage{m}}.bytes(t)
		rc := &readCounter{r: bytes.NewReader(data)}
		_, err := Decode(context.Background(), rc, int64(len(data)), "x", Options{MaxPixels: 1000})
		require.ErrorIs(t, err, ErrLimit)
		// The header and the directory, never the 120000 sample bytes.
		assert.Less(t, rc.read.Load(), int64(1000))
	})
}

// patch returns a copy of data with v written at off in little-endian.
func patch(data []byte, off int, v ...byte) []byte {
	out := bytes.Clone(data)
	copy(out[off:], v)
	return out
}

// entryAt is the offset of the entry for tag in the first directory of a
// classic little-endian file.
func entryAt(t *testing.T, data []byte, tag uint16) int {
	t.Helper()
	dir := int(binary.LittleEndian.Uint32(data[4:]))
	n := int(binary.LittleEndian.Uint16(data[dir:]))
	for i := range n {
		e := dir + 2 + 12*i
		if binary.LittleEndian.Uint16(data[e:]) == tag {
			return e
		}
	}
	t.Fatalf("tag %d not found", tag)
	return 0
}

// TestDecode_Malformed refuses files that are not well formed under
// ErrMalformed, without panicking and without allocating what they claim.
func TestDecode_Malformed(t *testing.T) {
	good := wfile{images: []wimage{grayU8()}}.bytes(t)
	_, err := decodeBytes(t, good, Options{})
	require.NoError(t, err)

	t.Run("every truncation", func(t *testing.T) {
		for _, f := range []wfile{
			{images: []wimage{grayU8()}},
			{big: true, order: binary.BigEndian, images: []wimage{grayU8()}},
		} {
			full := f.bytes(t)
			for n := range len(full) {
				_, err := decodeBytes(t, full[:n], Options{})
				require.Error(t, err, n)
				require.True(t, errors.Is(err, ErrMalformed), "%d: %v", n, err)
				_, err = Inspect(context.Background(), bytes.NewReader(full[:n]), int64(n))
				if n < len(full)-30 && err == nil {
					// The directory lies at the end: a cut before it cannot inspect.
					t.Fatalf("Inspect of %d bytes of %d succeeded", n, len(full))
				}
			}
		}
	})
	cases := []struct {
		name string
		data func() []byte
	}{
		{"not a tiff", func() []byte { return []byte("GIF89a, not at all a TIFF file") }},
		{"version 44", func() []byte { return patch(good, 2, 44) }},
		{"first directory past the end", func() []byte { return patch(good, 4, 0xff, 0xff, 0xff, 0x7f) }},
		{"first directory at 0", func() []byte { return patch(good, 4, 0, 0, 0, 0) }},
		{"cyclic chain", func() []byte {
			dir := binary.LittleEndian.Uint32(good[4:])
			n := int(binary.LittleEndian.Uint16(good[dir:]))
			next := int(dir) + 2 + 12*n
			return patch(good, next, good[4:8]...)
		}},
		{"cyclic chain of two", func() []byte {
			two := wfile{images: []wimage{grayU8(), grayU8()}}.bytes(t)
			dir := binary.LittleEndian.Uint32(two[4:])
			n := int(binary.LittleEndian.Uint16(two[dir:]))
			second := binary.LittleEndian.Uint32(two[int(dir)+2+12*n:])
			n2 := int(binary.LittleEndian.Uint16(two[second:]))
			return patch(two, int(second)+2+12*n2, two[4:8]...)
		}},
		{"strip past the end", func() []byte {
			e := entryAt(t, good, tagStripOffsets)
			return patch(good, e+8, 0xf0, 0xff, 0xff, 0x0f)
		}},
		{"strip count overflowing", func() []byte {
			e := entryAt(t, good, tagStripByteCounts)
			return patch(good, e+8, 0xff, 0xff, 0xff, 0xff)
		}},
		{"zero strip size", func() []byte {
			e := entryAt(t, good, tagStripByteCounts)
			return patch(good, e+8, 0, 0, 0, 0)
		}},
		{"offsets counting more values than the file holds", func() []byte {
			e := entryAt(t, good, tagStripOffsets)
			return patch(good, e+4, 0xff, 0xff, 0xff, 0x3f)
		}},
		{"rows per strip 0", func() []byte {
			e := entryAt(t, good, tagRowsPerStrip)
			return patch(good, e+8, 0, 0, 0, 0)
		}},
		{"width 0", func() []byte {
			e := entryAt(t, good, tagImageWidth)
			return patch(good, e+8, 0, 0, 0, 0)
		}},
		{"entry count past the end", func() []byte {
			dir := binary.LittleEndian.Uint32(good[4:])
			return patch(good, int(dir), 0xff, 0xff)
		}},
		{"no photometric", func() []byte {
			m := grayU8()
			m.omit = []uint16{tagPhotometric}
			return wfile{images: []wimage{m}}.bytes(t)
		}},
		{"palette without a colour map", func() []byte {
			m := grayU8()
			m.photometric = photoPalette
			return wfile{images: []wimage{m}}.bytes(t)
		}},
		{"rgb with one sample", func() []byte {
			m := grayU8()
			m.photometric = photoRGB
			return wfile{images: []wimage{m}}.bytes(t)
		}},
		{"tiles without offsets", func() []byte {
			m := grayU8()
			m.tiled, m.tw, m.th = true, 16, 16
			m.omit = []uint16{tagTileOffsets}
			return wfile{images: []wimage{m}}.bytes(t)
		}},
		{"truncated LZW", func() []byte {
			m := grayU8()
			m.compression = compressionLZW
			full := wfile{images: []wimage{m}}.bytes(t)
			e := entryAt(t, full, tagStripByteCounts)
			return patch(full, e+8, 3, 0, 0, 0)
		}},
		{"truncated deflate", func() []byte {
			m := grayU8()
			m.compression = compressionDeflate
			full := wfile{images: []wimage{m}}.bytes(t)
			e := entryAt(t, full, tagStripByteCounts)
			return patch(full, e+8, 5, 0, 0, 0)
		}},
		{"truncated packbits", func() []byte {
			m := grayU8()
			m.compression = compressionPackBits
			full := wfile{images: []wimage{m}}.bytes(t)
			e := entryAt(t, full, tagStripByteCounts)
			return patch(full, e+8, 2, 0, 0, 0)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireRefused(t, c.data(), ErrMalformed, "")
		})
	}
	t.Run("bigtiff with 4-byte offsets", func(t *testing.T) {
		big := wfile{big: true, images: []wimage{grayU8()}}.bytes(t)
		requireRefused(t, patch(big, 4, 4), ErrUnsupported, "BigTIFF offset size 4")
	})
}

// TestDecode_HonoursContext stops between chunks once ctx is done.
func TestDecode_HonoursContext(t *testing.T) {
	m := grayU8()
	m.rps = 1
	data := wfile{images: []wimage{m}}.bytes(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Decode(ctx, bytes.NewReader(data), int64(len(data)), "x", Options{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestSniff(t *testing.T) {
	assert.True(t, Sniff([]byte("II*\x00")))
	assert.True(t, Sniff([]byte("MM\x00*")))
	assert.True(t, Sniff([]byte("II+\x00\x08\x00")))
	assert.True(t, Sniff([]byte("MM\x00+")))
	assert.False(t, Sniff([]byte("II\x00*")))
	assert.False(t, Sniff([]byte("MM*\x00")))
	assert.False(t, Sniff([]byte("II*")))
	assert.False(t, Sniff([]byte("\x89PNG\r\n\x1a\n")))
	assert.False(t, Sniff(nil))
}

// TestDecode_Layouts covers the colour models and the bands without one.
func TestDecode_Layouts(t *testing.T) {
	cases := []struct {
		name        string
		spp         int
		photometric uint16
		extras      []uint16
		model       imaging.ColorModelE
		labels      []string
		note        string
	}{
		{"gray", 1, photoBlackIsZero, nil, imaging.ColorModelGray, []string{"gray"}, ""},
		{"gray alpha", 2, photoBlackIsZero, []uint16{2}, imaging.ColorModelGrayAlpha, []string{"gray", "alpha"}, ""},
		{"rgb", 3, photoRGB, nil, imaging.ColorModelRGB, []string{"red", "green", "blue"}, ""},
		{"rgba", 4, photoRGB, []uint16{2}, imaging.ColorModelRGBA, []string{"red", "green", "blue", "alpha"}, ""},
		{"associated alpha", 4, photoRGB, []uint16{1}, 0, []string{"1", "2", "3", "4"}, "associated alpha, which is not divided out"},
		{"unspecified extra", 2, photoBlackIsZero, []uint16{0}, 0, []string{"1", "2"}, "its extra sample is unspecified"},
		{"two alphas", 5, photoRGB, []uint16{2, 2}, 0, []string{"1", "2", "3", "4", "5"}, "2 extra samples (unassociated alpha, unassociated alpha)"},
		{"undeclared extras", 3, photoBlackIsZero, nil, 0, []string{"1", "2", "3"}, "2 extra samples, of which ExtraSamples describes 0"},
		{"min-is-white", 1, photoWhiteIsZero, nil, imaging.ColorModelGray, []string{"gray"}, "nothing was inverted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := grayU8()
			m.planes = samplePlanes(m.kind, m.w, m.h, c.spp, 1)
			m.photometric, m.extras = c.photometric, c.extras
			st, err := decodeBytes(t, wfile{images: []wimage{m}}.bytes(t), Options{})
			require.NoError(t, err)
			assert.Equal(t, c.model, st.Model())
			assert.Equal(t, c.labels, st.Axes()[0].Labels)
			notes := strings.Join(st.Provenance().Notes, " ")
			if c.note != "" {
				assert.Contains(t, notes, c.note)
			} else {
				assert.Empty(t, st.Provenance().Notes)
			}
		})
	}
}

// TestDecode_GDALTags applies GDAL's invalid value and band metadata where
// the planes can carry them, and notes them where not.
func TestDecode_GDALTags(t *testing.T) {
	meta := `<GDALMetadata><Item name="DESCRIPTION" sample="0" role="description">elevation</Item>` +
		`<Item name="SCALE" sample="0" role="scale">0.5</Item><Item name="OFFSET" sample="0" role="offset">-10</Item>` +
		`<Item name="AREA_OR_POINT">Area</Item><Item name="UNITTYPE" sample="0" role="unittype">m</Item></GDALMetadata>`
	cases := []struct {
		name    string
		kind    imaging.SampleKindE
		spp     int
		nodata  string
		meta    string
		invalid float64
		applied bool
		note    string
	}{
		{"i16", imaging.SampleKindI16, 1, "-9999", meta, -9999, true, "GDAL_METADATA: 2 items were not applied"},
		{"u8 out of range", imaging.SampleKindU8, 1, "300", "", 0, false, "is not a value u8 samples can hold exactly"},
		{"u16 fractional", imaging.SampleKindU16, 1, "1.5", "", 0, false, "is not a value u16 samples can hold exactly"},
		{"f32 nan", imaging.SampleKindF32, 1, "nan", "", 0, false, ""},
		{"f32 value", imaging.SampleKindF32, 1, "-3.4028234663852886e+38", "", -math.MaxFloat32, true, ""},
		{"f32 not a float32", imaging.SampleKindF32, 1, "0.1", "", 0, false, "is not a value f32 samples can hold exactly"},
		{"i32 nan", imaging.SampleKindI32, 1, "nan", "", 0, false, "is not a value i32 samples can hold exactly"},
		{"text", imaging.SampleKindU8, 1, "none", "", 0, false, "is not a number"},
		{"rgba", imaging.SampleKindU8, 4, "0", "", 0, false, "the samples are read with alpha"},
		{"rgb", imaging.SampleKindU8, 3, "0", "", 0, true, ""},
		{"bad xml", imaging.SampleKindU8, 1, "", "<GDALMetadata><Item", 0, false, "GDAL_METADATA could not be read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := grayU8()
			m.kind = c.kind
			m.planes = samplePlanes(c.kind, m.w, m.h, c.spp, 1)
			if c.spp >= 3 {
				m.photometric = photoRGB
			}
			if c.spp == 4 {
				m.extras = []uint16{2}
			}
			if c.nodata != "" {
				m.tags = append(m.tags, asciiTag(tagGDALNoData, c.nodata))
			}
			if c.meta != "" {
				m.tags = append(m.tags, asciiTag(tagGDALMetadata, c.meta))
			}
			st, err := decodeBytes(t, wfile{images: []wimage{m}}.bytes(t), Options{})
			require.NoError(t, err)
			for i := range st.PlaneCount() {
				p, _ := st.Plane(st.Index(i))
				got, has := p.Invalid()
				if c.spp == 4 && i == 3 {
					assert.False(t, has)
					continue
				}
				assert.Equal(t, c.applied, has)
				if c.applied {
					assert.Equal(t, c.invalid, got)
				}
			}
			notes := strings.Join(st.Provenance().Notes, " ")
			if c.note != "" {
				assert.Contains(t, notes, c.note)
			} else {
				assert.Empty(t, st.Provenance().Notes)
			}
			if c.meta == meta {
				p, _ := st.Plane([]int{0})
				assert.Equal(t, imaging.ValueTransform{Scale: 0.5, Offset: -10}, p.Transform())
				assert.Equal(t, []string{"elevation"}, st.Axes()[0].Labels)
			}
		})
	}
}
