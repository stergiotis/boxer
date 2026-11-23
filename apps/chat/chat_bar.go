package chat

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/tokenmeter"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Above the bar is the window's menu bar (ADR-0404 §SD8): View opens and
// closes the views beside the transcript, Conversation holds what this
// conversation tells the host and Open in mdedit. The bar is one row
// (§SD7): New conversation, the title, the state chip — whose inspector
// holds the conversation's facts — and what the model may do in words,
// which opens Settings. While a task runs, a row under it has Stop task and
// the marks the conversation carries; under that, at most one notice. The
// plain chat, a chatlet and a pane in another app draw the same bar; a pane
// wraps it, and puts the two menus at the end of its row of views instead of
// a menu bar of their own.

const (
	tipMay = "What the model may do in the windows you share: the most these settings let it ask for. It asks before it starts, and you decide. " +
		"Click for Settings, where each part is set and the scale shows what a running task was granted."
	tipHighlight = "A conversation in another window has a turn you have not seen; Conversations lists it."
)

// The menus' labels are fixed: a label that changes moves the menu's id and
// closes it.
var (
	atomsMenuView         = c.Atoms().Text("View").Keep()
	atomsMenuConversation = c.Atoms().Text("Conversation").Keep()
	atomsMenuRename       = c.Atoms().Text(icons.PhPencilSimple + " Rename").Keep()
)

// renderBar is the bar: its row, the task's row while there is one, and at
// most one notice.
func (inst *App) renderBar() {
	for range inst.barRow() {
		inst.renderNewButton()
		if inst.conv.started {
			inst.renderTitleText()
		}
		inst.renderTurnState()
		inst.renderMayWords()
		if n := len(inst.roster.notices); n > 0 && !inst.compact() {
			if c.Button(inst.ids.PrepareStr("highlights"), c.Atoms().Text(icons.PhStar+" "+strconv.Itoa(n)+" elsewhere").Keep()).
				Kind(c.ButtonKindGhost).Small().Extend().HoverText(tipHighlight).SendResp().HasPrimaryClicked() {
				inst.showTab(tabConversations)
			}
		}
	}
	inst.renderTask()
	inst.renderBarNotice()
}

// mayLevelWords is what a level of the ladder lets the model do, as a
// sentence's end.
var mayLevelWords = map[agent.LevelE]string{
	agent.LevelTalk:    "only talk",
	agent.LevelRead:    "read windows you share",
	agent.LevelView:    "read and steer windows you share",
	agent.LevelEdit:    "edit windows you share",
	agent.LevelRun:     "edit and run things in windows you share",
	agent.LevelOutside: "also publish and export, each time you confirm",
}

// mayBarWords is a level as the bar says it, short enough for its row,
// after "Model: ".
var mayBarWords = map[agent.LevelE]string{
	agent.LevelTalk:    "talks only",
	agent.LevelRead:    "may read your windows",
	agent.LevelView:    "may steer your windows",
	agent.LevelEdit:    "may edit your windows",
	agent.LevelRun:     "may edit and run in your windows",
	agent.LevelOutside: "may publish, if you confirm",
}

// mayWords is the bar's words for what the model may do: the most the
// settings let it ask for. What a running task was granted is in the
// state chip's inspector and in Settings.
func (inst *App) mayWords() (s string) {
	if inst.coord == nil || !inst.appsOn() {
		if inst.artefactOn() && inst.perms.artPolicy().write {
			return "Model: talks and writes the document"
		}
		return "Model: " + mayBarWords[agent.LevelTalk]
	}
	may, _ := inst.scores()
	return "Model: " + mayBarWords[may.Level]
}

// renderMayWords draws mayWords as a frameless button that opens Settings.
func (inst *App) renderMayWords() {
	// Extend, and the tooltip on the button itself: in the wrapping row the
	// words move to the next line whole, where a HoverText scope would keep
	// them on this one, past the window's edge.
	if c.Button(inst.ids.PrepareStr("may-words"), c.Atoms().Text(inst.mayWords()).Keep()).Frame(false).Extend().HoverText(tipMay).SendResp().HasPrimaryClicked() {
		inst.showTab(tabSettings)
	}
}

// renderMenuBar is the window's menu bar.
func (inst *App) renderMenuBar() {
	for range c.MenuBar().KeepIter() {
		inst.renderMenus()
	}
}

