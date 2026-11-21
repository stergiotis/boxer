package fits

import (
	"bufio"
	"context"
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

// The fixtures under testdata were written by astropy and the tables beside
// them are astropy's reading of them (testdata/SOURCES.txt), so the reader
// is held to a reference implementation and not only to this package's own
// test writer.

func readTable(t *testing.T, name string) [][]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var rows [][]string
	sc := bufio.NewScanner(f)
	header := true
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if header {
			header = false
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	require.NoError(t, sc.Err())
	require.NotEmpty(t, rows, name)
	return rows
}

func parseF(t *testing.T, s string) float64 {
	t.Helper()
	if s == "nan" {
		return math.NaN()
	}
	v, err := strconv.ParseFloat(s, 64)
	require.NoError(t, err, s)
	return v
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	require.NoError(t, err, s)
	return v
}

type fixtureCache map[string]imaging.Stack

func (inst fixtureCache) get(t *testing.T, file string, hdu int) imaging.Stack {
	t.Helper()
	key := file + "#" + strconv.Itoa(hdu)
	if s, ok := inst[key]; ok {
		return s
	}
	f, err := os.Open(filepath.Join("testdata", file))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	require.NoError(t, err)
	s, err := Decode(context.Background(), f, st.Size(), file, Options{UseHDU: true, HDU: hdu})
	require.NoError(t, err, key)
	inst[key] = s
	return s
}

func TestFixturesSampleForSampleAgainstAstropy(t *testing.T) {
	cache := fixtureCache{}
	n := 0
	for _, row := range readTable(t, "samples.tsv") {
		file, hdu := row[0], atoi(t, row[1])
		s := cache.get(t, file, hdu)
		parts := strings.Split(row[2], ",")
		idx := make([]int64, len(parts))
		for i, p := range parts {
			idx[i] = int64(atoi(t, p) - 1)
		}
		stored, value := parseF(t, row[3]), parseF(t, row[4])
		got, valid := storedAtFITS(t, s, idx)
		if math.IsNaN(stored) {
			assert.False(t, valid, "%s %v: NaN is invalid", file, idx)
			continue
		}
		require.Equal(t, stored, got, "%s hdu %d at %v", file, hdu, idx)
		sel := make([]int, 0, len(idx))
		for k := len(idx) - 1; k >= 2; k-- {
			sel = append(sel, int(idx[k]))
		}
		p, err := s.Plane(sel)
		require.NoError(t, err)
		v, valid := p.ValueAt(int(idx[0]), s.Height()-1-int(idx[1]))
		if math.IsNaN(value) {
			assert.False(t, valid, "%s %v: astropy reads BLANK as NaN", file, idx)
		} else {
			require.True(t, valid, "%s %v", file, idx)
			// astropy scales 16-bit data in float32.
			assert.InDelta(t, value, v, 1e-6*math.Max(1, math.Abs(value)), "%s %v", file, idx)
		}
		n++
	}
	assert.Greater(t, n, 1000)
}

func lonDiff(a, b float64) float64 {
	d := math.Mod(a-b, 360)
	if d > 180 {
		d -= 360
	}
	if d < -180 {
		d += 360
	}
	return d
}

func TestFixturesPlacementAgainstAstropyWCS(t *testing.T) {
	cache := fixtureCache{}
	files := map[string]bool{}
	for _, row := range readTable(t, "wcs.tsv") {
		file, hdu := row[0], atoi(t, row[1])
		s := cache.get(t, file, hdu)
		p := s.Placement()
		require.False(t, p.IsZero(), "%s: %v", file, s.Provenance().Notes)
		require.Equal(t, imaging.FrameKindCelestialGnomonic, p.Frame.Kind, file)
		p1, p2 := parseF(t, row[2]), parseF(t, row[3])
		lon, lat := parseF(t, row[4]), parseF(t, row[5])
		a, b, ok := p.World(p1-0.5, float64(s.Height())+0.5-p2)
		require.True(t, ok)
		assert.InDelta(t, 0, lonDiff(a, lon), 1e-9, "%s lon at (%v, %v)", file, p1, p2)
		assert.InDelta(t, lat, b, 1e-9, "%s lat at (%v, %v)", file, p1, p2)
		col, rowc, ok := p.Pixel(lon, lat)
		require.True(t, ok)
		assert.InDelta(t, p1-0.5, col, 1e-6, "%s inverse", file)
		assert.InDelta(t, float64(s.Height())+0.5-p2, rowc, 1e-6, "%s inverse", file)
		files[file] = true
	}
	for _, f := range []string{"tan_cd.fits", "tan_pc_latfirst.fits", "tan_crota.fits", "tan_gal_arcsec.fits", "tan_pole.fits", "mef.fits"} {
		assert.True(t, files[f], "%s has rows in wcs.tsv", f)
	}
	codes := map[string]string{"tan_cd.fits": "ICRS", "tan_pc_latfirst.fits": "FK5 J2000", "tan_crota.fits": "FK4 B1950",
		"tan_gal_arcsec.fits": "GALACTIC", "tan_pole.fits": "ICRS"}
	for f, code := range codes {
		assert.Equal(t, code, cache.get(t, f, 0).Placement().Frame.Code, f)
	}
	assert.Equal(t, 170.0, cache.get(t, "tan_pole.fits", 0).Placement().Frame.LonPole)
}

func TestFixturesCoordinatesAgainstAstropy(t *testing.T) {
	cache := fixtureCache{}
	for _, row := range readTable(t, "coords.tsv") {
		s := cache.get(t, row[0], atoi(t, row[1]))
		k, pos := atoi(t, row[2]), atoi(t, row[3])
		axes := s.Axes()
		a := axes[len(axes)-1-(k-3)]
		require.NotNil(t, a.Coords, row[0])
		assert.InDelta(t, parseF(t, row[4]), a.Coords[pos-1], 1e-6, "%s axis %d position %d", row[0], k, pos)
		assert.Equal(t, "FREQ", a.Name)
		assert.Equal(t, "Hz", a.Unit)
	}
}

func TestFixturesHDUsAgainstAstropy(t *testing.T) {
	infos := map[string]Info{}
	types := map[string]HDUTypeE{"primary": HDUTypePrimary, "IMAGE": HDUTypeImage, "BINTABLE": HDUTypeBinTable, "TABLE": HDUTypeTable}
	for _, row := range readTable(t, "hdus.tsv") {
		info, ok := infos[row[0]]
		if !ok {
			b, err := os.ReadFile(filepath.Join("testdata", row[0]))
			require.NoError(t, err)
			info, err = inspectBytes(b)
			require.NoError(t, err, row[0])
			infos[row[0]] = info
		}
		i := atoi(t, row[1])
		require.Less(t, i, len(info.HDUs), row[0])
		h := info.HDUs[i]
		assert.Equal(t, types[row[2]], h.Type, "%s %d", row[0], i)
		assert.Equal(t, row[3], h.ExtName, "%s %d", row[0], i)
		assert.Equal(t, atoi(t, row[4]), h.BitPix, "%s %d", row[0], i)
		assert.Len(t, h.Axes, atoi(t, row[5]), "%s %d", row[0], i)
	}
}

func TestFixturesRefusedAndNoted(t *testing.T) {
	open := func(name string, opts Options) (imaging.Stack, error) {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err)
		return decodeBytes(b, opts)
	}
	for name, says := range map[string]string{"bitpix64.fits": "BITPIX 64", "tilecomp.fits": "tile-compressed", "groups.fits": "random groups"} {
		_, err := open(name, Options{})
		assert.ErrorIs(t, err, ErrUnsupported, name)
		assert.Contains(t, err.Error(), says, name)
	}
	s, err := open("tan_sip.fits", Options{})
	require.NoError(t, err)
	assert.True(t, s.Placement().IsZero())
	notes := strings.Join(s.Provenance().Notes, "\n")
	assert.Contains(t, notes, "SIP distortion")
	assert.Contains(t, notes, "A_ORDER declares a distortion")

	s, err = open("sin.fits", Options{})
	require.NoError(t, err)
	assert.True(t, s.Placement().IsZero())
	assert.Contains(t, strings.Join(s.Provenance().Notes, "\n"), "the SIN projection")

	s, err = open("linear.fits", Options{})
	require.NoError(t, err)
	p := s.Placement()
	assert.Equal(t, imaging.FrameKindPhysical, p.Frame.Kind)
	assert.Equal(t, []string{"alternate description A"}, p.Unapplied)
	a, b, ok := p.World(0.5, float64(s.Height())-0.5)
	require.True(t, ok)
	assert.Equal(t, 100.0, a)
	assert.Equal(t, -40.0, b)

	s, err = open("mef.fits", Options{})
	require.NoError(t, err)
	notes = strings.Join(s.Provenance().Notes, "\n")
	assert.Contains(t, notes, "HDU 1 (SCI) of 4 was read")
	assert.Contains(t, notes, "1 table extension(s) (BINTABLE)")

	s, err = open("scaled.fits", Options{})
	require.NoError(t, err)
	assert.Contains(t, s.Provenance().Notes, "values are in Jy/beam")

	gz, err := open("bitpix16.fits.gz", Options{})
	require.NoError(t, err)
	plain, err := open("bitpix16.fits", Options{})
	require.NoError(t, err)
	assert.Equal(t, plain.Hash(), gz.Hash())

	tc, err := os.ReadFile(filepath.Join("testdata", "tilecomp.fits"))
	require.NoError(t, err)
	info, err := inspectBytes(tc)
	require.NoError(t, err)
	assert.Equal(t, -1, info.Default)
	assert.Contains(t, info.HDUs[1].Reason, "tile-compressed")
}
