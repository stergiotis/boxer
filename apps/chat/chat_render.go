package chat

import (
	"context"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/tokenmeter"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/editexternally"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

const (
	// composerRows is the composer's least height in text rows; dragging the
	// divider above it gives it more.
	composerRows = 3
	// composerChromePx is the input's own frame and margin, held back from
	// the rows the pane's height holds.
	composerChromePx float32 = 10 + 2*hoverSlackPx
	// hoverSlackPx is the room kept around the input inside its scroll
	// area: a hovered control is drawn past its own rect by the theme's
	// expansion (fresh's HOVER_LIFT, tokens::apply_stroke's note), and the
	// scroll area clips at its edge, which cut the outline's sides off.
	hoverSlackPx float32 = 1
	// rowSeedFactor turns the body font's size into a row height on the
	// first frame, before the measurement lands.
	rowSeedFactor = 1.45
	// rowProbeSalt names the row-height measurement, through the id stack.
	rowProbeSalt uint64 = 0x636861742d726f77
	// fallbackButtonW stands in for Send's measured width on the first
	// frame; buttonGap is the space between it and the input.
	fallbackButtonW float32 = 72
	buttonGap       float32 = 8
	// fallbackComposerW stands in for the pane probe on its first frame.
	fallbackComposerW float32 = 600
	// composerProbeSalt makes the composer's pane probe this window's own:
	// folded through the id stack, which carries the host's window salt.
	composerProbeSalt uint64 = 0x636861742d636f6d
	// buttonProbeSalt is the probe that measures Send (or Cancel).
	buttonProbeSalt uint64 = 0x636861742d62746e

	tipKeep     = "Keep this conversation's messages on boxer.facts, to read later in play. The host keeps them only when its BOXER_LLM_RETAIN is durable: the badge reads keep asked until the first answer says, then kept or not kept. Fixed at the first send."
	tipContext  = "Tokens the last answered call used, prompt and answer: about what the next request carries before your message. The conversation is resent each turn, so this grows with it."
	tipPast     = "This window holds one conversation for as long as it is open. A kept conversation is read afterwards in play, over SQL on boxer.facts: the llmMessage rows, whose text is their llmMessageBody."
	tipApps     = "The model may ask you to share windows with it and work in them through their operations. Fixed at the first send. The host answers only a chat listed in BOXER_AGENT_COORDINATORS."
	tipNoApps   = "This conversation was started without Apps: its model has no tools and cannot open, read or drive windows, and will say so if asked. Apps is chosen before the first message — New conversation, turn Apps on in Settings, and ask again."
	tipTainted  = "Content from outside your own words — a window's title, an app's content or error, what an image shows — reached the model, and may have steered what it wrote since. Grant dialogs say so, and every action records it, for as long as the conversation lasts."
	tipConfined = "A result derived from sealed data. Every later model call of this conversation is labelled confined, and the host sends it only to a loopback or trusted endpoint; elsewhere the call is refused. Its artefact cannot be exported to the lading store."
	hintDraft   = "Message the model — Ctrl+Enter sends"

	tipQuestions = "The model may ask you questions with options, drawn as a form in the conversation; the turn waits for your answer. Needs a model that calls tools. Fixed at the first send."

	tipConvId     = "This conversation's id. The host's records name it — keelson('llm_calls') and keelson('agent_actions') in their conversation column, and the audit trail on boxer.facts — so it is what to search for to find what this chat did. New conversation starts a new one."
	tipCopyConvId = "Copy the conversation's id"
	tipOpenTrail  = "Open what this conversation did in play: its rows of the audit trail's timeline on boxer.facts, oldest first"
	tipCopy       = "Copy this message's markdown, its references marked with how their checks came out and its citations under it"
	tipRetry      = "Send this message again"
	tipEditFailed = "Put this message back in the composer to change it"
	tipEdit       = "Take this turn back and put its message in the composer; sending replaces the answer. Cancel edit puts it back."
	tipRegenerate = "Answer the last message again; this answer is replaced. A kept conversation keeps both."
	tipCopyError  = "Copy the failure's details"
	tipOpenCall   = "Open the call's row of keelson('llm_calls') in play"
	tipMdedit     = "Open this conversation as a markdown document in mdedit — a document of its own, not autosaved: save it there to keep it"

	// contextBarW is the context meter's width; contextWarnPercent how
	// full the context is, in percent of the model's size, when the meter
	// turns and the composer warns while compaction is off — while it is
	// on, the trigger decides instead (contextFill).
	contextBarW        float32 = 180
	contextWarnPercent int64   = 80
)

var (
	atomsNew        = c.Atoms().Text(icons.PhPlus + " New conversation").Keep()
	atomsSend       = c.Atoms().Text(icons.PhPaperPlaneRight + " Send").Keep()
	atomsCancel     = c.Atoms().Text(icons.PhStop + " Cancel").Keep()
	atomsGotIt      = c.Atoms().Text(icons.PhX).Keep()
	atomsCopy       = c.Atoms().Text(icons.PhCopy).Keep()
	atomsRetry      = c.Atoms().Text(icons.PhArrowClockwise + " Retry").Keep()
	atomsEdit       = c.Atoms().Text(icons.PhPencilSimple + " Edit").Keep()
	atomsRegenerate = c.Atoms().Text(icons.PhArrowClockwise + " Regenerate").Keep()
	atomsCopyError  = c.Atoms().Text(icons.PhCopy + " Copy details").Keep()
	atomsOpenCall   = c.Atoms().Text(icons.PhTable + " Open call in play").Keep()
	atomsOpenTrail  = c.Atoms().Text(icons.PhFootprints).Keep()
	atomsCancelEdit = c.Atoms().Text("Cancel edit").Keep()
	atomsMdedit     = c.Atoms().Text(icons.PhMarkdownLogo + " Open in mdedit").Keep()
	atomsRenameOk   = c.Atoms().Text(icons.PhCheck).Keep()
)

// renameKeyMask is what the title's editor captures: Enter keeps the
// title, Escape leaves it.
var renameKeyMask = keycodes.MaskOf(keycodes.Enter, keycodes.Escape)

// codeLabels are the buttons above a reply's code block: Copy on every
// block, Open in play on SQL (codeMask).
var codeLabels = []string{icons.PhCopy + " Copy", icons.PhArrowSquareOut + " Open in play"}

const (
	codeCopy = iota
	codeOpenInPlay
)

func codeMask(_ string, lang string) (mask uint64) {
	mask = 1 << codeCopy
	if lang == "sql" {
		mask |= 1 << codeOpenInPlay
	}
	return
}

// escMask is what the composer captures while a turn runs: Escape cancels
// the turn. Not captured otherwise, so Escape leaves the field as usual.
var escMask = keycodes.MaskOf(keycodes.Escape)

func (inst *App) render() {
	inst.syncLevels()
	inst.syncLooked()
	inst.syncSide()
	inst.storeTitle()
	inst.publishTurn()
	inst.syncRoster()
	if inst.compact() {
		inst.renderCompact()
		return
	}
	for range c.PanelTopInside(inst.ids.PrepareStr("bar")).Resizable(false).KeepIter() {
		inst.renderMenuBar()
		inst.renderBar()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderDock()
	}
}

// renderNewButton is New conversation.
func (inst *App) renderNewButton() {
	if c.Button(inst.ids.PrepareStr("new"), atomsNew).Extend().SendResp().HasPrimaryClicked() {
		inst.newConversation()
	}
}

// renderBarNotice is at most one notice under the bar.
func (inst *App) renderBarNotice() {
	conv := inst.conv
	switch {
	case conv.notKept != "" && !conv.notKeptNoted:
		// Shown until read, once per conversation; the badge's hover keeps
		// the reason after.
		for range c.HorizontalTop().KeepIter() {
			// The host's reason names its configuration; the line says what
			// it means for the person, and the hover says why.
			badge.New(inst.ids.PrepareStr("not-kept"), "not kept: the host stores no messages to read in play").
				Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip("Not kept: " + conv.notKept + "\n\n" + tipKeep).Send()
			if c.Button(inst.ids.PrepareStr("not-kept-ok"), atomsGotIt).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
				conv.notKeptNoted = true
			}
		}
	case conv.started && conv.keep && conv.notKept == "" && !inst.pastNoted:
		for range c.HorizontalTop().KeepIter() {
			for range c.HoverText(tipPast).KeepIter() {
				for rt := range c.RichTextLabel("Closing this window ends the conversation here; it is kept, and read in play.") {
					rt.Small().Weak()
				}
			}
			if c.Button(inst.ids.PrepareStr("past-ok"), atomsGotIt).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
				inst.pastNoted = true
			}
		}
	}
}

