package carrierclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snap(nodes ...*TreeNode) *TreeSnapshot {
	return &TreeSnapshot{Nodes: nodes}
}

func node(id uint64, role, name string) *TreeNode {
	return &TreeNode{Id: id, Role: role, Name: name, W: 40, H: 20}
}

func TestResolveExactName(t *testing.T) {
	s := snap(node(1, "button", "Run"), node(2, "button", "Panes"))
	got, err := Resolve(s, Locator{Name: "Panes"})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.GetId())
}

func TestResolveAmbiguityIsAnError(t *testing.T) {
	// Two "Close" buttons is the normal state of a tabbed dock. Picking the
	// first silently is how a driver ends up closing the wrong tab, so the
	// ambiguity has to surface.
	s := snap(node(1, "button", "Close"), node(2, "button", "Close"))
	_, err := Resolve(s, Locator{Name: "Close"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestResolveNthDisambiguates(t *testing.T) {
	s := snap(node(1, "button", "Close"), node(2, "button", "Close"))
	got, err := Resolve(s, Locator{Name: "Close", Nth: 1, HasNth: true})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.GetId())

	_, err = Resolve(s, Locator{Name: "Close", Nth: 7, HasNth: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestResolveSkipsHiddenNodes(t *testing.T) {
	// A hidden node is laid out but off screen; actuating it would succeed on
	// the wire and change nothing a capture could show.
	hidden := node(1, "button", "Panes")
	hidden.Flags = FlagHidden
	s := snap(hidden, node(2, "button", "Panes"))
	got, err := Resolve(s, Locator{Name: "Panes"})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.GetId())
}

func TestResolveRoleNarrowsAName(t *testing.T) {
	s := snap(node(1, "label", "Conditions"), node(2, "check_box", "Conditions"))
	got, err := Resolve(s, Locator{Name: "Conditions", Role: "check_box"})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.GetId())
}

func TestResolveIDWinsOverEverything(t *testing.T) {
	s := snap(node(1, "button", "Run"), node(2, "button", "Panes"))
	got, err := Resolve(s, Locator{ID: 1, Name: "Panes"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.GetId(), "an exact id short-circuits the other fields")
}

func TestResolveEmptyLocatorMatchesNothing(t *testing.T) {
	// A criterion-free locator would otherwise match every node and resolve to
	// whichever happened to be first.
	s := snap(node(1, "button", "Run"))
	_, err := Resolve(s, Locator{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no node matches")
}

func TestResolveNilSnapshot(t *testing.T) {
	_, err := Resolve(nil, Locator{Name: "Run"})
	require.Error(t, err)
}

func TestCenterAndPath(t *testing.T) {
	root := node(1, "window", "SQL Playground")
	bar := node(2, "group", "")
	bar.Parent = 1
	btn := node(3, "button", "Run")
	btn.Parent = 2
	btn.X, btn.Y, btn.W, btn.H = 10, 20, 40, 20
	x, y := Center(btn)
	assert.InDelta(t, 30.0, x, 0.001)
	assert.InDelta(t, 30.0, y, 0.001)
	assert.Equal(t, "SQL Playground > group > Run", Path(snap(root, bar, btn), btn))
}

func TestPathToleratesACycle(t *testing.T) {
	// Parent links come off the wire; a malformed pair must not spin forever on
	// what is already an error path.
	a := node(1, "group", "a")
	b := node(2, "group", "b")
	a.Parent, b.Parent = 2, 1
	assert.NotEmpty(t, Path(snap(a, b), a))
}
