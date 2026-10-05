package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	// failed marks a user message whose turn got no answer; reason says why
	// in a line, fail what there is to inspect.
	failed bool
	reason string
	fail   failure
	// edited marks a message sent again after Edit, in place of the turn it
	// replaced.
	edited bool
	// stopped marks a user message whose turn called tools and then ended
	// without an answer: its calls happened, unlike a failed turn's.
	stopped bool
	// doc is the reply parsed as markdown, built on first draw.
	doc *markdown.Doc
	// steps are a tool entry's steps of the trail: the model call that
	// asked for it, on a round's first tool, and the tool call itself.
	steps []trailStep
}

// failure is what a failed turn leaves to inspect: the error's whole text,
// its class, how long the call took, and the call's row in the host's call
// record when it reached one (keelson('llm_calls')).
type failure struct {
	kind    string
	detail  string
	callId  string
	elapsed time.Duration
	// reasoning is the end of what the model reasoned before the call
	// failed, bounded.
	reasoning string
}

// maxFailureReasoning bounds the reasoning a failure keeps: its end, where
// the budget ran out.
const maxFailureReasoning = 1500

// failureOf is the inspectable side of err.
func failureOf(err error) (f failure) {
	var refused *llm.RefusedError
	var failed *llm.CallError
	switch {
	case err == nil:
		return
	case errors.As(err, &refused):
		f.kind = "refused"
	case errors.As(err, &failed):
		f.kind, f.elapsed = failed.Kind, failed.Elapsed
		if r := []rune(failed.Reasoning); len(r) > maxFailureReasoning {
			f.reasoning = "…" + string(r[len(r)-maxFailureReasoning:])
		} else {
			f.reasoning = failed.Reasoning
		}
	case errors.Is(err, context.Canceled):
		f.kind = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		f.kind = "timeout"
	default:
		f.kind = "error"
	}
	f.detail, f.callId = err.Error(), llm.CallIdOf(err)
	return
}

// turnMark is how the conversation stood before its last answered turn,
// so the turn can be taken back — to answer it again, or to send it
// edited. A taken-back turn stays on boxer.facts when it was kept; the
// next one names the same parent, a branch the service keeps (ADR-0264).
type turnMark struct {
	// user is the index of the turn's user entry; everything from it on
	// is the turn.
	user    int
	history []openaichat.Message
	parent  string
	omitTo  int
	lastIn  int32
	lastOut int32
	// artRev is the artefact's revision before the turn (ADR-0282 §SD1).
	artRev int
}

// rewound is a turn taken back for an edit, kept so the edit can be
// abandoned and the turn put back as it was. artDropped are the artefact
// revisions the turn made, and artAt the revision rewind left current.
type rewound struct {
	mark       turnMark
	entries    []entry
	artDropped []artRevision
	artAt      int
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
	// firstTurn is the id of the conversation's first turn, which the
	// title call names (ADR-0277 §SD5).
	firstTurn string
	// apps runs the turns as the coordinator's tool loop (ADR-0265 §SD6);
	// set at the first send, since the coordinator's system prompt is the
	// conversation's first message.
	apps bool
	// questions offers the model ask_user; set at the first send, for the
	// same reason.
	questions bool
	// artefact offers the model the artefact tools (ADR-0282), set at the
	// first send like questions; art is the document, there either way, and
	// artBefore its revision when the turn in flight began.
	artefact  bool
	art       *artefact
	artBefore int
	started   bool
	// kept says a turn's verdict was kept; notKept is the first reason one
	// was not. Neither set means no verdict yet.
	kept    bool
	entries []entry
	// history is what the next request resends: every answered turn, each
	// reply exactly as it came back, so the host keeps only what is new.
	history []openaichat.Message
	// parent is the call id of the last reply, the next turn's parent.
	parent string
	// omitTo is one past the message parent carried that history leaves
	// out — the last-round note; 0 when history resends parent whole.
	omitTo int
	// notKept is the first reason a turn was not kept, shown until
	// notKeptNoted.
	notKept      string
	notKeptNoted bool
	// lastIn and lastOut are the last answered call's tokens.
	lastIn  int32
	lastOut int32
	// mark is the last answered turn's, nil when there is none or a turn
	// failed after it.
	mark *turnMark
	// title is what the conversation is called, titleSource where that
	// came from (chat_title.go); titleAsked says the model was asked once,
	// titleNote why its answer did not land.
	title       string
	titleSource titleSourceE
	titleAsked  bool
	titleNote   string
	// startedAt is the first send, for the transcript's header.
	startedAt int64
}