// renderMenus are View and Conversation, in the menu bar or at the end of
// a compact pane's row.
func (inst *App) renderMenus() {
	for range c.MenuButton(atomsMenuView).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("menu-view")) {
			inst.renderMenuViews()
		}
	}
	for range c.MenuButton(atomsMenuConversation).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("menu-conversation")) {
			inst.renderConversationMenu()
		}
	}
}

// renderConversationMenu holds the conversation's commands: a new one, its
// name, what it tells the host (Watch, Urgent), and Open in mdedit. The
// items are the same in every state. One that does not apply yet is drawn
// disabled, and a line under them says why: a menu that opens on a sentence
// and no items reads as a tooltip (keelson-ux C4).
func (inst *App) renderConversationMenu() {
	if c.Button(inst.ids.PrepareStr("menu-new"), atomsNew).SendResp().HasPrimaryClicked() {
		inst.newConversation()
	}
	started := inst.conv.started
	for range c.EnabledUi(started).KeepIter() {
		if c.Button(inst.ids.PrepareStr("menu-rename"), atomsMenuRename).SendResp().HasPrimaryClicked() && started {
			inst.startRename()
		}
	}
	c.Separator().Send()
	inst.renderWatchToggle()
	inst.renderUrgentToggle()
	c.Separator().Send()
	hosted := started && inst.bus != nil
	for range c.HoverText(tipMdedit).KeepIter() {
		for range c.EnabledUi(hosted).KeepIter() {
			if c.Button(inst.ids.PrepareStr("open-mdedit"), atomsMdedit).SendResp().HasPrimaryClicked() && hosted {
				inst.openInMdedit()
			}
		}
	}
	switch {
	case !started:
		c.Separator().Send()
		weak(whyNotStarted)
	case inst.bus == nil:
		c.Separator().Send()
		weak(whyNoHost)
	}
}

// What the Conversation menu says under its disabled items.
const (
	whyNotStarted = "The rest apply after the first message."
	whyNoHost     = "Watch, Urgent and Open in mdedit need a host."
)

// startRename puts the title into its edit, from the title's click or the
// menu's Rename.
func (inst *App) startRename() {
	inst.renaming, inst.renameDraft = true, inst.conv.title
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.renameDraft)
}

// menuViews are the views the View menu opens and the dock's column holds,
// in that order: what the person works with first, the builder's last.
func (inst *App) menuViews() (views []chatTabE) {
	views = []chatTabE{tabConversations, tabSettings}
	if inst.artefactOn() {
		views = append(views, tabArtefact)
	}
	if inst.definition != nil {
		views = append(views, tabDefinition)
	} else {
		views = append(views, tabAnalytics)
	}
	return append(views, tabStops)
}

// viewTips say what each view holds, on its menu entry.
var viewTips = map[chatTabE]string{
	tabConversations: "The conversations other chat windows hold and the ones this host kept; a click resumes one.",
	tabSettings:      "What the model may do, what a new conversation starts with, and the model.",
	tabArtefact:      "The document the model writes beside the chat, its revisions and its images.",
	tabDefinition:    "The document this window was made from: what the model is told, and the settings it runs under.",
	tabAnalytics:     "For whoever builds the harness: the conversation's agent surface and its token and answer statistics.",
	tabStops:         "For whoever builds the harness: conditions that hold or end a turn.",
}

// renderMenuViews lists the views, each checked while it is open; a
// compact pane's row names some of them already, and those are left out
// here.
func (inst *App) renderMenuViews() {
	for _, t := range inst.menuViews() {
		if inst.compact() && (t == tabConversations || t == tabSettings || t == tabDefinition || t == tabArtefact) {
			continue
		}
		if t == tabSettings && inst.coord != nil && !inst.artefactOn() && !inst.compact() {
			inst.renderMenuView(t)
			// The menu lists the document in every conversation, in its
			// place; one started without it has the item disabled, with
			// the reason in its hover.
			off := false
			for range c.HoverText(tipNoDocument).KeepIter() {
				for range c.EnabledUi(false).KeepIter() {
					c.Checkbox(inst.ids.PrepareStr("no-document"), false, tabArtefact.title()).SendRespVal(&off)
				}
			}
			continue
		}
		inst.renderMenuView(t)
	}
}

