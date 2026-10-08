package grib

import (
	"bufio"
	"compress/bzip2"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

// expectation is one row of testdata/expect.tsv: one field of one message
// of one fixture, as the ecCodes oracle read it (ADR-0292 §R11).
type expectation struct {
	file, result               string
	msg, field                 int
	edition                    int
	offset, length             int64
	discipline, centre         int
	gdt, pdt, drt              int
	ni, nj                     string
	npoints                    int
	reftime, fcst, intervalEnd string
	nmissing                   int
	minV, maxV, sum            string
	digest                     string
	points                     string
}

func readExpectations(t *testing.T) (exp []expectation) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "expect.tsv"))
	require.NoError(t, err)
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	require.True(t, sc.Scan())
	hdr := strings.Split(sc.Text(), "\t")
	col := make(map[string]int, len(hdr))
	for i, h := range hdr {
		col[h] = i
	}
	atoi := func(s string) int {
		if s == "" {
			return 0
		}
		n, err := strconv.Atoi(s)
		require.NoError(t, err)
		return n
	}
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		require.Len(t, p, len(hdr))
		g := func(name string) string { return p[col[name]] }
		exp = append(exp, expectation{
			file: g("file"), result: g("result"), msg: atoi(g("msg")), field: atoi(g("field")), edition: atoi(g("edition")),
			offset: int64(atoi(g("offset"))), length: int64(atoi(g("length"))), discipline: atoi(g("discipline")), centre: atoi(g("centre")),
			gdt: atoi(g("gdt")), pdt: atoi(g("pdt")), drt: atoi(g("drt")), ni: g("ni"), nj: g("nj"), npoints: atoi(g("npoints")),
			reftime: g("reftime"), fcst: g("fcst_s"), intervalEnd: g("interval_end"), nmissing: atoi(g("nmissing")),
			minV: g("min"), maxV: g("max"), sum: g("sum"), digest: g("xxh3"), points: g("points"),
		})
	}
	require.NoError(t, sc.Err())
	return
}

func readFixture(t *testing.T, name string) (buf []byte) {
	t.Helper()
	buf, err := readFixtureE(name)
	require.NoError(t, err)
	return
}

func readFixtureE(name string) (buf []byte, err error) {
	buf, err = os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		return
	}
	if strings.HasSuffix(name, ".bz2") {
		buf, err = io.ReadAll(bzip2.NewReader(strings.NewReader(string(buf))))
	}
	return
}

// canonicalNaN is the NaN pattern the digest uses for a missing value, so
// that the oracle's Python and this reader hash the same bytes.
const canonicalNaN = 0x7FF8000000000000

func digestValues(values []float64) (hex string, missing int, minV, maxV, sum float64) {
	buf := make([]byte, 8*len(values))
	minV, maxV = math.Inf(1), math.Inf(-1)
	for i, v := range values {
		bits := math.Float64bits(v)
		if math.IsNaN(v) {
			bits = canonicalNaN
			missing++
		} else {
			minV = math.Min(minV, v)
			maxV = math.Max(maxV, v)
			sum += v
		}
		binary.LittleEndian.PutUint64(buf[8*i:], bits)
	}
	hex = strconv.FormatUint(xxh3.Hash(buf), 16)
	for len(hex) < 16 {
		hex = "0" + hex
	}
	return
}

// TestFixturesMatchEcCodes holds every field of every fixture against the
// oracle: framing (message count and offsets), the template numbers, the
// reference and forecast times, the decoded values by digest, and sampled
// grid points — or the named refusal the oracle's row predicts.
func TestFixturesMatchEcCodes(t *testing.T) {
	exp := readExpectations(t)
	byFile := make(map[string][]expectation)
	for _, e := range exp {
		byFile[e.file] = append(byFile[e.file], e)
	}
	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)
	fixtures := 0
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".tsv") {
			continue
		}
		fixtures++
		rows := byFile[name]
		require.NotEmpty(t, rows, "fixture %s has no expectation rows", name)
		t.Run(name, func(t *testing.T) {
			checkFixture(t, name, readFixture(t, name), rows)
		})
	}
	require.Greater(t, fixtures, 25)
}

