package chat

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The window's body is one dock (ADR-0404 §SD7, §SD8): the transcript, the
// composer split below it, and a column beside them for the views the
// person opened — Conversations, Settings, Document, Analytics or
// Definition, Stops. A side view is declared only while it is open, so with
// none open there is no column and the transcript has the window. The
// column is declared whole every frame; the dock puts a view that opens in
// the leaf of the views already open, or makes the column again where it
// was declared when none is. Every divider drags, and a tab can be dragged
// beside another; the dock keeps where the person put it. Views are opened
// from the View menu (chat_bar.go) and closed from it or from the view's own
// tab, whose close button the dock reports here: the view is then no longer
// declared, and the menu's check follows. The transcript and the composer
// cannot be closed, and while each is alone in its leaf it has no tab strip:
// a pane with one view needs nothing to switch views with.

// chatTabE names a view of the window; the values are stable, and key the
// views' widget scopes and probes.
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

// The dock's declared splits: the share the transcript and the composer
// keep beside the column, which at the window's default width leaves the
// column room for the document's previews (artefactPanelW), and the share
// the transcript keeps above the composer.
const (
	dockSplitColumn   float32 = 0.48
	dockSplitComposer float32 = 0.80
)

// chatWindowWidth is the window's default width.
const chatWindowWidth = 860

// tabProbeSalt names each view's visibility probe, through the id stack and
// with the view's id folded in.
const tabProbeSalt uint64 = 0x636861742d746162

// sideState is which side views are open. asked is the decision the
// document was last brought forward for, so closing the document over it
// does not reopen it at once; artConv is the conversation whose document was
// last brought forward.
type sideState struct {
	open    [tabEnd]bool
	asked   string
	artConv string
}

// show opens tab.
func (inst *sideState) show(tab chatTabE) {
	if tab < tabEnd {
		inst.open[tab] = true
	}
}

// close closes tab; the dock brings another of the column's to the front.
func (inst *sideState) close(tab chatTabE) {
	if tab < tabEnd {
		inst.open[tab] = false
	}
}

// opened lists the open views among views, in that order.
func (inst *sideState) opened(views []chatTabE) (out []chatTabE) {
	for _, t := range views {
		if inst.open[t] {
			out = append(out, t)
		}
	}
	return
}

// showTab opens view tab and brings it to the front: in the dock, or in a
// compact pane's place of the transcript, which takes it on its next frame.
// The dock declares an opened view from the frame it is opened in, so a
// click in the bar, drawn before the dock, shows the view in that frame.
func (inst *App) showTab(tab chatTabE) {
	inst.dockActivate = tab
	if !inst.compact() && tab != tabTranscript && tab != tabComposer {
		inst.side.show(tab)
	}
}

// viewOpen is whether view tab is open: a column tab of the dock, or the
// view a compact pane draws in the transcript's place.
func (inst *App) viewOpen(tab chatTabE) bool {
	if inst.compact() {
		return inst.embed.shown == tab
	}
	return inst.side.open[tab]
}

// setView opens or closes view tab, as the View menu's check box asks.
func (inst *App) setView(tab chatTabE, open bool) {
	switch {
	case open:
		inst.showTab(tab)
	case inst.compact():
		inst.embed.shown = 0
	default:
		inst.side.close(tab)
	}
}

// tabShown is whether view tab was drawn last frame: a hidden view's body
// is not laid out, and its probe reports nothing.
func (inst *App) tabShown(tab chatTabE) bool { return inst.tabSeen[tab] }

// markTabShown arms view tab's probe; call it first in the view's body.
func (inst *App) markTabShown(tab chatTabE) (w float32) {
	w, _, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(tabProbeSalt ^ uint64(tab)).Derive())
	inst.tabSeen[tab] = ok && w > 0
	return
}

// syncSide opens the document when it first has content or a decision
// waits in it: once for each such occasion. The render goroutine calls it
// once per frame, before the body is drawn.
func (inst *App) syncSide() {
	if !inst.artefactOn() {
		if inst.side.open[tabArtefact] && !inst.conv.started {
			inst.side.close(tabArtefact)
		}
		return
	}
	n, _ := inst.conv.art.head()
	waiting := inst.artefactAsks()
	if (n > 0 || waiting != "") && inst.side.artConv != inst.conv.id {
		// Once per conversation, when it first has something in it: one
		// that opens with content, or a resumed one, brings it forward too.
		inst.side.artConv = inst.conv.id
		inst.showTab(tabArtefact)
	}
	if waiting != "" && waiting != inst.side.asked {
		inst.side.asked = waiting
		inst.showTab(tabArtefact)
	}
}

// artefactAsks names the decision waiting in the artefact, "" for none.
func (inst *App) artefactAsks() string {
	if p := inst.conv.art.pending(); p != nil {
		return "proposal-" + strconv.FormatUint(p.seq, 10)
	}
	if a := inst.coord.pixelAskNow(); a != nil {
		return "pixels-" + a.hash
	}
	return ""
}