// minted salts the conversation and turn ids minted in this process.
var minted atomic.Uint64

// newConversation starts an empty conversation with a fresh id.
func newConversation() (inst *conversation) {
	id := "chat-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "-" + strconv.FormatUint(minted.Add(1), 36)
	inst = &conversation{id: id, art: newArtefact()}
	return
}

// newTurn mints the id of a turn: one message the person sends and all that
// answering it takes. Every model call and tool call of the turn carries it
// (ADR-0277 §SD5).
func newTurn() (id string) {
	return "turn-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "-" + strconv.FormatUint(minted.Add(1), 36)
}

// request is the call for a new user message: the history, then the
// message, under this conversation's id, a fresh turn and the last reply as
// parent — which the host records whether or not the text is kept; on the
// retained subject when keep is on.
func (inst *conversation) request(text string) (r llm.Request) {
	msgs := make([]openaichat.Message, 0, len(inst.history)+1)
	msgs = append(msgs, inst.history...)
	msgs = append(msgs, openaichat.Message{Role: openaichat.ChatRoleUser, Content: text})
	r = llm.Request{Purpose: purpose, Messages: msgs, Retain: inst.keep,
		Conversation: inst.id, Turn: newTurn(), ParentCallId: inst.parent}
	if inst.omitTo > 0 {
		r.OmitFrom, r.OmitTo = inst.omitTo-1, inst.omitTo
	}
	if inst.firstTurn == "" {
		inst.firstTurn = r.Turn
	}
	return
}

// begin shows the user's message as sent; edited marks it as an edit of
// a turn taken back.
func (inst *conversation) begin(text string, atMs int64, edited bool) {
	if !inst.started {
		inst.startedAt = atMs
	}
	if inst.titleSource == titleNone {
		inst.title, inst.titleSource = firstLineTitle(text), titleFirstLine
	}
	inst.started = true
	inst.artBefore, _ = inst.art.head()
	inst.entries = append(inst.entries, entry{speaker: speakerUser, text: text, atMs: atMs, edited: edited})
}

// lastUser is the index of the last user entry, -1 for none.
func (inst *conversation) lastUser() (i int) {
	for i = len(inst.entries) - 1; i >= 0; i-- {
		if inst.entries[i].speaker == speakerUser {
			return
		}
	}
	return -1
}

// canRewind says the last turn was answered and can be taken back.
func (inst *conversation) canRewind() bool {
	return inst.mark != nil && inst.mark.user == inst.lastUser()
}

// rewind takes the last answered turn back: the transcript, the history
// and the parent stand as they did before it. It returns the turn's text
// and what an abandoned edit puts back.
func (inst *conversation) rewind() (text string, undo *rewound, ok bool) {
	if !inst.canRewind() {
		return "", nil, false
	}
	m := *inst.mark
	text = inst.entries[m.user].text
	undo = &rewound{mark: turnMark{user: m.user, history: inst.history, parent: inst.parent, omitTo: inst.omitTo, lastIn: inst.lastIn, lastOut: inst.lastOut},
		entries: append([]entry(nil), inst.entries[m.user:]...)}
	// The turn's artefact revisions go with it.
	undo.artDropped, undo.artAt = inst.art.truncate(m.artRev), m.artRev
	inst.entries = inst.entries[:m.user:m.user]
	inst.history, inst.parent, inst.omitTo, inst.lastIn, inst.lastOut = m.history, m.parent, m.omitTo, m.lastIn, m.lastOut
	inst.mark = nil
	return text, undo, true
}

