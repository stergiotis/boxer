package keycodes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryShellChordIsDescribed(t *testing.T) {
	for bit := ShellChordBit(0); bit < shellChordCount; bit++ {
		assert.NotEmpty(t, bit.Description(), "chord bit %d has no description", bit)
	}
}

// The committed help page is what ShellChordsHelpPage renders. To refresh
// it after changing the table, run this test with BOXER_WRITE_HELP=1.
func TestShellChordsHelpPageIsCurrent(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", ShellChordsHelpPath)
	want := ShellChordsHelpPage()
	if os.Getenv("BOXER_WRITE_HELP") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(want), 0o644))
	}
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, string(got), "help page drifted from the chord table; rerun with BOXER_WRITE_HELP=1")
}
