package track

import (
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/config/env"
)

// PeaksCacheDir names the directory holding the sidecar peaks files of
// ADR-0208 §SD4, and — per §SD12 — of every other product derived from the
// same recording, each in a file of its own keyed by the same identity.
// Empty resolves through [ResolvePeaksCacheDir].
var PeaksCacheDir = env.NewPath(env.Spec{
	Name:        "BOXER_AUDIO_PEAKS_CACHE_DIR",
	Description: "directory holding cached audio peaks pyramids (ADR-0208 §SD4); empty uses <user cache dir>/boxer/audio-peaks",
	Category:    env.CategorySystem,
})

// ResolvePeaksCacheDir returns the directory a peaks file is read from and
// written to: [PeaksCacheDir] when set, else <user cache dir>/boxer/audio-peaks,
// else a directory under [os.TempDir] for a host with no resolvable cache
// directory. It never fails — a cache is an optimisation, and a caller that
// cannot write to what this returns loses the cache rather than the track.
func ResolvePeaksCacheDir() (dir string) {
	if dir = PeaksCacheDir.Get(); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err == nil {
		return filepath.Join(cache, "boxer", "audio-peaks")
	}
	return filepath.Join(os.TempDir(), "boxer-audio-peaks")
}
