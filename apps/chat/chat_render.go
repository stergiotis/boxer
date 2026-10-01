package chat

import (
	"context"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

const (
	// composerRows is the composer's height in text rows.
	composerRows = 3
	// composerMaxHeight is the ceiling on the composer's scroll area.
	composerMaxHeight float32 = 140
	// fallbackComposerW stands in for the pane probe on its first frame.
	fallbackComposerW float32 = 600
	// composerProbeSalt makes the composer's pane probe this window's own:
	// folded through the id stack, which carries the host's window salt.
	composerProbeSalt uint64 = 0x636861742d636f6d

	tipKeep    = "Keep this conversation on boxer.facts, where the host's BOXER_LLM_RETAIN is durable. Fixed at the first send."
	tipContext = "Tokens the last answered call used, prompt and answer. The whole conversation is resent each turn, so this grows until the model's context is full, and then the turn fails."
	tipPast    = "This window holds one conversation for as long as it is open. A kept conversation is read afterwards in play, over SQL on the llmMessage kind."
	hintDraft  = "Message the model — Ctrl+Enter sends"
)

var (
	atomsNew    = c.Atoms().Text(icons.PhPlus + " New conversation").Keep()
	atomsSend   = c.Atoms().Text(icons.PhPaperPlaneRight + " Send").Keep()
	atomsCancel = c.Atoms().Text("Cancel").Keep()
	atomsGotIt  = c.Atoms().Text(icons.PhX).Keep()
)

func (inst *App) render() {
	for range c.PanelTopInside(inst.ids.PrepareStr("bar")).Resizable(false).KeepIter() {
		inst.renderBar()
	}
	for range c.PanelBottomInside(inst.ids.PrepareStr("composer")).Resizable(false).KeepIter() {
		inst.renderComposer()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderTranscript()
	}
}

// renderBar is the model and host, the Keep toggle, New conversation and
// the context used (ADR-0265 §SD4), then the notices.
func (inst *App) renderBar() {
	conv := inst.conv
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("new"), atomsNew).SendResp().HasPrimaryClicked() {
			inst.newConversation()
			conv = inst.conv
		}
		for range c.HoverText(tipKeep).KeepIter() {
			if conv.started {
				label, tone := keepBadge(conv)
				badge.New(inst.ids.PrepareStr("keep-state"), label).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
			} else {
				c.Checkbox(inst.ids.PrepareStr("keep"), inst.keep, "Keep this conversation").SendRespVal(&inst.keep)
			}
		}
		if used := int64(conv.lastIn) + int64(conv.lastOut); used > 0 {
			for range c.HoverText(tipContext).KeepIter() {
				c.Label("context " + strconv.FormatInt(used, 10) + " tokens").Selectable(false).Send()
			}
		}
		switch {
		case !inst.answered:
			c.Label("asking the host for a model…").Selectable(false).Send()
		case inst.model.Configured:
			c.Label("→ " + inst.model.Model + " · " + inst.model.EndpointHost).Selectable(false).Send()
		}
	}
	inst.renderApps()
	if conv.notKept != "" {
		badge.New(inst.ids.PrepareStr("not-kept"), "not kept: "+conv.notKept).
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
	}
	if !inst.pastNoted && conv.started {
		for range c.HorizontalTop().KeepIter() {
			for range c.HoverText(tipPast).KeepIter() {
				for rt := range c.RichTextLabel("Closing this window ends the conversation here; a kept one is read in play.") {
					rt.Small().Weak()
				}
			}
			if c.Button(inst.ids.PrepareStr("past-ok"), atomsGotIt).Frame(false).Small().SendResp().HasPrimaryClicked() {
				inst.pastNoted = true
			}
		}
	}
}

