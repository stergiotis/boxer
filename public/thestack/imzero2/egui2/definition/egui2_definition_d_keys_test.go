package definition

import (
	"os"
	"path/filepath"
	"testing"

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