// renderConvId shows the conversation's id, to read and to copy: the key the
// host's records carry for everything this chat does (ADR-0277 §SD1). scope
// keeps the widget ids of its two places apart.
func (inst *App) renderConvId(scope string) {
	id := inst.conv.id
	for range c.IdScope(inst.ids.PrepareStr(scope)) {
		for range c.HoverText(tipConvId).KeepIter() {
			for rt := range c.RichTextLabel(id) {
				rt.Small().Monospace()
			}
		}
		for range c.HoverText(tipCopyConvId).KeepIter() {
			if c.Button(inst.ids.PrepareStr("copy"), atomsCopy).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
				inst.copyText("the conversation's id", id)
			}
		}
		for range c.HoverText(tipOpenTrail).KeepIter() {
			if c.Button(inst.ids.PrepareStr("trail"), atomsOpenTrail).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
				inst.openTrailInPlay(id)
			}
		}
	}
}

// renderTitleText is the title, or its editor while it is renamed.
func (inst *App) renderTitleText() {
	conv := inst.conv
	if inst.renaming {
		inst.renderRename()
		return
	}
	title := conv.title
	if title == "" {
		title = "Untitled"
	}
	tip := titleTip(conv)
	if short := barTitle(title); short != title {
		tip = title + "\n\n" + tip
		title = short
	}
	if c.Button(inst.ids.PrepareStr("title"), c.Atoms().BeginRichText(title).Strong().End().Keep()).Frame(false).Extend().HoverText(tip).SendResp().HasPrimaryClicked() {
		inst.startRename()
	}
	if conv.titleAsked && conv.titleSource == titleFirstLine && conv.titleNote == "" {
		c.Spinner().Send()
	}
}

// barTitleRunes is the most of a title the bar shows: the bar's buttons
// keep their labels on one line (ButtonFluid.Extend) and move to the next
// line of the row when they do not fit, so a title longer than a narrow
// window would run past its edge.
const barTitleRunes = 32

