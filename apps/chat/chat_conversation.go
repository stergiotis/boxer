package chat

import (
	"context"
	"errors"
	"strings"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// purpose names the app's calls in the call record (ADR-0254 §SD4).
const purpose = "chat/turn"

// speakerE is who said an entry.
type speakerE uint8

const (
	speakerUser speakerE = iota
	speakerModel
	// speakerTool is a line of the coordinator's activity: a tool call and
	// how it ended.
	speakerTool
)

// entry is one message of the transcript as the app shows it.
type entry struct {
	speaker speakerE
	text    string
	atMs    int64
	// failed marks a user message whose turn got no answer; reason says why.
	failed bool
	reason string
	// doc is the reply parsed as markdown, built on first draw.
	doc *markdown.Doc
}

// conversation is the app's state for one conversation (ADR-0265 §SD2):
// what the transcript shows, and what the model has seen. Only answered
// turns reach history, so a failed turn is shown but never resent, and the
// next turn continues from the last reply (§SD3). No method here touches
// the UI.
type conversation struct {
	id string
	// keep sends the turns on llm.retain.complete; set at the first send.
	keep bool
	// apps runs the turns as the coordinator's tool loop (ADR-0265 §SD6);
	// set at the first send, since the coordinator's system prompt is the
	// conversation's first message.
	apps    bool
	started bool
	// kept says a turn's verdict was kept; notKept is the first reason one
	// was not. Neither set means no verdict yet.
	kept    bool
	entries []entry
	// history is what the next request resends: every answered turn, each
	// reply exactly as it came back, so the host keeps only what is new.
	history []openaichat.Message
	// parent is the call id of the last reply, the next turn's parent.
	parent string
	// notKept is the first reason a turn was not kept, shown once.
	notKept string
	// lastIn and lastOut are the last answered call's tokens.
	lastIn  int32
	lastOut int32
}

// minted salts conversation ids minted in this process.
var minted atomic.Uint64

// newConversation starts an empty conversation with a fresh id.
func newConversation() (inst *conversation) {
	id := "chat-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "-" + strconv.FormatUint(minted.Add(1), 36)
	inst = &conversation{id: id}
	return
}

// request is the call for a new user message: the history, then the
// message; on the retained subject with this conversation's id and the
// last reply as parent when keep is on.
func (inst *conversation) request(text string) (r llm.Request) {
	msgs := make([]openaichat.Message, 0, len(inst.history)+1)
	msgs = append(msgs, inst.history...)
	msgs = append(msgs, openaichat.Message{Role: openaichat.ChatRoleUser, Content: text})
	r = llm.Request{Purpose: purpose, Messages: msgs}
	if inst.keep {
		r.Retain, r.Conversation, r.ParentCallId = true, inst.id, inst.parent
	}
	return
}

// begin shows the user's message as sent.
func (inst *conversation) begin(text string, atMs int64) {
	inst.started = true
	inst.entries = append(inst.entries, entry{speaker: speakerUser, text: text, atMs: atMs})
}

// land applies a finished turn: req is what was sent, res the answer or
// err the failure. The user's entry is the last one begin added.
func (inst *conversation) land(req llm.Request, res *llm.Response, err error, atMs int64) {
	last := len(inst.entries) - 1
	if err != nil || res == nil {
		if last >= 0 {
			inst.entries[last].failed = true
			inst.entries[last].reason = failureReason(err)
		}
		return
	}
	reply := openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: res.Content, ToolCalls: res.ToolCalls}
	inst.history = append(append(inst.history[:0:0], req.Messages...), reply)
	inst.parent = res.CallId
	inst.lastIn, inst.lastOut = res.InputTokens, res.OutputTokens
	inst.entries = append(inst.entries, entry{speaker: speakerModel, text: res.Content, atMs: atMs})
	switch res.Retention {
	case llm.RetentionKept:
		inst.kept = true
	case llm.RetentionNotKept:
		if inst.notKept == "" {
			inst.notKept = res.RetentionReason
			if inst.notKept == "" {
				inst.notKept = "the host did not keep this conversation"
			}
		}
	}
}

// landTurn applies a turn that ran the tool loop (ADR-0269 M5): the
// activity lines first, as system entries, then the answer; the history
// becomes every message the model saw.
func (inst *conversation) landTurn(req llm.Request, res *turnResult, err error, atMs int64) {
	if err != nil || res == nil {
		inst.land(req, nil, err, atMs)
		return
	}
	asked := len(inst.entries) - 1
	for _, a := range res.activity {
		inst.entries = append(inst.entries, entry{speaker: speakerTool, text: a, atMs: atMs})
	}
	if res.stopped != "" {
		// Not answered, so not resent (§SD3); the calls it made stay shown.
		if asked >= 0 {
			inst.entries[asked].failed, inst.entries[asked].reason = true, res.stopped
		}
		return
	}
	inst.land(req, &res.final, nil, atMs)
	inst.history = append(inst.history[:0:0], res.messages...)
}

// failureReason is the line a failed bubble shows.
func failureReason(err error) (s string) {
	var refused *llm.RefusedError
	switch {
	case err == nil:
		return "no answer"
	case errors.Is(err, context.Canceled):
		// The client told the host to stop the call (llm.cancel, ADR-0254).
		return "cancelled"
	case errors.As(err, &refused):
		return refused.Reason
	case errors.Is(err, context.DeadlineExceeded):
		return "no answer before the timeout"
	case errors.Is(err, openaichat.ErrPaymentRequired):
		// The provider's own message says how much is left and where to
		// top up.
		return "the model provider is out of credit or quota for this account (" + providerAnswer(err) + ")"
	case strings.Contains(err.Error(), "HTTP "):
		return "the model provider answered " + providerAnswer(err)
	default:
		return err.Error()
	}
}

// providerAnswer is the provider's part of a failure — its status and
// message — without the layers it passed through.
func providerAnswer(err error) (s string) {
	s = err.Error()
	if i := strings.Index(s, "HTTP "); i >= 0 {
		s = s[i:]
	}
	if i := strings.LastIndex(s, ": openaichat: "); i >= 0 {
		s = s[:i]
	}
	return
}
