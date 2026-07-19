package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
)

// TestPlayAppInstancesDeriveDisjointIds pins the per-instance widget-id salt
// (playInstanceSalt + SetBaseSalt): two PlayApp instances rendering in the
// same frame — two applet windows (ADR-0132), or two play windows — must not
// derive the same effective id for the same widget label, or they collide in
// the global seenIds registry and share egui widget state across windows.
func TestPlayAppInstancesDeriveDisjointIds(t *testing.T) {
	a := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- a")
	defer a.Close()
	b := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- b")
	defer b.Close()

	assert.NotEqual(t,
		a.ids.PrepareStr("topbar").Derive(),
		b.ids.PrepareStr("topbar").Derive(),
		"the main stacks must be instance-salted")
	assert.NotEqual(t,
		a.mapDriver.ids.PrepareStr("x").Derive(),
		b.mapDriver.ids.PrepareStr("x").Derive(),
		"driver-owned stacks must carry the same instance salt")
}
