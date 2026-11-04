package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// Asynchronous operations (ADR-0358). A call to an Async command still
// running when the wait ends gets an outcome in form pending, and a watcher
// follows it off the turn. When it settles the watcher writes a completed
// entry at once, under the content's label, so what the run has read is
// known on arrival; the model reads it through a note written at a round
// boundary, where nothing can separate a reply from its tool results. The
// note has no turn, so a failed turn's drop leaves it standing. When a note
// waits and no turn runs, the window starts one (chat.go), within the limits
// wakeDue states. The entries are the record: a resume finds a call with no
// completed entry and settles it as unknown, and a completed entry with no
// note and tells the model.

const (
	// completionNoteName names the note that tells the model of a completion.
	completionNoteName = "completion"
	// completedFormUnknown is a completed entry for a call whose end nobody
	// saw: the conversation ended, or the wait for it ran out.
	completedFormUnknown = "unknown"
	// watchPoll is one poll of a pending call. The dispatcher's wait does not
	// see a cancel, so a poll holds a retiring conversation's drain up for as
	// long as it lasts.
	watchPoll = 250 * time.Millisecond
	// runningReason is what the model reads of a pending call.
	runningReason = "still running when the wait ended. Its result reaches you in a note when it settles, and a new turn starts for it if none is running; do other work or answer in the meantime, and do not poll for it"
)

// callLimits are the run's limits on calls: those of the profile it is
// pinned to, or what the variables say before it has one. The profile is read
// from the coordinator and not from the registry, whose validation reaches
// the tools that use these limits.
func (inst *coordinator) callLimits() (callWait time.Duration, pendingMax time.Duration, wakesMax int) {
	inst.mu.Lock()
	p := inst.profile
	inst.mu.Unlock()
	if p.Id == "" {
		return CallWaitEnv.Get(), PendingMaxEnv.Get(), int(WakesMaxEnv.Get())
	}
	return p.CallWait, p.PendingMax, p.WakesMax
}

// pendingCall is a call a watcher follows.
type pendingCall struct {
	handle string
	key    string
	turn   string
	op     string
	// where says which window and operation, as the person reads it.
	where string
}

// completion is a settled call the model has not been told of.
type completion struct {
	key   string
	body  string
	label agentrun.Label
}

// notePending remembers the running call for the round to pick up once its
// outcome is written; the watcher starts after that outcome, so a completed
// entry never precedes it.
func (inst *coordinator) notePending(p pendingCall) {
	inst.mu.Lock()
	inst.lastPending = &p
	inst.mu.Unlock()
}

// takePending is the running call of the call just made, nil for none.
func (inst *coordinator) takePending() (p *pendingCall) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p, inst.lastPending = inst.lastPending, nil
	return
}

// watch follows a pending call until it settles, the wait for it runs out or
// the conversation ends. It counts as work in the scope, so the run closes
// only after its entry is written; the work context ends at Retire, so it
// does not hold a drain up.
func (inst *coordinator) watch(w *agentrun.Writer, p pendingCall) {
	leave, entered := inst.enterTurn()
	if !entered {
		// The conversation ended first: a resume settles it from the record.
		return
	}
	inst.mu.Lock()
	inst.pendingCalls[p.key] = p
	inst.mu.Unlock()
	go func() {
		defer leave()
		ctx := inst.scope.Ctx()
		jctx := inst.scope.Tail(ctx)
		content, phase, label, settled := inst.follow(ctx, p)
		inst.mu.Lock()
		delete(inst.pendingCalls, p.key)
		inst.mu.Unlock()
		if !settled {
			phase, label = completedFormUnknown, harnessLabel()
		}
		// A note's label raises the run when it is written; this one raises
		// it now, whether or not the model is in a turn (ADR-0358 §SD4).
		if _, err := inst.append(jctx, w, agentrun.Entry{Kind: agentrun.KindCompleted, Key: p.key, Name: p.op, Form: phase, Body: content, Turn: p.turn, Label: label}); err != nil {
			log.Warn().Err(err).Str("key", p.key).Msg("chat: a completion was not recorded")
			return
		}
		if ctx.Err() != nil {
			// The conversation ended; a resume tells the model.
			return
		}
		inst.mu.Lock()
		inst.inbox = append(inst.inbox, completion{key: p.key, body: completionBody(p.where, content), label: label})
		inst.mu.Unlock()
	}()
}

// follow polls the call until it is final. settled is false when its end was
// not seen, and content then says so.
func (inst *coordinator) follow(ctx context.Context, p pendingCall) (content string, phase string, label agentrun.Label, settled bool) {
	_, pendingMax, _ := inst.callLimits()
	deadline := time.Now().Add(pendingMax)
	for ctx.Err() == nil && time.Now().Before(deadline) {
		out, err := inst.cli.Status(ctx, p.handle, p.key, watchPoll)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return "error: its status could not be read: " + err.Error(), "", agentrun.Label{}, false
		}
		if !out.Final() {
			continue
		}
		co := callOutcome{Phase: out.Phase, Reason: out.Reason, Revisions: out.Revisions}
		untrustedSource, confined := inst.readResult(ctx, p.handle, p.key, out, &co)
		b, err := json.Marshal(co)
		if err != nil {
			return "error: " + err.Error(), "", agentrun.Label{}, false
		}
		content = string(b)
		if untrustedSource != "" {
			content = wrapUntrusted(untrustedSource, content)
		}
		return content, out.Phase, outcomeLabel(untrustedSource != "", confined, inst.confinedTier), true
	}
	if ctx.Err() != nil {
		return "error: the conversation ended while the work ran; read its state before relying on it", "", agentrun.Label{}, false
	}
	return "error: no end was seen after " + pendingMax.String() + "; read its state before relying on it", "", agentrun.Label{}, false
}

