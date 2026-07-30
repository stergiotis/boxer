package app

import (
	"testing"

	"github.com/rs/zerolog"
)

// A context that says nothing about focus means a single-surface host, and
// the sole instance there is the active one — so the constructed default
// must be focused, or every app gating global input on this capability
// would go deaf outside the multi-window shell.
func TestStaticFrameContextWindowFocusedDefaultsTrue(t *testing.T) {
	mc := NewStaticMountContext("test-app", zerolog.Nop(), nil, nil, nil)
	fc := NewStaticFrameContext(mc, nil)
	if !fc.WindowFocused() {
		t.Error("a fresh frame context must read as focused")
	}
	// The capability is reached by type assertion from the interface, the
	// way an app consumes it.
	var ctx FrameContextI = fc
	f, ok := ctx.(WindowFocusI)
	if !ok {
		t.Fatal("StaticFrameContext must expose WindowFocusI")
	}
	if !f.WindowFocused() {
		t.Error("focused must survive the interface hop")
	}
	fc.SetWindowFocused(false)
	if f.WindowFocused() {
		t.Error("SetWindowFocused(false) must be visible through the capability")
	}
}
