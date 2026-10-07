package grib

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func runCli(t *testing.T, args ...string) (out string) {
	t.Helper()
	var buf bytes.Buffer
	app := &cli.App{Name: "test", Writer: &buf, ErrWriter: &buf, Commands: []*cli.Command{NewCliCommand()}}
	require.NoError(t, app.Run(append([]string{"test", "grib"}, args...)))
	out = buf.String()
	return
}

// TestLsInventoriesFieldsAndRefusals: one line per field, the refusal named
// on the line, and a malformed message ending the listing.
func TestLsInventoriesFieldsAndRefusals(t *testing.T) {
	out := runCli(t, "ls", filepath.Join("testdata", "ec_tigge_pf_ecmwf.grib2"))
	require.Equal(t, 38, strings.Count(out, "\n"))
	require.Contains(t, out, "member=")
	out = runCli(t, "ls", filepath.Join("testdata", "syn_ccsds_b16_j32_r128_f14.grib2"))
	require.Contains(t, out, "drt=42")
	out = runCli(t, "ls", filepath.Join("testdata", "gfs_apcp6.grib2"))
	require.Contains(t, out, "until=2026-09-27T06:00:00Z")
	out = runCli(t, "ls", filepath.Join("testdata", "ec_run_length_packing.grib2"))
	require.Contains(t, out, "refuse: packing template 5.200")
	out = runCli(t, "ls", filepath.Join("testdata", "ec_bad.grib"))
	require.Contains(t, out, "refuse: grib1 spectral packing")
	require.Contains(t, out, "!! ")
}

// TestDumpPrintsStatisticsAndValues covers the decode path of the command
// and the values-with-points listing on a regular grid.
func TestDumpPrintsStatisticsAndValues(t *testing.T) {
	out := runCli(t, "dump", filepath.Join("testdata", "ec_regular_latlon_surface.grib2"))
	require.Contains(t, out, "grid: template 3.0, 496 points")
	require.Contains(t, out, "values: 496, 0 missing")
	out = runCli(t, "dump", "--values", "--points", "--field", "1", filepath.Join("testdata", "ec_regular_latlon_surface.grib2"))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.True(t, strings.HasPrefix(lines[len(lines)-1], "0.000000 30.000000 "), lines[len(lines)-1])
	require.True(t, strings.HasPrefix(lines[len(lines)-496], "60.000000 0.000000 "), lines[len(lines)-496])
}

// TestIndexRoundTrips: the index lists every field, reads back, and each
// entry fetches its field again from the file.
func TestIndexRoundTrips(t *testing.T) {
	buf := readFixture(t, "ec_tigge_pf_ecmwf.grib2")
	entries, err := IndexBytesE(buf)
	require.NoError(t, err)
	require.Len(t, entries, 38)
	var out bytes.Buffer
	require.NoError(t, WriteIndexE(&out, entries))
	lines := out.String()
	back, err := ReadIndexE(strings.NewReader(lines))
	require.NoError(t, err)
	require.Equal(t, entries, back)
	e := back[5]
	require.EqualValues(t, 11, e.ProductTemplate)
	f, err := e.ReadE(bytes.NewReader(buf), int64(len(buf)))
	require.NoError(t, err)
	require.EqualValues(t, 11, f.Product.Template)
	require.Equal(t, e.RefTime, f.Message.Ident.RefTime)
	// A message fetched on its own, by range, is read at offset zero.
	own := buf[e.Offset : e.Offset+e.Length]
	f, err = e.At(0).ReadE(bytes.NewReader(own), int64(len(own)))
	require.NoError(t, err)
	require.EqualValues(t, 11, f.Product.Template)
	// The command prints the same lines.
	printed := runCli(t, "index", filepath.Join("testdata", "ec_tigge_pf_ecmwf.grib2"))
	require.Equal(t, lines, printed)
	// Refusals are named in the index.
	entries, err = IndexBytesE(readFixture(t, "ec_run_length_packing.grib2"))
	require.NoError(t, err)
	require.Equal(t, "packing template 5.200", entries[0].Refuses)
	// A truncated tail ends the listing with an error after the good entries.
	entries, err = IndexBytesE(buf[:len(buf)-100])
	require.ErrorIs(t, err, ErrMalformed)
	require.Len(t, entries, 37)
}

// TestNamesComeFromTheTables: ls and dump name what the WMO tables name.
func TestNamesComeFromTheTables(t *testing.T) {
	out := runCli(t, "ls", filepath.Join("testdata", "gfs_apcp6.grib2"))
	require.Contains(t, out, `"Total precipitation"`)
	out = runCli(t, "dump", filepath.Join("testdata", "gfs_apcp6.grib2"))
	require.Contains(t, out, "(NCEP")
	require.Contains(t, out, `"Ground or water surface"`)
}