func checkFixture(t *testing.T, name string, buf []byte, rows []expectation) {
	msgIdx := 0
	ri := 0
	for m, err := range ScanBytes(buf) {
		msgIdx++
		if err != nil {
			require.Less(t, ri, len(rows), "%s: scan error past the last expected row: %v", name, err)
			require.Equal(t, "malformed", rows[ri].result, "%s message %d: %v", name, msgIdx, err)
			require.ErrorIs(t, err, ErrMalformed)
			ri++
			break
		}
		require.Less(t, ri, len(rows), "%s: more messages than expected", name)
		e := rows[ri]
		require.Equal(t, e.msg, msgIdx, "%s: message numbering", name)
		require.Equal(t, e.edition, int(m.Edition), "%s message %d edition", name, msgIdx)
		require.Equal(t, e.offset, m.Offset, "%s message %d offset", name, msgIdx)
		if countFields(rows, ri) == 1 {
			// The oracle reports a multi-field message's length per field.
			require.Equal(t, e.length, m.Length, "%s message %d length", name, msgIdx)
		}
		require.Len(t, m.Fields, countFields(rows, ri), "%s message %d field count", name, msgIdx)
		for _, f := range m.Fields {
			e = rows[ri]
			require.Equal(t, e.field, f.Index+1)
			where := name + " msg " + strconv.Itoa(msgIdx) + " field " + strconv.Itoa(f.Index+1)
			checkField(t, where, m, f, e)
			ri++
		}
	}
	require.Equal(t, len(rows), ri, "%s: fewer messages than expected", name)
}

func countFields(rows []expectation, from int) (n int) {
	for i := from; i < len(rows) && rows[i].msg == rows[from].msg; i++ {
		n++
	}
	return
}

func checkField(t *testing.T, where string, m *Message, f *Field, e expectation) {
	require.Equal(t, e.centre, int(m.Ident.Centre), where)
	require.Equal(t, e.npoints, f.NumPoints(), where)
	require.Equal(t, e.reftime, m.Ident.RefTime.Format("20060102150405"), where)
	if e.ni != "" {
		ni, nj, ok := f.Grid.Dims()
		require.True(t, ok, where)
		require.Equal(t, e.ni, strconv.Itoa(int(ni)), where)
		require.Equal(t, e.nj, strconv.Itoa(int(nj)), where)
	}
	if m.Edition == 1 {
		// The oracle's grid column is code table 6, its product column the
		// time range indicator.
		h := m.Grib1
		require.NotNil(t, h, where)
		require.Equal(t, e.gdt, int(f.Grid.Grib1Type), where)
		require.Equal(t, e.pdt, int(h.TimeRange), where)
		require.True(t, f.Packing.Grib1, where)
		if e.fcst != "" {
			require.True(t, h.ForecastOffsetOK, where)
			require.Equal(t, e.fcst, strconv.FormatInt(int64(h.ForecastOffset.Seconds()), 10), where)
		}
		if e.intervalEnd != "" {
			require.True(t, h.HasInterval, where)
			require.Equal(t, e.intervalEnd, m.Ident.RefTime.Add(h.IntervalEnd).Format("20060102150405"), where)
		}
	} else {
		require.Equal(t, e.discipline, int(m.Discipline), where)
		require.Equal(t, e.gdt, int(f.Grid.Template), where)
		require.Equal(t, e.pdt, int(f.Product.Template), where)
		require.Equal(t, e.drt, int(f.Packing.Template), where)
	}
	if m.Edition == 2 && f.Product.Prefixed {
		if e.fcst != "" {
			require.True(t, f.Product.ForecastOffsetOK, where)
			require.Equal(t, e.fcst, strconv.FormatInt(int64(f.Product.ForecastOffset.Seconds()), 10), where)
		}
		if e.intervalEnd != "" {
			require.NotNil(t, f.Product.Statistics, where)
			require.Equal(t, e.intervalEnd, f.Product.Statistics.IntervalEnd.Format("20060102150405"), where)
		}
	}
	values, err := f.ValuesE(nil)
	if feature, ok := strings.CutPrefix(e.result, "unsupported:"); ok {
		require.ErrorIs(t, err, ErrUnsupported, where)
		got, named := UnsupportedFeature(err)
		require.True(t, named, where)
		require.Equal(t, feature, got, where)
		return
	}
	require.Equal(t, "ok", e.result, where)
	if feature, ok := UnsupportedFeature(err); ok {
		t.Fatalf("%s: unexpected refusal of %q", where, feature)
	}
	require.NoError(t, err, where)
	require.Len(t, values, e.npoints, where)
	digest, missing, minV, maxV, sum := digestValues(values)
	require.Equal(t, e.nmissing, missing, where)
	if e.minV != "" {
		wantMin, err := strconv.ParseFloat(e.minV, 64)
		require.NoError(t, err)
		wantMax, err := strconv.ParseFloat(e.maxV, 64)
		require.NoError(t, err)
		require.InDelta(t, wantMin, minV, 1e-9*math.Max(1, math.Abs(wantMin)), where)
		require.InDelta(t, wantMax, maxV, 1e-9*math.Max(1, math.Abs(wantMax)), where)
	}
	wantSum, err := strconv.ParseFloat(e.sum, 64)
	require.NoError(t, err)
	require.InDelta(t, wantSum, sum, 1e-9*math.Max(1, math.Abs(wantSum)), where+" sum")
	require.Equal(t, e.digest, digest, where+" digest")
	checkPoints(t, where, f, e.points)
}

