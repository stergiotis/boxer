package agent

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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

// autoAccepts reports whether a proposal is accepted on arrival: one that
// suggest mode made, not a consequential command waiting for confirmation.
func (inst *Service) autoAccepts(spec app.OperationSpec, consent string) (yes bool) {
	return inst.Unattended() && !(spec.Effect == app.OperationEffectConsequential && consent == "")
}

// autoApprove approves r in the person's place. The caller holds mu; a held
// call is routed after it is released.
func (inst *Service) autoApprove(r *request) (route *held) {
	return inst.approveAs(r, "host", reasonUnattended)
}