// barTitle is a title as the bar shows it, cut at a word with "…" past
// barTitleRunes; the whole title is in its hover.
func barTitle(title string) string {
	r := []rune(title)
	if len(r) <= barTitleRunes {
		return title
	}
	cut := string(r[:barTitleRunes])
	if i := strings.LastIndexByte(cut, ' '); i > barTitleRunes/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

// renderRename is the title's inline editor: Enter or the check keeps the
// text, Escape leaves the title as it was, and an empty title goes back
// to the first line.
func (inst *App) renderRename() {
	commit, cancel := false, false
	if inst.renameId != 0 {
		for _, k := range c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(inst.renameId)) {
			commit = commit || k.Code == keycodes.Enter
			cancel = cancel || k.Code == keycodes.Escape
		}
	}
	te := c.TextEdit(inst.ids.PrepareStr("rename"), inst.renameDraft, false).
		DesiredWidth(320).HintText("a title — empty goes back to the first line").CaptureKeys(uint64(renameKeyMask))
	inst.renameId = te.Id()
	te.SendRespVal(&inst.renameDraft)
	if c.Button(inst.ids.PrepareStr("rename-ok"), atomsRenameOk).Small().SendResp().HasPrimaryClicked() {
		commit = true
	}
	if c.Button(inst.ids.PrepareStr("rename-cancel"), atomsGotIt).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
		cancel = true
	}
	switch {
	case cancel:
		inst.renaming = false
	case commit:
		inst.conv.rename(inst.renameDraft)
		inst.renaming = false
	}
}

// contextFill is what the context meter and the composer's warning read:
// the last answered call's tokens, or the re-projected size right after
// a compaction until the next answer, the model's context size, the
// compaction trigger, zero with no route or run (ADR-0316), and whether
// the thrash check turned compaction off for the conversation, with why.
// The reported prompt is the base the compaction plan anchors to, so a
// fill at the trigger is a next turn that compacts before it is sent.
// over says the meter turns: at the trigger where there is one, on or
// off, so the warning the person saw at the trigger does not vanish when
// compaction goes off, and at contextWarnPercent of the size without one.
func (inst *App) contextFill() (used int64, limit int64, trigger int64, over bool, off bool, why string) {
	conv := inst.conv
	used = tokenmeter.Used(conv.lastIn, conv.lastOut, nil)
	limit = int64(inst.model.ContextTokens)
	if inst.coord != nil {
		trigger, off, why = inst.coord.compactState()
		// Right after a compaction the request's re-projected size is the
		// fill, not what the answer before the cut reported (ADR-0316).
		if p := inst.coord.projectedSize(); p > 0 {
			used = p
		}
	}
	if trigger > 0 {
		over = used >= trigger
	} else {
		over = tokenmeter.Over(used, limit, contextWarnPercent)
	}
	return
}

// contextWarning is the line above the composer once the meter turns:
// the next turn compacts, or, with compaction off, may not fit.
func (inst *App) contextWarning() (line string, ok bool) {
	used, limit, trigger, over, off, why := inst.contextFill()
	if !over || limit <= 0 {
		return "", false
	}
	pct := strconv.FormatInt(used*100/limit, 10)
	switch {
	case trigger > 0 && !off:
		return "This conversation fills " + pct + " % of the model's context, past the " + strconv.FormatInt(trigger*100/limit, 10) + " % at which it compacts: the next turn elides or summarises older turns first. Your messages are never summarised.", true
	case off:
		return "This conversation fills " + pct + " % of the model's context, and compaction is off for it: " + why + ". Every turn resends it, so the next answer may not fit. New conversation starts over.", true
	}
	return "This conversation fills " + pct + " % of the model's context, and every turn resends it: the next answer may not fit. New conversation starts over.", true
}

// tokens is a token count as shown: 950, 12.3k, 131k.
func tokens(n int64) (s string) {
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 100000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	default:
		return strconv.FormatInt(n/1000, 10) + "k"
	}
}

