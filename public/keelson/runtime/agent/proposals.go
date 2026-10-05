package agent

import (
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// Proposals (ADR-0269 §SD5): in suggest mode a command waits for the person
// to accept or reject it, and a consequential command waits for the person's
// confirmation in every mode that allows it. A proposal becomes stale when a
// revision it expects moves before it is accepted.

// proposalRef names one pending proposal for the chrome.
type proposalRef struct {
	t   *task
	rec *callRec
}

// proposals lists the pending proposals of window key, or of every window
// when key is zero, oldest first. The caller holds mu.
func (inst *Service) proposals(key uint64, confirm bool) (out []proposalRef) {
	for _, t := range inst.tasks {
		for _, rec := range t.keys {
			if rec.proposal == nil || rec.outcome.Phase != opwire.PhaseProposed || rec.proposal.confirm != confirm ||
				rec.proposal.taken {
				continue
			}
			if key != 0 && rec.instance != key {
				continue
			}
			out = append(out, proposalRef{t: t, rec: rec})
		}
	}
	slices.SortFunc(out, func(a, b proposalRef) int { return a.rec.created.Compare(b.rec.created) })
	return
}

// stale reports whether a revision the proposal expects has moved. The
// caller holds mu.
func (inst *Service) stale(rec *callRec) (moved bool) {
	if rec.proposal.confirm || inst.cfg.Host == nil || len(rec.proposal.expects) == 0 {
		return
	}
	revs, ok := inst.cfg.Host.OpsRevisions(rec.instance)
	if !ok {
		return
	}
	for r, v := range rec.proposal.expects {
		if revs[r] != v {
			return true
		}
	}
	return
}

// accept routes a proposal the person accepted; a stale one ends as stale.
func (inst *Service) accept(p proposalRef) {
	inst.mu.Lock()
	rec := p.rec
	if rec.outcome.Phase != opwire.PhaseProposed || rec.proposal.taken || p.t.revoked != "" {
		inst.mu.Unlock()
		return
	}
	if inst.stale(rec) {
		rec.outcome = phaseOutcome(opwire.PhaseStale, "a revision it expected moved before it was accepted")
		inst.mu.Unlock()
		return
	}
	req := rec.req
	req.Expects = rec.proposal.expects
	rec.proposal.taken = true
	e := rec.entry
	inst.grantEvent(trail.GrantEventConfirmed, "person", proposalReason(rec), p.t, nil)
	inst.mu.Unlock()
	go inst.route(p.t, rec, req, rec.spec, e)
}

// rejectProposal ends a proposal the person rejected.
func (inst *Service) rejectProposal(p proposalRef) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if p.rec.outcome.Phase == opwire.PhaseProposed {
		p.rec.outcome = phaseOutcome(opwire.PhaseRejected, "rejected by the person")
		inst.grantEvent(trail.GrantEventDeclined, "person", proposalReason(p.rec), p.t, nil)
	}
}

// proposalReason names the proposal a confirmation or a decline was about:
// the operation, the window and the call's key.
func proposalReason(rec *callRec) (s string) {
	return rec.spec.Name + " in window " + strconv.FormatUint(rec.instance, 10) + ", call " + rec.key
}

// discardProposals ends a task's pending proposals in window key, or in
// every window when key is zero. The caller holds mu.
func (inst *Service) discardProposals(t *task, key uint64, why string) {
	for _, rec := range t.keys {
		if rec.proposal != nil && rec.outcome.Phase == opwire.PhaseProposed && (key == 0 || rec.instance == key) {
			rec.outcome = phaseOutcome(opwire.PhaseExpired, why)
		}
	}
}

// requeueAsProposals turns a task's commands still queued in window key
// into proposals, when the person lowers the mode to suggest.
func (inst *Service) requeueAsProposals(t *task, key uint64) {
	inst.mu.Lock()
	var queued []*callRec
	for _, rec := range t.keys {
		if rec.instance == key && rec.routed && rec.outcome.Phase == opwire.PhaseAccepted {
			queued = append(queued, rec)
		}
	}
	inst.mu.Unlock()
	if inst.cfg.Host == nil {
		return
	}
	for _, rec := range queued {
		out, ok := inst.cfg.Host.OpsCancel(key, rec.callId)
		if !ok || out.Phase != opwire.PhaseCancelled {
			// It applied before the mode moved.
			continue
		}
		inst.mu.Lock()
		rec.routed = false
		rec.outcome = phaseOutcome(opwire.PhaseProposed, "the person lowered the mode; now a proposal")
		rec.proposal = &proposal{expects: rec.req.Expects}
		inst.mu.Unlock()
	}
}
