package leewaywidgets_demo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestApp_InstancesAreIndependent: two Open() calls must yield
// independent *App values with their own selectedView so clicking a
// tree leaf in one window does not flip the active view in the other.
func TestApp_InstancesAreIndependent(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets")
	require.NoError(t, err)

	app1, ok := a1.(*App)
	if !ok {
		t.Skip("registry is in tour-mode dispatch (IMZERO2_SCREENSHOT_DIR set); skip per-window assertion")
	}
	app2, ok := a2.(*App)
	require.True(t, ok)

	assert.NotSame(t, app1, app2, "factory must allocate a fresh App per Open")

	app1.selectedView = viewKeyJSON
	assert.Equal(t, viewKeyTable2, app2.selectedView, "selectedView must not leak between windows")
}
