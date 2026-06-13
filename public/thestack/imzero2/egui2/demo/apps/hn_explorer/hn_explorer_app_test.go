package hn_explorer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestApp_InstancesAreIndependent: two Open() calls must yield
// independent *App values with their own currentMode, filterText,
// filterType, sortMode, and selectedIdx so adjustments in one window
// do not affect the other.
func TestApp_InstancesAreIndependent(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/hn_explorer")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/hn_explorer")
	require.NoError(t, err)

	app1 := a1.(*App)
	app2 := a2.(*App)

	assert.NotSame(t, app1, app2, "factory must allocate a fresh App per Open")

	// Mutate every per-window field on app1; app2 must stay at defaults.
	app1.currentMode = "focus"
	app1.filterText = "rust"
	app1.filterType = "story"
	app1.sortMode = "score"
	app1.selectedIdx = 42

	assert.Equal(t, "stream", app2.currentMode, "currentMode must not leak between windows")
	assert.Equal(t, "", app2.filterText, "filterText must not leak between windows")
	assert.Equal(t, "all", app2.filterType, "filterType must not leak between windows")
	assert.Equal(t, "time", app2.sortMode, "sortMode must not leak between windows")
	assert.Equal(t, -1, app2.selectedIdx, "selectedIdx must not leak between windows")
}
