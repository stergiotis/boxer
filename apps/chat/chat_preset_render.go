package chat

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// renderPresetEmpty is the head of a preset's empty transcript: its name,
// its greeting and its starters.
func (inst *App) renderPresetEmpty() {
	p := inst.preset
	for rt := range c.RichTextLabel(p.Manifest.Display) {
		rt.Heading()
	}
	if p.Greeting != "" {
		c.LabelAtoms(c.Atoms().Text(p.Greeting).Keep()).Wrap().Selectable(true).Send()
	}
	if len(p.Starters) > 0 {
		c.AddSpace(8)
		for _, s := range p.Starters {
			for range c.HoverText(s.Text).KeepIter() {
				if c.Button(inst.ids.PrepareStr("starter-"+s.Label), c.Atoms().Text(s.Label).Keep()).SendResp().HasPrimaryClicked() {
					inst.fillDraft(s.Text)
				}
			}
		}
		c.AddSpace(8)
	}
	for _, l := range inst.perms.plainAbilities(inst.apps && inst.coord != nil, inst.questions && inst.coord != nil, inst.artefact && inst.coord != nil) {
		c.LabelAtoms(c.Atoms().BeginRichText(l).Weak().End().Keep()).Wrap().Selectable(true).Send()
	}
}

// plainAbilities says what the settings let the model do, in words a
// newcomer reads: derived from them, so it cannot drift from what the host
// enforces. It claims nothing a level does not, so an app's own narrower rule
// (play only runs reads for an agent) stays the document's to say.
func (inst permissions) plainAbilities(apps bool, questions bool, artefact bool) (lines []string) {
	var can, cannot []string
	if !apps {
		can = append(can, "talk with you")
		cannot = append(cannot, "open or work in windows")
	} else {
		if inst.launch {
			can = append(can, "open app windows")
		} else {
			cannot = append(cannot, "open windows")
		}
		can = append(can, []string{
			allowRead:    "read the windows you share",
			allowView:    "read and navigate the windows you share",
			allowEdit:    "read and edit the windows you share",
			allowRun:     "read, edit and run things in the windows you share",
			allowOutside: "read, edit and run things in the windows you share, and publish or export once you confirm",
		}[inst.allow])
		switch inst.reach {
		case agent.ReachHost:
			can = append(can, "reach this host")
		case agent.ReachData:
			can = append(can, "reach this host and its data servers")
		default:
			can = append(can, "reach this host, its data servers and the network")
		}
		if inst.allow < allowOutside {
			cannot = append(cannot, "publish or export")
		}
		if inst.reach < agent.ReachNetwork {
			cannot = append(cannot, "reach the network")
		}
		if !inst.desktop {
			cannot = append(cannot, "arrange your desktop")
		}
	}
	if questions {
		can = append(can, "ask you questions")
	}
	if artefact && inst.artPolicy().write {
		can = append(can, "write a document for you")
	}
	lines = append(lines, "It can "+wordList(can, "and")+".")
	if len(cannot) > 0 {
		lines = append(lines, "It cannot "+wordList(cannot, "or")+".")
	}
	if apps {
		line := "Nothing is shared until it asks and you agree."
		if inst.changes == changesAsk && inst.allow > allowRead {
			line += " Each change waits for your OK."
		}
		lines = append(lines, line)
	}
	return
}

// wordList joins items as a sentence does, last before conj.
func wordList(items []string, conj string) string {
	if n := len(items); n > 1 {
		return strings.Join(items[:n-1], ", ") + " " + conj + " " + items[n-1]
	}
	return strings.Join(items, "")
}

const tipDetails = "The conversation's id, what the model may do, its context and model, Watch and Urgent. What holds a task and Stop stay in view."

// renderSimpleBar is the bar of a simple window: New conversation,
// the title and Details, which unfolds the rest. It is a toggle in the row
// rather than a collapsing header, which would take a row of its own in the
// bar it exists to shorten.
func (inst *App) renderSimpleBar() {
	for range c.HorizontalTop().KeepIter() {
		inst.renderNewButton()
		if inst.conv.started {
			inst.renderTitleText()
		}
		caret := icons.PhCaretRight
		if inst.detailsOpen {
			caret = icons.PhCaretDown
		}
		for range c.HoverText(tipDetails).KeepIter() {
			if c.Button(inst.ids.PrepareStr("bar-details"), c.Atoms().Text(caret+" Details").Keep()).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
				inst.detailsOpen = !inst.detailsOpen
			}
		}
	}
	if !inst.detailsOpen {
		return
	}
	for range c.HorizontalTop().KeepIter() {
		inst.renderScale()
		inst.renderTurnState()
		if inst.conv.started {
			inst.renderTitleActions()
		}
	}
	inst.renderConversationRow()
}

// renderFollowups offers a preset's next messages under the latest answer,
// while nothing is running. A click fills the composer; the person sends.
func (inst *App) renderFollowups(i int, e *entry) {
	p := inst.preset
	if p == nil || len(p.Followups) == 0 || inst.pending != nil || inst.conv.stranded ||
		e.speaker != speakerModel || e.failed || i != len(inst.conv.entries)-1 {
		return
	}
	for range c.HorizontalWrapped().KeepIter() {
		for _, s := range p.Followups {
			for range c.HoverText(s.Text).KeepIter() {
				if c.Button(inst.ids.PrepareStr("followup-"+s.Label), c.Atoms().Text(s.Label).Keep()).Small().SendResp().HasPrimaryClicked() {
					inst.fillDraft(s.Text)
				}
			}
		}
	}
}

// turnNote is the running turn's progress line. A guided window's says which
// step it is on: "round 3 of 48" reads as a bar that is nearly empty, and 48
// is only a limit.
func turnNote(guided bool, round int, limit int, doing string) (note string) {
	note = "round " + strconv.Itoa(round+1) + " of " + strconv.Itoa(limit)
	if guided {
		note = "step " + strconv.Itoa(round+1)
	}
	if doing != "" {
		note += " · " + doing
	}
	return
}

// artefactShown is whether a conversation's artefact was brought forward.
type artefactShown struct {
	conv  string
	shown bool
}

// advance says whether to bring the artefact forward now: the first time a
// conversation has something in it. One that opens with something in it, a
// resumed one, stays where the person left it.
func (inst *artefactShown) advance(conv string, written bool) (bring bool) {
	if inst.conv != conv {
		inst.conv, inst.shown = conv, written
		return false
	}
	if written && !inst.shown {
		inst.shown = true
		return true
	}
	return false
}

// syncArtefactTab brings a guided window's artefact forward when the model
// first writes it or proposes to: the result of the conversation should not
// wait in a tab the person has to know about. The render goroutine calls it
// once per frame.
func (inst *App) syncArtefactTab() {
	if !inst.guided() || !inst.artefactOn() {
		return
	}
	n, _ := inst.conv.art.head()
	if inst.artShown.advance(inst.conv.id, n > 0 || inst.conv.art.pending() != nil) {
		inst.showTab(tabArtefact)
	}
}

// tipDefinition says what the tab is, above the document.
const tipDefinition = "The document this window was made from: its body is what the model is told, and its properties are the settings it runs under."

// renderDefinitionTab is the preset's document, with its frontmatter last,
// where the helper is designed to sit.
func (inst *App) renderDefinitionTab() {
	weak(tipDefinition)
	for range c.IdScope(inst.ids.PrepareStr("definition-body")) {
		markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "definition-doc", Doc: inst.definition})
		markdown.RenderFrontmatter(inst.definition)
	}
}
