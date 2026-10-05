package fsmview

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// TestRenderHeadless renders the chip and the open popup without a host —
// plain, tethered and every level-2 renderer — and checks a quiet frame
// reports no events.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	sm := c.CurrentApplicationState.StateManager
	m := NewMachine("red", 16, MachineOptions[string]{StateOrder: []string{"red", "green"}}).
		AddRule("red", "green").AddRule("green", "red")
	_ = m.Transition("green")
	for _, tethered := range []bool{false, true} {
		v := New(ids, "t", m, Options[string]{Title: "traffic", Tethered: tethered, ShowSubscript: true})
		v.Open()
		for _, r := range []RendererE{RendererTable, RendererGraph, RendererHistory} {
			v.SetRenderer(r)
			if ev := v.Render(); ev.Toggled || ev.Driven {
				t.Fatalf("quiet frame reported %+v (tethered=%v renderer=%v)", ev, tethered, r)
			}
			sm.ScriptReset() // the frame boundary
		}
		if ev := v.RenderChip(); ev != (Events{}) {
			t.Fatalf("chip alone reported %+v", ev)
		}
		v.RenderPopup()
		sm.ScriptReset()
	}
}