// renderEmpty is the transcript before the first send: what this window
// talks to, what it keeps, and how to send.
func (inst *App) renderEmpty() {
	if inst.preset != nil {
		// It says more than a plain chat's, in a window no taller.
		c.AddSpace(8)
	} else {
		c.AddSpace(48)
	}
	for range c.VerticalCentered().KeepIter() {
		switch {
		case !inst.answered:
			for rt := range c.RichTextLabel("Asking the host for a model…") {
				rt.Weak()
			}
		case !inst.model.Configured:
			for rt := range c.RichTextLabel("No model to talk to") {
				rt.Heading()
			}
			c.LabelAtoms(c.Atoms().BeginRichText(inst.model.Reason).Weak().End().Keep()).Wrap().Selectable(true).Send()
		default:
			// Where the model runs, in words; its endpoint is in the state
			// chip's inspector (ADR-0404 §SD7).
			first := "The model runs " + modelWhere(inst.model.Local, inst.model.Trusted) + "."
			preset := inst.preset != nil
			if preset {
				inst.renderPresetEmpty()
				first = "The model, " + inst.model.Model + ", runs " + modelWhere(inst.model.Local, inst.model.Trusted) + "."
			} else {
				for rt := range c.RichTextLabel("Chat with " + inst.model.Model) {
					rt.Heading()
				}
			}
			// A started conversation is empty only while its one turn is
			// taken back for an edit: what it was started with stands, and
			// the window's settings are for the next conversation.
			conv := inst.conv
			if !preset && !conv.started {
				// Before the first message, the choices it fixes are offered
				// here, where the person is about to type, as well as in
				// Settings.
				inst.renderFirstSendChoices(first)
				return
			}
			keep, questions := inst.keep, inst.questions
			if conv.started {
				keep, questions = conv.keep && conv.notKept == "", conv.questions
			}
			lines := []string{first}
			switch {
			case keep:
				lines = append(lines, "Kept where the host allows, to read later in play.")
			case conv.started && conv.notKept != "":
				lines = append(lines, "Not kept: "+conv.notKept+".")
			default:
				lines = append(lines, "Not kept: only the number of calls is recorded.")
			}
			switch {
			case preset:
				// renderPresetEmpty said what the model can do, in words.
			case inst.appsOn():
				lines = append(lines, "Apps on: the model may ask you to share windows with it.")
			case inst.coord != nil && conv.started:
				lines = append(lines, "Apps off for this conversation: the model cannot work in windows. New conversation starts one with the settings in Settings.")
			case inst.coord != nil:
				lines = append(lines, "Apps off: the model cannot open or work in windows. Turn Apps on in Settings before the first message.")
			}
			if !preset && questions && inst.coord != nil {
				lines = append(lines, "Questions on: the model may ask you with a form of options.")
			}
			if !preset {
				// A preset's composer hint says the first, and this has to fit.
				lines = append(lines, "Ctrl+Enter sends · Esc cancels a running answer")
			}
			for _, l := range lines {
				for rt := range c.RichTextLabel(l) {
					rt.Weak()
				}
			}
		}
	}
}

// renderFirstSendChoices is the plain chat's empty transcript under its
// heading: where the model is, the four choices the first message fixes —
// the same fields Settings binds — and the keys.
func (inst *App) renderFirstSendChoices(where string) {
	for rt := range c.RichTextLabel(where) {
		rt.Weak()
	}
	c.AddSpace(styletokens.PaddingInner(styletokens.ActiveDensity()))
	for rt := range c.RichTextLabel("This conversation — fixed at its first message:") {
		rt.Small().Weak()
	}
	for range c.IdScope(inst.ids.PrepareStr("first-send")) {
		if inst.coord != nil {
			for range c.HoverText(tipApps).KeepIter() {
				c.Checkbox(inst.ids.PrepareStr("apps"), inst.apps, "Apps — the model may ask to work in your windows").SendRespVal(&inst.apps)
			}
			for range c.HoverText(tipQuestions).KeepIter() {
				c.Checkbox(inst.ids.PrepareStr("questions"), inst.questions, "Questions — the model may ask you with a form").SendRespVal(&inst.questions)
			}
			for range c.HoverText(tipArtefact).KeepIter() {
				c.Checkbox(inst.ids.PrepareStr("artefact"), inst.artefact, "Artefact — a document the model writes beside the chat").SendRespVal(&inst.artefact)
			}
		}
		for range c.HoverText(tipKeep).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("keep"), inst.keep, "Keep — store the messages, to read later in play").SendRespVal(&inst.keep)
		}
	}
	c.AddSpace(styletokens.PaddingInner(styletokens.ActiveDensity()))
	for rt := range c.RichTextLabel("Ctrl+Enter sends · Esc cancels a running answer") {
		rt.Small().Weak()
	}
}

