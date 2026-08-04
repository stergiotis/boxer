package widgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func BeforeFirstFrameInitHandler() error {
	return nil
}

// demoProbeSeq is one gallery demo's own r21 pane-probe slot. The demo name
// alone cannot key it: the gallery is factory-registered, so two open windows
// run the same demo under the same name and each would size itself from the
// other's pane — the process-wide-slot failure r18 had, reproduced inside the
// seq-keyed register. Threading the caller's per-window id stack through the
// seq separates them. Derived per call rather than cached, like imztop's: the
// call site's nesting is the same every frame.
func demoProbeSeq(ids *c.WidgetIdStack, demo, role string) (seq uint64) {
	const demoProbeSalt uint64 = 0x9d4c72e3b085af16
	return c.ProbeSeq(demo, role) ^ ids.PrepareHighEntropy(demoProbeSalt).Derive()
}

// RenderLoopHandlerMinimal is a bare smoke-test loop. Not used by the
// standard demo dispatch; kept for manual debugging of the FFFI2
// pipeline. Owns its own WidgetIdStack because it runs outside any
// gallery App that would supply one via MountCtx.Ids().
func RenderLoopHandlerMinimal() error {
	ids := c.NewWidgetIdStack()
	for range c.Window(ids.PrepareStr("imzero2"), c.WidgetText().Text("imzero2").Keep()).KeepIter() {
		c.Label("Hello").Send()
		c.Button(ids.PrepareStr("btn"), c.Atoms().Text("btn").Keep()).Send()
	}
	return nil
}
