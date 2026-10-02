package appops

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// WidgetEditing reports whether the person is editing through the widget h:
// it holds keyboard focus, or its value changed in the last frame. It reads
// the response flags of the last sync, so it answers for the frame whose
// write-back just landed; call it on the render goroutine. A zero handle is
// never editing.
func WidgetEditing(h widgethandle.WidgetHandle) (editing bool) {
	if h.IsZero() {
		return
	}
	f := c.CurrentApplicationState.StateManager.GetResponse(h)
	editing = f.HasFocus() || f.HasChanged()
	return
}
