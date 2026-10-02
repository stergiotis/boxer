package bindings

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestButtonKindsMatchRust holds ButtonKindE to the KIND_* constants the
// client's IdsButton reads, so a kind added on one side fails here.
func TestButtonKindsMatchRust(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// .../public/thestack/imzero2/egui2/bindings → repo root is five up.
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "rust", "imzero2", "imzero2_egui", "src", "style", "button.rs"))
	require.NoError(t, err)

	rust := map[string]uint8{}
	for _, m := range regexp.MustCompile(`pub const KIND_([A-Z_]+): u8 = (\d+);`).FindAllStringSubmatch(string(b), -1) {
		v, err := strconv.ParseUint(m[2], 10, 8)
		require.NoError(t, err)
		rust[strings.ReplaceAll(strings.ToLower(m[1]), "_", "-")] = uint8(v)
	}
	goKinds := map[string]uint8{}
	for _, k := range AllButtonKinds {
		goKinds[k.String()] = uint8(k)
	}
	require.Equal(t, rust, goKinds)
}
