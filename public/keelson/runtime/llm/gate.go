package llm

import (
	"context"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/harness"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// The policy point (ADR-0254 §SD3, ADR-0383 §SD5): every call, whatever
// its verb, passes these checks before anything leaves, in this order —
// the journal entry written before it (ADR-0357), the delegation check
// (ADR-0269 §SD6), the sensitivity wall, the tier wall and its approval
// path (ADR-0312 §SD2–§SD3), the canaries (§SD6), admission (ADR-0300
// §SD5) and the write-ahead (ADR-0277 §SD3). It is the only such point in
// the tree; a verb supplies what the checks read and does its call
// between the passage's opening and its close.

// exitRoute is a model route as the policy point sees it: where it is and
// whether the sensitivity wall counts it as inside the box.
type exitRoute struct {
	endpoint string
	host     string
	local    bool
	// trusted says local holds by TrustedHosts, not loopback.
	trusted bool
}

// gateIn is what the policy point reads of one call.
type gateIn struct {
	route exitRoute
	// The journal entry the call names, and the content digest that entry
	// must record (ADR-0357 §SD1), taken only once a journal is wired.
	run        uint64
	journalKey string
	digestOf   func() (string, error)
	approval   string
	// The agent task the call's work is, empty for the app's own.
	onBehalfTask  string
	onBehalfEpoch uint64
	onBehalfCall  string
	// body is the bytes that leave: what an approval binds to (ADR-0312
	// §SD3) and the exit budget counts (§SD5). leaks is the text the
	// canaries read (§SD6).
	body  []byte
	leaks string
	// cancelKey and deadline are the requester's handle for runtime.llm.cancel
	// and its ctx deadline in Unix nanoseconds, 0 for none.
	cancelKey string
	deadline  int64
	timeout   time.Duration
	// estimatedInput and maxOutput are what admission reserves.
	estimatedInput int64
	maxOutput      int64
	// admitted runs once admission let the call through, before the
	// write-ahead: where a verb applies what the decision clamped.
	admitted func(d ration.Decision)
}

// passage is a call the policy point let through: the context the call
// runs under, how admission decided it, and what the call releases when
// it is done. The verb settles what the call used, then closes it.
type passage struct {
	inst     *Service
	ctx      context.Context
	decision ration.Decision
	chain    ration.Chain
	class    ration.ClassE
	bytesOut int64
	ticket   *ration.Ticket
	settled  bool
	claim    approvalClaim
	left     bool
	cleanup  []func()
	closed   bool
}

// settle charges what the call used, and the bytes that left the cell —
// only once they did: a call refused after admission sent nothing.
func (inst *passage) settle(used ration.Usage) {
	if inst.settled || inst.ticket == nil {
		return
	}
	inst.settled = true
	if inst.bytesOut > 0 && inst.left {
		if used == nil {
			used = ration.Usage{}
		}
		// Settled as reserved: what left the cell.
		used[ration.QuantityBytesSent] = inst.bytesOut
	}
	inst.inst.publishThresholds(inst.inst.ledger.Settle(inst.ticket, used))
}

// close releases the passage: an unsettled call is settled as using
// nothing, a held approval is consumed when the bytes left and given back
// otherwise, and the call stops being cancellable. A verb closes it
// before it replies, so a requester that acts on the reply finds the
// approval already spent or given back. Safe on a nil passage, and a
// second close does nothing: by then a resend may hold the call's
// approval, and giving it back again would take it from that resend.
func (inst *passage) close() {
	if inst == nil || inst.closed {
		return
	}
	inst.closed = true
	if inst.ticket != nil && !inst.settled {
		inst.settle(nil)
	}
	if inst.claim.callId != "" {
		if inst.left {
			inst.inst.pending.consume(inst.claim.callId)
		} else {
			inst.inst.pending.release(inst.claim.callId)
		}
	}
	for i := len(inst.cleanup) - 1; i >= 0; i-- {
		inst.cleanup[i]()
	}
	inst.cleanup = nil
}

// gate runs the policy point over one call. On a refusal it answers msg,
// records rec (and t's messages) and returns nil; otherwise the passage,
// which the caller closes. rec gains the verdicts as they are reached:
// the request entry, the task, the tier, the approval, the admission.
func (inst *Service) gate(msg *app.Msg, in gateIn, rec *CallRecord, t *turn) (p *passage) {
	var err error
	// Before any wall or admission: a call the journal does not know of goes
	// no further, and its refusal is on the trail (ADR-0357 §SD1).
	if rec.RequestEntry, err = inst.verifyJournal(in.run, in.journalKey, in.digestOf, msg.Sender); err != nil {
		inst.refuse(msg, "the journal does not cover the call: "+err.Error(), *rec, t)
		return nil
	}
	if in.onBehalfTask != "" {
		ref := inst.delegation.Load()
		if ref == nil {
			inst.refuse(msg, "agent-caused work, and no dispatcher to check its grant", *rec, t)
			return nil
		}
		// The task goes on the row only as the dispatcher attests it
		// (ADR-0277 §SD1): a context it does not confirm is refused and
		// recorded as the sender's own call.
		cc, ok, why := ref.d.CallContext(in.onBehalfTask, in.onBehalfEpoch, in.onBehalfCall, msg.Sender, msg.SenderInstance)
		if !ok {
			inst.refuse(msg, "agent-caused work the dispatcher does not attest: "+why, *rec, t)
			return nil
		}
		rec.Task, rec.TaskEpoch, rec.TaskCall = cc.Task, cc.Epoch, cc.Call
		if ok, why := ref.d.AllowDestination(cc.Task, cc.Epoch, DelegationDestination); !ok {
			inst.refuse(msg, "agent-caused work: "+why, *rec, t)
			return nil
		}
	}
	// The sensitivity wall (ADR-0254 §SD3, the ADR-0145 rule): confined
	// content leaves for a loopback provider and nowhere else, whatever
	// the declaration says. The tier wall below is the generalisation
	// for labelled content; it does not loosen this one (ADR-0307 §SD6:
	// a managed host is the stricter case), so no approval path opens
	// sealed data to a remote route.
	if rec.Sensitivity == queryengine.SensitivityConfined && !in.route.local {
		inst.refuse(msg, "the content derives from sealed data that must not leave this box, and "+in.route.host+" is neither loopback nor a trusted host", *rec, t)
		return nil
	}
	// The same wall in tiers (ADR-0312 §SD2): the route is a sink with a
	// clearance when it is outside the cell, a ceiling. The tier on the
	// row is the one the wall saw, confined content raised to the
	// confined tier.
	decl := inst.cellOf()
	tier := decl.TierOf(agentrun.TierE(rec.Tier), rec.Sensitivity == queryengine.SensitivityConfined)
	rec.Tier = uint8(tier)
	clearance := decl.RouteClearance(in.route.endpoint)
	p = &passage{inst: inst}
	// An approved resend holds its refused exit until the bytes leave or
	// a check below the wall refuses it; a refusal gives it back, so the
	// person is not asked again for a budget or a trail that recovers.
	refused := true
	defer func() {
		if refused {
			p.close()
			p = nil
		}
	}()
	if tier > clearance {
		reason := "the content is " + tier.String() + ", above the clearance (" + clearance.String() + ") of the model route " + in.route.host + ", a sink of the cell"
		sink := cell.Sink{Kind: cell.SinkKindModelRoute, Edge: in.route.host, Clearance: clearance}
		d, path, why := cell.Decide(agentrun.Label{Tier: tier}, sink, decl.Approvals)
		switch {
		case d == harness.DecisionAsk && in.approval != "":
			// The caller journaled the approval before resending
			// (ADR-0312 §SD3); the route verifies it names this call,
			// this sink and these bytes before anything leaves, and the
			// entry's id goes on the row for laws L14 and L33.
			var verr error
			if p.claim, verr = inst.verifyApproval(in.approval, in.body, tier, sink, msg.Sender, msg.SenderInstance, in.run); verr != nil {
				inst.refuse(msg, reason+"; the approval "+in.approval+" does not cover it: "+verr.Error(), *rec, t)
				return
			}
			rec.ApprovalEntry = p.claim.entry
		case d == harness.DecisionAsk:
			digest := requestDigest(in.body)
			inst.pending.remember(rec.CallId, pendingExit{digest: digest, tier: tier, sink: sink, sender: msg.Sender, instance: msg.SenderInstance, run: in.run}, time.Now())
			inst.refuseForApproval(msg, reason+"; "+why, path.String(), sink.Kind.String()+" "+sink.Edge, tier.String(), digest, *rec, t)
			return
		default:
			inst.refuse(msg, reason+"; "+why, *rec, t)
			return
		}
	}
	// The canaries (ADR-0312 §SD6, law L10): a planted token or a protected
	// body of a scope above the route's clearance in the bytes about to
	// leave, or in the strings they encode, refuses the exit whatever the
	// label said.
	if leaks := decl.Leaks(in.leaks, clearance); len(leaks) > 0 {
		inst.refuse(msg, "the request carries a canary of scope "+strings.Join(leaks, ", ")+" above the clearance ("+clearance.String()+") of the model route "+in.route.host, *rec, t)
		return
	}
	// The call's bound starts here, so time spent waiting in the queue
	// counts against it, and a cancel reaches a call still waiting.
	ctx, cancel := context.WithTimeout(inst.base, in.timeout)
	p.cleanup = append(p.cleanup, cancel)
	if in.cancelKey != "" {
		k := cancelKey{sender: msg.Sender, instance: msg.SenderInstance, key: in.cancelKey}
		inst.mu.Lock()
		inst.running[k] = cancel
		inst.mu.Unlock()
		p.cleanup = append(p.cleanup, func() {
			inst.mu.Lock()
			delete(inst.running, k)
			inst.mu.Unlock()
		})
	}
	if in.deadline > 0 {
		if d := time.Unix(0, in.deadline); d.Before(time.Now().Add(in.timeout)) {
			var c2 context.CancelFunc
			ctx, c2 = context.WithDeadline(ctx, d)
			p.cleanup = append(p.cleanup, c2)
		}
	}
	// Admission (ADR-0300 §SD5): after the delegation check and the
	// sensitivity wall, before anything leaves the machine.
	chain := ration.Chain{App: string(msg.Sender), Instance: msg.SenderInstance, Task: rec.Task, Purpose: rec.Purpose}
	class := inst.classOf(msg.SenderInstance)
	if rec.Task != "" && inst.isModerator(msg.Sender) {
		inst.noteModeratorTask(msg.Sender, rec.Task)
	}
	// A moderator's cancel carries its cause, so a call it stops in the
	// queue is told to stop rather than to retry.
	ctx, cancelCause := context.WithCancelCause(ctx)
	p.cleanup = append(p.cleanup, func() { cancelCause(nil) })
	inst.track(rec.CallId, activeCall{chain: chain, cancel: cancelCause})
	p.cleanup = append(p.cleanup, func() { inst.untrack(rec.CallId) })
	// The exit budget's quantity (ADR-0312 §SD5): the bytes that leave the
	// cell, reserved exactly so the call that would cross it is refused.
	if !in.route.local {
		p.bytesOut = int64(len(in.body))
	}
	decision, ticket := inst.ledger.Admit(ctx, ration.Request{Chain: chain, Class: class,
		EstimatedInput: in.estimatedInput, MaxOutput: in.maxOutput, BytesOut: p.bytesOut})
	rec.Admission, rec.AdmissionRule, rec.Queued = decision.Outcome.String(), decision.Rule, decision.Queued
	if decision.Outcome == ration.OutcomeRefused {
		inst.refuseAdmission(msg, decision, *rec, t, chain, class)
		return
	}
	p.ctx, p.decision, p.ticket = ctx, decision, ticket
	p.chain, p.class = chain, class
	if in.admitted != nil {
		in.admitted(decision)
	}
	// Written ahead: what the request adds to the conversation is on the
	// trail before the request leaves the machine, so a process that dies
	// mid-call leaves "sent, outcome unknown" rather than nothing.
	if t != nil {
		inst.writeRequest(ctx, rec, t)
	}
	ahead, refuse := inst.cfg.Trail.WriteAhead(inst.base)
	if refuse != nil {
		inst.refuse(msg, refuse.Error(), *rec, t)
		return
	}
	if t != nil {
		t.notAhead = !ahead
	}
	p.left = true
	refused = false
	return
}

// chatRoute is the chat-completion route as the policy point sees it.
func (inst *Service) chatRoute() exitRoute {
	return exitRoute{endpoint: inst.cfg.Endpoint, host: inst.host, local: inst.local, trusted: inst.trusted}
}
