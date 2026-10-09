// inscribe — the overlay agents point things out through (ADR-0297), shown
// on a small sample app without an agent.
//
// The demo runs the real inscribe scene, layout and drawing over its own
// widgets: each sample widget sits in a layout block of its own, whose rect
// c.CaptureUiRect stamps and StateManager.GetUiRect returns a frame later.
// A resolver hands those rects to the overlay as viewport anchors, so every
// mark below is laid out and drawn exactly as on the desktop. What it does
// not show is the agent path — grants, records, the window host's
// resolution; the chat-inscribe scene covers that.

package widgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
)

// The capture sequence numbers of the sample widgets' rects.
const (
	inscribeSeqSave uint64 = 0x1E5C0001 + iota
	inscribeSeqCancel
	inscribeSeqRow
	inscribeSeqStatus
	inscribeSeqPanel
	inscribeSeqStage
)

type inscribeDemoState struct {
	overlay *inscribe.Overlay
	// The marks shown, one toggle each.
	callout, highlight, arrow, steps, spotlight bool
	// behind draws every target as covered by another window; second puts
	// the arrow under another agent's hue.
	behind, second bool
}

func init() {
	registry.Register(registry.Demo{
		Name:     "inscribe",
		Category: "Graphics & canvas",
		Title:    "inscribe — marks agents point things out with",
		Stage:    [2]float32{1100, 560},
		Kind:     registry.DemoKindMixed,
		Description: "The overlay of ADR-0297 on a sample app: a pen circle round a button, a highlighter swipe over a row or a line, " +
			"corner brackets round a panel, notes on dotted paper with a curved arrow, numbered steps, a spotlight, and a dashed sketch for a target behind another window. " +
			"The real scene, layout and drawing, without an agent.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &inscribeDemoState{overlay: inscribe.NewOverlay(inscribe.NewScene()),
				callout: true, highlight: true, arrow: true, steps: true}
			return
		},
		RenderStateful: demoInscribe,
	})
}

// demoResolver resolves the demo's viewport anchors as given, behind other
// windows when asked; there are no windows of its own to resolve.
type demoResolver struct{ behind bool }

func (inst demoResolver) Resolve(a inscribe.Anchor) (inscribe.Rect, inscribe.VisibilityE) {
	if a.Viewport == nil {
		return inscribe.Rect{}, inscribe.VisibilityGone
	}
	if inst.behind {
		return *a.Viewport, inscribe.VisibilityBehind
	}
	return *a.Viewport, inscribe.VisibilityShown
}
func (inst demoResolver) Window(uint64) (inscribe.Rect, bool) { return inscribe.Rect{}, false }
func (inst demoResolver) Windows() []inscribe.Rect            { return nil }

