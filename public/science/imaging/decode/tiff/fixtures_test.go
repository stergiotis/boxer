package tiff

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func openFixture(t *testing.T, name string) (*os.File, int64) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	st, err := f.Stat()
	require.NoError(t, err)
	return f, st.Size()
}

func decodeFixture(t *testing.T, name string) (imaging.Stack, error) {
	t.Helper()
	f, size := openFixture(t, name)
	return Decode(context.Background(), f, size, name, Options{})
}

// sampleDigest is the SHA-256 of a stack's samples as expect.tsv defines
// it: plane order, each plane row-major, samples little-endian.
func sampleDigest(st imaging.Stack) string {
	h := sha256.New()
	for i := range st.PlaneCount() {
		idx := st.Index(i)
		p, _ := st.Plane(idx)
		b := make([]byte, 0, p.Width()*p.Height()*p.Kind().Bytes())
		for _, v := range storedBits(p) {
			switch p.Kind().Bytes() {
			case 1:
				b = append(b, byte(v))
			case 2:
				b = binary.LittleEndian.AppendUint16(b, uint16(v))
			case 4:
				b = binary.LittleEndian.AppendUint32(b, uint32(v))
			case 8:
				b = binary.LittleEndian.AppendUint64(b, v)
			}
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestDecode_ReferenceFixtures reads every file a reference implementation
// wrote and compares it with what that implementation and GDAL say the file
// holds (testdata/SOURCES.txt).
func TestDecode_ReferenceFixtures(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "expect.tsv"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	rows := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		c := strings.Split(line, "\t")
		require.Len(t, c, 15, line)
		name := c[0]
		rows++
		t.Run(name, func(t *testing.T) {
			st, err := decodeFixture(t, name)
			require.NoError(t, err)
			prov := st.Provenance()
			assert.Equal(t, c[1], prov.Format)
			assert.Equal(t, c[2], strconv.Itoa(st.Width()))
			assert.Equal(t, c[3], strconv.Itoa(st.Height()))
			assert.Equal(t, c[4], st.Kind().String())
			pages := 1
			axes := st.Axes()
			if axes[0].Name == "page" {
				pages = axes[0].Size
			}
			assert.Equal(t, c[5], strconv.Itoa(pages))
			band, ok := st.BandAxis()
			require.True(t, ok)
			assert.Equal(t, c[6], strconv.Itoa(axes[band].Size))
			model := st.Model().String()
			if st.Model() == 0 {
				model = "-"
			}
			assert.Equal(t, c[7], model)
			assert.Equal(t, c[8], sampleDigest(st))

			pl := st.Placement()
			if c[9] == "-" {
				assert.NotEqual(t, imaging.FrameKindReferenceSystem, pl.Frame.Kind)
			} else {
				parts := strings.Split(c[9], ",")
				require.Len(t, parts, 6)
				for i, s := range parts {
					want, perr := strconv.ParseFloat(s, 64)
					require.NoError(t, perr)
					assert.InDelta(t, want, pl.Affine[i], 1e-9*max(1, math.Abs(want)), "affine term %d", i)
				}
				assert.Equal(t, c[10], pl.Frame.Code)
			}
			if c[11] == "-" {
				p, _ := st.Plane(make([]int, len(axes)))
				_, has := p.Invalid()
				assert.False(t, has)
			} else {
				want, _ := strconv.ParseFloat(c[11], 64)
				p, _ := st.Plane(make([]int, len(axes)))
				got, has := p.Invalid()
				assert.True(t, has)
				assert.Equal(t, want, got)
			}
			if c[12] != "-" {
				assert.Equal(t, strings.Split(c[12], "|"), axes[band].Labels)
			}
			if c[13] != "-" {
				scales, offsets := strings.Split(c[13], "|"), strings.Split(c[14], "|")
				for b := range scales {
					idx := make([]int, len(axes))
					idx[band] = b
					p, _ := st.Plane(idx)
					s, _ := strconv.ParseFloat(scales[b], 64)
					o, _ := strconv.ParseFloat(offsets[b], 64)
					assert.Equal(t, imaging.ValueTransform{Scale: s, Offset: o}, p.Transform(), "band %d", b)
				}
			}
		})
	}
	require.NoError(t, sc.Err())
	assert.GreaterOrEqual(t, rows, 13)
}

// TestDecode_FixtureDetails holds what expect.tsv has no column for.
func TestDecode_FixtureDetails(t *testing.T) {
	t.Run("pixel-is-point and its pixel-is-area twin place the same ground", func(t *testing.T) {
		area, err := decodeFixture(t, "geo_area.tif")
		require.NoError(t, err)
		point, err := decodeFixture(t, "geo_point.tif")
		require.NoError(t, err)
		pa, pp := area.Placement(), point.Placement()
		assert.Equal(t, pa.Affine, pp.Affine)
		assert.Contains(t, pa.Source, "PixelIsArea")
		assert.Contains(t, pp.Source, "PixelIsPoint")
		// The file ties the centre of pixel (0, 0) to (2600000.25, 1199999.875).
		x, y, ok := pp.World(0.5, 0.5)
		require.True(t, ok)
		assert.Equal(t, 2600000.25, x)
		assert.Equal(t, 1199999.875, y)
		assert.Equal(t, [2]string{"E", "N"}, pp.Frame.AxisNames)
		assert.Equal(t, [2]string{"m", "m"}, pp.Frame.Units)
		assert.Equal(t, imaging.FrameKindReferenceSystem, pp.Frame.Kind)
	})
	t.Run("geographic transformation", func(t *testing.T) {
		st, err := decodeFixture(t, "geo_transform_i32.tif")
		require.NoError(t, err)
		p := st.Placement()
		assert.Equal(t, [2]string{"lon", "lat"}, p.Frame.AxisNames)
		assert.Equal(t, [2]string{"deg", "deg"}, p.Frame.Units)
		assert.Contains(t, p.Source, "ModelTransformation")
	})
	t.Run("palette", func(t *testing.T) {
		st, err := decodeFixture(t, "palette_u8.tif")
		require.NoError(t, err)
		prov := st.Provenance()
		assert.True(t, prov.Derivations.Has(imaging.DerivationPaletteExpanded))
		assert.Equal(t, uint8(8), prov.StoredBitDepth)
		assert.Contains(t, prov.StoredColorType, "Palette")
	})
	t.Run("min-is-white is read as stored and says so", func(t *testing.T) {
		st, err := decodeFixture(t, "miniswhite_u8.tif")
		require.NoError(t, err)
		prov := st.Provenance()
		assert.Contains(t, prov.StoredColorType, "WhiteIsZero")
		assert.Contains(t, strings.Join(prov.Notes, " "), "nothing was inverted")
	})
	t.Run("resolution in centimetres", func(t *testing.T) {
		st, err := decodeFixture(t, "resolution_cm_u16.tif")
		require.NoError(t, err)
		p := st.Placement()
		assert.Equal(t, imaging.FrameKindPhysical, p.Frame.Kind)
		assert.Equal(t, [2]string{"cm", "cm"}, p.Frame.Units)
		assert.InDelta(t, 1.0/200, p.Affine[1], 1e-12)
		assert.InDelta(t, 1.0/400, p.Affine[5], 1e-12)
	})
	t.Run("multipage", func(t *testing.T) {
		st, err := decodeFixture(t, "multipage_u8.tif")
		require.NoError(t, err)
		assert.Contains(t, st.Provenance().Notes, "1 reduced-resolution directory was not read.")
	})
	t.Run("bands without a model", func(t *testing.T) {
		st, err := decodeFixture(t, "bigtiff_tiled_planar_i16.tif")
		require.NoError(t, err)
		axes := st.Axes()
		assert.Equal(t, []string{"1", "2", "3"}, axes[0].Labels)
		assert.Contains(t, strings.Join(st.Provenance().Notes, " "), "Read as 3 bands without a colour model")
	})
	for name, feature := range map[string]string{
		"refused_jpeg.tif": "compression 7 (JPEG)",
		"refused_zstd.tif": "compression 50000 (Zstandard)",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeFixture(t, name)
			require.ErrorIs(t, err, ErrUnsupported)
			assert.Contains(t, err.Error(), feature)
			f, size := openFixture(t, name)
			info, err := Inspect(context.Background(), f, size)
			require.NoError(t, err)
			assert.Equal(t, DirectoryStatusRefused, info.Directories[0].Status)
			assert.Contains(t, info.Directories[0].Reason, feature)
		})
	}
}
