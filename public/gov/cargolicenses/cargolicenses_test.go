package cargolicenses

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crate lays out a crate source directory and returns its manifest path.
func crate(t *testing.T, root string, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	manifest := filepath.Join(dir, "Cargo.toml")
	require.NoError(t, os.WriteFile(manifest, []byte("[package]\n"), 0o600))
	for n, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte(body), 0o600))
	}
	return manifest
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	ring := crate(t, src, "ring", map[string]string{
		"LICENSE":    "ring terms",
		"README.md":  "not a notice",
		"COPYING.md": "copying terms",
	})
	dav1d := crate(t, src, "dav1d", map[string]string{"LICENSE-MIT": "mit terms"})
	// Declared last, sorts first, and carries no notice file of its own.
	bare := crate(t, src, "bare", nil)

	metadata := `{"packages":[
		{"name":"ring","version":"0.17.8","license":"Apache-2.0 AND ISC","manifest_path":"` + ring + `"},
		{"name":"dav1d","version":"0.10.3","license":"BSD-2-Clause","manifest_path":"` + dav1d + `"},
		{"name":"bare","version":"1.0.0","license":"","manifest_path":"` + bare + `"}
	]}`
	metaPath := filepath.Join(root, "metadata.json")
	require.NoError(t, os.WriteFile(metaPath, []byte(metadata), 0o600))

	out := filepath.Join(root, "out")
	n, err := Run(metaPath, out)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	index, err := os.ReadFile(filepath.Join(out, indexName))
	require.NoError(t, err)
	assert.Equal(t, "bare-1.0.0: "+undeclared+"\n"+
		"dav1d-0.10.3: BSD-2-Clause\n"+
		"ring-0.17.8: Apache-2.0 AND ISC\n", string(index))

	body, err := os.ReadFile(filepath.Join(out, "ring-0.17.8", "LICENSE"))
	require.NoError(t, err)
	assert.Equal(t, "ring terms", string(body))
	assert.FileExists(t, filepath.Join(out, "ring-0.17.8", "COPYING.md"))
	assert.NoFileExists(t, filepath.Join(out, "ring-0.17.8", "README.md"))
	assert.FileExists(t, filepath.Join(out, "dav1d-0.10.3", "LICENSE-MIT"))
	// A crate with no notice file gets no directory, only its index line.
	assert.NoDirExists(t, filepath.Join(out, "bare-1.0.0"))
}

func TestRunIsDeterministic(t *testing.T) {
	root := t.TempDir()
	a := crate(t, filepath.Join(root, "src"), "aaa", map[string]string{"LICENSE": "a"})
	b := crate(t, filepath.Join(root, "src"), "bbb", map[string]string{"LICENSE": "b"})
	metaPath := filepath.Join(root, "metadata.json")

	read := func(order string) string {
		require.NoError(t, os.WriteFile(metaPath, []byte(order), 0o600))
		out := t.TempDir()
		_, err := Run(metaPath, out)
		require.NoError(t, err)
		body, err := os.ReadFile(filepath.Join(out, indexName))
		require.NoError(t, err)
		return string(body)
	}
	forward := read(`{"packages":[
		{"name":"aaa","version":"1.0.0","license":"MIT","manifest_path":"` + a + `"},
		{"name":"bbb","version":"2.0.0","license":"MIT","manifest_path":"` + b + `"}]}`)
	reversed := read(`{"packages":[
		{"name":"bbb","version":"2.0.0","license":"MIT","manifest_path":"` + b + `"},
		{"name":"aaa","version":"1.0.0","license":"MIT","manifest_path":"` + a + `"}]}`)
	assert.Equal(t, forward, reversed)
}

func TestIsNoticeFile(t *testing.T) {
	for _, name := range []string{"LICENSE", "license.txt", "LICENSE-APACHE", "COPYING", "NOTICE", "UNLICENSE"} {
		assert.True(t, isNoticeFile(name), name)
	}
	for _, name := range []string{"README.md", "Cargo.toml", "src", "build.rs", "relicense-notes.md"} {
		assert.False(t, isNoticeFile(name), name)
	}
}
