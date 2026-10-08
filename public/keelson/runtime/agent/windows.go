package agent

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// The window verbs (ADR-0276 §SD3): arrange, raise and place change the
// desktop's geometry, not an app's state, so they go to the host rather than
// to a window's operations. Each is a call of its own — keyed, budgeted and
// recorded with effect view — that completes once the host has queued it;
// where the windows ended up is keelson('windows') a frame or more later.

// Verb names, as the record's operation and the subject's last segment.
const (
	verbArrange = "arrange"
	verbRaise   = "raise"
	verbPlace   = "place"
)

// reasonQueued is what a completed window verb says: the reply comes before
// the windows move.
const reasonQueued = "queued; the windows move over the next frames — read keelson('windows') for where they ended"

func (inst *Service) windowAct(msg *app.Msg) (rep wireCallReply) {
	rep.V = wireVersion
	req, err := decode[wireWindowAct](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok = true
	verb := strings.TrimPrefix(msg.Subject, SubjectPrefix)
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	if req.Key == "" {
		inst.mu.Unlock()
		rep.Outcome = wireOutcomeOf(phaseOutcome(opwire.PhaseRefused, "every call carries a key"), "", "")
		return
	}
	if prev, seen := t.keys[req.Key]; seen {
		rep.Outcome = inst.outcomeOf(t, prev, 0)
		inst.mu.Unlock()
		return
	}
	rec := &callRec{key: req.Key, callId: inst.mintCallId(t), instance: req.Instance, spec: app.OperationSpec{Name: verb, Effect: app.OperationEffectView},
		turn: req.Turn, cause: req.cause()}
	t.keys[req.Key] = rec
	if e := t.entries[req.Instance]; e != nil {
		rec.app = e.app
	}
	out = windowCheck(t, verb, req)
	if out.Phase == opwire.PhaseUnspecified {
		t.callsUsed++
	}
	inst.mu.Unlock()
	if out.Phase == opwire.PhaseUnspecified {
		// Moving windows is a change the person sees, paced like the rest.
		inst.pace(t)
		switch verb {
		case verbArrange:
			err = inst.cfg.Host.OpsArrange(req.Command, req.Instances)
		case verbRaise:
			err = inst.cfg.Host.OpsRaise(req.Instance)
		case verbPlace:
			err = inst.cfg.Host.OpsPlace(req.Instance, req.X, req.Y, req.W, req.H)
		}
		if err != nil {
			out = phaseOutcome(opwire.PhaseRefused, err.Error())
		} else {
			out = phaseOutcome(opwire.PhaseCompleted, reasonQueued)
		}
	}
	inst.settle(t, rec, out, false)
	inst.record(t, rec, "dispatch", out)
	rep.Outcome = wireOutcomeOf(out, "", "")
	return
}

// windowCheck is what the grant allows of a window verb (ADR-0276 §SD4):
// arrange needs the desktop in act, raise and place act on the window. An
// unspecified phase lets the call through. The caller holds mu.
func windowCheck(t *task, verb string, req wireWindowAct) (out opwire.Outcome) {
	switch {
	case t.callsUsed >= t.callsBudget:
		out = phaseOutcome(opwire.PhaseInputRequired, "the task's call budget is spent; request more")
	case verb == verbArrange && t.ceiling.refuseDesktop() != "":
		out = phaseOutcome(opwire.PhaseRefused, t.ceiling.refuseDesktop())
	case verb == verbArrange:
		if t.desktop != ModeAct {
			out = phaseOutcome(opwire.PhaseInputRequired, "arranging windows needs the desktop in act mode; request it with desktop act")
		}
	case verb == verbRaise || verb == verbPlace:
		e := t.entries[req.Instance]
		switch {
		case e == nil:
			out = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover this instance")
		case e.mode == ModeAct && t.modeOf(e) != ModeAct:
			out = phaseOutcome(opwire.PhaseRefused, t.ceiling.refuseMode(ModeAct))
		case e.mode != ModeAct:
			out = phaseOutcome(opwire.PhaseInputRequired, verb+" needs act mode on this window; request it")
		}
	default:
		out = phaseOutcome(opwire.PhaseRefused, "no such window verb")
	}
	return
}
