package widgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func BeforeFirstFrameInitHandler() error {
	return nil
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
