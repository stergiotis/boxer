package tiff

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func decodeBytes(t testing.TB, b []byte, opts Options) (imaging.Stack, error) {
	t.Helper()
	return Decode(context.Background(), bytes.NewReader(b), int64(len(b)), "test.tif", opts)
}

// layoutCase is one sample layout: samples per pixel, the photometric
// interpretation and extra samples, and what the stack should be.
type layoutCase struct {
	spp         int
	photometric uint16
	extras      []uint16
	model       imaging.ColorModelE
}

var layoutCases = []layoutCase{
	{1, photoBlackIsZero, nil, imaging.ColorModelGray},
	{3, photoRGB, nil, imaging.ColorModelRGB},
	{4, photoRGB, []uint16{2}, imaging.ColorModelRGBA},
	{7, photoBlackIsZero, []uint16{0, 0, 0, 0, 0, 0}, 0},
}

func predictorsFor(kind imaging.SampleKindE) []uint16 {
	if kind.IsFloat() {
		return []uint16{1, 3}
	}
	return []uint16{1, 2}
}

// TestDecode_Matrix writes a file for every combination of flavour, byte
// order, chunk layout, planar configuration, compression, predictor, sample
// kind and sample layout, and reads every sample back.
func TestDecode_Matrix(t *testing.T) {
	compressions := []uint16{compressionNone, compressionPackBits, compressionLZW, compressionDeflate, compressionDeflateOld}
	type chunking struct {
		name   string
		tiled  bool
		tw, th int
		rps    int
	}
	chunkings := []chunking{
		{name: "one strip"},
		{name: "strips of 4", rps: 4},
		{name: "tiles 16", tiled: true, tw: 16, th: 16},
		{name: "tiles 8x4", tiled: true, tw: 8, th: 4},
	}
	const w, h = 13, 11
	n := 0
	for _, big := range []bool{false, true} {
		for _, order := range []byteOrderI{binary.LittleEndian, binary.BigEndian} {
			for _, ch := range chunkings {
				for _, planar := range []bool{false, true} {
					for _, comp := range compressions {
						for _, kind := range imaging.AllSampleKinds {
							for _, pred := range predictorsFor(kind) {
								for _, lc := range layoutCases {
									n++
									planes := samplePlanes(kind, w, h, lc.spp, n)
									img := wimage{w: w, h: h, kind: kind, planes: planes, photometric: lc.photometric,
										extras: lc.extras, planar: planar, tiled: ch.tiled, tw: ch.tw, th: ch.th, rps: ch.rps,
										compression: comp, predictor: pred}
									name := fmt.Sprintf("big=%v %v %s planar=%v comp=%d %s pred=%d spp=%d",
										big, order, ch.name, planar, comp, kind, pred, lc.spp)
									data := wfile{big: big, order: order, images: []wimage{img}}.bytes(t)
									st, err := decodeBytes(t, data, Options{})
									if !assert.NoError(t, err, name) {
										continue
									}
									checkStack(t, name, st, kind, lc.model, [][][]float64{planes})
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d files", n)
}

// checkStack asserts a stack holds the given pages of planes exactly.
func checkStack(t *testing.T, name string, st imaging.Stack, kind imaging.SampleKindE, model imaging.ColorModelE, pages [][][]float64) {
	t.Helper()
	require.Equal(t, model, st.Model(), name)
	require.Equal(t, len(pages)*len(pages[0]), st.PlaneCount(), name)
	axes := st.Axes()
	bandAxis, ok := st.BandAxis()
	require.True(t, ok, name)
	require.Equal(t, len(pages[0]), axes[bandAxis].Size, name)
	if len(pages) > 1 {
		require.Equal(t, "page", axes[0].Name, name)
		require.Equal(t, imaging.AxisRoleSelector, axes[0].Role, name)
	}
	outKind := kind
	for pg, planes := range pages {
		for b, want := range planes {
			index := []int{b}
			if len(pages) > 1 {
				index = []int{pg, b}
			}
			p, err := st.Plane(index)
			require.NoError(t, err, name)
			require.Equal(t, outKind, p.Kind(), name)
			if !slices.Equal(expectedBits(kind, want), storedBits(p)) {
				t.Fatalf("%s: page %d band %d differs", name, pg, b)
			}
		}
	}
}

// TestDecode_EdgeCases covers chunks that do not divide the image: tiles
// larger than it, a last strip shorter than RowsPerStrip, odd widths under
// each predictor; and directories beside the image: pages, reduced
// resolutions, a mask and one unlike the first.
func TestDecode_EdgeCases(t *testing.T) {
	t.Run("tile larger than the image", func(t *testing.T) {
		planes := samplePlanes(imaging.SampleKindU16, 5, 3, 3, 1)
		data := wfile{images: []wimage{{w: 5, h: 3, kind: imaging.SampleKindU16, planes: planes, photometric: photoRGB,
			tiled: true, tw: 32, th: 48, compression: compressionLZW, predictor: 2}}}.bytes(t)
		st, err := decodeBytes(t, data, Options{})
		require.NoError(t, err)
		checkStack(t, "", st, imaging.SampleKindU16, imaging.ColorModelRGB, [][][]float64{planes})
	})
	t.Run("last strip short", func(t *testing.T) {
		for _, rps := range []int{1, 2, 3, 6, 7, 100} {
			planes := samplePlanes(imaging.SampleKindI32, 9, 7, 1, rps)
			data := wfile{big: true, images: []wimage{{w: 9, h: 7, kind: imaging.SampleKindI32, planes: planes,
				photometric: photoBlackIsZero, rps: rps, compression: compressionDeflate, predictor: 2}}}.bytes(t)
			st, err := decodeBytes(t, data, Options{})
			require.NoError(t, err)
			checkStack(t, fmt.Sprint("rps ", rps), st, imaging.SampleKindI32, imaging.ColorModelGray, [][][]float64{planes})
		}
	})
	t.Run("odd widths", func(t *testing.T) {
		for _, w := range []int{1, 2, 3, 17, 31} {
			for _, kind := range imaging.AllSampleKinds {
				for _, pred := range predictorsFor(kind) {
					planes := samplePlanes(kind, w, 3, 3, w)
					data := wfile{order: binary.BigEndian, images: []wimage{{w: w, h: 3, kind: kind, planes: planes,
						photometric: photoRGB, compression: compressionLZW, predictor: pred, tiled: true, tw: 16, th: 16}}}.bytes(t)
					st, err := decodeBytes(t, data, Options{})
					require.NoError(t, err)
					checkStack(t, fmt.Sprint(w, kind, pred), st, kind, imaging.ColorModelRGB, [][][]float64{planes})
				}
			}
		}
	})
	t.Run("pages and directories that are not", func(t *testing.T) {
		kind := imaging.SampleKindF32
		mk := func(seed int, subfile uint32, w int) wimage {
			return wimage{w: w, h: 6, kind: kind, planes: samplePlanes(kind, w, 6, 1, seed), photometric: photoBlackIsZero,
				compression: compressionDeflate, predictor: 3, subfile: subfile, rps: 2}
		}
		images := []wimage{mk(1, 0, 10), mk(2, 1, 5), mk(3, 2, 10), mk(4, 4, 10), mk(5, 0, 9), mk(6, 0, 10), mk(7, 1, 3)}
		images[5].tiled, images[5].tw, images[5].th = true, 16, 16
		images[5].planar = true
		data := wfile{big: true, order: binary.BigEndian, images: images}.bytes(t)
		st, err := decodeBytes(t, data, Options{})
		require.NoError(t, err)
		checkStack(t, "pages", st, kind, imaging.ColorModelGray,
			[][][]float64{images[0].planes, images[2].planes, images[5].planes})
		notes := strings.Join(st.Provenance().Notes, "\n")
		assert.Contains(t, notes, "2 reduced-resolution directories were not read.")
		assert.Contains(t, notes, "1 mask directory was not read.")
		assert.Contains(t, notes, "1 directory unlike the first in size, sample kind or layout was not read.")

		info, err := Inspect(context.Background(), bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
		assert.Equal(t, "bigtiff", info.Format)
		assert.True(t, info.BigEndian)
		assert.Equal(t, 3, info.Pages)
		var statuses []DirectoryStatusE
		for _, d := range info.Directories {
			statuses = append(statuses, d.Status)
		}
		assert.Equal(t, []DirectoryStatusE{DirectoryStatusRead, DirectoryStatusSkipped, DirectoryStatusRead,
			DirectoryStatusSkipped, DirectoryStatusSkipped, DirectoryStatusRead, DirectoryStatusSkipped}, statuses)
		assert.Equal(t, "differs from the first directory in size", info.Directories[4].Reason)
		assert.Equal(t, "a reduced-resolution image", info.Directories[1].Reason)
		assert.Equal(t, "Deflate", info.Directories[0].CompressionName)
		assert.True(t, info.Directories[5].Tiled)
		assert.True(t, info.Directories[5].Planar)
	})
	t.Run("a later page that is refused refuses the file", func(t *testing.T) {
		a := wimage{w: 4, h: 4, kind: imaging.SampleKindU8, planes: samplePlanes(imaging.SampleKindU8, 4, 4, 1, 1),
			photometric: photoBlackIsZero, compression: compressionNone}
		b := a
		b.compression = 7
		data := wfile{images: []wimage{a, b}}.bytes(t)
		_, err := decodeBytes(t, data, Options{})
		require.ErrorIs(t, err, ErrUnsupported)
		assert.Contains(t, err.Error(), "compression 7 (JPEG)")
		info, err := Inspect(context.Background(), bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
		assert.Equal(t, DirectoryStatusRefused, info.Directories[1].Status)
		assert.Equal(t, 0, info.Pages)
	})
}

// TestLZW_LongStreams holds the test-side LZW encoder to the decoder over
// streams long enough to pass every code width and a table reset.
func TestLZW_LongStreams(t *testing.T) {
	for _, n := range []int{0, 1, 2, 300, 600, 5000, 40000} {
		src := make([]byte, n)
		for i := range src {
			src[i] = byte((i*i*31 + i/7) >> 3)
		}
		img := wimage{w: max(n, 1), h: 1, kind: imaging.SampleKindU8, photometric: photoBlackIsZero, compression: compressionLZW}
		v := make([]float64, max(n, 1))
		for i := range n {
			v[i] = float64(src[i])
		}
		img.planes = [][]float64{v}
		data := wfile{images: []wimage{img}}.bytes(t)
		st, err := decodeBytes(t, data, Options{})
		require.NoError(t, err, n)
		checkStack(t, fmt.Sprint(n), st, imaging.SampleKindU8, imaging.ColorModelGray, [][][]float64{img.planes})
	}
}
