package agent

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// The unattended mode (ADR-0298): the host decides in the person's place
// what it can decide within bounds set before the run — a grant request, a
// widening, a suggest-mode proposal — and leaves to the person what would
// move a bound: more calls, more time, a consequential command. The ceiling
// binds as it does for the person, since it is checked before either is
// asked.

// reasonUnattended is the reason a grant event the host decided in the
// person's place carries; DecidedBy is "host".
const reasonUnattended = "unattended"

// Unattended reports whether the service decides in the person's place:
// built with the boxer_unattended tag and turned on in Config.
func (inst *Service) Unattended() (on bool) {
	return Unattended && inst.cfg.Unattended
}

// autoDecides reports whether the host approves request r in the person's
// place. Only under a ceiling: without one nothing bounds what the request
// may hold. A task past its deadline and a spent budget are the person's:
// an approval would move the bound the mode runs inside. The caller holds
// mu.
func (inst *Service) autoDecides(r *request) (yes bool) {
	if !inst.Unattended() || r.state != reqStatePending {
		return false
	}
	if r.ceiling == nil && (r.task == nil || r.task.ceiling == nil) {
		return false
	}
	if r.task != nil && time.Now().After(r.task.deadline) {
		return false
	}
	if r.held != nil && (r.held.need == needBudget || r.held.need == needDeadline) {
		return false
	}
	return true
}

// leftToPerson is why the host, running unattended, left request r to
// the person — for the dialog to say; "" when the mode is off. The caller
// holds mu.
func (inst *Service) leftToPerson(r *request) (why string) {
	switch {
	case !inst.Unattended():
		return ""
	case r.ceiling == nil && (r.task == nil || r.task.ceiling == nil):
		return "its coordinator set no ceiling to bound what the host may approve"
	case r.held != nil && r.held.need == needBudget:
		return "the task's call budget is spent, and more calls are yours to give"
	case r.held != nil && r.held.need == needDeadline, r.task != nil && time.Now().After(r.task.deadline):
		return "the task's deadline passed, and more time is yours to give"
	}
	return "it is not one the host decides"
}

// autoAccepts reports whether a proposal is accepted on arrival: one the
// window's grant made by sharing it in suggest mode, under a ceiling that
// allows act. A proposal the ceiling forced — a suggest ceiling, or none to
// bound the host — and a consequential command waiting for confirmation
// stay the person's. The caller holds mu.
func (inst *Service) autoAccepts(t *task, spec app.OperationSpec, consent string) (yes bool) {
	if !inst.Unattended() || t.ceiling == nil || t.ceiling.normal().Mode < ModeAct {
		return false
	}
	return !(spec.Effect == app.OperationEffectConsequential && consent == "")
}

// acceptOnArrival accepts a proposal in the person's place when
// autoAccepts allows it and records the confirmation; accepted is then the
// zero outcome, for the caller to route rec as any call and pace it. The
// caller holds mu.
func (inst *Service) acceptOnArrival(t *task, rec *callRec, out opwire.Outcome) (next opwire.Outcome, accepted bool) {
	if out.Phase != opwire.PhaseProposed || !inst.autoAccepts(t, rec.spec, rec.consent) {
		return out, false
	}
	inst.grantEventAsked(trail.GrantEventConfirmed, "host", reasonUnattended+": "+proposalReason(rec), t, nil, rec.asked())
	return opwire.Outcome{}, true
}

// autoApprove approves r in the person's place. The caller holds mu; a held
// call is routed after it is released.
func (inst *Service) autoApprove(r *request) (route *held) {
	return inst.approveAs(r, "host", reasonUnattended)
}
