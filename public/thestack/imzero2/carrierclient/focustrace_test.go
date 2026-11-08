package carrierclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// focusTree is a window (id 1) holding two buttons (2, 3) and a list row (4),
// with focus on the given id, at the given pass.
func focusTree(pass uint64, focus uint64, rowID uint64) *TreeSnapshot {
	return &TreeSnapshot{
		Pass:  pass,
		Focus: focus,
		Nodes: []*TreeNode{
			{Id: 1, Role: "window", Name: "App", Children: []uint64{2, 3, rowID}},
			{Id: 2, Role: "button", Name: "Run", Parent: 1},
			{Id: 3, Role: "button", Name: "Stop", Parent: 1},
			{Id: rowID, Role: "list_box_option", Name: "row 7", Parent: 1},
		},
	}
}

func TestFocusTraceAcceptsOneMove(t *testing.T) {
	before := focusTree(1, 2, 4)
	after := []*TreeSnapshot{focusTree(2, 2, 4), focusTree(3, 3, 4), focusTree(4, 3, 4)}
	assert.Empty(t, FocusFailures(before, after))
}

func TestFocusTraceAcceptsNoMove(t *testing.T) {
	before := focusTree(1, 2, 4)
	assert.Empty(t, FocusFailures(before, []*TreeSnapshot{focusTree(2, 2, 4)}))
}

func TestFocusTraceRefusesFocusFallingToTheRoot(t *testing.T) {
	before := focusTree(1, 2, 4)
	f := FocusFailures(before, []*TreeSnapshot{focusTree(2, 1, 4)})
	assert.Len(t, f, 1)
	assert.Contains(t, f[0], "root")
}

func TestFocusTraceRefusesTwoMovesForOneKey(t *testing.T) {
	before := focusTree(1, 2, 4)
	f := FocusFailures(before, []*TreeSnapshot{focusTree(2, 3, 4), focusTree(3, 4, 4)})
	assert.Len(t, f, 1)
	assert.Contains(t, f[0], "moved 2 times")
}

func TestFocusTraceRefusesTheSameItemUnderANewID(t *testing.T) {
	before := focusTree(1, 4, 4)
	f := FocusFailures(before, []*TreeSnapshot{focusTree(2, 5, 5)})
	assert.Len(t, f, 1)
	assert.Contains(t, f[0], "new node id")
}

func TestFocusTraceIgnoresRepeatedSnapshotsOfOnePass(t *testing.T) {
	before := focusTree(1, 2, 4)
	after := []*TreeSnapshot{focusTree(2, 3, 4), focusTree(2, 3, 4), focusTree(2, 3, 4)}
	assert.Empty(t, FocusFailures(before, after))
}
