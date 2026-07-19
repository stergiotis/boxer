package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEditorTabPresenceDrivesParamsStrip pins the ADR-0132 §SD3 re-homing
// rule: the params strip renders in the top panel exactly when an embedder
// removed the Editor tab, so param widgets have one render site per frame.
func TestEditorTabPresenceDrivesParamsStrip(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x")
	defer inst.Close()

	assert.True(t, inst.editorTabPresent(), "stock tab set carries the editor")
	require.NoError(t, inst.Tabs().Remove("editor"))
	assert.False(t, inst.editorTabPresent(), "removal flips the strip on")
}
