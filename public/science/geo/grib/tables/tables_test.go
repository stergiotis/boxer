package tables

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func sourceDirs(t *testing.T) (grib2Dir, cctDir string) {
	t.Helper()
	root, ok := CorpusDir.Lookup()
	if !ok || root == "" {
		t.Skipf("%s is not set", "BOXER_GRIB_CORPUS")
	}
	grib2Dir = filepath.Join(root, "wmo-im-GRIB2")
	cctDir = filepath.Join(root, "wmo-im-CCT")
	for _, d := range []string{grib2Dir, cctDir} {
		if _, err := os.Stat(d); err != nil {
			t.Skipf("checkout missing: %v", err)
		}
	}
	return
}

// TestGenerateWmoTables regenerates wmo/ from the checkouts under the
// corpus directory. It is the house shape for a generated artefact: run
// it by name after moving the checkouts to a newer tag.
func TestGenerateWmoTables(t *testing.T) {
	grib2Dir, cctDir := sourceDirs(t)
	v, err := os.ReadFile(filepath.Join("wmo", "VERSION"))
	require.NoError(t, err)
	g, err := Generate(grib2Dir, cctDir, string(bytesTrim(v)))
	require.NoError(t, err)
	require.NoError(t, g.Write("wmo"))
}

func bytesTrim(b []byte) (out []byte) {
	out = b
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == '\r' || out[len(out)-1] == ' ') {
		out = out[:len(out)-1]
	}
	return
}

// TestEmbeddedTablesMatchTheCheckout holds the committed wmo/ files against
// a fresh reduction of the checkouts, so a bump of the source without a
// regeneration is caught.
func TestEmbeddedTablesMatchTheCheckout(t *testing.T) {
	grib2Dir, cctDir := sourceDirs(t)
	g, err := Generate(grib2Dir, cctDir, Version())
	require.NoError(t, err)
	for name, want := range map[string][]byte{"codes.tsv": g.Codes, "flags.tsv": g.Flags, "templates.tsv": g.Templates, "centres.tsv": g.Centres} {
		got, err := wmoFS.ReadFile("wmo/" + name)
		require.NoError(t, err, name)
		require.Equal(t, string(want), string(got), name)
	}
}

// TestLookupsAgainstTheManual: rows a reader of the Manual can check.
func TestLookupsAgainstTheManual(t *testing.T) {
	require.Equal(t, "v37", Version())
	e, ok := Parameter(0, 0, 0)
	require.True(t, ok)
	require.Equal(t, "Temperature", e.Meaning)
	require.Equal(t, "K", e.Unit)
	e, ok = Parameter(0, 1, 8)
	require.True(t, ok)
	require.Equal(t, "Total precipitation", e.Meaning)
	e, ok = Parameter(0, 2, 2)
	require.True(t, ok)
	require.Equal(t, "u-component of wind", e.Meaning)
	_, ok = Parameter(0, 0, 200)
	require.False(t, ok, "local range is never named")
	_, ok = Parameter(0, 0, 255)
	require.False(t, ok)
	e, ok = Code("4.5", 103)
	require.True(t, ok)
	require.Equal(t, "Specified height level above ground", e.Meaning)
	e, ok = Code("4.5", 150)
	require.True(t, ok)
	require.Contains(t, e.Meaning, "Generalized vertical")
	e, ok = Code("0.0", 10)
	require.True(t, ok)
	require.Equal(t, "Oceanographic products", e.Meaning)
	e, ok = Code("5.0", 42)
	require.True(t, ok)
	require.Contains(t, e.Meaning, "CCSDS")
	e, ok = Code("3.1", 101)
	require.True(t, ok)
	require.Contains(t, e.Meaning, "unstructured")
	m, ok := Flag("3.4", 1, 1)
	require.True(t, ok)
	require.Contains(t, m, "-i")
	m, ok = Flag("3.4", 4, 1)
	require.True(t, ok)
	require.Contains(t, m, "opposite")
	name, ok := Centre(98)
	require.True(t, ok)
	require.Contains(t, name, "ECMWF")
	name, ok = Centre(78)
	require.True(t, ok)
	require.Contains(t, name, "Offenbach")
	_, ok = Centre(65535)
	require.False(t, ok)
	layout := Template(4, 40)
	require.NotEmpty(t, layout)
	require.Equal(t, "12-13", layout[2].Octets)
	require.Contains(t, layout[2].Contents, "constituent")
	require.Equal(t, "4.230", layout[2].CodeTable)
	require.Nil(t, Template(9, 9))
	// A range row of a code table resolves for every value in it.
	e, ok = Code("4.5", 200)
	require.True(t, ok)
	require.Equal(t, uint32(192), e.Lo)
}
