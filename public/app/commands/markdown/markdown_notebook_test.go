package markdown

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureDir = "../../../semistructured/markdown/mdnotebook/fixture"

func runNotebook(t *testing.T, args ...string) (out string, err error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := NewCliCommand()
	cmd.Writer = &buf
	err = cmd.Run(context.Background(), append([]string{"markdown", "notebook"}, args...))
	return buf.String(), err
}

// The fixture is sealed, and two of its cells are there to be refused: the
// check says both, and fails for the cells.
func TestNotebookCheck(t *testing.T) {
	out, err := runNotebook(t, "check", filepath.Join(fixtureDir, "w45.md"))
	assert.Error(t, err)
	assert.Contains(t, out, "sql chart: 48 rows × 3 columns")
	assert.Contains(t, out, "WHERE: a record filters, a cell selects")
	assert.Contains(t, out, "\nsealed\tdata ")

	// An edited record is stale, and the notebook no longer sealed.
	src, err := os.ReadFile(filepath.Join(fixtureDir, "w45.md"))
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(fixtureDir)))
	edited := strings.Replace(string(src), "count() AS samples", "count() AS samples -- edited", 1)
	require.NotEqual(t, string(src), edited)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "w45.md"), []byte(edited), 0o644))
	out, err = runNotebook(t, "check", filepath.Join(dir, "w45.md"))
	assert.Error(t, err)
	assert.Contains(t, out, "stale\trecord stats was edited since collection")
	assert.Contains(t, out, "\nnot sealed\tdata ")
}

// Collection replaces the notebook where it lies and as it was: its
// permissions kept, and a symbolic link to it left a link.
func TestWriteFileAtomicKeepsModeAndLink(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "private.md")
	require.NoError(t, os.WriteFile(file, []byte("draft"), 0o600))
	link := filepath.Join(dir, "link.md")
	require.NoError(t, os.Symlink("private.md", link))

	require.NoError(t, writeFileAtomic(link, []byte("sealed")))
	got, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "sealed", string(got), "the notebook the link names was replaced")
	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	linkInfo, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the link is still a link")

	blob := filepath.Join(dir, "private.blobs", "00.arrow")
	require.NoError(t, writeFileAtomic(blob, []byte("blob")))
	info, err = os.Stat(blob)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "a new file is readable")
}

func TestNotebookBuffer(t *testing.T) {
	out, err := runNotebook(t, "buffer", filepath.Join(fixtureDir, "w45.md"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "WITH `stats` AS (\n"), out)
	assert.Contains(t, out, "FROM spans GROUP BY source, target\n")
}