// restore puts back a turn rewind took, when nothing was sent since.
func (inst *conversation) restore(undo *rewound) (ok bool) {
	if undo == nil || len(inst.entries) != undo.mark.user {
		return false
	}
	if len(undo.artDropped) > 0 && !inst.art.reinstate(undo.artAt, undo.artDropped) {
		// The artefact moved since — a revert in the panel: the turn's
		// revisions cannot go back on top of it.
		return false
	}
	before := turnMark{user: undo.mark.user, history: inst.history, parent: inst.parent, omitTo: inst.omitTo, lastIn: inst.lastIn, lastOut: inst.lastOut, artRev: undo.artAt}
	inst.entries = append(inst.entries, undo.entries...)
	inst.history, inst.parent, inst.omitTo, inst.lastIn, inst.lastOut = undo.mark.history, undo.mark.parent, undo.mark.omitTo, undo.mark.lastIn, undo.mark.lastOut
	inst.mark = &before
	return true
}

// dropFailed takes back the last turn when it failed: a failed turn never
// reached the history, so only the transcript changes. It returns the
// turn's text, to send again or edit.
func (inst *conversation) dropFailed() (text string, ok bool) {
	i := inst.lastUser()
	if i < 0 || !inst.entries[i].failed {
		return "", false
	}
	text = inst.entries[i].text
	inst.entries = inst.entries[:i:i]
	return text, true
}

// land applies a finished turn: req is what was sent, res the answer or
// err the failure. The user's entry is the last one begin added.
func (inst *conversation) land(req llm.Request, res *llm.Response, err error, atMs int64) {
	asked := inst.lastUser()
	if err != nil || res == nil {
		inst.fail(asked, failureReason(err), failureOf(err))
		return
	}
	inst.mark = &turnMark{user: asked, history: inst.history, parent: inst.parent, omitTo: inst.omitTo, lastIn: inst.lastIn, lastOut: inst.lastOut, artRev: inst.artBefore}
	reply := openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: res.Content, ToolCalls: res.ToolCalls}
	inst.history = append(append(inst.history[:0:0], req.Messages...), reply)
	inst.parent, inst.omitTo = res.CallId, 0
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
	asked := inst.lastUser()
	steps := stepsOfTools(res.steps)
	for i, a := range res.activity {
		e := entry{speaker: speakerTool, text: a, atMs: atMs}
		if i < len(steps) {
			e.steps = steps[i]
		}
		inst.entries = append(inst.entries, e)
	}
	if res.stopped != "" {
		// Not answered, so not resent (§SD3); the calls it made stay shown.
		f := failureOf(res.stoppedErr)
		if res.stoppedErr == nil {
			f = failure{kind: "stopped", detail: res.stopped}
		}
		inst.fail(asked, res.stopped, f)
		if asked >= 0 {
			inst.entries[asked].stopped = true
		}
		return
	}
	inst.land(req, &res.final, nil, atMs)
	inst.history, inst.omitTo = append(inst.history[:0:0], res.messages...), res.omitTo
}

// fail marks the user entry i as not answered. A failed turn after an
// answered one leaves nothing to take back but itself.
func (inst *conversation) fail(i int, reason string, f failure) {
	inst.mark = nil
	if i < 0 {
		return
	}
	e := &inst.entries[i]
	e.failed, e.reason, e.fail = true, reason, f
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
	case errors.Is(err, openaichat.ErrIncompleteCompletion):
		// The service says how it ended and what it spent (ADR-0254,
		// update of 2026-10-03).
		var failed *llm.CallError
		if errors.As(err, &failed) && failed.Reason != "" {
			return failed.Reason
		}
		return "the provider ended the answer early, with no answer text"
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
