package windowhost

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// A window is opening until its body has met a returned Mount; the agent
// service's launch reports what OpsInstances says (ADR-0269 §SD3).
func TestOpsInstancesReportsHowFarAWindowHasLoaded(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())
	_, err := h.Open("test.a")
	require.NoError(t, err)

	infos := h.OpsInstances()
	require.Len(t, infos, 1)
	assert.Equal(t, opwire.LoadOpening, infos[0].Load, "no frame has drawn the body yet")
	assert.True(t, opsBusy(h.windows), "an opening window asks for frames")

	// What renderWindowBody stores; it needs the render runtime to run.
	h.windows[0].loaded.Store(&windowLoad{failed: true, reason: "boom"})
	infos = h.OpsInstances()
	assert.Equal(t, opwire.LoadFailed, infos[0].Load)
	assert.Equal(t, "boom", infos[0].Reason)

	h.windows[0].loaded.Store(&windowLoad{})
	infos = h.OpsInstances()
	assert.Equal(t, opwire.LoadReady, infos[0].Load)
	assert.Empty(t, infos[0].Reason)
	assert.False(t, opsBusy(h.windows), "a loaded window without an engine needs no frames")
}

// A window that never draws its body — collapsed — stops asking for frames.
func TestAnOpeningWindowAsksForFramesOnlyForABoundedTime(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())
	_, err := h.Open("test.a")
	require.NoError(t, err)
	h.windows[0].opened = time.Now().Add(-openingFrames)
	assert.False(t, opsBusy(h.windows))
	assert.Equal(t, opwire.LoadOpening, h.OpsInstances()[0].Load, "it is still reported as opening")
}
