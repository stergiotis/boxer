package agent

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
)

// The mark verbs (ADR-0297 §SD8): mark puts a mark on the overlay
// the window host draws above every window, unmark removes the task's. Like
// the window verbs they change what the person sees, not an app's state, so
// they go to the host; each is a keyed call, recorded with effect view.

const (
	verbMark   = "mark"
	verbUnmark = "unmark"
)

func (inst *Service) mark(msg *app.Msg) (rep wireCallReply) {
	rep.V = wireVersion
	req, err := decode[wireMark](msg.Payload)
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
	var first uint64
	for _, a := range req.Anchors {
		if a.Window != 0 {
			first = a.Window
			break
		}
	}
	rec := &callRec{key: req.Key, callId: inst.mintCallId(t), instance: first,
		spec: app.OperationSpec{Name: verb, Effect: app.OperationEffectView}, turn: req.Turn, cause: req.cause()}
	t.keys[req.Key] = rec
	if e := t.entries[first]; e != nil {
		rec.app = e.app
	}
	if verb == verbMark {
		out = markCheck(t, req)
	} else if t.callsUsed >= t.callsBudget {
		out = phaseOutcome(opwire.PhaseInputRequired, "the task's call budget is spent; request more")
	}
	if out.Phase == opwire.PhaseUnspecified {
		t.callsUsed++
	}
	task := t.id
	inst.mu.Unlock()
	if out.Phase == opwire.PhaseUnspecified {
		switch {
		case inst.cfg.Host == nil:
			out = phaseOutcome(opwire.PhaseRefused, "no window host draws marks")
		case verb == verbMark:
			out = inst.putMark(task, req)
		default:
			n := inst.cfg.Host.Marks().Clear(task, req.Id)
			out = phaseOutcome(opwire.PhaseCompleted, "cleared "+strconv.Itoa(n)+" mark(s)")
		}
	}
	inst.settle(t, rec, out, false)
	inst.record(t, rec, "dispatch", out)
	rep.Outcome = wireOutcomeOf(out, "", "")
	return
}

func (inst *Service) putMark(task string, req wireMark) (out opwire.Outcome) {
	op, ok := inscribe.ParseOp(req.Op)
	if !ok {
		return phaseOutcome(opwire.PhaseRefused, "no mark op "+strconv.Quote(req.Op)+"; highlight, callout, arrow, step or spotlight")
	}
	targets := make([]inscribe.Anchor, 0, len(req.Anchors))
	for _, a := range req.Anchors {
		r := inscribe.Rect{X: a.X, Y: a.Y, W: a.W, H: a.H}
		switch {
		case a.Viewport:
			targets = append(targets, inscribe.Anchor{Viewport: &r})
		case a.Rect:
			targets = append(targets, inscribe.Anchor{Window: a.Window, Local: &r})
		default:
			targets = append(targets, inscribe.Anchor{Window: a.Window})
		}
	}
	if err := inst.cfg.Host.Marks().Put(inscribe.Mark{Task: task, Id: req.Id, Op: op, Targets: targets, Text: req.Text}); err != nil {
		return phaseOutcome(opwire.PhaseRefused, err.Error())
	}
	return phaseOutcome(opwire.PhaseCompleted, "drawn; it follows its window until the person clears it")
}

// markCheck is what the grant allows of a mark (ADR-0297 §SD8):
// suggest or act on every window it points at, the desktop in suggest or act
// for a viewport rect. An unspecified phase lets the call through. The
// caller holds mu.
func markCheck(t *task, req wireMark) (out opwire.Outcome) {
	if t.callsUsed >= t.callsBudget {
		return phaseOutcome(opwire.PhaseInputRequired, "the task's call budget is spent; request more")
	}
	if len(req.Anchors) == 0 {
		return phaseOutcome(opwire.PhaseRefused, "a mark names at least one target")
	}
	for _, a := range req.Anchors {
		if a.Viewport {
			switch {
			case t.ceiling.refuseDesktop() != "":
				return phaseOutcome(opwire.PhaseRefused, t.ceiling.refuseDesktop())
			case t.desktop < ModeSuggest:
				return phaseOutcome(opwire.PhaseInputRequired, "marking the desktop needs the desktop in suggest mode; request it")
			}
			continue
		}
		e := t.entries[a.Window]
		w := strconv.FormatUint(a.Window, 10)
		switch {
		case e == nil:
			return phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover window "+w)
		case e.mode >= ModeSuggest && t.modeOf(e) < ModeSuggest:
			return phaseOutcome(opwire.PhaseRefused, t.ceiling.refuseMode(ModeSuggest))
		case e.mode < ModeSuggest:
			return phaseOutcome(opwire.PhaseInputRequired, "marking window "+w+" needs suggest mode on it; request it")
		}
	}
	return
}