// renderComposer is the status line, the edit strip, then the text input
// with Send beside it — Cancel while a turn runs.
func (inst *App) renderComposer() {
	if !inst.answered || !inst.model.Configured {
		// The empty transcript says why.
		return
	}
	if s, ok := inst.noteShown(); ok {
		for range c.HorizontalTop().KeepIter() {
			if s.failed {
				for rt := range c.RichTextLabelColored(color.Hex(styletokens.ErrorDefault.AsHex()), color.Transparent, icons.PhWarningCircle) {
					rt.Small()
				}
			}
			c.LabelAtoms(c.Atoms().BeginRichText(s.text).Small().Weak().End().Keep()).Wrap().Selectable(true).Send()
			if s.failed && c.Button(inst.ids.PrepareStr("note-ok"), atomsGotIt).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
				inst.note = status{}
			}
		}
		c.RequestRepaint()
	}
	if line, ok := inst.contextWarning(); ok {
		for range c.HorizontalTop().KeepIter() {
			for rt := range c.RichTextLabelColored(color.Hex(styletokens.WarningDefault.AsHex()), color.Transparent, icons.PhWarning) {
				rt.Small()
			}
			c.LabelAtoms(c.Atoms().BeginRichText(line).Small().End().Keep()).Wrap().Selectable(true).Send()
		}
	}
	if inst.coord != nil {
		if running, waiting := inst.coord.pendingNow(); running > 0 || waiting > 0 {
			// A result goes to the model in the turn that is running or about
			// to start for it, unless a wake is withheld.
			line := backgroundLine(running, waiting, inst.pending != nil || inst.wakeReady())
			for rt := range c.RichTextLabel(line) {
				rt.Small().Weak()
			}
			if running > 0 {
				// A watcher settles the call off the render thread, and
				// only a frame lands it: its line, and the turn it wakes.
				c.RequestRepaintAfter(backgroundRepaintSeconds)
			}
		}
	}
	if inst.editing != nil {
		for range c.HorizontalTop().KeepIter() {
			for rt := range c.RichTextLabel(icons.PhPencilSimple + " Editing your last message: sending replaces its answer.") {
				rt.Small().Weak()
			}
			if c.Button(inst.ids.PrepareStr("cancel-edit"), atomsCancelEdit).Small().SendResp().HasPrimaryClicked() {
				inst.cancelEdit()
			}
		}
	}
	inst.renderTransformPicker()
	inst.renderSendRow()
	// The draft handed to the host's editor app, each save taking its place
	// unless it was edited here meanwhile; drawn only where the host
	// registers one editor app. Its buttons stand under Send; its status
	// line, which can be long, goes here, across the composer's width.
	_, hasEditor := externaledit.Editor()
	hasEditor = hasEditor && inst.bus != nil
	if hasEditor {
		if line := editexternally.StatusLine(&inst.externalEdit); line != "" {
			c.LabelAtoms(c.Atoms().BeginRichText(line).Small().Weak().End().Keep()).Wrap().Send()
		}
	}
	w, h, ok := c.CapturePaneSize(c.ProbeSeq("chat", "composer") ^ inst.ids.PrepareHighEntropy(composerProbeSalt).Derive())
	if !ok || w <= 0 {
		w = fallbackComposerW
	}
	if ok && h > 0 {
		inst.composerH = h
	}
	rows := uint32(composerRows)
	if rh := inst.composerRowHeight(); inst.composerH > 0 && rh > 0 {
		rows = max(rows, uint32(max((inst.composerH-composerChromePx)/rh, 0)))
	}
	btnProbe := c.ProbeSeq("chat", "composer-button") ^ inst.ids.PrepareHighEntropy(buttonProbeSalt).Derive()
	btnW := fallbackButtonW
	if r, got := c.CurrentApplicationState.StateManager.GetUiRect(btnProbe); got && r.MaxX > r.MinX {
		btnW = r.MaxX - r.MinX
	}
	busy := inst.pending != nil
	cancel := false
	if busy && inst.draftId != 0 {
		for _, k := range c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(inst.draftId)) {
			cancel = cancel || k.Code == keycodes.Escape
		}
	}
	inst.ensureHighlight()
	send := false
	// A right-to-left row: the button is placed first, at the right edge,
	// and the input fills what is left — a left-to-right row lets the
	// scroll area take the whole width and push the button out of view.
	for range c.UiWithLayout().MainDirRightToLeft().CrossAlignMin().KeepIter() {
		// Send over the editor gesture, in a column at the right edge.
		for range c.UiWithLayout().MainDirTopDown().CrossAlignMax().KeepIter() {
			// While a turn runs, Cancel takes Send's place: Button has no
			// disabled state, and a Send that does nothing would read as broken.
			if !busy {
				send = c.Button(inst.ids.PrepareStr("send"), atomsSend).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked()
			} else {
				for range c.HoverText("Stop waiting for this answer and tell the host to stop the call (Esc in the composer)").KeepIter() {
					cancel = c.Button(inst.ids.PrepareStr("cancel"), atomsCancel).SendResp().HasPrimaryClicked() || cancel
				}
			}
			if hasEditor {
				// Drawn before the input, so a taken save reaches its binding
				// in this frame.
				if r := editexternally.Render(editexternally.Input{
					Ids: inst.ids, ScopeKey: "draft-external", Editing: &inst.externalEdit, Bus: inst.bus, BusCtx: inst.ioCtx,
					Name: "message.md", Text: inst.draft, Small: true, NoStatus: true,
				}); r.Replace {
					inst.draft = r.Text
					c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draft)
				}
			}
		}
		// The row so far is the button column: its width, read next frame,
		// is what the input leaves free.
		c.CaptureUiRect(btnProbe)
		// Hscroll keeps the input's width from feeding back into the
		// window's minimum width (mdedit's editor found this). The input
		// stays editable while a turn runs, so the next message can be
		// written; it is sent once the answer is in. The frame inside is
		// the hover slack (hoverSlackPx).
		for range c.ScrollArea().Hscroll(true).Vscroll(true).MaxHeight(max(inst.composerH, 1)).AutoShrink(false, true).KeepIter() {
			for range c.Frame(inst.ids.PrepareStr("draft-slack")).Fill(color.Transparent).InnerMargin(hoverSlackPx).KeepIter() {
				b := c.TextEdit(inst.ids.PrepareStr("draft"), inst.draft, true).
					DesiredWidth(max(w-btnW-buttonGap-2*hoverSlackPx, 120)).
					DesiredRows(rows).
					HintText(hintDraft).
					AccessibleName("Message")
				if busy {
					b = b.CaptureKeys(uint64(escMask))
				}
				if inst.hlOk {
					b = b.HighlightJob(inst.hlJob)
				}
				inst.draftId = b.Id()
				flags := b.SendRespVal(&inst.draft)
				if inst.embed != nil {
					inst.embed.draftFocused = flags.HasFocus()
				}
			}
		}
	}
	if cancel {
		inst.cancelTurn()
	}
	// The chord is read for the whole process; only the focused window acts
	// on it, so two open chats do not both send (play's claimRunChord).
	if pressed, _ := c.CurrentApplicationState.StateManager.GetCommandEnterPressed(); pressed && inst.focused {
		if busy {
			inst.busyNote()
		} else {
			send = true
		}
	}
	if send && !busy {
		inst.send()
	}
}

