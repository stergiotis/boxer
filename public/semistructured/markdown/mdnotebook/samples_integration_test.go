//go:build integration

package mdnotebook_test

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

var updateSamples = flag.Bool("update-samples", false, "collect the sample notebooks again and write them")

const samplesDir = "samples"

// sameRows compares two records by what a reader sees: the column names and
// every value's text. The bytes are not compared, since how a server types a
// column in Arrow is its own choice from one release to the next.
func sameRows(t *testing.T, name string, want, got arrow.RecordBatch) {
	t.Helper()
	require.Equal(t, want.NumCols(), got.NumCols(), name)
	require.Equal(t, want.NumRows(), got.NumRows(), name)
	for col := 0; col < int(want.NumCols()); col++ {
		require.Equal(t, want.ColumnName(col), got.ColumnName(col), name)
		for row := 0; row < int(want.NumRows()); row++ {
			require.Equalf(t, want.Column(col).ValueStr(row), got.Column(col).ValueStr(row),
				"%s.%s, row %d", name, want.ColumnName(col), row)
		}
	}
}

// The sample notebooks hold what their own statements answer: each one,
// collected again against a local ClickHouse, has the rows it stores and
// the figures its text reads. With -update-samples the files are written
// instead, which is how a sample is added or changed: edit its text, then
// collect.
func TestSamplesAreWhatCollectionWrites(t *testing.T) {
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		t.Skip("clickhouse-local is not installed")
	}
	files, err := filepath.Glob(filepath.Join(samplesDir, "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".md")
		t.Run(stem, func(t *testing.T) {
			src, rErr := os.ReadFile(file)
			require.NoError(t, rErr)
			nb, rErr := mdnotebook.Read(src)
			require.NoError(t, rErr)
			blobDir := stem + ".blobs"
			out, cErr := mdnotebook.Collect(context.Background(), nb, blobDir, clickhouseLocal(""))
			require.NoError(t, cErr)

			if *updateSamples {
				require.NoError(t, os.MkdirAll(filepath.Join(samplesDir, blobDir), 0o755))
				for p, data := range out.Blobs {
					require.NoError(t, os.WriteFile(filepath.Join(samplesDir, filepath.FromSlash(p)), data, 0o644))
				}
				entries, dErr := os.ReadDir(filepath.Join(samplesDir, blobDir))
				require.NoError(t, dErr)
				for _, e := range entries {
					p := blobDir + "/" + e.Name()
					if _, kept := out.Blobs[p]; !kept && strings.HasSuffix(e.Name(), mdnotebook.RecordExt) {
						require.NoError(t, os.Remove(filepath.Join(samplesDir, blobDir, e.Name())))
					}
				}
				require.NoError(t, os.WriteFile(file, out.Text, 0o644))
				return
			}

			again, rErr := mdnotebook.Read(out.Text)
			require.NoError(t, rErr)
			assert.Equal(t, string(nb.Src[nb.BodyStart:]), string(again.Src[again.BodyStart:]),
				"the text is not what collection writes; run with -update-samples")

			stored := mdnotebook.Load(nb, os.DirFS(samplesDir))
			defer stored.Release()
			require.Empty(t, mdnotebook.Problems(nb, stored))
			require.Empty(t, stored.Stale, "a record was edited since the sample was collected; run with -update-samples")
			// What collection wrote, over the files the notebook keeps
			// beside it and collection does not make: a recording, an image.
			fresh := fstest.MapFS{}
			kept, dErr := os.ReadDir(filepath.Join(samplesDir, blobDir))
			require.NoError(t, dErr)
			for _, e := range kept {
				if strings.HasSuffix(e.Name(), mdnotebook.RecordExt) {
					continue
				}
				data, fErr := os.ReadFile(filepath.Join(samplesDir, blobDir, e.Name()))
				require.NoError(t, fErr)
				fresh[blobDir+"/"+e.Name()] = &fstest.MapFile{Data: data}
			}
			for p, data := range out.Blobs {
				fresh[p] = &fstest.MapFile{Data: data}
			}
			collected := mdnotebook.Load(again, fresh)
			defer collected.Release()
			require.Empty(t, collected.Problems)
			require.Len(t, collected.Records, len(stored.Records))
			for name, want := range stored.Records {
				got := collected.Records[name]
				require.NotNil(t, got, name)
				sameRows(t, name, want, got)
			}
		})
	}
}
