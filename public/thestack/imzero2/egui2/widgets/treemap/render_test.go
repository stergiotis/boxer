package treemap

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

func renderTestTree() (root, dir *layout.Node) {
	a := &layout.Node{Name: "a", Size: 3}
	b := &layout.Node{Name: "b", Size: 1}
	dir = &layout.Node{Name: "dir", Children: []*layout.Node{a, b}}
	root = &layout.Node{Name: "root", Children: []*layout.Node{dir, {Name: "c", Size: 2}}}
	return
}

// TestRenderHeadless renders frames without a host: a quiet frame reports
// nothing, the size given to Render is the container, and a host-driven
// navigation between frames arrives in the next frame's Events.Nav.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	root, dir := renderTestTree()
	tm := New(c.NewWidgetIdStack(), "t", root, Options{LeafClickSensing: true})

	ev := tm.Render(400, 300)
	if len(ev.Nav) != 0 || ev.ClickedLeaf != nil || ev.Hovered != nil {
		t.Fatalf("a quiet frame reported %+v", ev)
	}
	if r := tm.containerRect(); r.W != 400 || r.H != 300 {
		t.Fatalf("container: got %vx%v want 400x300", r.W, r.H)
	}
	sm.ScriptReset()

	if err := tm.DrillTo(dir); err != nil {
		t.Fatal(err)
	}
	ev = tm.Render(0, 0)
	if len(ev.Nav) != 1 || ev.Nav[0].Kind != NavKindDrillIn || ev.Nav[0].Trigger != NavTriggerExternal {
		t.Fatalf("Nav after DrillTo: %+v", ev.Nav)
	}
	if r := tm.containerRect(); r.W != 400 {
		t.Fatalf("a non-positive size must keep the last one, got %v", r.W)
	}
	sm.ScriptReset()
	if ev = tm.Render(400, 300); len(ev.Nav) != 0 {
		t.Fatalf("Nav is reported once, got %+v", ev.Nav)
	}
}
