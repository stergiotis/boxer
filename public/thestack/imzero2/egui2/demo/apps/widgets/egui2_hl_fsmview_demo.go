//go:build llm_generated_opus47

package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
)

// =============================================================================
// fsmview widget demo — two-level finite-state-machine viewer
//
// A small traffic-light FSM exercises the level-1 chip + level-2 popup
// (Table / Graph / History toggle). Init pre-seeds a few transitions so
// the History tab + chip "Xs ago" subscript both have content to show
// during the 4-frame screenshot tour, and the popup is opened by default
// so the tour captures the level-2 surface.
// =============================================================================

type fsmviewDemoState struct {
	tl *fsmview.Widget[string]
	m  *fsmview.Machine[string]
}

func init() {
	registry.Register(registry.Demo{
		Name:     "fsmview",
		Category: "Inspectors & feedback",
		Title:    icons.IconTreeStructure + " fsmview",
		Stage:    [2]float32{1024, 700},
		Kind:     registry.DemoKindUX,
		Description: "Two-level FSM viewer (statetrooper-backed). Level 1: compact chip showing the current state + an optional \"Xs ago\" subscript. Level 2 (click chip): floating popup with Table, Graph (egui_graphs force-directed), and History views. Init seeds a few transitions so all three tabs have content to render in the tour.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			m := fsmview.NewMachine("red", 16,
				fsmview.WithStateOrder([]string{"red", "yellow", "green"}),
			)
			m.AddRule("red", "green").
				AddRule("green", "yellow").
				AddRule("yellow", "red").
				EdgeLabel("red", "green", "go").
				EdgeLabel("green", "yellow", "slow").
				EdgeLabel("yellow", "red", "stop")
			// Pre-seed history so the tour PNG shows a populated History
			// tab and the "Xs ago" subscript displays a real value.
			_ = m.Transition("green")
			_ = m.Transition("yellow")
			_ = m.Transition("red")
			// AutoAnchor pins the popup at the cursor position the frame
			// the chip is clicked (R20 pointer fetcher; M3a-ii). For the
			// 4-frame screenshot tour the popup is pre-opened from Init
			// so no click happens — we keep an explicit PopupAnchor as
			// the fallback so the tour PNG captures the popup at a
			// predictable location instead of egui's cascade default.
			w := fsmview.New(ids, "traffic", m).
				Title("Traffic light").
				ShowSubscript(true).
				AutoAnchor(true).
				PopupAnchor(60, 220)
			w.Open()
			state = &fsmviewDemoState{tl: w, m: m}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoFsmview(ids, state.(*fsmviewDemoState))
		},
		SourceFunc: demoFsmview,
	})
}

func demoFsmview(ids *c.WidgetIdStack, st *fsmviewDemoState) {
	c.Label("Click the chip to toggle the popup; switch Table / Graph / History at the top.").Send()
	c.AddSpace(gapInline())
	for range c.Horizontal().KeepIter() {
		c.Label("Current:").Send()
		c.AddSpace(padInner())
		st.tl.Render()
	}
	c.AddSpace(gapInline())

	c.Separator().Horizontal().Send()
	c.Label("Drive the FSM:").Send()
	for range c.Horizontal().KeepIter() {
		current := st.m.Current()
		for _, target := range []string{"red", "yellow", "green"} {
			label := fmt.Sprintf("→ %s", target)
			atoms := c.Atoms().Text(label).Keep()
			enabled := st.m.CanTransition(target) && target != current
			resp := c.Button(ids.PrepareStr("trans-"+target), atoms).SendResp()
			if enabled && resp.HasPrimaryClicked() {
				_ = st.m.Transition(target)
			}
			c.AddSpace(padInner())
		}
	}
	c.AddSpace(gapInline())
	c.Label(fmt.Sprintf("History: %d transition(s) — popup open=%v, renderer=%v",
		st.m.HistoryLen(), st.tl.IsOpen(), st.tl.SelectedRenderer())).Send()
}
