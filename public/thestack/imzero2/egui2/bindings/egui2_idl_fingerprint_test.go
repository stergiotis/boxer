package bindings

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// The Rust client's generated enums carry the same IDL fingerprint as these
// bindings: the two are written by one generator from one IR, so a mismatch
// means only one side was regenerated (ADR-0278 SD6, proposed). The browser
// worker refuses such a pair at start; this catches it before a build.
func TestIdlFingerprintMatchesTheRustClient(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "rust", "imzero2", "src", "imzero2", "enums_out.rs"))
	require.NoError(t, err)
	m := regexp.MustCompile(`pub const IDL_FINGERPRINT: u64 = 0x([0-9a-f]{16});`).FindSubmatch(b)
	require.NotNil(t, m, "enums_out.rs carries IDL_FINGERPRINT")
	v, err := strconv.ParseUint(string(m[1]), 16, 64)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%016x", IdlFingerprint), fmt.Sprintf("%016x", v))
}