// renderDock is the window's body.
func (inst *App) renderDock() {
	// A view not drawn this frame was not shown, whatever its probe said
	// last frame; each drawn one's probe says so below.
	inst.tabSeen = [tabEnd]bool{}
	views := inst.menuViews()
	for dock := range c.DockArea(inst.ids.PrepareStr("chat-dock")) {
		// A side view closed from its tab last frame is closed now, before
		// the open views are declared (ADR-0404 §SD8).
		for _, t := range views {
			if inst.side.open[t] && dock.CloseRequested(uint64(t)) {
				inst.side.close(t)
			}
		}
		if a := inst.dockActivate; a != 0 {
			dock.ActivateTab(uint64(a))
			inst.dockActivate = 0
		}
		// The column is declared whole, open or not, so a view that opens
		// goes to it (ADR-0404 §SD8). Declared before the composer's split,
		// it takes the window's height; the composer sits under the
		// transcript alone. root keeps addressing the transcript's leaf
		// after each split.
		root := dock.InitRoot(uint64(tabTranscript))
		column := make([]uint64, 0, len(views))
		for _, t := range views {
			column = append(column, uint64(t))
		}
		_ = dock.Split(root, c.DockRight, dockSplitColumn, column...)
		_ = dock.Split(root, c.DockBelow, dockSplitComposer, uint64(tabComposer))
		// The transcript and the composer scroll themselves.
		dock.HideTabBarWhenAlone(uint64(tabTranscript))
		dock.HideTabBarWhenAlone(uint64(tabComposer))
		for range dock.TabNoScroll(uint64(tabTranscript), tabTranscript.title()) {
			inst.markTabShown(tabTranscript)
			inst.renderTranscriptBody()
		}
		for range dock.TabNoScroll(uint64(tabComposer), tabComposer.title()) {
			inst.markTabShown(tabComposer)
			inst.renderComposer()
		}
		for _, t := range inst.side.opened(views) {
			dock.Closable(uint64(t))
			switch t {
			case tabArtefact:
				// The document holds a dock of its own views, which needs
				// the leaf's height rather than a scroll area's.
				for range dock.TabNoScroll(uint64(t), inst.viewTitle(t)) {
					inst.markTabShown(t)
					inst.renderArtefactTab()
				}
			case tabAnalytics:
				for range dock.Tab(uint64(t), inst.viewTitle(t)) {
					w := inst.markTabShown(t)
					inst.renderStatsTab(w)
				}
			default:
				for range dock.Tab(uint64(t), inst.viewTitle(t)) {
					inst.markTabShown(t)
					inst.renderViewBody(t)
				}
			}
		}
	}
}

// renderViewBody is a view's body; its dock tab or the compact pane
// scrolls it.
func (inst *App) renderViewBody(tab chatTabE) {
	switch tab {
	case tabConversations:
		inst.renderRunsTab()
	case tabSettings:
		for range c.IdScope(inst.ids.PrepareStr("settings-body")) {
			inst.renderSettings()
		}
	case tabDefinition:
		inst.renderDefinitionTab()
	case tabStops:
		for range c.IdScope(inst.ids.PrepareStr("stops-body")) {
			inst.renderStopsTab()
		}
	}
}

// viewTitle is a view's name where it is offered or switched to: the
// Conversations and Stops views carry their counts.
func (inst *App) viewTitle(tab chatTabE) string {
	switch tab {
	case tabConversations:
		return inst.conversationsTitle()
	case tabStops:
		return inst.stopsTitle()
	case tabArtefact:
		return inst.artefactTitle()
	}
	return tab.title()
}

// renderTranscriptBody is the transcript, or the empty state before the
// first message.
func (inst *App) renderTranscriptBody() {
	switch {
	case len(inst.conv.entries) != 0 || inst.pending != nil:
		inst.renderTranscript()
	default:
		// The empty state can be taller than the pane, and its last lines
		// say where the conversation goes.
		for range c.PushId(inst.ids.PrepareStr("empty")).KeepIter() {
			for range c.ScrollArea().Vscroll(true).Hscroll(false).KeepIter() {
				inst.renderEmpty()
			}
		}
	}
}

// conversationsTitle carries a highlight in another conversation.
func (inst *App) conversationsTitle() (title string) {
	title = tabConversations.title()
	if n := len(inst.roster.notices); n > 0 {
		title += " " + icons.PhStar + strconv.Itoa(n)
	}
	return
}

// artefactTitle carries a change of the artefact waiting.
func (inst *App) artefactTitle() (title string) {
	title = tabArtefact.title()
	if inst.artefactOn() && (inst.conv.art.pending() != nil || inst.coord.pixelAskNow() != nil) {
		title += " " + icons.PhHourglassMedium
	}
	return
}
