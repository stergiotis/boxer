package chat

import (
	"iter"

	"github.com/stergiotis/boxer/public/functional"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The compact layout (ADR-0368) is for a pane with little room:
// the bar, then a row naming the tabs and ending in the window's menus, then
// the transcript over the composer, or the one tab picked in their place. No divider drags and
// nothing docks, so the pane fits whatever region the host gives it.

// compactTabs are the row's tabs after the transcript, in the dock
// column's order. A pane is always a preset's, so Definition stands where
// the plain chat has Analytics.
var compactTabs = [...]chatTabE{tabConversations, tabSettings, tabDefinition, tabArtefact}

// compactComposerPx is the composer's height before the person drags it.
const compactComposerPx float32 = 110

// compact is whether this window draws the compact layout.
func (inst *App) compact() bool {
	return inst.embed != nil && inst.embed.layout == EmbedLayoutCompact
}

// compactTabTitle is the row's name for tab.
func (inst *App) compactTabTitle(tab chatTabE) string {
	switch tab {
	case tabConversations:
		return inst.conversationsTitle()
	case tabArtefact:
		return inst.artefactTitle()
	case tabTranscript:
		return "Chat"
	}
	return tab.title()
}

func (inst *App) renderCompact() {
	e := inst.embed
	if a := inst.dockActivate; a != 0 {
		inst.dockActivate = 0
		e.shown = a
		if a == tabTranscript || a == tabComposer {
			e.shown = 0
		}
	}
	if e.shown == tabDefinition && inst.definition == nil {
		e.shown = 0
	}
	// A tab not drawn this frame was not shown, whatever its probe said
	// last frame; the drawn one's probe says so below.
	inst.tabSeen = [tabEnd]bool{}
	for range c.PanelTopInside(inst.ids.PrepareStr("bar")).Resizable(false).KeepIter() {
		inst.renderBar()
		inst.renderCompactRow()
	}
	if e.shown == 0 {
		for range c.PanelBottomInside(inst.ids.PrepareStr("compact-composer")).Resizable(true).DefaultSize(compactComposerPx).KeepIter() {
			inst.markTabShown(tabComposer)
			inst.renderComposer()
		}
		for range c.PanelCentralInside().KeepIter() {
			inst.markTabShown(tabTranscript)
			inst.renderTranscriptBody()
		}
		return
	}
	for range c.PanelCentralInside().KeepIter() {
		w := inst.markTabShown(e.shown)
		switch e.shown {
		case tabArtefact:
			// The artefact scrolls itself, as in its dock tab.
			inst.renderArtefactTab()
		default:
			for range c.PushId(inst.ids.PrepareStr("compact-tab")).KeepIter() {
				for range c.ScrollArea().Vscroll(true).Hscroll(false).KeepIter() {
					if e.shown == tabAnalytics {
						inst.renderStatsTab(w)
					} else {
						inst.renderViewBody(e.shown)
					}
				}
			}
		}
	}
}

// renderCompactRow names the tabs; the picked one is drawn in the
// transcript's place, and Chat brings the transcript back.
func (inst *App) renderCompactRow() {
	e := inst.embed
	for range inst.barRow() {
		if c.SelectableLabel(inst.ids.PrepareStr("compact-chat"), e.shown == 0, inst.compactTabTitle(tabTranscript)).SendResp().HasPrimaryClicked() {
			e.shown = 0
		}
		for _, tab := range compactTabs {
			if tab == tabDefinition && inst.definition == nil {
				continue
			}
			if c.SelectableLabel(inst.ids.PrepareSeq(uint64(tab)), e.shown == tab, inst.compactTabTitle(tab)).SendResp().HasPrimaryClicked() {
				e.shown = tab
			}
		}
		// The window's menus end the row: a pane has little height, and a
		// menu bar of its own would take a row for two words (ADR-0404 §SD8).
		inst.renderMenus()
	}
}

// barRow is a row of the bar. It wraps: a window or a pane narrower than
// the row gets a second line rather than controls past its edge.
func (inst *App) barRow() iter.Seq[functional.NilIteratorValueType] {
	return c.HorizontalWrapped().KeepIter()
}
