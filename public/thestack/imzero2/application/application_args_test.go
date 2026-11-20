//go:build !bootstrap

package application

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The client refuses to start without the IDL fingerprint (ADR-0278 SD6), so
// every launch passes it, as the hex the client parses, right after the
// command and ahead of anything optional.
func TestClientArgsCarryTheIdlFingerprint(t *testing.T) {
	for _, cfg := range []*Config{
		{},
		{MainFontTTF: "/f.ttf", MainFontSizeInPixels: 15, ImZeroSkiaClientConfig: &ImZeroClientConfig{Fullscreen: "on"}},
	} {
		args := clientArgs(cfg)
		require.GreaterOrEqual(t, len(args), 3)
		assert.Equal(t, []string{"imzero2", idlFingerprintFlag, fmt.Sprintf("%016x", bindings.IdlFingerprint)}, args[:3])
		assert.Equal(t, 1, countOf(args, idlFingerprintFlag), "the flag is passed once: %v", args)
	}
	args := clientArgs(&Config{MainFontTTF: "/f.ttf"})
	assert.True(t, slices.Contains(args, "/f.ttf"), "the configured flags follow: %v", args)
}

func countOf(args []string, s string) (n int) {
	for _, a := range args {
		if a == s {
			n++
		}
	}
	return
}
