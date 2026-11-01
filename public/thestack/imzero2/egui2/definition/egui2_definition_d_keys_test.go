package definition

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The key vocabulary crosses the FFI as a u8 (ADR-0177 SD4) and lives in two
// places: the Go table and the Rust match arm. This is what keeps them one
// thing — the Rust file is rebuilt from the table here and compared, so a key
// added on one side and not the other fails `go test` rather than arriving as
// the wrong key at runtime.
func TestKeyCodesRustFileMatchesTheGoTable(t *testing.T) {
	// The definition package sits four levels under the repo root.
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	require.NoError(t, err)
	path := filepath.Join(root, KeyCodesRustPath)

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err, "%s is missing; regenerate it from KeyCodesRustFile", KeyCodesRustPath)

	assert.Equal(t, KeyCodesRustFile(), string(onDisk),
		"%s has drifted from keycodes.Table.\n"+
			"Rewrite it with the content of definition.KeyCodesRustFile() and rebuild the Rust client.",
		KeyCodesRustPath)
}

// The shell's chord table (ADR-0346 §SD5) is kept the same way: the Rust arm
// is rebuilt from keycodes.ShellChords and compared.
func TestShellChordsRustFileMatchesTheGoTable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	require.NoError(t, err)
	path := filepath.Join(root, ShellChordsRustPath)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err, "%s is missing; regenerate it from ShellChordsRustFile", ShellChordsRustPath)
	assert.Equal(t, ShellChordsRustFile(), string(onDisk),
		"%s has drifted from keycodes.ShellChords.\n"+
			"Rewrite it with the content of definition.ShellChordsRustFile() and rebuild the Rust client.",
		ShellChordsRustPath)
}

// A (key, shift) pair names at most one chord, every bit fits the set, and
// every key is in the vocabulary the Rust arm is built from.
func TestShellChordsAreUnambiguous(t *testing.T) {
	known := map[keycodes.Code]bool{}
	for _, e := range keycodes.Table {
		known[e.Code] = true
	}
	seen := map[[2]any]string{}
	for _, ch := range keycodes.ShellChords {
		assert.True(t, known[ch.Key], "%s names a key outside keycodes.Table", ch.Name)
		k := [2]any{ch.Key, ch.Shift}
		if prev, dup := seen[k]; dup {
			t.Errorf("%s and %s share a key", prev, ch.Name)
		}
		seen[k] = ch.Name
		assert.Less(t, int(ch.Bit), 64)
		assert.Less(t, int(ch.Bit), keycodes.ShellChordCount)
	}
}