// ensureHighlight colours the draft as markdown, mdedit's editor pipeline:
// the lexer, not the canonicalising highlighter, because the spans must
// index the draft's own bytes. Rebuilt only when the draft changed, on the
// render goroutine since building the job issues opcodes; an empty draft
// gets no job, so the hint text shows.
func (inst *App) ensureHighlight() {
	if inst.hlSrc == inst.draft {
		return
	}
	inst.hlSrc = inst.draft
	inst.hlOk = inst.draft != ""
	if inst.hlOk {
		inst.hlJob = codeview.BuildMarkdownFromSpans(inst.draft, markdownhighlight.HighlightLex([]byte(inst.draft)))
	}
}

// renderTranscript is chatview over the conversation, every message drawn
// as markdown — the composer highlights it, so a fence typed there is a
// fence in the bubble — with the message's actions under it, a failed
// turn's failure, and a waiting bubble while a turn is in flight. The
// bubbles give their contents the pointer (InteractiveBlocks), so the
// buttons and the code blocks' rows take clicks.
func (inst *App) renderTranscript() {
	conv := inst.conv
	m, kinds := transcriptModel(conv, inst.looked.mark, inst.pending != nil, time.Now().UnixMilli())
	// A message's action changes the conversation the transcript is being
	// drawn from; it runs once the transcript is drawn.
	inst.later = inst.later[:0]
	defer func() {
		for _, f := range inst.later {
			f()
		}
		inst.later = inst.later[:0]
	}()
	chatview.Render(chatview.Input{
		Ids: inst.ids, ScopeKey: "transcript", Model: m, State: &inst.view,
		Viewer: 0, Layout: chatview.LayoutThread, Location: time.Local, FillHost: true,
		InteractiveBlocks: true, SystemBlocks: true,
		Block: func(ord int) (chatview.Block, bool) {
			k := kinds[ord]
			switch {
			case k.mark:
				return chatview.Block{Render: inst.renderLookedMark}, true
			case k.pending:
				if o := inst.openAsk(); o != nil {
					return chatview.Block{Render: func() { inst.renderAsk(o) }}, true
				}
				return chatview.Block{Render: inst.renderWaiting}, true
			case k.entry >= 0 && (conv.entries[k.entry].speaker == speakerTool || conv.entries[k.entry].woken):
				// A woken turn's line is the host's, as a tool call's is: it
				// says why a turn started, and nobody wrote it (ADR-0358 §SD5).
				e := &conv.entries[k.entry]
				return chatview.Block{Render: func() { inst.renderToolEntry(e) }}, true
			case k.entry >= 0:
				e := &conv.entries[k.entry]
				if e.doc == nil {
					e.doc = markdown.Parse([]byte(e.shown()))
				}
				i := k.entry
				// chatview draws the bubble inside its row's
				// PrepareSeq(ordinal) scope already; a second scope on the
				// same ordinal would cancel it — the id stack is an XOR —
				// and give every message's widgets the same ids.
				return chatview.Block{Render: func() {
					for range c.IdScope(inst.ids.PrepareStr("entry")) {
						inst.renderEntry(i, e)
					}
				}}, true
			}
			return chatview.Block{}, false
		},
	})
}

// renderWaiting is the waiting bubble: how long, and with Apps on which
// round and what the model is doing.
func (inst *App) renderWaiting() {
	p := inst.pending
	if p == nil {
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.Spinner().Send()
		line := "working… " + elapsed(p.started)
		if note := inst.turn.Snapshot().Note; note != "" {
			// The tool loop's round, so a long turn reads as working.
			line += " · " + note
		}
		if steps := inst.stepsNow(); oneModelCall(steps) {
			// renderSteps draws no step line for it: the phase is said here.
			line += " · " + modelPhase(&steps[0])
		}
		c.Label(line).Selectable(false).Send()
	}
	inst.renderSteps()
	steps := inst.stepsNow()
	if text := liveAnswer(steps); text != "" {
		// The answer as the model writes it, paced, drawn as the landed
		// turn will be; the landed turn is drawn from the reply.
		now := time.Now()
		if shown := p.live.answer.advance(len(steps)-1, text, now); shown != "" {
			p.live.seen = true
			markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "live", Doc: p.live.answerDoc(shown, now)})
		}
	}
	c.RequestRepaint()
}

// renderEntry is one message: its markdown, a failure under a turn that
// got no answer, and its actions.
func (inst *App) renderEntry(i int, e *entry) {
	res := markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "doc", Doc: e.doc,
		ActionLabels: codeLabels, CodeActionMask: codeMask, LinkClaims: inst.mdeditLinkClaims()})
	inst.followMdeditLinks(res)
	for _, a := range res.Actions {
		switch a.Button {
		case codeCopy:
			inst.copyText("the code block", a.Text)
		case codeOpenInPlay:
			inst.openSqlInPlay(a.Text)
		}
	}
	inst.renderSent(e)
	if e.failed {
		inst.renderFailure(e)
	}
	inst.renderActions(i, e)
	if inst.renderCiteHold("entry", "copy the message", e.text) {
		inst.copyCited("the message", e.text, e.shown())
	}
	inst.renderFollowups(i, e)
}

