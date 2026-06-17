package videooutput

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// This file is the demo/gallery seam (ADR-0088). The widget gallery and the
// screenshot tour have no live remote viewer to fetch capabilities from, so
// they cannot drive the control through [ShowStatus] (which fetches each
// frame). NewGalleryState builds a State over a fixed model, and ShowGallery
// renders the shared dialog body inline — embedded in the demo panel rather
// than as a floating window, which a gallery scene wants. Production never
// calls them: it holds a zero [State], lets [ShowStatus] populate it, and opens
// the floating [ShowDialog].

// NewGalleryState builds a State over a fixed model, for the widget gallery and
// the screenshot tour.
func NewGalleryState(model videopipeline.Model) *State {
	return &State{model: model, dialogOpen: true}
}

// ShowGallery renders the settings content inline (no floating window, no Close
// button) so a gallery scene embeds it in the demo panel. It is the same
// [showContent] body the production [ShowDialog] draws, under a "Video output"
// header that stands in for the window title bar.
func ShowGallery(ids *c.WidgetIdStack, st *State) {
	if len(st.model.Offered()) == 0 {
		return
	}
	for range c.IdScope(ids.PrepareStr("videoout-panel")) {
		for rt := range c.RichTextLabel("Video output") {
			rt.Strong()
		}
		c.Separator().Horizontal().Send()
		showContent(ids, st)
	}
}
