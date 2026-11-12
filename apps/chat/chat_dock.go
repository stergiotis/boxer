package chat

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The window's body is one dock, as play's is: the transcript, the composer
// split below it, and a column of tabs beside them — Conversations,
// Settings, Analytics, Artefact, Stops. Every divider drags. The split fractions
// are a preset: once the person drags one, the dock's own state wins for
// the life of the window. The bar above the dock stays fixed; the tabs
// themselves are how a pane is reached, and their titles carry its state.
//
// The tab set is fixed: a tab declared later would land in the first leaf,
// beside the transcript, rather than in the column. So a tab whose feature
// is off stays, and says how to turn it on. Definition is fixed per window:
// a preset's window has it in Analytics' place from the first frame, since
// the column has room for five titles; the plain chat never has it.

// chatTabE names a tab of the dock; the values are its stable dock ids.
type chatTabE uint64

const (
	tabTranscript chatTabE = iota + 1
	tabComposer
	tabConversations
	tabSettings
	tabAnalytics
	tabArtefact
	tabDefinition
	tabStops
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
	case tabDefinition:
		return "Definition"
	case tabStops:
		return "Stops"
	}
	return "?"
}

// The dock's initial splits: the share the transcript keeps above the
// composer, and the share the transcript and composer keep beside the
// column — at the window's default width, what leaves the column room for
// its five tab titles, Stops with a two-digit count among them.
const (
	dockSplitComposer float32 = 0.80
	dockSplitColumn   float32 = 0.52
)

// chatWindowWidth is the window's default width: with dockSplitColumn, the
// column's five titles fit and the transcript keeps the width it had under
// four.
const chatWindowWidth = 860

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
		third := tabAnalytics
		if inst.definition != nil {
			third = tabDefinition
		}
		_ = dock.Split(root, c.DockRight, dockSplitColumn,
			uint64(tabConversations), uint64(tabSettings), uint64(third), uint64(tabArtefact), uint64(tabStops))
		_ = dock.Split(root, c.DockBelow, dockSplitComposer, uint64(tabComposer))
		// The transcript and the composer scroll themselves.
		for range dock.TabNoScroll(uint64(tabTranscript), tabTranscript.title()) {
			inst.markTabShown(tabTranscript)
			inst.renderTranscriptBody()
		}
		for range dock.TabNoScroll(uint64(tabComposer), tabComposer.title()) {
			inst.markTabShown(tabComposer)
			inst.renderComposer()
		}
		// The tabs carry what the bar's buttons used to: a highlight in
		// another conversation, a change of the artefact waiting.
		for range dock.Tab(uint64(tabConversations), inst.conversationsTitle()) {
			inst.markTabShown(tabConversations)
			inst.renderRunsTab()
		}
		for range dock.Tab(uint64(tabSettings), tabSettings.title()) {
			inst.markTabShown(tabSettings)
			for range c.IdScope(inst.ids.PrepareStr("settings-body")) {
				inst.renderSettings()
			}
		}
		if third == tabAnalytics {
			for range dock.Tab(uint64(tabAnalytics), tabAnalytics.title()) {
				w := inst.markTabShown(tabAnalytics)
				inst.renderStatsTab(w)
			}
		} else {
			for range dock.Tab(uint64(tabDefinition), tabDefinition.title()) {
				inst.markTabShown(tabDefinition)
				inst.renderDefinitionTab()
			}
		}
		for range dock.TabNoScroll(uint64(tabArtefact), inst.artefactTitle()) {
			inst.markTabShown(tabArtefact)
			inst.renderArtefactTab()
		}
		for range dock.Tab(uint64(tabStops), inst.stopsTitle()) {
			inst.markTabShown(tabStops)
			for range c.IdScope(inst.ids.PrepareStr("stops-body")) {
				inst.renderStopsTab()
			}
		}
	}
}

// renderTranscriptBody is the transcript, or the empty state before the
// first message.
func (inst *App) renderTranscriptBody() {
	switch {
	case len(inst.conv.entries) != 0 || inst.pending != nil:
		inst.renderTranscript()
	case inst.preset != nil:
		// A preset's empty state can be taller than the tab, and its
		// last lines say where the conversation goes.
		for range c.PushId(inst.ids.PrepareStr("preset-empty")).KeepIter() {
			for range c.ScrollArea().Vscroll(true).Hscroll(false).KeepIter() {
				inst.renderEmpty()
			}
		}
	default:
		inst.renderEmpty()
	}
}

// conversationsTitle carries a highlight in another conversation.
func (inst *App) conversationsTitle() (title string) {
	title = tabConversations.title()
	if n := len(inst.roster.notices); n > 0 {
		title += " · " + icons.PhStar + " " + strconv.Itoa(n)
	}
	return
}

// artefactTitle carries a change of the artefact waiting.
func (inst *App) artefactTitle() (title string) {
	title = tabArtefact.title()
	if inst.artefactOn() && (inst.conv.art.pending() != nil || inst.coord.pixelAskNow() != nil) {
		title += " · waiting"
	}
	return
}
