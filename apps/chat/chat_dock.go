package chat

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The window's body is one dock, as play's is: the transcript, the composer
// split below it, and a column of tabs beside them — Conversations,
// Settings, Analytics, Artefact. Every divider drags. The split fractions
// are a preset: once the person drags one, the dock's own state wins for
// the life of the window. The bar above the dock stays fixed; its buttons
// bring a tab to the front.
//
// The tab set is fixed: a tab declared later would land in the first leaf,
// beside the transcript, rather than in the column. So a tab whose feature
// is off stays, and says how to turn it on.

// chatTabE names a tab of the dock; the values are its stable dock ids.
type chatTabE uint64

const (
	tabTranscript chatTabE = iota + 1
	tabComposer
	tabConversations
	tabSettings
	tabAnalytics
	tabArtefact
	tabEnd
)

func (inst chatTabE) title() string {
	switch inst {
	case tabTranscript:
		return "Transcript"
	case tabComposer:
		return "Message"
	case tabConversations:
		return "Conversations"
	case tabSettings:
		return "Settings"
	case tabAnalytics:
		return "Analytics"
	case tabArtefact:
		return "Artefact"
	}
	return "?"
}

// The dock's initial splits: the share the transcript keeps above the
// composer, and the share the transcript and composer keep beside the
// column.
const (
	dockSplitComposer float32 = 0.80
	dockSplitColumn   float32 = 0.64
)

// tabProbeSalt names each tab's visibility probe, through the id stack and
// with the tab's id folded in.
const tabProbeSalt uint64 = 0x636861742d746162

// showTab brings tab to the front on the next frame.
func (inst *App) showTab(tab chatTabE) { inst.dockActivate = tab }

// tabShown is whether tab was drawn last frame: a hidden tab's body is not
// laid out, and its probe reports nothing.
func (inst *App) tabShown(tab chatTabE) bool { return inst.tabSeen[tab] }

// markTabShown arms tab's probe; call it first in the tab's body.
func (inst *App) markTabShown(tab chatTabE) (w float32) {
	w, _, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(tabProbeSalt ^ uint64(tab)).Derive())
	inst.tabSeen[tab] = ok && w > 0
	return
}

// renderDock is the window's body.
func (inst *App) renderDock() {
	for dock := range c.DockArea(inst.ids.PrepareStr("chat-dock")) {
		if inst.dockActivate != 0 {
			dock.ActivateTab(uint64(inst.dockActivate))
			inst.dockActivate = 0
		}
		// The column takes the whole height; the composer sits under the
		// transcript alone. root keeps addressing the transcript's leaf
		// after each split.
		root := dock.InitRoot(uint64(tabTranscript))
		_ = dock.Split(root, c.DockRight, dockSplitColumn,
			uint64(tabConversations), uint64(tabSettings), uint64(tabAnalytics), uint64(tabArtefact))
		_ = dock.Split(root, c.DockBelow, dockSplitComposer, uint64(tabComposer))
		// The transcript and the composer scroll themselves.
		for range dock.TabNoScroll(uint64(tabTranscript), tabTranscript.title()) {
			inst.markTabShown(tabTranscript)
			if len(inst.conv.entries) == 0 && inst.pending == nil {
				inst.renderEmpty()
			} else {
				inst.renderTranscript()
			}
		}
		for range dock.TabNoScroll(uint64(tabComposer), tabComposer.title()) {
			inst.markTabShown(tabComposer)
			inst.renderComposer()
		}
		for range dock.Tab(uint64(tabConversations), tabConversations.title()) {
			inst.markTabShown(tabConversations)
			inst.renderRunsTab()
		}
		for range dock.Tab(uint64(tabSettings), tabSettings.title()) {
			inst.markTabShown(tabSettings)
			for range c.IdScope(inst.ids.PrepareStr("settings-body")) {
				inst.renderSettings()
			}
		}
		for range dock.Tab(uint64(tabAnalytics), tabAnalytics.title()) {
			w := inst.markTabShown(tabAnalytics)
			inst.renderStatsTab(w)
		}
		title := tabArtefact.title()
		if inst.artefactOn() && (inst.conv.art.pending() != nil || inst.coord.pixelAskNow() != nil) {
			title += " · waiting"
		}
		for range dock.TabNoScroll(uint64(tabArtefact), title) {
			inst.markTabShown(tabArtefact)
			inst.renderArtefactTab()
		}
	}
}
