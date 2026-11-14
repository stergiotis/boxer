package samples_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
)

// A sample is sealed, with no server asked: its blobs are the ones it
// names, no record is stale, every figure reads its stored value and every
// presentation cell has an answer.
func TestSamplesAreSealed(t *testing.T) {
	onDisk, err := fs.Glob(samples.FS(), "*.md")
	require.NoError(t, err)
	assert.ElementsMatch(t, onDisk, samples.Names(), "Names lists every sample")
	for _, name := range samples.Names() {
		t.Run(name, func(t *testing.T) {
			text, rErr := fs.ReadFile(samples.FS(), name)
			require.NoError(t, rErr)
			nb, rErr := mdnotebook.Read(text)
			require.NoError(t, rErr)
			ld := mdnotebook.Load(nb, samples.FS())
			defer ld.Release()
			require.Empty(t, mdnotebook.Problems(nb, ld))
			require.Empty(t, ld.Stale)
			require.NotEmpty(t, nb.Title)
			cells := 0
			for i := range nb.Cells {
				c := &nb.Cells[i]
				if c.Kind != mdnotebook.CellKindPresentation {
					continue
				}
				cells++
				res, eErr := mdnotebook.Evaluate(c.Body, ld.Records)
				require.NoErrorf(t, eErr, "the %s cell: %s", c.Renderer, c.Body)
				res.Release()
			}
			assert.Positive(t, cells)
		})
	}
}