// completionBody is the note's text for a settled call.
func completionBody(where string, content string) string {
	return "Work you started earlier has settled — " + where + ":\n" + content
}

// deliverCompletions writes a note for each completion the model has not
// been told of. It runs at a round's start, where every tool result of the
// round before is written. An item stays queued until its note is.
func (inst *coordinator) deliverCompletions(ctx context.Context, w *agentrun.Writer) (err error) {
	inst.mu.Lock()
	queued := inst.inbox
	inst.inbox = nil
	inst.mu.Unlock()
	for i, c := range queued {
		if _, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindNote, Key: c.key, Name: completionNoteName, Form: contextFormNotice.String(), Body: c.body, Label: c.label}); err != nil {
			inst.mu.Lock()
			inst.inbox = append(queued[i:], inst.inbox...)
			inst.mu.Unlock()
			return
		}
	}
	return
}

// configureCompletions is the configuration chain's first element: what
// settled reaches the model before this round's projection (ADR-0358 §SD4).
func configureCompletions(s *requestState) {
	s.err = s.coord.deliverCompletions(s.jctx, s.w)
}

// settlePending writes what a resume owes for calls the record leaves
// pending, and for completions never told: a completed entry in form unknown
// for each call with none, and a note for each completed entry with none.
// The process that followed them is gone, and so are the windows. It returns
// a line per call for the person.
func settlePending(ctx context.Context, w *agentrun.Writer) (lines []string, err error) {
	entries := w.Entries()
	completed := map[string]bool{}
	told := map[string]bool{}
	for _, e := range entries {
		switch {
		case e.Kind == agentrun.KindCompleted:
			completed[e.Key] = true
		case e.Kind == agentrun.KindNote && e.Name == completionNoteName:
			told[e.Key] = true
		}
	}
	var writes []agentrun.Entry
	for _, e := range entries {
		switch {
		case e.Kind == agentrun.KindOutcome && e.Form == agentrun.FormPending && !completed[e.Key]:
			body := "error: the host stopped while the work ran, and its end was not seen; read its state before relying on it"
			writes = append(writes,
				agentrun.Entry{Kind: agentrun.KindCompleted, Key: e.Key, Form: completedFormUnknown, Body: body, Turn: e.Turn, Label: harnessLabel()},
				agentrun.Entry{Kind: agentrun.KindNote, Key: e.Key, Name: completionNoteName, Form: contextFormNotice.String(), Body: completionBody(e.Key, body), Label: harnessLabel()})
			lines = append(lines, "work started in call "+e.Key+" was still running when the host stopped; its end is unknown")
		case e.Kind == agentrun.KindCompleted && !told[e.Key]:
			writes = append(writes, agentrun.Entry{Kind: agentrun.KindNote, Key: e.Key, Name: completionNoteName, Form: contextFormNotice.String(), Body: completionBody(e.Key, e.Body), Label: e.Label})
		}
	}
	if len(writes) > 0 {
		_, err = w.Append(ctx, writes...)
	}
	return
}

// wakeDue says whether a completion should start a turn with nobody asking
// (ADR-0358 §SD5). It should not after the person's Stop, when the task has
// ended and the model holds no grant, or past the wakes allowed since the
// person last spoke; the completion then waits for their next message.
func (inst *coordinator) wakeDue() (due bool) {
	_, _, limit := inst.callLimits()
	h := inst.handle()
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.inbox) > 0 && h != "" && !inst.wakesHeld && inst.wakes < limit
}

// noteWake counts a wake, and says what the transcript shows for it.
func (inst *coordinator) noteWake() (line string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.wakes++
	return "work finished in the background, so the model was asked to read it (" + plural(len(inst.inbox), "result") + ", wake " + strconv.Itoa(inst.wakes) + " since you last spoke)"
}

// personSpoke opens the wakes again: the person sent a message.
func (inst *coordinator) personSpoke() {
	inst.mu.Lock()
	inst.wakes, inst.wakesHeld = 0, false
	inst.mu.Unlock()
}

// holdWakes withholds wakes until the person next speaks: they pressed Stop.
// The work goes on; the app's own cancel stops that (ADR-0358 §SD6).
func (inst *coordinator) holdWakes() {
	inst.mu.Lock()
	inst.wakesHeld = true
	inst.mu.Unlock()
}

// pendingNow is how many calls are running, and how many settled calls wait
// for the model to be told.
func (inst *coordinator) pendingNow() (running int, waiting int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.pendingCalls), len(inst.inbox)
}
