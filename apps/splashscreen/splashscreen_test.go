package splashscreen

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestManifestRegistered confirms the init() side-effect registration landed
// in the default registry with a windowed surface and the required labels.
func TestManifestRegistered(t *testing.T) {
	m, ok := app.DefaultRegistry.LookupManifest(ManifestId)
	if !ok {
		t.Fatalf("app not registered under %q", ManifestId)
	}
	if m.Surface != app.SurfaceWindowed {
		t.Errorf("Surface = %v, want SurfaceWindowed", m.Surface)
	}
	if m.Display == "" || m.Title == "" {
		t.Error("Display and Title must both be non-empty")
	}
}

// TestNoticeEmbedded checks the committed NOTICE copy embeds and loads.
func TestNoticeEmbedded(t *testing.T) {
	loadAssets()
	if !strings.Contains(noticeText, "boxer") {
		t.Errorf("NOTICE does not look like the project NOTICE; first line = %q", firstLine(noticeText))
	}
}

// TestSplashImageDecodesWhenPresent exercises the decode/pack path. The
// artwork is git-ignored, so on a checkout without it (CI, fresh clone)
// splashErr is set and the test skips — running without the asset is a
// supported, deliberately-degraded mode.
func TestSplashImageDecodesWhenPresent(t *testing.T) {
	loadAssets()
	if splashErr != nil {
		t.Skipf("splash artwork not bundled in this checkout: %v", splashErr)
	}
	if splashW == 0 || splashH == 0 {
		t.Fatalf("decoded dims = %dx%d, want non-zero", splashW, splashH)
	}
	if got, want := len(splashPixels), int(splashW)*int(splashH); got != want {
		t.Fatalf("pixel count = %d, want %d (w*h)", got, want)
	}
}

func firstLine(s string) (out string) {
	out, _, _ = strings.Cut(s, "\n")
	return
}
