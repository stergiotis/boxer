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
	fc := NewStaticFrameContext(mc)
	if !fc.WindowFocused() {
		t.Error("a fresh frame context must read as focused")
	}
	var ctx FrameContextI = fc
	fc.SetWindowFocused(false)
	if ctx.WindowFocused() {
		t.Error("SetWindowFocused(false) must be visible through the capability")
	}
}
