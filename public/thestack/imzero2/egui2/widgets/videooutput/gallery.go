package videooutput

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// This file is the demo/gallery seam (ADR-0088). The widget gallery and the
// screenshot tour have no live remote viewer to fetch capabilities from, so
// they cannot drive the control through [RenderStatus] (which fetches each
// frame). NewGalleryState builds a State over a fixed model, and
// RenderGallery renders the shared dialog body inline — embedded in the demo
// panel rather than as a floating window, which a gallery scene wants.
// Production never calls them: it holds a zero [State], lets [RenderStatus]
// populate it, and opens the floating [RenderDialog].

// NewGalleryState builds a State over a fixed model, for the widget gallery and
// the screenshot tour.
func NewGalleryState(model videopipeline.Model) *State {
	return &State{model: model, dialogOpen: true}
}

// RenderGallery renders the settings content inline (no floating window, no
// Close button) so a gallery scene embeds it in the demo panel. It is the
// same [renderContent] body the production [RenderDialog] draws, under a
// "Video output" header that stands in for the window title bar.
func RenderGallery(in Input) (res Result) {
	res, ok := in.check()
	if !ok {
		return
	}
	if len(in.State.model.Offered()) == 0 {
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		for rt := range c.RichTextLabel("Video output") {
			rt.Strong()
		}
		c.Separator().Horizontal().Send()
		renderContent(in.Ids, in.State)
	}
	res.DialogOpen = in.State.dialogOpen
	return
}