// renderComposer is the text input, Send, and the turn in flight; with no
// model it is the host's reason instead.
func (inst *App) renderComposer() {
	if !inst.answered {
		return
	}
	if !inst.model.Configured {
		for rt := range c.RichTextLabel("No model to talk to: " + inst.model.Reason) {
			rt.Weak()
		}
		return
	}
	w, _, ok := c.CapturePaneSize(c.ProbeSeq("chat", "composer") ^ inst.ids.PrepareHighEntropy(composerProbeSalt).Derive())
	if !ok || w <= 0 {
		w = fallbackComposerW
	}
	busy := inst.pending != nil
	// Hscroll keeps the input's width from feeding back into the window's
	// minimum width (mdedit's editor found this).
	inst.ensureHighlight()
	for range c.ScrollArea().Hscroll(true).Vscroll(true).MaxHeight(composerMaxHeight).AutoShrink(false, true).KeepIter() {
		b := c.TextEdit(inst.ids.PrepareStr("draft"), inst.draft, true).
			DesiredWidth(w).
			DesiredRows(composerRows).
			HintText(hintDraft).
			Interactive(!busy)
		if inst.hlOk {
			b = b.HighlightJob(inst.hlJob)
		}
		b.SendRespVal(&inst.draft)
	}
	// While a turn runs, Cancel takes Send's place: Button has no disabled
	// state, and a Send that does nothing would read as broken.
	send := false
	for range c.HorizontalTop().KeepIter() {
		if !busy {
			send = c.Button(inst.ids.PrepareStr("send"), atomsSend).SendResp().HasPrimaryClicked()
		} else {
			if c.Button(inst.ids.PrepareStr("cancel"), atomsCancel).SendResp().HasPrimaryClicked() {
				inst.turn.Cancel()
			}
			c.Spinner().Send()
			c.Label("waiting for the model · " + elapsed(inst.pending.started)).Selectable(false).Send()
			c.RequestRepaint()
		}
	}
	// The chord is read for the whole process; only the focused window acts
	// on it, so two open chats do not both send (play's claimRunChord).
	if pressed, _ := c.CurrentApplicationState.StateManager.GetCommandEnterPressed(); pressed && inst.focused {
		send = true
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
// fence in the bubble — and a placeholder bubble while a turn is in flight.
func (inst *App) renderTranscript() {
	conv := inst.conv
	m, kinds := transcriptModel(conv, inst.pending != nil, time.Now().UnixMilli())
	chatview.Render(chatview.Input{
		Ids: inst.ids, ScopeKey: "transcript", Model: m, State: &inst.view,
		Viewer: 0, Layout: chatview.LayoutDialogue, Location: time.Local, FillHost: true,
		Block: func(ord int) (chatview.Block, bool) {
			k := kinds[ord]
			switch {
			case k.pending:
				return chatview.Block{Render: func() {
					for range c.HorizontalTop().KeepIter() {
						c.Spinner().Send()
						c.Label("thinking…").Selectable(false).Send()
					}
				}}, true
			case k.entry >= 0:
				e := &conv.entries[k.entry]
				if e.doc == nil {
					e.doc = markdown.Parse([]byte(e.text))
				}
				doc := e.doc
				return chatview.Block{Render: func() {
					for range c.IdScope(inst.ids.PrepareSeq(uint64(0x5100 + ord))) {
						markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "doc", Doc: doc})
					}
				}}, true
			}
			return chatview.Block{}, false
		},
	})
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

// ordinalKind says what a transcript ordinal shows: an entry, a failure
// line under a failed entry, or the pending bubble.
type ordinalKind struct {
	entry   int
	pending bool
}

// transcriptModel builds the chatview model: participant 0 is the user,
// 1 the model; a failed user message is marked failed and followed by a
// system line with the reason.
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
		add(e.atMs, sender, e.text, 0, status, ordinalKind{entry: i})
		if e.failed {
			add(e.atMs, -1, "not answered: "+e.reason, chatview.FlagSystem, chatview.StatusNone, ordinalKind{entry: -1})
		}
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

// renderApps is the coordinator's row (ADR-0269): whether the model may ask
// for windows to work in, and the task it holds — with whether its
// conversation read untrusted content and holds confined content.
func (inst *App) renderApps() {
	if inst.coord == nil {
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("apps"), inst.apps, "Apps: let the model ask to work in windows").SendRespVal(&inst.apps)
		task, tainted, confined := inst.coord.state()
		if task == "" {
			continue
		}
		label := "task " + task
		if tainted {
			label += " · read untrusted content"
		}
		if confined {
			label += " · holds confined content"
		}
		c.Label(label).Selectable(false).Send()
		if c.Button(inst.ids.PrepareStr("stop-task"), c.Atoms().Text("Stop task").Keep()).SendResp().HasPrimaryClicked() {
			coord, cli := inst.coord, inst.agentCli
			h := coord.handle()
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), agent.DefaultTimeout)
				defer cancel()
				_ = cli.Stop(ctx, h)
			}()
			coord.mu.Lock()
			coord.grant = agent.Grant{}
			coord.mu.Unlock()
		}
	}
}
