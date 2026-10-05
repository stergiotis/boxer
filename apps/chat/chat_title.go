package chat

// A conversation's title (ADR-0265 §SD4): the first line of its first
// message until the model names it after the first answer — one small
// call under the first turn's id, on the retained subject when the
// conversation is kept, so its text is kept with the turns it names — or
// the person renames it. A rename is not recorded (ADR-0264 §SD7).

import (
	"context"
	"strings"
	"unicode"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// titlePurpose names the title call in the call record.
const titlePurpose = "chat/title"

// maxConvTitleRunes bounds a title as shown; titleExcerptRunes bounds what of
// the first answer the title call sends.
const (
	maxConvTitleRunes = 72
	titleExcerptRunes = 2000
)

// titlePrompt is the title call's system message.
const titlePrompt = "Write a title of three to six words for the conversation below. Answer with the title alone: no quotes, no markdown, no final punctuation."

// titleSourceE is where a conversation's title came from.
type titleSourceE uint8

const (
	titleNone titleSourceE = iota
	titleFirstLine
	titleModel
	titleManual
)

// firstLineTitle is the title a message gives before the model names it:
// its first non-empty line, markdown markers off, bounded.
func firstLineTitle(text string) (t string) {
	for _, l := range strings.Split(text, "\n") {
		if l = cleanConvTitle(l); l != "" {
			return l
		}
	}
	return ""
}

// cleanConvTitle is a title as shown: one line, without the marks a model
// or a heading leaves around it, bounded.
func cleanConvTitle(s string) (t string) {
	t = strings.Join(strings.Fields(s), " ")
	t, _ = strings.CutPrefix(t, "Title:")
	t = strings.TrimLeft(t, "#>-*` ")
	t = strings.TrimFunc(t, func(r rune) bool {
		return r == '"' || r == '\'' || r == '`' || r == '*' || r == '“' || r == '”' || r == '«' || r == '»' || unicode.IsSpace(r)
	})
	t = strings.TrimRight(t, ".:;")
	if r := []rune(t); len(r) > maxConvTitleRunes {
		t = string(r[:maxConvTitleRunes-1]) + "…"
	}
	return
}

// modelTitle is the title an answer to the title call gives: its first
// non-empty line, cleaned.
func modelTitle(content string) (t string) {
	return firstLineTitle(content)
}

// titleRequest is the title call for a conversation's first exchange. It
// declares the sensitivity the conversation holds, so a confined one is
// titled only where its content may go.
func titleRequest(conversation string, turn string, keep bool, question string, answer string, sensitivity queryengine.SensitivityE) (r llm.Request) {
	if a := []rune(answer); len(a) > titleExcerptRunes {
		answer = string(a[:titleExcerptRunes]) + "…"
	}
	return llm.Request{Purpose: titlePurpose, Conversation: conversation, Turn: turn, Retain: keep, Sensitivity: sensitivity, Messages: []openaichat.Message{
		{Role: openaichat.ChatRoleSystem, Content: titlePrompt},
		{Role: openaichat.ChatRoleUser, Content: "The person asked:\n" + question + "\n\nThe answer began:\n" + answer},
	}}
}

// titled is a title call's answer, for the conversation that asked.
type titled struct {
	conversation string
	title        string
}

// maybeTitle asks the model for a title once the conversation's first
// turn is answered, unless one was asked for already or the person named
// it.
func (inst *App) maybeTitle() {
	conv := inst.conv
	if conv.titleAsked || conv.titleSource == titleManual || inst.cli == nil {
		return
	}
	var question, answer string
	for _, e := range conv.entries {
		switch {
		case e.speaker == speakerUser && question == "":
			question = e.text
		case e.speaker == speakerModel && question != "":
			answer = e.text
		}
		if answer != "" {
			break
		}
	}
	if answer == "" {
		return
	}
	sensitivity := queryengine.SensitivityOrdinary
	if conv.apps && inst.coord != nil {
		sensitivity = inst.coord.sensitivity()
	}
	conv.titleAsked = true
	req, cli, id := titleRequest(conv.id, conv.firstTurn, conv.keep, question, answer, sensitivity), inst.cli, conv.id
	inst.titleJob.Start(nil, bgjob.Spec{Kind: "chat-title", Title: "title the conversation"},
		func(ctx context.Context) (out *titled, err error) {
			res, err := cli.Complete(ctx, req)
			if err != nil {
				return
			}
			// An empty title keeps the first line; drainTitle says so.
			return &titled{conversation: id, title: modelTitle(res.Content)}, nil
		})
}

// drainTitle lands a title on the conversation that asked for it, unless
// the person renamed it meanwhile. A failed call keeps the first line and
// says why in the title's hover.
func (inst *App) drainTitle() {
	conv := inst.conv
	if t, _, ok := inst.titleJob.TakeResult(); ok {
		if t.conversation == conv.id && conv.titleSource != titleManual {
			if t.title != "" {
				conv.title, conv.titleSource = t.title, titleModel
			} else {
				conv.titleNote = "the model gave no title"
			}
		}
		return
	}
	if snap := inst.titleJob.Snapshot(); snap.State == bgjob.StateFailed {
		inst.titleJob.Invalidate()
		conv.titleNote = "asking the model for a title failed"
		if snap.Err != nil {
			conv.titleNote += ": " + failureReason(snap.Err)
		}
	}
}

// rename sets the title the person typed; empty goes back to the first
// line.
func (conv *conversation) rename(title string) {
	if t := cleanConvTitle(title); t != "" {
		conv.title, conv.titleSource = t, titleManual
		return
	}
	for _, e := range conv.entries {
		if e.speaker == speakerUser {
			conv.title, conv.titleSource = firstLineTitle(e.text), titleFirstLine
			return
		}
	}
	conv.title, conv.titleSource = "", titleNone
}

// titleTip says where the title came from.
func titleTip(conv *conversation) (tip string) {
	switch conv.titleSource {
	case titleModel:
		tip = "Titled by the model after the first answer."
	case titleManual:
		tip = "Named by you."
	default:
		tip = "The first line of the first message."
		if conv.titleAsked && conv.titleNote == "" {
			tip += " The model is asked for a title."
		}
	}
	if conv.titleNote != "" {
		tip += " " + conv.titleNote + "."
	}
	return tip + " Held while the window is open. Click to rename."
}