// renderMenuView is one view's checked item.
func (inst *App) renderMenuView(t chatTabE) {
	on := inst.viewOpen(t)
	box := on
	for range c.HoverText(viewTips[t]).KeepIter() {
		if c.Checkbox(inst.ids.PrepareSeq(uint64(t)), on, inst.viewTitle(t)).SendRespVal(&box).HasPrimaryClicked() {
			inst.setView(t, !on)
		}
	}
}

const tipNoDocument = "This conversation has no document. Whether it has one is fixed at its first message: tick Document before sending it."

// renderConversationFacts is the state chip's inspector body (ADR-0404
// §SD7): the conversation's id, what its first message fixed, the model and
// its endpoint, the context used, its label, and the task.
func (inst *App) renderConversationFacts() {
	conv := inst.conv
	d := styletokens.ActiveDensity()
	factHead("Conversation")
	inst.renderConvId("conv-id-facts")
	if conv.started {
		label, _ := keepBadge(conv)
		parts := []string{label}
		if conv.notKept != "" {
			parts[0] += " (" + conv.notKept + ")"
		}
		if inst.coord != nil {
			parts = append(parts, onOff("apps", conv.apps), onOff("questions", conv.questions), onOff("document", conv.artefact))
		}
		for range c.HoverText(tipKeep).KeepIter() {
			c.Label("Fixed at its first message: " + strings.Join(parts, " · ")).Wrap().Send()
		}
	} else {
		c.Label("Not started: its first message fixes Keep, Apps, Questions and Document.").Wrap().Send()
	}
	c.AddSpace(styletokens.PaddingInner(d))
	factHead("Model")
	switch {
	case !inst.answered:
		c.Label("asking the host for a model…").Send()
	case !inst.model.Configured:
		c.Label("The host offers no model: " + inst.model.Reason).Wrap().Send()
	default:
		m := inst.model
		line := m.Model + " at " + m.EndpointHost + " · " + modelWhere(m.Local, m.Trusted)
		inst.factCopy("model", line, "the model and its endpoint")
		inst.renderContextFact()
	}
	if inst.coord == nil {
		return
	}
	c.AddSpace(styletokens.PaddingInner(d))
	inst.renderLabelFacts()
	task, _, _ := inst.coord.stateOrNone()
	if task == "" {
		return
	}
	c.AddSpace(styletokens.PaddingInner(d))
	factHead("Task")
	_, now := inst.scores()
	inst.factCopy("task", task, "the task's id")
	c.Label("Granted: " + mayLevelWords[now.Level] + ". The model works in your windows under it until it is stopped or ends.").Wrap().Send()
}

// renderContextFact is how full the model's context is: the last answered
// call's prompt and answer against the size the host reports, as a bar
// once the size is known.
func (inst *App) renderContextFact() {
	used, limit, _, over, _, _ := inst.contextFill()
	switch {
	case used <= 0:
		c.Label("Context: nothing sent yet").Send()
	case limit <= 0:
		for range c.HoverText(tipContext).KeepIter() {
			c.Label("Context: " + tokens(used) + " tokens used; the host does not know the model's size").Wrap().Send()
		}
	default:
		for range c.HoverText(tipContext).KeepIter() {
			bar := c.ProgressBar(min(tokenmeter.Fraction(used, limit), 1)).DesiredWidth(contextBarW).Text("context " + tokens(used) + " / " + tokens(limit) + " tokens")
			if over {
				bar = bar.Fill(color.Hex(styletokens.WarningDefault.AsHex()))
			}
			bar.Send()
		}
	}
}

// factHead is a heading inside the inspector.
func factHead(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Strong()
	}
}

// factCopy is a fact as selectable text with a copy button beside it.
func (inst *App) factCopy(key string, text string, what string) {
	for range c.IdScope(inst.ids.PrepareStr("fact-" + key)) {
		for range c.HorizontalWrapped().KeepIter() {
			c.Label(text).Selectable(true).Send()
			for range c.HoverText("Copy " + what).KeepIter() {
				if c.Button(inst.ids.PrepareStr("copy"), atomsCopy).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
					inst.copyText(what, text)
				}
			}
		}
	}
}

// onOff is a first-message choice as the inspector names it.
func onOff(name string, on bool) string {
	if on {
		return name
	}
	return "no " + name
}
