package runtime

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFingerprintContextNamesVersion ties the context string to Version: a
// form bump that forgets to move the context would keep old fingerprints
// valid for new bytes.
func TestFingerprintContextNamesVersion(t *testing.T) {
	require.Contains(t, FingerprintContextV1, " v"+strconv.FormatUint(Version, 10)+" ")
	require.True(t, strings.HasSuffix(FingerprintContextV1, " entity"))
}

// TestFingerprintPinned pins the derived key and one fingerprint. A change to
// either is a change to every fingerprint anyone has stored beside a record,
// so it must be a deliberate edit of this test, not a side effect.
func TestFingerprintPinned(t *testing.T) {
	key := FingerprintKey()
	require.Equal(t,
		"f30d531795a38e0c55ce6f19b4117b72da1fad7dea950f2c39ba21d54adf0976",
		hex.EncodeToString(key[:]), "derived key")
	item := []byte{0x83, 0x01, 0xa0, 0xa0} // [1, {}, {}] — the empty entity
	fp := Fingerprint(item)
	require.Equal(t,
		"14e3a704fc4ec019a6f6b4027c2b12f20f8b5723e207b03955d598907a389770",
		hex.EncodeToString(fp[:]), "fingerprint of the empty entity")
	again := NewFingerprinter().Sum(item)
	require.Equal(t, fp, again)
	other := Fingerprint([]byte{0x83, 0x01, 0xa0, 0xa1, 0x60, 0x80})
	require.NotEqual(t, fp, other)
}
