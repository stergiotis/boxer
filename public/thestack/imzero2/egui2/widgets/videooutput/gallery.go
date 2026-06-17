package videooutput

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// This file is the demo/gallery seam (ADR-0088). The widget gallery and the
// screenshot tour have no live remote viewer to fetch capabilities from, so
// they cannot drive the control through [ShowStatus] (which fetches each
// frame). These helpers let a gallery scene render the real [ShowDialog] body
// over a fixed model instead. Production never calls them — it holds a zero
// [State] and lets [ShowStatus] populate it.

// NewGalleryState builds an open-dialog State over a fixed model, for the
// widget gallery and the screenshot tour.
func NewGalleryState(model videopipeline.Model) *State {
	return &State{model: model, dialogOpen: true}
}

// ShowGallery renders the settings dialog over a gallery State, forcing it open
// each frame — the gallery has no status-bar indicator to reopen it after a
// Close click. It is otherwise the production [ShowDialog], so the gallery and
// the tour exercise the real window, codec table, and disabled-encoder
// rendering.
func ShowGallery(ids *c.WidgetIdStack, st *State) {
	st.dialogOpen = true
	ShowDialog(ids, st)
}
