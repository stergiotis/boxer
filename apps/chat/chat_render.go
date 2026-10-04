package chat

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

const (
	// composerRows is the composer's height in text rows.
	composerRows = 3
	// composerMaxHeight is the ceiling on the composer's scroll area.
	composerMaxHeight float32 = 140
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
	tipContext  = "Tokens the last answered call used, prompt and answer. The whole conversation is resent each turn, so this grows until the model's context is full, and then the turn fails."
	tipPast     = "This window holds one conversation for as long as it is open. A kept conversation is read afterwards in play, over SQL on boxer.facts: the llmMessage rows, whose text is their llmMessageBody."
	tipApps     = "The model may ask you to share windows with it and work in them through their operations. Fixed at the first send. The host answers only a chat listed in BOXER_AGENT_COORDINATORS."
	tipNoApps   = "This conversation was started without Apps: its model has no tools and cannot open, read or drive windows, and will say so if asked. Apps is chosen before the first message — New conversation, turn Apps on in Settings, and ask again."
	tipTainted  = "Text from outside your messages — a window's title, an app's content or error — reached the model, and may have steered what it wrote since. Grant dialogs say so, and every action records it, for as long as the conversation lasts."
	tipConfined = "A result derived from sealed data. Every later model call of this conversation is labelled confined, and the host sends it only to a loopback or trusted endpoint; elsewhere the call is refused."
	hintDraft   = "Message the model — Ctrl+Enter sends"

	tipQuestions = "The model may ask you questions with options, drawn as a form in the conversation; the turn waits for your answer. Needs a model that calls tools. Fixed at the first send."

	tipConvId     = "This conversation's id. The host's records name it — keelson('llm_calls') and keelson('agent_actions') in their conversation column, and the audit trail on boxer.facts — so it is what to search for to find what this chat did. New conversation starts a new one."
	tipCopyConvId = "Copy the conversation's id"
	tipCopy       = "Copy this message's markdown"
	tipRetry      = "Send this message again"
	tipEditFailed = "Put this message back in the composer to change it"
	tipEdit       = "Take this turn back and put its message in the composer; sending replaces the answer. Cancel edit puts it back."
	tipRegenerate = "Answer the last message again; this answer is replaced. A kept conversation keeps both."
	tipCopyError  = "Copy the failure's details"
	tipOpenCall   = "Open the call's row of keelson('llm_calls') in play"
	tipMdedit     = "Open this conversation as a markdown document in mdedit — a document of its own, not autosaved: save it there to keep it"

	// contextBarW is the context meter's width; contextWarnFrac how full
	// the context is when the meter turns and the composer warns.
	contextBarW     float32 = 180
	contextWarnFrac float32 = 0.8
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
	for range c.PanelTopInside(inst.ids.PrepareStr("bar")).Resizable(false).KeepIter() {
		inst.renderBar()
	}
	for range c.PanelBottomInside(inst.ids.PrepareStr("composer")).Resizable(false).KeepIter() {
		inst.renderComposer()
	}
	inst.renderStatsPanel()
	inst.renderSettingsPanel()
	inst.renderArtefactPanel()
	for range c.PanelCentralInside().KeepIter() {
		if len(inst.conv.entries) == 0 && inst.pending == nil {
			inst.renderEmpty()
		} else {
			inst.renderTranscript()
		}
	}
}

