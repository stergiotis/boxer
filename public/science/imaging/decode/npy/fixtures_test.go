package npy

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// The fixtures under testdata were written by numpy, and values.tsv is
// numpy's reading of them (testdata/SOURCES.txt).

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
	require.NotEmpty(t, rows)
	return rows
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestFixturesSampleForSampleAgainstNumpy(t *testing.T) {
	cache := map[string]imaging.Stack{}
	n := 0
	for _, row := range readTable(t, "values.tsv") {
		key := row[0] + "#" + row[1]
		s, ok := cache[key]
		if !ok {
			var err error
			s, err = decodeBytes(fixture(t, row[0]), Options{Member: row[1]})
			require.NoError(t, err, key)
			cache[key] = s
		}
		parts := strings.Split(row[2], ",")
		idx := make([]int64, len(parts))
		for i, p := range parts {
			v, err := strconv.ParseInt(p, 10, 64)
			require.NoError(t, err)
			idx[i] = v
		}
		got, valid := storedAtC(t, s, idx)
		if row[3] == "nan" {
			assert.False(t, valid, "%s %v", key, idx)
			continue
		}
		want, err := strconv.ParseFloat(row[3], 64)
		require.NoError(t, err)
		require.True(t, valid, "%s %v", key, idx)
		require.Equal(t, want, got, "%s %v", key, idx)
		n++
	}
	assert.Greater(t, n, 500)
	for _, name := range []string{"fortran_i4.npy", "fortran2d_f8.npy", "fortran4d_u1.npy"} {
		assert.True(t, cache[name+"#"].Provenance().Derivations.Has(imaging.DerivationAxesReordered), name)
	}
	assert.Len(t, cache["nd4_i2.npy#"].Axes(), 2)
	assert.Equal(t, uint8(32), cache["v3_f4.npy#"].Provenance().StoredBitDepth)
}

func TestFixturesArchiveListingAgainstNumpy(t *testing.T) {
	byFile := map[string][]Member{}
	for _, row := range readTable(t, "members.tsv") {
		ms, ok := byFile[row[0]]
		if !ok {
			b := fixture(t, row[0])
			var err error
			ms, err = InspectArchive(context.Background(), bytes.NewReader(b), int64(len(b)))
			require.NoError(t, err)
			byFile[row[0]] = ms
		}
		var m Member
		for _, c := range ms {
			if c.Name == row[1] {
				m = c
			}
		}
		require.Equal(t, row[1], m.Name)
		assert.Equal(t, row[2], m.Header.Descr, row[1])
		var shape []string
		for _, s := range m.Header.Shape {
			shape = append(shape, strconv.FormatInt(s, 10))
		}
		assert.Equal(t, row[3], strings.Join(shape, ","), row[1])
	}
	_, err := decodeBytes(fixture(t, "multi.npz"), Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "image.npy")
	s, err := decodeBytes(fixture(t, "single.npz"), Options{})
	require.NoError(t, err, "the only array is the default")
	assert.Equal(t, imaging.SampleKindI16, s.Kind())
	_, err = decodeBytes(fixture(t, "multi.npz"), Options{Member: "line"})
	assert.Contains(t, err.Error(), "one-dimensional")
	_, err = decodeBytes(fixture(t, "multi.npz"), Options{Member: "flags"})
	assert.Contains(t, err.Error(), "boolean")
}

func TestFixturesRefusedByName(t *testing.T) {
	cases := map[string]string{"bool.npy": "boolean", "i8.npy": "64-bit integers", "u4.npy": "32-bit unsigned", "f2.npy": "16-bit floats",
		"c16.npy": "complex", "str.npy": "strings", "datetime.npy": "datetimes", "structured.npy": "structured dtype",
		"line.npy": "one-dimensional", "object.npy": "unpickling"}
	for name, says := range cases {
		_, err := decodeBytes(fixture(t, name), Options{})
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrUnsupported, name)
		assert.Contains(t, err.Error(), says, name)
	}
}
