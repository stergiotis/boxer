package inspector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAnchorToggleNilPinnedIsNoop verifies the nil-short-circuit before any
// FFFI2 buffer write occurs. The deeper rendering / click-capture path is
// only exercisable inside an active frame (StateManager + FFFI2 IO buffer);
// this package's tests, like the sibling distsummary tests, stay on the
// pure-computation side and rely on the demo carousel + screenshot tour
// for render-path coverage.
func TestAnchorToggleNilPinnedIsNoop(t *testing.T) {
	clicked := AnchorToggle(nil, nil)
	assert.False(t, clicked, "nil pinned must short-circuit and return false")
}
