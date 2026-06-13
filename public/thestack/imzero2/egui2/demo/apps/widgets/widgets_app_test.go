package widgets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestApp_InstancesAreIndependent guards the per-window gallery
// state isolation: two Open() calls must yield independent *App
// values with their own filter substring and frame counter.
//
// Per-demo state (inside individual egui2_hl_*_demo.go files) remains
// shared across windows by design — this is a showcase, not a
// production multi-window app. The test documents this scope.
func TestApp_InstancesAreIndependent(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets")
	require.NoError(t, err)

	app1, ok := a1.(*App)
	if !ok {
		t.Skip("registry is in tour-mode dispatch (IMZERO2_SCREENSHOT_DIR set); skip per-window assertion")
	}
	app2 := a2.(*App)

	assert.NotSame(t, app1, app2, "factory must allocate a fresh App per Open")

	app1.filter = "graph"
	app1.frame = 17
	assert.Empty(t, app2.filter, "gallery filter must not leak between windows")
	assert.Zero(t, app2.frame, "frame counter must not leak between windows")
}
