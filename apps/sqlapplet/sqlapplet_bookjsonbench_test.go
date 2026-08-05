package sqlapplet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBookJsonbenchRegistered guards the embed + RegisterBook pair: a book
// whose directory is renamed or whose init is dropped fails silently at
// runtime (RegisterBook only logs), so the assertion belongs in a test.
func TestBookJsonbenchRegistered(t *testing.T) {
	entries, err := bookjsonbenchFS.ReadDir("bookjsonbench")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "bookjsonbench must embed at least one page")

	var found bool
	booksMu.Lock()
	for _, b := range books {
		if b.id == "jsonbench" {
			found = true
			break
		}
	}
	booksMu.Unlock()
	require.True(t, found, "the jsonbench book must be registered by init")
}
