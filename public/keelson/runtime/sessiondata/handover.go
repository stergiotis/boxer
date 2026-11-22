package sessiondata

import (
	"context"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// HandOverPrefix starts every hand-over token.
const HandOverPrefix = "sdh_"

// HandOverTimeout is how long a hand-over token can be claimed after it was
// minted.
const HandOverTimeout = 2 * time.Minute

// AuditHandOver is a hand-over token minted for a person's data session;
// its claim is an AuditAttachSession row.
const AuditHandOver = "hand-over"

// handOver is one token not yet claimed.
type handOver struct {
	s       *session
	expires time.Time
}

// IsHandOver reports whether s has the shape of a hand-over token. A launch
// configuration carries one where it hands a person's data session to the
// window it opens.
func IsHandOver(s string) (yes bool) {
	return strings.HasPrefix(s, HandOverPrefix) && len(s) == len(HandOverPrefix)+32
}

// HandOver mints a token that lets one other window attach to the person's
// data session ticket names, once and within HandOverTimeout; by must hold
// the session. The token goes where the ticket must not — a launch
// configuration, which the host may store — because it is spent once it is
// claimed and worthless once it lapses. A task's data session is not handed
// over: the task's windows reach it through the task.
func (inst *Service) HandOver(ctx context.Context, by Identity, ticket string) (token string, err error) {
	s := inst.lookupTicket(ticket)
	switch {
	case s == nil:
		return "", eh.Errorf("no data session by that ticket")
	case s.kind == sessionKindTask:
		return "", eh.Errorf("a task's data session is not handed to another window; the task's windows reach it through the task")
	case !s.holds(by):
		return "", eh.Errorf("the window does not hold the data session")
	}
	token, err = newToken(HandOverPrefix)
	if err != nil {
		return
	}
	now := time.Now()
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return "", eh.Errorf("the session data service has stopped")
	}
	if inst.handOvers == nil {
		inst.handOvers = make(map[string]handOver)
	}
	for k, h := range inst.handOvers {
		if !now.Before(h.expires) || h.s.isClosed() {
			delete(inst.handOvers, k)
		}
	}
	inst.handOvers[token] = handOver{s: s, expires: now.Add(HandOverTimeout)}
	inst.mu.Unlock()
	inst.audit(ctx, AuditRecord{Action: AuditHandOver, Outcome: trail.OutcomeOk, Session: s, By: by})
	return
}

// Claim spends a hand-over token: by's window becomes a holder of the data
// session the token was minted for, and the session's ticket is returned
// for the window's later requests. A token is claimed once.
func (inst *Service) Claim(ctx context.Context, by Identity, token string) (ticket string, err error) {
	inst.mu.Lock()
	h, ok := inst.handOvers[token]
	delete(inst.handOvers, token)
	inst.mu.Unlock()
	switch {
	case !ok:
		return "", eh.Errorf("no hand-over by that token; it was claimed already or never minted")
	case !time.Now().Before(h.expires):
		return "", eh.Errorf("the hand-over lapsed before it was claimed")
	}
	s := h.s
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", eh.Errorf("the data session has ended")
	}
	s.holders[by] = struct{}{}
	s.mu.Unlock()
	inst.audit(ctx, AuditRecord{Action: AuditAttachSession, Outcome: trail.OutcomeOk, Session: s, By: by, Reason: "by hand-over"})
	return s.ticket, nil
}

// isClosed reports whether the session has ended.
func (inst *session) isClosed() (yes bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.closed
}
