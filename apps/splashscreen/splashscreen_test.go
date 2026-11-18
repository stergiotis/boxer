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
	if !strings.Contains(boxerNotice, "boxer") {
		t.Errorf("NOTICE does not look like the project NOTICE; first line = %q", firstLine(boxerNotice))
	}
}

// TestSplashImageDecodesWhenPresent exercises the decode/pack path. On a
// build without the artwork boxerArtErr is set and the test skips —
// running without the asset is a supported, deliberately-degraded mode.
func TestSplashImageDecodesWhenPresent(t *testing.T) {
	img, err := boxerImage()
	if err != nil {
		t.Skipf("splash artwork not bundled in this checkout: %v", err)
	}
	if img.w == 0 || img.h == 0 {
		t.Fatalf("decoded dims = %dx%d, want non-zero", img.w, img.h)
	}
	if got, want := len(img.pixels), int(img.w)*int(img.h); got != want {
		t.Fatalf("pixel count = %d, want %d (w*h)", got, want)
	}
}

func firstLine(s string) (out string) {
	out, _, _ = strings.Cut(s, "\n")
	return
}