func demoInscribe(ids *c.WidgetIdStack, anyState any) {
	st := anyState.(*inscribeDemoState)
	sm := c.CurrentApplicationState.StateManager
	density := styletokens.ActiveDensity()
	captured := func(seq uint64) (r inscribe.Rect, ok bool) {
		v, ok := sm.GetUiRect(seq)
		if ok {
			r = inscribe.Rect{X: v.MinX, Y: v.MinY, W: v.MaxX - v.MinX, H: v.MaxY - v.MinY}
		}
		return
	}

	for range c.Vertical().KeepIter() {
		// The toggles, above the app so the marks have room beside it.
		for range c.Horizontal().KeepIter() {
			c.Checkbox(ids.PrepareStr("callout"), st.callout, "Callout on Save").SendRespVal(&st.callout)
			c.Checkbox(ids.PrepareStr("highlight"), st.highlight, "Highlight the status line").SendRespVal(&st.highlight)
			c.Checkbox(ids.PrepareStr("arrow"), st.arrow, "Arrow from Cancel to the row").SendRespVal(&st.arrow)
			c.Checkbox(ids.PrepareStr("steps"), st.steps, "Steps").SendRespVal(&st.steps)
			c.Checkbox(ids.PrepareStr("spotlight"), st.spotlight, "Spotlight the row").SendRespVal(&st.spotlight)
		}
		for range c.Horizontal().KeepIter() {
			c.Checkbox(ids.PrepareStr("behind"), st.behind, "Targets behind another window").SendRespVal(&st.behind)
			c.Checkbox(ids.PrepareStr("second"), st.second, "The arrow from a second agent").SendRespVal(&st.second)
		}
		c.AddSpace(styletokens.GapSections(density))
		// The sample app the marks point at.
		for range c.Horizontal().KeepIter() {
			for range c.Frame(ids.PrepareStr("sample-app")).KeepIter() {
				for range c.Vertical().KeepIter() {
					// Wide and tall enough to read as a panel: its step marks it
					// by its corners.
					c.UiSetMinWidth(24 * styletokens.GapSections(density))
					c.Label("Sample app").Send()
					c.AddSpace(styletokens.GapItems(density))
					for range c.Horizontal().KeepIter() {
						for range c.Horizontal().KeepIter() {
							_ = c.Button(ids.PrepareStr("save"), c.Atoms().Text("Save").Keep()).SendResp()
							c.CaptureUiRect(inscribeSeqSave)
						}
						for range c.Horizontal().KeepIter() {
							_ = c.Button(ids.PrepareStr("cancel"), c.Atoms().Text("Cancel").Keep()).SendResp()
							c.CaptureUiRect(inscribeSeqCancel)
						}
					}
					c.AddSpace(styletokens.GapSections(density))
					c.Label("Aircraft     Positions     Altitude").Send()
					c.Label("B738         15 202        29 958").Send()
					for range c.Horizontal().KeepIter() {
						c.Label("A320         19 976        24 482").Send()
						c.CaptureUiRect(inscribeSeqRow)
					}
					c.Label("A21N          9 403        26 414").Send()
					c.AddSpace(styletokens.GapSections(density))
					for range c.Horizontal().KeepIter() {
						c.Label("Note: 28 characters · level 50.0").Send()
						c.CaptureUiRect(inscribeSeqStatus)
					}
				}
				c.CaptureUiRect(inscribeSeqPanel)
			}
			// Room to the right of the app, where the notes go.
			c.AddSpace(60 * styletokens.GapSections(density))
		}
		// The stage is everything drawn above: captured last, once it is
		// laid out, it bounds where notes may go.
		c.CaptureUiRect(inscribeSeqStage)
	}

	save, ok1 := captured(inscribeSeqSave)
	cancel, ok2 := captured(inscribeSeqCancel)
	row, ok3 := captured(inscribeSeqRow)
	status, ok4 := captured(inscribeSeqStatus)
	panel, ok5 := captured(inscribeSeqPanel)
	stage, ok6 := captured(inscribeSeqStage)
	scene := st.overlay.Scene()
	scene.ClearAll()
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
		// The rects arrive a frame after the first; nothing to point at yet.
		return
	}
	at := func(r inscribe.Rect) inscribe.Anchor { return inscribe.Anchor{Viewport: &r} }
	const task = "task-demo1a"
	put := func(on bool, m inscribe.Mark) {
		if on {
			_ = scene.Put(m)
		}
	}
	put(st.steps, inscribe.Mark{Task: task, Id: "step-panel", Op: inscribe.OpStep, Targets: []inscribe.Anchor{at(panel)}, Text: "the sample app"})
	put(st.steps, inscribe.Mark{Task: task, Id: "step-save", Op: inscribe.OpStep, Targets: []inscribe.Anchor{at(save)}, Text: "save the note"})
	put(st.callout, inscribe.Mark{Task: task, Id: "save", Op: inscribe.OpCallout, Targets: []inscribe.Anchor{at(save)}, Text: "Save writes the note"})
	put(st.highlight, inscribe.Mark{Task: task, Id: "status", Op: inscribe.OpHighlight, Targets: []inscribe.Anchor{at(status)}})
	arrowTask := task
	if st.second {
		arrowTask = "task-demo2b"
	}
	put(st.arrow, inscribe.Mark{Task: arrowTask, Id: "arrow", Op: inscribe.OpArrow, Targets: []inscribe.Anchor{at(cancel), at(row)}, Text: "this row"})
	put(st.spotlight, inscribe.Mark{Task: task, Id: "spot", Op: inscribe.OpSpotlight, Targets: []inscribe.Anchor{at(row)}})
	st.overlay.Frame(demoResolver{behind: st.behind}, stage, false)
}
