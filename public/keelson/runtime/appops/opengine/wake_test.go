package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostwake"
)

// A queued command asks the client for a pass at once, so under the reactive
// cadence it does not wait for the heartbeat to be applied (ADR-0363 §M4,
// proposed). A refused one does not.
func TestAQueuedCommandWakesTheClient(t *testing.T) {
	_, eng, _ := workEngine(t)
	wakes := 0
	hostwake.Set(func() { wakes++ })
	defer hostwake.Set(nil)

	out := eng.Submit("start", opwire.CallRequest{CallId: "c1"})
	require.Equal(t, opwire.PhaseAccepted, out.Phase)
	assert.Equal(t, 1, wakes)

	out = eng.Submit("start", opwire.CallRequest{CallId: "c1"})
	require.Equal(t, opwire.PhaseRefused, out.Phase, "a reused call id")
	assert.Equal(t, 1, wakes, "nothing was queued")
}
