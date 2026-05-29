//go:build llm_generated_opus47

package imztop

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestApp_InstancesAreIndependent guards the multi-window UX fix:
// two Open() calls must yield independent *App values with their own
// netSelectedIfaceIdx so clicking interfaces in one window does not
// leak the selection into the other.
func TestApp_InstancesAreIndependent(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/apps/imztop")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/apps/imztop")
	require.NoError(t, err)

	// In interactive mode the factory yields *App directly; in tour
	// mode it yields a SeededFuncApp wrapper. The test runs only the
	// interactive path because per-window state is exclusive to it.
	app1, ok := a1.(*App)
	if !ok {
		t.Skip("registry is in tour-mode dispatch (IMZERO2_SCREENSHOT_DIR set); skip per-window assertion")
	}
	app2, ok := a2.(*App)
	require.True(t, ok)

	assert.NotSame(t, app1, app2, "factory must allocate a fresh App per Open")

	app1.netSelectedIfaceIdx = 3
	assert.Equal(t, 0, app2.netSelectedIfaceIdx, "selected-interface state must not leak between windows")
}