// renderActions is the row under a message. Retry, Edit and Regenerate
// act on the last turn only: an earlier one is in the history every later
// turn resent.
func (inst *App) renderActions(i int, e *entry) {
	conv := inst.conv
	last := conv.lastUser()
	for range c.HorizontalTop().KeepIter() {
		// Icon-only actions are ghost buttons: no frame until hovered, but the
		// frame's padding, so the target is wider than the glyph (a frameless
		// one was the glyph's 13 points; ADR-0363 (proposed) M1).
		action := func(key string, atoms typed.RetainedFffiHolderTyped[c.AtomsS], tip string) (clicked bool) {
			for range c.HoverText(tip).KeepIter() {
				clicked = c.Button(inst.ids.PrepareStr(key), atoms).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked()
			}
			return
		}
		if action("copy", atomsCopy, tipCopy) {
			inst.copyCited("the message", e.text, e.shown())
		}
		switch {
		case e.speaker == speakerUser && i == last && e.failed && !e.woken:
			if action("retry", atomsRetry, tipRetry) {
				inst.later = append(inst.later, inst.retry)
			}
			if action("edit", atomsEdit, tipEditFailed) {
				inst.later = append(inst.later, inst.edit)
			}
		case e.speaker == speakerUser && i == last && conv.canRewind():
			if action("edit", atomsEdit, tipEdit) {
				inst.later = append(inst.later, inst.edit)
			}
		case e.speaker == speakerModel && i == len(conv.entries)-1 && conv.canRewind():
			if action("regenerate", atomsRegenerate, tipRegenerate) {
				inst.later = append(inst.later, inst.regenerate)
			}
		}
	}
}

// renderFailure is why a turn got no answer, in a line, and what there is
// to inspect under Details: the class, the call's id and time, and the
// error's whole text — selectable, copyable, and openable in play when the
// call reached the host's call record.
func (inst *App) renderFailure(e *entry) {
	errTone := color.Hex(styletokens.ErrorDefault.AsHex())
	for range c.Frame(inst.ids.PrepareStr("failure")).
		Fill(color.Hex(styletokens.ErrorSubtle.AsHex())).
		CornerRadius(styletokens.RoundingMd).
		Stroke(styletokens.StrokeHair, errTone).
		InnerMargin(styletokens.PaddingTight(styletokens.ActiveDensity())).KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			for rt := range c.RichTextLabelColored(errTone, color.Transparent, icons.PhWarningCircle) {
				rt.Small()
			}
			c.LabelAtoms(c.Atoms().BeginRichText("Not answered: " + e.reason).Small().End().Keep()).Wrap().Selectable(true).Send()
		}
		for range c.CollapsingHeader(inst.ids.PrepareStr("failure-details"), c.WidgetText().Text("Details").Keep()).KeepIter() {
			for _, l := range failureLines(e) {
				c.LabelAtoms(c.Atoms().BeginRichText(l).Small().Monospace().End().Keep()).Wrap().Selectable(true).Send()
			}
			for range c.HorizontalTop().KeepIter() {
				if c.Button(inst.ids.PrepareStr("copy-error"), atomsCopyError).Small().SendResp().HasPrimaryClicked() {
					inst.copyText("the failure's details", strings.Join(failureLines(e), "\n"))
				}
				if e.fail.callId != "" {
					for range c.HoverText(tipOpenCall).KeepIter() {
						if c.Button(inst.ids.PrepareStr("open-call"), atomsOpenCall).Small().SendResp().HasPrimaryClicked() {
							inst.openCallInPlay(e.fail.callId)
						}
					}
				}
			}
		}
	}
}

// failureLines are a failure's details as shown and copied.
func failureLines(e *entry) (lines []string) {
	f := e.fail
	add := func(k, v string) {
		if v != "" {
			lines = append(lines, k+": "+v)
		}
	}
	add("kind", f.kind)
	add("call", f.callId)
	add("at", time.UnixMilli(e.atMs).Format(time.RFC3339))
	if d := f.elapsed.Round(time.Millisecond); d > 0 {
		add("elapsed", d.String())
	}
	add("error", f.detail)
	if f.detail == "" {
		add("error", e.reason)
	}
	add("reasoning, its end", f.reasoning)
	return
}

// keepBadge says what the host did with a started conversation, not what
// was asked: a keep the host declined reads as not kept, and one with no
// verdict yet as asked.
func keepBadge(conv *conversation) (label string, tone badge.ToneE) {
	switch {
	case !conv.keep:
		return "not kept", badge.ToneNeutral
	case conv.notKept != "":
		return "not kept", badge.ToneWarning
	case conv.kept:
		return "kept", badge.ToneSuccess
	default:
		return "keep asked", badge.ToneNeutral
	}
}

// ordinalKind says what a transcript ordinal shows: an entry, the line
// above what landed unseen (mark), or the
// waiting bubble.
type ordinalKind struct {
	entry   int
	pending bool
	mark    bool
}

