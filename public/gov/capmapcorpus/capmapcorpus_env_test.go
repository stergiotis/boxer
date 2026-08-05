package capmapcorpus

// Internal, unlike the rest of the package's tests, for one reason: the env
// handle caches its value on the first Get for the life of the process, so a
// plain t.Setenv is invisible to it once anything has read it. env's
// SetForTest sets the variable and resets that cache, and the handle is
// unexported.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOneCapVault materialises a vault holding a single competence.
func writeOneCapVault(t *testing.T, slug string) (root string) {
	t.Helper()
	root = t.TempDir()
	body := "---\nname: " + slug + "\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, slug+".md"), []byte(body), 0o644))
	return root
}

// A vault pointed at a path that is not a directory must fail loudly, naming
// both the variable and the offending path.
//
// The error must mention the path, not only the variable: the fallback branch
// (nothing set, nothing found above the working directory) also names the
// variable, so asserting on that alone would pass even if the setting were
// ignored entirely.
func TestResolveVaultRefusesNonDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	envVaultDir.SetForTest(t, missing)
	_, err := ResolveVault()
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvVaultDirName)
	assert.Contains(t, err.Error(), missing, "the env branch must be the one that failed")
}

// The setting wins over the walk-up, so a process started inside a checkout
// that has a conventional vault can still be aimed elsewhere.
func TestResolveVaultHonoursTheSetting(t *testing.T) {
	root := writeOneCapVault(t, "alpha")
	envVaultDir.SetForTest(t, root)
	got, err := ResolveVault()
	require.NoError(t, err)
	assert.Equal(t, root, got)
}

// Load degrades to nothing rather than erroring: its callers are introspection
// surfaces where "no corpus" is a legible answer.
func TestLoadYieldsNothingWhenVaultIsUnresolvable(t *testing.T) {
	envVaultDir.SetForTest(t, filepath.Join(t.TempDir(), "does-not-exist"))
	resetLoadMemo()
	corpus := Load()
	caps, rels := corpus.Competences, corpus.Relations
	assert.Empty(t, caps)
	assert.Empty(t, rels)
}

// Two reads inside the window are one snapshot — the property that keeps a
// query joining competences to relations from tearing across an edit.
func TestLoadSharesOneSnapshotWithinTheWindow(t *testing.T) {
	root := writeOneCapVault(t, "alpha")
	envVaultDir.SetForTest(t, root)
	resetLoadMemo()

	first := Load().Competences
	require.Len(t, first, 1)

	// Add a competence the second read must not see: inside the window the
	// snapshot is reused, which is the whole point.
	require.NoError(t, os.WriteFile(filepath.Join(root, "beta.md"),
		[]byte("---\nname: beta\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n"), 0o644))

	second := Load().Competences
	require.Len(t, second, 1, "a read inside the window reuses the snapshot")
	assert.Equal(t, &first[0], &second[0], "and returns the same backing array, not a copy")

	// Past the window the corpus is re-read, so the new competence appears.
	resetLoadMemo()
	third := Load().Competences
	assert.Len(t, third, 2)
}