// renderBar is a row of controls — New conversation, Settings with the scale
// of what the model may do (ADR-0280), Analytics — and a row of what the
// conversation is: what it was started with, the context used and the model
// (ADR-0265 §SD4). Then the task while Apps is on, then at most one notice.
// The options themselves are in the Settings panel.
func (inst *App) renderBar() {
	conv := inst.conv
	if conv.started {
		inst.renderTitle()
	}
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("new"), atomsNew).SendResp().HasPrimaryClicked() {
			inst.newConversation()
			conv = inst.conv
		}
		inst.renderSettingsToggle()
		inst.renderStatsToggle()
		inst.renderArtefactToggle()
		inst.renderTurnState()
	}
	// A row of its own for what the conversation is: the controls above
	// already fill a narrow window.
	for range c.HorizontalTop().KeepIter() {
		inst.renderConvId("conv-id-bar")
		if conv.started {
			label, tone := keepBadge(conv)
			tip := tipKeep
			if conv.notKept != "" {
				tip = "Not kept: " + conv.notKept + "\n\n" + tipKeep
			}
			badge.New(inst.ids.PrepareStr("keep-state"), label).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tip).Send()
			if conv.apps {
				badge.New(inst.ids.PrepareStr("apps-state"), "apps").Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipApps).Send()
			} else if inst.coord != nil {
				badge.New(inst.ids.PrepareStr("apps-state"), "no apps").Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipNoApps).Send()
			}
			if conv.questions {
				badge.New(inst.ids.PrepareStr("questions-state"), "questions").Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipQuestions).Send()
			}
			if conv.artefact {
				badge.New(inst.ids.PrepareStr("artefact-state"), "artefact").Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipArtefact).Send()
			}
		}
		inst.renderContext()
		switch {
		case !inst.answered:
			c.Label("asking the host for a model…").Selectable(false).Send()
		case inst.model.Configured:
			tip := "The host's model, and the endpoint it is reached at."
			if inst.model.Trusted {
				// Not loopback: the deployment trusts it with sealed data.
				tip += " The host trusts this endpoint with sealed data (BOXER_LLM_TRUSTED_HOSTS)."
			}
			for range c.HoverText(tip).KeepIter() {
				label := "→ " + inst.model.Model + " · " + inst.model.EndpointHost
				if inst.model.Trusted {
					label += " · trusted"
				}
				c.Label(label).Selectable(false).Send()
			}
		}
	}
	inst.renderTask()
	switch {
	case conv.notKept != "" && !conv.notKeptNoted:
		// Shown until read, once per conversation; the badge's hover keeps
		// the reason after.
		for range c.HorizontalTop().KeepIter() {
			badge.New(inst.ids.PrepareStr("not-kept"), "not kept: "+conv.notKept).
				Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
			if c.Button(inst.ids.PrepareStr("not-kept-ok"), atomsGotIt).Frame(false).Small().SendResp().HasPrimaryClicked() {
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
			if c.Button(inst.ids.PrepareStr("past-ok"), atomsGotIt).Frame(false).Small().SendResp().HasPrimaryClicked() {
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
			if c.Button(inst.ids.PrepareStr("copy"), atomsCopy).Frame(false).Small().SendResp().HasPrimaryClicked() {
				inst.copyText("the conversation's id", id)
			}
		}
	}
}

// renderTitle is the conversation's title — click to rename — and Open in
// mdedit.
func (inst *App) renderTitle() {
	conv := inst.conv
	for range c.HorizontalTop().KeepIter() {
		if inst.renaming {
			inst.renderRename()
		} else {
			title := conv.title
			if title == "" {
				title = "Untitled"
			}
			for range c.HoverText(titleTip(conv)).KeepIter() {
				if c.Button(inst.ids.PrepareStr("title"), c.Atoms().BeginRichText(title).Strong().End().Keep()).Frame(false).SendResp().HasPrimaryClicked() {
					inst.renaming, inst.renameDraft = true, conv.title
					c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.renameDraft)
				}
			}
			if conv.titleAsked && conv.titleSource == titleFirstLine && conv.titleNote == "" {
				c.Spinner().Send()
			}
		}
		for range c.HoverText(tipMdedit).KeepIter() {
			if c.Button(inst.ids.PrepareStr("open-mdedit"), atomsMdedit).Small().SendResp().HasPrimaryClicked() {
				inst.openInMdedit()
			}
		}
	}
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
	if c.Button(inst.ids.PrepareStr("rename-cancel"), atomsGotIt).Frame(false).Small().SendResp().HasPrimaryClicked() {
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

// renderContext is how full the model's context is: the last answered
// call's prompt and answer against the size llm.describe reports, a bar
// once the size is known, and a count without it.
func (inst *App) renderContext() {
	conv := inst.conv
	used := int64(conv.lastIn) + int64(conv.lastOut)
	if used <= 0 {
		return
	}
	limit := int64(inst.model.ContextTokens)
	if limit <= 0 {
		for range c.HoverText(tipContext + " The host does not know the model's context size; BOXER_LLM_CONTEXT_TOKENS states it.").KeepIter() {
			c.Label("context " + tokens(used)).Selectable(false).Send()
		}
		return
	}
	frac := float32(used) / float32(limit)
	tip := tipContext + " The size is " + strconv.FormatInt(limit, 10) + " tokens, from " + inst.model.ContextSource + "."
	for range c.HoverText(tip).KeepIter() {
		bar := c.ProgressBar(min(frac, 1)).DesiredWidth(contextBarW).Text("context " + tokens(used) + " / " + tokens(limit))
		if frac >= contextWarnFrac {
			bar = bar.Fill(color.Hex(styletokens.WarningDefault.AsHex()))
		}
		bar.Send()
	}
}

// contextWarning is the line above the composer once the conversation
// nears the model's context size.
func (inst *App) contextWarning() (line string, ok bool) {
	conv := inst.conv
	limit := int64(inst.model.ContextTokens)
	used := int64(conv.lastIn) + int64(conv.lastOut)
	if limit <= 0 || float32(used) < contextWarnFrac*float32(limit) {
		return "", false
	}
	pct := strconv.FormatInt(used*100/limit, 10)
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
	c.AddSpace(48)
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
			for rt := range c.RichTextLabel("Chat with " + inst.model.Model) {
				rt.Heading()
			}
			lines := []string{"at " + inst.model.EndpointHost}
			if inst.keep {
				lines = append(lines, "kept on boxer.facts where the host allows; read afterwards in play")
			} else {
				lines = append(lines, "not kept: only the call counts are recorded")
			}
			switch {
			case inst.apps:
				lines = append(lines, "Apps on: the model may ask you to share windows with it")
			case inst.coord != nil:
				lines = append(lines, "Apps off: the model gets no tools and cannot open or drive windows — turn Apps on in Settings before the first message")
			}
			if inst.questions && inst.coord != nil {
				lines = append(lines, "Questions on: the model may ask you with a form of options")
			}
			lines = append(lines, "Ctrl+Enter sends · Esc cancels a running answer")
			for _, l := range lines {
				for rt := range c.RichTextLabel(l) {
					rt.Weak()
				}
			}
		}
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
			if s.failed && c.Button(inst.ids.PrepareStr("note-ok"), atomsGotIt).Frame(false).Small().SendResp().HasPrimaryClicked() {
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
	w, _, ok := c.CapturePaneSize(c.ProbeSeq("chat", "composer") ^ inst.ids.PrepareHighEntropy(composerProbeSalt).Derive())
	if !ok || w <= 0 {
		w = fallbackComposerW
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
		// While a turn runs, Cancel takes Send's place: Button has no
		// disabled state, and a Send that does nothing would read as broken.
		if !busy {
			send = c.Button(inst.ids.PrepareStr("send"), atomsSend).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked()
		} else {
			for range c.HoverText("Stop waiting for this answer and tell the host to stop the call (Esc in the composer)").KeepIter() {
				cancel = c.Button(inst.ids.PrepareStr("cancel"), atomsCancel).SendResp().HasPrimaryClicked() || cancel
			}
		}
		// The row so far is the button: its width, read next frame, is
		// what the input leaves free.
		c.CaptureUiRect(btnProbe)
		// Hscroll keeps the input's width from feeding back into the
		// window's minimum width (mdedit's editor found this). The input
		// stays editable while a turn runs, so the next message can be
		// written; it is sent once the answer is in.
		for range c.ScrollArea().Hscroll(true).Vscroll(true).MaxHeight(composerMaxHeight).AutoShrink(false, true).KeepIter() {
			b := c.TextEdit(inst.ids.PrepareStr("draft"), inst.draft, true).
				DesiredWidth(max(w-btnW-buttonGap, 120)).
				DesiredRows(composerRows).
				HintText(hintDraft)
			if busy {
				b = b.CaptureKeys(uint64(escMask))
			}
			if inst.hlOk {
				b = b.HighlightJob(inst.hlJob)
			}
			inst.draftId = b.Id()
			b.SendRespVal(&inst.draft)
		}
	}
	if cancel {
		inst.turn.Cancel()
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
	m, kinds := transcriptModel(conv, inst.pending != nil, time.Now().UnixMilli())
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
		Viewer: 0, Layout: chatview.LayoutDialogue, Location: time.Local, FillHost: true,
		InteractiveBlocks: true,
		Block: func(ord int) (chatview.Block, bool) {
			k := kinds[ord]
			switch {
			case k.pending:
				if o := inst.openAsk(); o != nil {
					return chatview.Block{Render: func() { inst.renderAsk(o) }}, true
				}
				return chatview.Block{Render: inst.renderWaiting}, true
			case k.entry >= 0:
				e := &conv.entries[k.entry]
				if e.doc == nil {
					e.doc = markdown.Parse([]byte(e.text))
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
		line := "thinking… " + elapsed(p.started)
		if note := inst.turn.Snapshot().Note; note != "" {
			// The tool loop's round, so a long turn reads as working.
			line += " · " + note
		}
		c.Label(line).Selectable(false).Send()
	}
	c.RequestRepaint()
}

// renderEntry is one message: its markdown, a failure under a turn that
// got no answer, and its actions.
func (inst *App) renderEntry(i int, e *entry) {
	res := markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "doc", Doc: e.doc,
		ActionLabels: codeLabels, CodeActionMask: codeMask})
	for _, a := range res.Actions {
		switch a.Button {
		case codeCopy:
			inst.copyText("the code block", a.Text)
		case codeOpenInPlay:
			inst.openSqlInPlay(a.Text)
		}
	}
	if e.failed {
		inst.renderFailure(e)
	}
	inst.renderActions(i, e)
}

// renderActions is the row under a message. Retry, Edit and Regenerate
// act on the last turn only: an earlier one is in the history every later
// turn resent.
func (inst *App) renderActions(i int, e *entry) {
	conv := inst.conv
	last := conv.lastUser()
	for range c.HorizontalTop().KeepIter() {
		action := func(key string, atoms typed.RetainedFffiHolderTyped[c.AtomsS], tip string) (clicked bool) {
			for range c.HoverText(tip).KeepIter() {
				clicked = c.Button(inst.ids.PrepareStr(key), atoms).Frame(false).Small().SendResp().HasPrimaryClicked()
			}
			return
		}
		if action("copy", atomsCopy, tipCopy) {
			inst.copyText("the message", e.text)
		}
		switch {
		case e.speaker == speakerUser && i == last && e.failed:
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

// ordinalKind says what a transcript ordinal shows: an entry, or the
// waiting bubble.
type ordinalKind struct {
	entry   int
	pending bool
}

// transcriptModel builds the chatview model: participant 0 is the user,
// 1 the model, ordinal i is entry i and the waiting bubble comes last. A
// failed user message is marked failed; its bubble draws why.
//
// A message's Body is its text and, after a NUL, what decides the actions
// drawn under it: chatview fits the user's bubbles to a body measured once
// per Body, so a bubble that gains a row — a failure, Retry — has to be
// measured again.
func transcriptModel(conv *conversation, pending bool, nowMs int64) (m *chatview.Model, kinds []ordinalKind) {
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
	last := conv.lastUser()
	rewind := conv.canRewind()
	for i, e := range conv.entries {
		if e.speaker == speakerTool {
			// The coordinator's activity: a tool call and how it ended.
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
		body := e.text
		if e.speaker == speakerUser {
			body += "\x00" + strconv.FormatBool(e.failed) + strconv.FormatBool(i == last && (e.failed || rewind))
		}
		add(e.atMs, sender, body, flags, status, ordinalKind{entry: i})
	}
	if pending {
		add(nowMs, 1, "…", 0, chatview.StatusNone, ordinalKind{entry: -1, pending: true})
	}
	return
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

// renderTask is the coordinator's row (ADR-0269) while it holds a task:
// the task, whether its conversation read untrusted content and holds
// confined content, and Stop.
func (inst *App) renderTask() {
	if inst.coord == nil {
		return
	}
	task, tainted, confined := inst.coord.state()
	if task == "" {
		return
	}
	for range c.HorizontalTop().KeepIter() {
		for range c.HoverText("The task the model works in your windows under: " + task).KeepIter() {
			c.Label("task " + strings.TrimPrefix(task, "task-")).Selectable(false).Send()
		}
		if tainted {
			badge.New(inst.ids.PrepareStr("tainted"), "read untrusted content").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipTainted).Send()
		}
		if confined {
			badge.New(inst.ids.PrepareStr("confined"), "holds confined content").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tipConfined).Send()
		}
		if c.Button(inst.ids.PrepareStr("stop-task"), c.Atoms().Text("Stop task").Keep()).SendResp().HasPrimaryClicked() {
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
		}
	}
}