func checkPoints(t *testing.T, where string, f *Field, want string) {
	points, err := f.Grid.PointsE()
	if want == "" {
		return
	}
	if want == "unsupported" {
		require.ErrorIs(t, err, ErrUnsupported, where)
		return
	}
	if feature, ok := UnsupportedFeature(err); ok {
		t.Fatalf("%s: unexpected refusal of points: %q", where, feature)
	}
	require.NoError(t, err, where)
	wanted := make(map[int][2]float64)
	for _, item := range strings.Split(want, ";") {
		idx, coords, _ := strings.Cut(item, "=")
		latS, lonS, _ := strings.Cut(coords, "/")
		i, err := strconv.Atoi(idx)
		require.NoError(t, err)
		lat, err := strconv.ParseFloat(latS, 64)
		require.NoError(t, err)
		lon, err := strconv.ParseFloat(lonS, 64)
		require.NoError(t, err)
		wanted[i] = [2]float64{lat, lon}
	}
	// The oracle prints five decimals; a rotated grid is un-rotated by both
	// sides through their own trigonometry, which agree to about 3 metres.
	tol := 2e-5
	if f.Grid.LatLon != nil && f.Grid.LatLon.Rotated != nil {
		tol = 1e-3
	}
	n := 0
	for lat, lon := range points {
		if w, ok := wanted[n]; ok {
			require.InDelta(t, w[0], lat, tol, "%s point %d latitude", where, n)
			dlon := math.Mod(math.Abs(lon-w[1]), 360)
			if dlon > 180 {
				dlon = 360 - dlon
			}
			require.LessOrEqual(t, dlon, tol, "%s point %d longitude: got %v want %v", where, n, lon, w[1])
		}
		n++
	}
	require.Equal(t, f.NumPoints(), n, where+" point count")
}

// TestUnsupportedIsNamed checks the refusal contract on a message the reader
// parses but does not decode, and that the error chain is the documented one.
func TestUnsupportedIsNamed(t *testing.T) {
	err := unsupportedE("packing template 5.42")
	require.ErrorIs(t, err, ErrUnsupported)
	feature, ok := UnsupportedFeature(err)
	require.True(t, ok)
	require.Equal(t, "packing template 5.42", feature)
	_, ok = UnsupportedFeature(errors.New("other"))
	require.False(t, ok)
}
