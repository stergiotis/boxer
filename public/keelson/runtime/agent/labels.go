package agent

import (
	"encoding/json/v2"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// Labels and taint (ADR-0269 §SD7). A result carries the label of the window
// it came from; confined content bound for a model endpoint that is not
// local stays a data handle. Taint belongs to the conversation: it is set
// once untrusted content reached the model through the host, and lasts as
// long as the conversation.

// modelLocal reports whether the coordinator's model endpoint is local, so
// confined content may reach it (ADR-0254 §SD3).
func (inst *Service) modelLocal() (local bool) {
	if inst.cfg.ModelLocal != nil {
		local = inst.cfg.ModelLocal()
	}
	return
}

func conversationKey(t *task) (k string) {
	return string(t.actor) + "|" + strconv.FormatUint(t.actorInstance, 10) + "|" + t.conversation
}

// taint marks t's conversation as having read untrusted content.
func (inst *Service) taint(t *task) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.taints[conversationKey(t)] = true
}

// tainted reports whether t's conversation read untrusted content. The
// caller holds mu.
func (inst *Service) tainted(t *task) (tainted bool) {
	return inst.taints[conversationKey(t)]
}

// resolveRefs checks the arguments an operation declares as references and
// resolves each to the result it names, for the app; the model never sees
// what they hold. A reference must name a result of this task, and a
// confined one does not cross to another window: its locality is not
// proven (ADR-0145 §SD5).
func (inst *Service) resolveRefs(t *task, spec app.OperationSpec, req wireCall) (data map[string][]byte, why string) {
	if len(spec.Refs) == 0 {
		return
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(req.Args), &args); err != nil {
		why = "the arguments are not a JSON object"
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	data = make(map[string][]byte, len(spec.Refs))
	for _, field := range spec.Refs {
		raw, present := args[field]
		if !present || raw == nil {
			continue
		}
		name, isString := raw.(string)
		ref := t.refs[name]
		switch {
		case !isString || ref == nil:
			why = "the reference " + field + " names no result of this task"
			return
		case ref.confined && ref.instance != req.Instance:
			why = "the reference " + field + " holds confined content, which does not cross to another window"
			return
		}
		data[field] = ref.data
	}
	return
}

// onBehalfOf is the context stamped on a routed call (ADR-0269 §SD6).
func (inst *Service) onBehalfOf(t *task, e *entry, callId string) (obo *app.OnBehalfOf) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	obo = &app.OnBehalfOf{Task: t.id, Epoch: t.epoch, Call: callId, Principal: "person",
		Act: []string{"person", string(t.actor) + "#" + strconv.FormatUint(t.actorInstance, 10),
			string(e.app) + "#" + strconv.FormatUint(e.instance, 10)},
		Destinations: slices.Clone(t.destinations)}
	return
}

// AllowDestination answers a host service that reaches outside: the task
// must be live at this epoch and its grant must list the destination
// (ADR-0269 §SD6).
func (inst *Service) AllowDestination(taskId string, epoch uint64, destination string) (ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var t *task
	for _, cand := range inst.tasks {
		if cand.id == taskId {
			t = cand
		}
	}
	switch {
	case t == nil:
		reason = "no task by that id"
	case t.revoked != "":
		reason = "the task ended: " + t.revoked
	case t.epoch != epoch:
		reason = "the task's epoch moved; the work belongs to a stopped turn"
	case t.ceiling.refuseDestination(destination) != "":
		reason = t.ceiling.refuseDestination(destination)
	case !slices.Contains(t.destinations, destination):
		reason = "the task's grant does not list " + destination
	default:
		ok = true
	}
	return
}

var _ app.DelegationI = (*Service)(nil)

// CallContext answers a host service that records agent-caused work
// (ADR-0288 (proposed) §SD5): the task must be live at this epoch, and the
// call must be one the dispatcher sent to the sender's app and window,
// in flight or answered. The answer is the dispatcher's record of the call, so a
// sender cannot attach its work to a turn or a call that did not cause it.
func (inst *Service) CallContext(taskId string, epoch uint64, callId string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var t *task
	for _, cand := range inst.tasks {
		if cand.id == taskId {
			t = cand
		}
	}
	switch {
	case t == nil:
		reason = "no task by that id"
		return
	case t.revoked != "":
		reason = "the task ended: " + t.revoked
		return
	case t.epoch != epoch:
		reason = "the task's epoch moved; the work belongs to a stopped turn"
		return
	}
	var rec *callRec
	for _, r := range t.keys {
		if r.callId == callId {
			rec = r
			break
		}
	}
	switch {
	case callId == "" || rec == nil:
		reason = "the task has no call by that id"
		return
	case !rec.sent:
		// Refused before sending, or turned back into a proposal. A call
		// in flight is sent: a handler that publishes before it replies is
		// attested.
		reason = "the call did not reach a window"
		return
	case rec.app != sender || rec.instance != senderInstance:
		reason = "the call was routed to another window"
		return
	}
	conversation := t.conversation
	if conversation == "" {
		conversation = rec.conversation
	}
	cc = app.CallContext{Task: t.id, Epoch: t.epoch, Call: rec.callId, Conversation: conversation, Turn: rec.turn,
		App: rec.app, Instance: rec.instance, Operation: rec.spec.Name, InFlight: rec.outcome.Phase == opwire.PhaseUnspecified}
	if rec.cause.Has {
		cc.ModelCall = rec.cause.Val.ModelCall
		cc.ToolIndex = rec.cause.Val.ToolIndex
		if rec.cause.Val.ToolCall.Has {
			cc.ToolCall = rec.cause.Val.ToolCall.Val
		}
	}
	return cc, true, ""
}

var _ app.CallContextI = (*Service)(nil)
