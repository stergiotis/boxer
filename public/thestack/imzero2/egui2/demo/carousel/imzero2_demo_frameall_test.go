package demo

import (
	"testing"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/apptest"
)

// TestEveryWindowedAppFramesHeadless is ADR-0396 §8 over the whole tree:
// this package blank-imports every app, so each windowed app mounts, draws
// two frames under the discard harness with no bus or storage behind it,
// and unmounts, and an id used twice or an unbalanced pop fails.
func TestEveryWindowedAppFramesHeadless(t *testing.T) {
	if testing.Short() {
		t.Skip("frames every registered app")
	}
	apptest.FrameAll(t, runtimeapp.DefaultRegistry, apptest.FrameOpts{Strict: true}, nil)
}
