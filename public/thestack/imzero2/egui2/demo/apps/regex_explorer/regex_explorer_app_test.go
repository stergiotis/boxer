//go:build llm_generated_opus47

package regex_explorer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestAppInstance_IsolatedState: two Open() calls must yield
// independent *AppInstance values with their own *App state so the
// pattern, haystack, mode flags, and query results don't bleed
// between windows. Widget id isolation is the host's responsibility
// (see MountCtx.Ids() + windowhost's per-window IdScope salt), so
// this test no longer asserts on instance seeds — those have been
// removed from AppInstance.
func TestAppInstance_IsolatedState(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer")
	require.NoError(t, err)

	inst1, ok := a1.(*AppInstance)
	if !ok {
		t.Skip("registry is in tour-mode dispatch (IMZERO2_SCREENSHOT_DIR set); skip per-window assertion")
	}
	inst2, ok := a2.(*AppInstance)
	require.True(t, ok)

	assert.NotSame(t, inst1, inst2, "factory must allocate a fresh AppInstance per Open")
	assert.NotSame(t, inst1.state, inst2.state, "each instance owns its own *App state")

	// Mutate inst1's state — inst2 must stay at defaults.
	inst1.state.pattern = `\w+`
	inst1.state.haystack = "hello world"
	inst1.state.caseInsensitive = true

	assert.Empty(t, inst2.state.pattern, "pattern must not leak between windows")
	assert.Empty(t, inst2.state.haystack, "haystack must not leak between windows")
	assert.False(t, inst2.state.caseInsensitive, "caseInsensitive must not leak between windows")
}
