package mdedit

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdoutline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// sideViewE is which view the side panel shows. One panel with a tab strip
// rather than a column per view: the views compete for the same width
// beside the preview, and a toggle per view read as a setting rather than as
// a place to go.
type sideViewE uint8

const (
	sideOutline sideViewE = iota
	sideFiles
)

const (
	// sidePanelFrac is the panel's share of the window, taken from the
	// preview rather than from the source — the source pane's width is what
	// the writer is working in. One share for every view, so switching tabs
	// does not move the panes beside it.
	sidePanelFrac = float32(0.24)

	// sidePanelMinWindowPx is the width below which the panel hides itself. A
	// fixed share of a narrow window is a column too thin to read a heading
	// in, and it would take that width from the two panes that are doing the
	// work. Hiding is not a failure mode here; it is the honest answer to
	// "there is no room".
	sidePanelMinWindowPx = float32(760)
)

const tipSidePanel = "Show the side panel beside the preview: the heading outline, or the lading snapshot store's files. The panel needs a window wide enough to spare it, and hides itself below that rather than starving the two panes doing the work."

var atomsSidePanel = c.Atoms().Text(icons.PhSidebarSimple + " Panel").Keep()

// sideVisible reports whether the panel renders this frame: the reader has
// it switched on AND the window is wide enough to give it a share.
func (inst *App) sideVisible() (yes bool) {
	if !inst.showSide {
		return false
	}
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	return winW >= sidePanelMinWindowPx
}

// sideWidth is the panel's width for this frame, derived every frame for the
// reason the source split is: a retained panel width clamps destructively and
// never recovers.
func (inst *App) sideWidth() (px float32) {
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	return winW * sidePanelFrac
}

// sideInnerWidth is the width the panel's content has: the measurement once
// it lands, the panel's share less its margins before.
func (inst *App) sideInnerWidth() (px float32) {
	if inst.sidePaneW > 0 {
		return inst.sidePaneW
	}
	return inst.sideWidth() - 2*styletokens.PaddingOuter(styletokens.ActiveDensity())
}

// renderSideToggle is the bar's switch for the panel.
func (inst *App) renderSideToggle() {
	for range c.HoverText(tipSidePanel).KeepIter() {
		if c.Button(inst.ids.PrepareStr("side"), atomsSidePanel).
			Selected(inst.showSide).SendResp().HasPrimaryClicked() {
			inst.showSide = !inst.showSide
		}
	}
}

// renderSide draws the panel: the tab strip, then the chosen view. The
// outline tab names its count, so the strip says what the tab would show.
// Callers own the enclosing panel.
func (inst *App) renderSide() {
	if w, _, ok := c.CapturePaneSize(inst.paneProbeSeq("side-pane")); ok && w > 0 {
		inst.sidePaneW = w
	}
	var headings []markdown.HeadingInfo
	if inst.doc != nil {
		headings = inst.doc.Headings()
	}
	selector.Segmented(inst.ids, "side-tabs", &inst.sideView).
		Style(selector.StyleSelectable).
		Gap(styletokens.GapInline(styletokens.ActiveDensity())).
		OptionIcon(sideOutline, icons.PhListBullets, outlineSummary(headings), tipOutline).
		OptionIcon(sideFiles, icons.PhFolders, "Files", tipFiles).
		SendResp()
	c.Separator().Send()
	switch inst.sideView {
	case sideFiles:
		inst.renderFiles()
	default:
		if mdoutline.Count(headings) == 0 {
			renderEmptyState(icons.PhListBullets, "No headings yet",
				"A line starting with # is a heading; the outline lists them, nested by level.")
			return
		}
		inst.renderOutline()
	}
}

// renderEmptyState is what a view shows when it has nothing to show: what is
// missing, and how to get it.
func renderEmptyState(icon, title, hint string) {
	d := styletokens.ActiveDensity()
	c.AddSpace(styletokens.PaddingOuter(d))
	c.LabelAtoms(c.Atoms().BeginRichText(icon).Heading().Weak().End().Keep()).Send()
	c.LabelAtoms(c.Atoms().BeginRichText(title).Strong().End().Keep()).Send()
	if hint != "" {
		c.LabelAtoms(c.Atoms().BeginRichText(hint).Small().Weak().End().Keep()).Send()
	}
	c.AddSpace(styletokens.PaddingInner(d))
}
