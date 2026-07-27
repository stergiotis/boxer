package adrcorpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ReadContents returns the file whole — frontmatter included — so
// len(Content) is exactly the BodyBytes the parse already reported. The two
// are the same measurement of the same bytes, and a reader is entitled to
// join on that.
func TestReadContentsIsTheWholeFile(t *testing.T) {
	dir := t.TempDir()
	src := "---\nstatus: accepted\ndate: 2026-07-27\n---\n\n# ADR-0001: A Title\n\nBody.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "0001-a-title.md"), []byte(src), 0o644))

	adrs, err := ParseDir(dir)
	require.NoError(t, err)
	require.Len(t, adrs, 1)

	rows := ReadContents(adrs)
	require.Len(t, rows, 1)
	assert.Equal(t, adrs[0].Num, rows[0].Num)
	assert.Equal(t, adrs[0].Path, rows[0].Path)
	assert.Equal(t, src, rows[0].Content, "the source is carried verbatim, frontmatter included")
	assert.Equal(t, adrs[0].BodyBytes, len(rows[0].Content),
		"length(content) and body_bytes measure the same bytes")
}

// A file that has gone missing since the parse drops its row rather than
// yielding an empty one: an empty string is a legible answer (an ADR that
// really is empty) and a reader could not tell the two apart.
func TestReadContentsDropsUnreadableRows(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"0001-first.md", "0002-second.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("# T\n"), 0o644))
	}
	adrs, err := ParseDir(dir)
	require.NoError(t, err)
	require.Len(t, adrs, 2)

	require.NoError(t, os.Remove(filepath.Join(dir, "0001-first.md")))

	rows := ReadContents(adrs)
	require.Len(t, rows, 1, "the vanished ADR drops out rather than arriving empty")
	assert.Equal(t, 2, rows[0].Num)
}

// An empty corpus yields no rows and no error — the posture every reader in
// this package shares.
func TestReadContentsEmptyCorpus(t *testing.T) {
	assert.Empty(t, ReadContents(nil))
}
