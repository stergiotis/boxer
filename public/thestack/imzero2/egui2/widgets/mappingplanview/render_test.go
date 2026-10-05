package mappingplanview

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// TestRenderHeadless renders one frame of the playground without a host: the
// recompute runs on the dirty seed, a second frame is quiet, and a removed
// row's inspector view is pruned.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	sm := c.CurrentApplicationState.StateManager
	m := NewModel("k", "p", "T")
	r := m.AddRow()
	r.GoField, r.Section = "ID", "id"
	recomputes := 0
	v := New(ids, "t", Options{Recompute: func(m *Model) {
		recomputes++
		m.SetOutputs(Output{TabID: 1, Title: "go", Lang: LangGo, Source: "package p"})
	}})
	if ev := v.Render(m); !ev.Recomputed || recomputes != 1 {
		t.Fatalf("first frame did not recompute the dirty seed: %+v", ev)
	}
	sm.ScriptReset() // the frame boundary
	if ev := v.Render(m); ev.Recomputed || ev.Edited {
		t.Fatalf("quiet frame reported %+v", ev)
	}
	sm.ScriptReset()
	if len(v.fieldViews) != 1 {
		t.Fatalf("one row, %d inspector views", len(v.fieldViews))
	}
	m.removeByUID(r.uid)
	v.Render(m)
	sm.ScriptReset()
	if len(v.fieldViews) != 0 {
		t.Fatal("a removed row's inspector view was not pruned")
	}
	if v.Render(nil) != (Events{}) {
		t.Fatal("a nil model must draw nothing")
	}
}