// transcriptModel builds the chatview model: participant 0 is the user,
// 1 the model, ordinal i is entry i — one more from mark on, where the
// line above what landed unseen is its own row (ordinalOf) — and the
// waiting bubble comes last. A
// failed user message is marked failed; its block draws why.
func transcriptModel(conv *conversation, mark int, pending bool, nowMs int64) (m *chatview.Model, kinds []ordinalKind) {
	m = &chatview.Model{Participants: []chatview.Participant{{Name: "You"}, {Name: "Model"}}}
	add := func(atMs int64, sender int32, body string, flags chatview.FlagsE, status chatview.StatusE, k ordinalKind) {
		if n := len(m.TimeMS); n > 0 && atMs < m.TimeMS[n-1] {
			atMs = m.TimeMS[n-1]
		}
		m.TimeMS = append(m.TimeMS, atMs)
		m.Sender = append(m.Sender, sender)
		m.Body = append(m.Body, body)
		m.ReplyTo = append(m.ReplyTo, -1)
		m.Flags = append(m.Flags, flags)
		m.Status = append(m.Status, status)
		kinds = append(kinds, k)
	}
	for i, e := range conv.entries {
		if i == mark {
			add(e.atMs, -1, "new since you looked", chatview.FlagSystem|chatview.FlagMarker, chatview.StatusNone, ordinalKind{entry: -1, mark: true})
		}
		if e.speaker == speakerTool || e.woken {
			// The coordinator's activity: a tool call and how it ended, or
			// why a turn started with nobody asking.
			add(e.atMs, -1, "⚙ "+e.text, chatview.FlagSystem, chatview.StatusNone, ordinalKind{entry: i})
			continue
		}
		sender := int32(0)
		if e.speaker == speakerModel {
			sender = 1
		}
		status := chatview.StatusNone
		if e.failed {
			status = chatview.StatusFailed
		}
		var flags chatview.FlagsE
		if e.edited {
			flags |= chatview.FlagEdited
		}
		add(e.atMs, sender, e.shown(), flags, status, ordinalKind{entry: i})
	}
	if pending {
		add(nowMs, 1, "…", 0, chatview.StatusNone, ordinalKind{entry: -1, pending: true})
	}
	return
}

// ordinalOf is entry's ordinal in the transcript the model of mark builds.
func ordinalOf(entry int, mark int) int32 {
	if mark >= 0 && entry >= mark {
		return int32(entry + 1)
	}
	return int32(entry)
}

// elapsed is a wait as seconds.
func elapsed(since time.Time) (s string) {
	return strconv.Itoa(int(time.Since(since).Seconds())) + " s"
}

// openAsk is the ask_user waiting for the person in the turn in flight,
// nil when none is.
func (inst *App) openAsk() (o *openAsk) {
	if inst.pending == nil || inst.coord == nil {
		return nil
	}
	return inst.coord.ask.current()
}

// renderTask is the coordinator's row (ADR-0269): whether its conversation
// read untrusted content and holds confined content, and, while it holds a
// task, Stop task. The task's id and the label behind the two marks are in
// the state chip's inspector (ADR-0404 §SD7). The marks are the
// conversation's, not the task's: they stay when the task ends, and a
// conversation that never held one — an image the person sent (ADR-0385
// §SD5) — carries them too.
func (inst *App) renderTask() {
	if inst.coord == nil {
		return
	}
	task, tainted, confined := inst.coord.state()
	if task == "" && !tainted && !confined {
		return
	}
	for range inst.barRow() {
		if tainted {
			badge.New(inst.ids.PrepareStr("tainted"), "read untrusted content").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipTainted).Send()
		}
		if confined {
			badge.New(inst.ids.PrepareStr("confined"), "holds confined content").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipConfined).Send()
		}
		if task == "" {
			continue
		}
		for range c.HoverText(tipStopTask).KeepIter() {
			if c.Button(inst.ids.PrepareStr("stop-task"), c.Atoms().Text(icons.PhStop+" Stop task").Keep()).SendResp().HasPrimaryClicked() {
				inst.stopTask()
			}
		}
	}
}

const tipStopTask = "End the model's work in your windows: what it was granted is taken back, and a running turn stops. The conversation stays; your next message can ask again."

// stopTask ends the conversation's task, as the person's act.
func (inst *App) stopTask() {
	coord, cli := inst.coord, inst.agentCli
	h := coord.handle()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agent.DefaultTimeout)
		defer cancel()
		_ = cli.StopWith(ctx, h, agent.StopRequest{ByPerson: true, Reason: "the person stopped it in the chat"})
	}()
	coord.mu.Lock()
	coord.grant = agent.Grant{}
	coord.mu.Unlock()
	// The person stopped the work, not only the grant: a turn left
	// running reads the task's end as a refusal and asks for a new one.
	if inst.pending != nil {
		inst.cancelTurn()
	}
}

// cancelTurn stops the turn in flight and withholds wakes until the person
// next speaks.
func (inst *App) cancelTurn() {
	inst.turn.Cancel()
	if inst.coord != nil {
		inst.coord.holdWakes()
	}
}

// composerRowHeight is the composer's row height: measured once the
// measurement lands, seeded from the body font's size before; re-armed every
// frame, as Sync resets the binding (mdedit's measureRowHeight).
func (inst *App) composerRowHeight() float32 {
	pt := styletokens.ScaledPt(styletokens.BodyPt, styletokens.DensityStandard)
	if inst.rowFontPt != pt {
		inst.rowFontPt, inst.rowPx = pt, float64(pt)*rowSeedFactor
	}
	seq := inst.ids.PrepareHighEntropy(rowProbeSalt).Derive()
	c.MeasureTextSizeBind(seq, seq^1, "Mg", pt, false, nil, &inst.rowPx)
	return max(float32(inst.rowPx), pt)
}
