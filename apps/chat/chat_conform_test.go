package chat

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/apptest"
)

// TestConforms runs the conformance kit (apptest, ADR-0396 §8): the
// manifest registers as it is, the help book is sound, and the window
// mounts, draws two frames headless with no id used twice, and unmounts.
func TestConforms(t *testing.T) {
	apptest.ManifestOK(t, manifest)
	apptest.HelpCorpus(t, ManifestId)
	apptest.FrameHeadless(t, app.DefaultRegistry, ManifestId, apptest.FrameOpts{Strict: true})
}
