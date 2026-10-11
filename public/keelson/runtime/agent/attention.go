package agent

import (
	"iter"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The dispatcher as a source of the person's queue (ADR-0324, proposed): its
// grant requests, widenings, confirmations and moderators' questions are
// items there, decided in host cards rather than modals. The dispatcher keeps
// their state; the queue reads it each frame.

// Item keys: the source prefix, the item's kind, its own key.
const (
	cardPrefix        = "agent/"
	cardRequest       = cardPrefix + "req/"
	cardConfirmation  = cardPrefix + "confirm/"
	cardModeratorAsks = cardPrefix + "moderator/"
)

var _ attention.SourceI = (*Service)(nil)

// WaitingItems lists what the person owes the dispatcher, expiring stale
// requests.
func (inst *Service) WaitingItems() iter.Seq[attention.Item] {
	return func(yield func(attention.Item) bool) {
		items := make([]attention.Item, 0, 4)
		inst.mu.Lock()
		timeout := inst.requestTimeout()
		for _, r := range inst.pending() {
			kind := attention.KindGrantRequest
			conv := r.conversation
			bound, why := r.created.Add(timeout), "expires"
			if r.task != nil || r.held != nil {
				kind = attention.KindWidening
			}
			if r.task != nil {
				if conv == "" {
					conv = r.task.conversation
				}
				if !r.task.deadline.IsZero() && r.task.deadline.Before(bound) && (r.held == nil || r.held.need != needDeadline) {
					bound, why = r.task.deadline, "task deadline"
				}
			}
			items = append(items, attention.Item{Key: cardRequest + r.key, Kind: kind, App: r.actor, Instance: r.actorInstance,
				Conversation: conv, Summary: inst.requestSummary(r), Created: r.created, Bound: bound, BoundWhy: why})
		}
		for _, p := range inst.proposals(0, true) {
			it := attention.Item{Key: cardConfirmation + p.t.id + "/" + p.rec.key, Kind: attention.KindConfirmation,
				Effect: app.OperationEffectConsequential, App: p.t.actor, Instance: p.t.actorInstance, Conversation: p.t.conversation,
				Summary: "confirm " + p.rec.spec.Summary + " in window " + strconv.FormatUint(p.rec.instance, 10), Created: p.rec.created}
			if !p.t.deadline.IsZero() {
				it.Bound, it.BoundWhy = p.t.deadline, "task deadline"
			}
			items = append(items, it)
		}
		inst.mu.Unlock()
		m := &inst.moderation
		m.mu.Lock()
		for _, q := range m.questions {
			items = append(items, attention.Item{Key: cardModeratorAsks + q.key, Kind: attention.KindModeratorQuestion,
				App: q.moderator, Summary: inst.display(q.moderator) + " asks: " + clip(q.text, 80), Created: q.created})
		}
		m.mu.Unlock()
		for _, it := range items {
			if !yield(it) {
				return
			}
		}
	}
}

// requestSummary is the queue's one line for r: host-written, never the
// model's words. The caller holds mu.
func (inst *Service) requestSummary(r *request) (s string) {
	who := inst.display(r.actor)
	switch {
	case r.held != nil:
		return who + " asks to call " + r.held.req.Operation + " in window " + strconv.FormatUint(r.held.req.Instance, 10)
	case r.task != nil:
		return who + " asks to widen task " + r.task.id
	default:
		return who + " asks to start a task"
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// admit counts one item the person is asked for against the flood limit
// (ADR-0324 §SD6); reason is the refusal the asking model reads. The
// stream is the conversation, else the asking window.
func (inst *Service) admit(actor app.AppIdT, instance uint64, conversation string) (ok bool, reason string) {
	if inst.cfg.Attention == nil {
		return true, ""
	}
	stream := conversation
	if stream == "" {
		stream = string(actor) + "#" + strconv.FormatUint(instance, 10)
	}
	return inst.cfg.Attention.Flood.Admit(stream, time.Now())
}

// admitConfirmation counts a consequential command's confirmation against
// the flood limit; on a refusal it takes the proposal back and returns the
// reason. The caller holds mu.
func (inst *Service) admitConfirmation(t *task, rec *callRec) (refused string) {
	if rec.proposal == nil || !rec.proposal.confirm {
		return ""
	}
	if ok, why := inst.admit(t.actor, t.actorInstance, t.conversation); !ok {
		rec.proposal = nil
		return why
	}
	return ""
}

// RenderCard draws the body of the card for item key, a key this source
// reported; decided says the person decided it and the card may close. The
// caller is the render goroutine.
func (inst *Chrome) RenderCard(key string, ids *c.WidgetIdStack) (decided bool) {
	svc := inst.svc
	switch {
	case strings.HasPrefix(key, cardRequest):
		return inst.renderRequestCard(strings.TrimPrefix(key, cardRequest), ids)
	case strings.HasPrefix(key, cardConfirmation):
		rest := strings.TrimPrefix(key, cardConfirmation)
		taskId, recKey, _ := strings.Cut(rest, "/")
		svc.mu.Lock()
		var found *proposalRef
		for _, p := range svc.proposals(0, true) {
			if p.t.id == taskId && p.rec.key == recKey {
				found = &p
				break
			}
		}
		svc.mu.Unlock()
		if found == nil {
			c.Label("This confirmation is no longer waiting.").Send()
			return true
		}
		confirm, decline := inst.renderConfirmationBody(*found, 1, ids)
		switch {
		case confirm:
			svc.accept(*found)
		case decline:
			svc.rejectProposal(*found)
		}
		return confirm || decline
	case strings.HasPrefix(key, cardModeratorAsks):
		qKey := strings.TrimPrefix(key, cardModeratorAsks)
		m := &svc.moderation
		m.mu.Lock()
		var q *question
		for _, x := range m.questions {
			if x.key == qKey {
				q = x
			}
		}
		m.mu.Unlock()
		if q == nil {
			c.Label("This question is no longer waiting.").Send()
			return true
		}
		allow, decline := inst.renderQuestionBody(q, 1, ids)
		switch {
		case allow:
			svc.answer(q, true)
		case decline:
			svc.answer(q, false)
		}
		return allow || decline
	}
	return true
}

// renderRequestCard draws a request as a card. The windows it offers are
// fixed when the card first draws: a request is decided on what it showed
// (ADR-0324 §SD2), so a listed window that closes disables Approve.
func (inst *Chrome) renderRequestCard(reqKey string, ids *c.WidgetIdStack) (decided bool) {
	svc := inst.svc
	svc.mu.Lock()
	r := svc.requests[reqKey]
	if r != nil {
		svc.expireRequest(r)
	}
	if r == nil || r.state != reqStatePending {
		svc.mu.Unlock()
		c.Label("This request is no longer waiting.").Send()
		return true
	}
	if r.cardWindows == nil {
		r.cardWindows = make([]windowRow, 0, 8)
		if svc.cfg.Host != nil {
			for _, info := range svc.cfg.Host.OpsInstances() {
				if info.Key != r.actorInstance {
					r.cardWindows = append(r.cardWindows, windowRow{key: info.Key, title: info.Title})
				}
			}
		}
	}
	windows := r.cardWindows
	svc.mu.Unlock()
	stale := ""
	if svc.cfg.Host != nil {
		live := make(map[uint64]bool, len(windows))
		for _, info := range svc.cfg.Host.OpsInstances() {
			live[info.Key] = true
		}
		for _, w := range windows {
			if !live[w.key] {
				stale = "A window this request lists has closed since the card opened. Close the card and open it again to decide on what is open now."
				break
			}
		}
	}
	if stale != "" {
		c.Label(stale).Wrap().Send()
	}
	approve, decline := inst.renderRequest(r, windows, 1, ids)
	if stale != "" {
		approve = false
	}
	if !approve && !decline {
		return false
	}
	svc.mu.Lock()
	var route *held
	if approve {
		route = svc.approve(r)
	} else {
		svc.reject(r)
	}
	svc.mu.Unlock()
	if route != nil {
		svc.routeHeld(route)
	}
	return true
}

// ForgetCard drops what a card fixed when it opened, so it shows the
// current state when opened again.
func (inst *Chrome) ForgetCard(key string) {
	if !strings.HasPrefix(key, cardRequest) {
		return
	}
	svc := inst.svc
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if r := svc.requests[strings.TrimPrefix(key, cardRequest)]; r != nil {
		r.cardWindows = nil
	}
}

// OwnsCard reports whether key is one of this source's items.
func (inst *Chrome) OwnsCard(key string) bool { return strings.HasPrefix(key, cardPrefix) }
