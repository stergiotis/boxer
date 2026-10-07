//go:build integration

package grib

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/grib/tables"
)

// TestCorpusMatchesTheOracleDumps walks every survey corpus under
// BOXER_GRIB_CORPUS (ADR-0292 §R11): each sample whose first field
// the oracle dumped is decoded and held to the dump at 10⁻⁹ relative, with
// the dump's −9.999×10²⁰ sentinel as missing; every other sample is scanned
// and decoded, and only a named refusal is allowed to stop a field.
func TestCorpusMatchesTheOracleDumps(t *testing.T) {
	root, ok := tables.CorpusDir.Lookup()
	if !ok || root == "" {
		t.Skipf("%s is not set", "BOXER_GRIB_CORPUS")
	}
	surveys, _ := filepath.Glob(filepath.Join(root, "survey-*"))
	if len(surveys) == 0 {
		t.Skipf("no survey corpus under %s", root)
	}
	const sentinel = -9.999e20
	compared, refused, decoded := 0, 0, 0
	for _, survey := range surveys {
		var files []string
		require.NoError(t, filepath.WalkDir(filepath.Join(survey, "samples"), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".txt", ".md5", ".zip", ".idx", ".index", ".html", ".tsv", ".py":
				return nil
			}
			files = append(files, path)
			return nil
		}))
		for _, path := range files {
			name := filepath.Base(path)
			t.Run(name, func(t *testing.T) {
				buf, err := os.ReadFile(path)
				require.NoError(t, err)
				stem := strings.TrimSuffix(strings.TrimSuffix(name, ".grib2"), ".bin")
				dumpPath := filepath.Join(survey, "expect", stem+".f64")
				dump, dumpErr := os.ReadFile(dumpPath)
				first := true
				for m, scanErr := range ScanBytes(buf) {
					if scanErr != nil {
						// The corpus has a deliberately broken message and
						// byte-range cuts; both must be malformed, not panics.
						require.ErrorIs(t, scanErr, ErrMalformed)
						break
					}
					for _, f := range m.Fields {
						values, err := f.ValuesE(nil)
						if err != nil {
							if feature, named := UnsupportedFeature(err); named {
								t.Logf("%s message at %d: refuses %s", name, m.Offset, feature)
								refused++
								first = false
								continue
							}
							require.NoError(t, err, "%s message at %d", name, m.Offset)
						}
						decoded++
						if first && dumpErr == nil {
							require.Len(t, dump, 8*len(values), "%s: dump length", name)
							for i, v := range values {
								want := math.Float64frombits(binary.LittleEndian.Uint64(dump[8*i:]))
								if want == sentinel {
									require.True(t, math.IsNaN(v), "%s value %d: want missing, got %v", name, i, v)
									continue
								}
								require.False(t, math.IsNaN(v), "%s value %d: got missing, want %v", name, i, want)
								require.InDelta(t, want, v, 1e-9*math.Max(1, math.Abs(want)), "%s value %d", name, i)
							}
							compared++
						}
						first = false
						if points, err := f.Grid.PointsE(); err == nil {
							n := 0
							for range points {
								n++
							}
							require.Equal(t, f.NumPoints(), n, "%s: point count", name)
						} else {
							require.True(t, errors.Is(err, ErrUnsupported), "%s: points: %v", name, err)
						}
					}
				}
			})
		}
	}
	t.Logf("compared %d first fields against dumps, decoded %d fields, %d refusals", compared, decoded, refused)
	require.Greater(t, compared, 50)
}
