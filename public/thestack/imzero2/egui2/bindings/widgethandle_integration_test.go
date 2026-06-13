package bindings

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stretchr/testify/require"
)

// TestButtonRetainedHandleRoundTrip verifies that a Button factory writes its
// widget ID at the offset recorded by WriteWidgetId, so calling GetWidgetHandle
// on the retained holder recovers the same raw ID.
func TestButtonRetainedHandleRoundTrip(t *testing.T) {
	ids := NewWidgetIdStack()

	// Derive the expected raw ID by mirroring what the factory does.
	expectedId := ids.PrepareStr("integration-test-button").Derive()
	// Reset and re-prepare since Derive consumed the state.
	expectedId2 := ids.PrepareStr("integration-test-button").Derive()
	require.Equal(t, expectedId, expectedId2, "derivation should be deterministic")

	// Now build the button via the factory, which re-derives and writes the ID.
	held := Button(ids.PrepareStr("integration-test-button"), Atoms().Text("x").Keep()).Keep()

	// Extract the handle from the retained holder.
	h := held.GetWidgetHandle()
	require.NotEqual(t, widgethandle.NoWidget, h, "handle must be populated")
	require.Equal(t, expectedId, h.Resolve(), "handle should resolve to the factory-derived ID")
}

// TestFrameRetainedHandleRoundTrip verifies the same for a container widget
// (Frame uses DeriveStacked, which affects the id stack beyond the current frame).
func TestFrameRetainedHandleRoundTrip(t *testing.T) {
	ids := NewWidgetIdStack()

	held := Frame(ids.PrepareStr("integration-test-frame")).Keep()
	// Pop the stacked ID the factory pushed so the stack is clean for other tests.
	defer ids.PopIdFromStack()

	h := held.GetWidgetHandle()
	require.NotEqual(t, widgethandle.NoWidget, h)

	// The raw ID embedded in the holder's bytes should match what a fresh
	// derivation produces (modulo re-peeking the stack, which is the same).
	ids2 := NewWidgetIdStack()
	expected := ids2.PrepareStr("integration-test-frame").Derive()
	require.Equal(t, expected, h.Resolve())
}
