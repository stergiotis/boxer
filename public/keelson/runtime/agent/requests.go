package agent

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/instanceclosed"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// The person's side of a grant (ADR-0269 §SD5, §SD6): a coordinator's
// request waits until the person decides in host chrome, and a call outside
// the grant waits as a widening.

// CoordinatorsEnv names the apps the person registered as coordinators: the
// only apps whose grant requests reach the person (ADR-0269 §SD6). A manifest
// capability for runtime.agent is necessary and not sufficient.
var CoordinatorsEnv = env.NewString(env.Spec{
	Name:        "BOXER_AGENT_COORDINATORS",
	Description: "comma-separated app ids or subject aliases allowed to ask the person for an agent task grant (ADR-0269), e.g. chat,agentconsole; empty allows none",
	Category:    env.CategorySystem,
})

// ParseCoordinators splits CoordinatorsEnv's value.
func ParseCoordinators(s string) (names []string) {
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return
}

// requestTimeout bounds how long a request waits for the person.
const requestTimeout = 5 * time.Minute

type reqStateE uint8

const (
	reqStatePending reqStateE = iota
	reqStateApproved
	reqStateRejected
	reqStateExpired
)

func (inst reqStateE) String() (s string) {
	switch inst {
	case reqStatePending:
		s = "pending"
	case reqStateApproved:
		s = "approved"
	case reqStateRejected:
		s = "rejected"
	default:
		s = "expired"
	}
	return
}

// need names what a held call is missing.
type needE uint8

const (
	needInstance needE = iota + 1
	needOperation
	needMode
	needBudget
	// needDeadline is a call to a task whose deadline passed.
	needDeadline
)

// held is a call waiting on a widening.
type held struct {
	rec  *callRec
	req  wireCall
	need needE
	mode ModeE
	// extra says, for a deadline, how much time approving adds.
	extra string
}

// request is one decision the person owes: a new grant, or the widening of
// a task's grant, asked for by its coordinator or by a call outside it.
type request struct {
	key           string
	actor         app.AppIdT
	actorInstance uint64
	conversation  string
	// task is the task being widened; nil for a new grant.
	task         *task
	plan         string
	wanted       map[uint64]ModeE
	wantedOps    map[uint64][]string
	destinations []string
	calls        uint32
	deadline     time.Duration
	held         *held
	launches     []wireLaunchEntry
	created      time.Time
	state        reqStateE
	why          string
	// The person's choices, edited in the dialog on the render goroutine.
	// shareFlag holds the checkbox bindings, which need stable pointers.
	share     map[uint64]bool
	shareFlag map[uint64]*bool
	mode      map[uint64]ModeE
	// desktop is the desktop mode asked for, ModeUnspecified when none;
	// desktopShare the person's choice, desktopFlag its checkbox binding.
	desktop      ModeE
	desktopShare bool
	desktopFlag  *bool
}

func (inst *Service) isCoordinator(id app.AppIdT) (ok bool) {
	for _, n := range inst.cfg.Coordinators {
		if n == string(id) || n == id.SubjectAlias() {
			return true
		}
	}
	return
}

// requestGrant answers runtime.agent.request: a test grant at once where
// those are on, otherwise a pending request the person decides.
func (inst *Service) requestGrant(msg *app.Msg) (rep wireGrantReply) {
	rep.V = wireVersion
	req, err := decode[wireGrantRequest](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if len(req.Launches) > 0 {
		var reason string
		if req.Launches, reason = inst.resolveLaunches(req.Launches); reason != "" {
			rep.Reason = reason
			inst.recordGrantRefusal(msg, req, reason)
			inst.grantEvent(trail.GrantEventRefused, "host", reason, nil, asked(msg, req))
			return
		}
	}
	if inst.cfg.TestGrants && req.Handle == "" {
		return inst.testGrant(msg, req)
	}
	var t *task
	if req.Handle != "" {
		inst.mu.Lock()
		var out opwire.Outcome
		var ok bool
		// A widening of a task past its deadline is how it gets more time.
		t, out, ok, _ = inst.resolveLate(req.Handle, msg, true)
		inst.mu.Unlock()
		if !ok {
			rep.Reason = out.Reason
			return
		}
	} else if !inst.isCoordinator(msg.Sender) {
		rep.Reason = "this app is not registered as a coordinator (BOXER_AGENT_COORDINATORS)"
		inst.grantEvent(trail.GrantEventRefused, "host", rep.Reason, nil, asked(msg, req))
		return
	}
	r := &request{key: "req-" + randomHex(8), actor: msg.Sender, actorInstance: msg.SenderInstance, conversation: req.Conversation, task: t,
		plan: req.Plan, wanted: make(map[uint64]ModeE), wantedOps: make(map[uint64][]string),
		destinations: req.Destinations, calls: req.Calls, deadline: time.Duration(req.DeadlineSecs) * time.Second,
		launches: req.Launches,
		created:  time.Now(), share: make(map[uint64]bool), shareFlag: make(map[uint64]*bool), mode: make(map[uint64]ModeE),
		desktop: ParseMode(req.Desktop)}
	r.desktopShare = r.desktop != ModeUnspecified
	for _, e := range req.Entries {
		m := ParseMode(e.Mode)
		if m == ModeUnspecified {
			m = ModeObserve
		}
		r.wanted[e.Instance], r.wantedOps[e.Instance] = m, e.Operations
		r.share[e.Instance], r.mode[e.Instance] = true, m
	}
	inst.mu.Lock()
	inst.grantEvent(trail.GrantEventRequested, "coordinator", "", nil, r)
	inst.mu.Unlock()
	if inst.cfg.TestGrants {
		// A widening under test grants: the test grant stands in for the
		// person here too, since nobody answers the dialog (ADR-0269 §SD6).
		return inst.testWiden(r)
	}
	inst.mu.Lock()
	inst.requests[r.key] = r
	inst.requestOrder = append(inst.requestOrder, r.key)
	inst.mu.Unlock()
	rep.Ok, rep.Key, rep.Phase = true, r.key, r.state.String()
	return
}

// requestStatus answers status on a request key; only the requesting
// instance may ask. The caller holds mu.
func (inst *Service) requestStatus(key string, msg *app.Msg) (w wireOutcome) {
	r := inst.requests[key]
	if r == nil || r.actor != msg.Sender || r.actorInstance != msg.SenderInstance {
		w = wireOutcome{Phase: opwire.PhaseRefused.String(), Reason: "no request by that key from this instance"}
		return
	}
	inst.expireRequest(r)
	w = wireOutcome{Phase: r.state.String(), Reason: r.why}
	if r.state == reqStateApproved {
		t := r.task
		if t != nil {
			w.Task, w.Handle = t.id, t.handle
		}
	}
	return
}

// expireRequest ends a request the person left too long. The caller holds
// mu.
func (inst *Service) expireRequest(r *request) {
	if r.state != reqStatePending || time.Since(r.created) < requestTimeout {
		return
	}
	r.state, r.why = reqStateExpired, "the person did not decide in time"
	inst.grantEvent(trail.GrantEventRefused, "host", r.why, nil, r)
	if r.held != nil {
		r.held.rec.outcome = phaseOutcome(opwire.PhaseExpired, r.why)
	}
}

// holdForWidening turns a call outside the grant into a widening the
// person decides; the call stays input_required until then. The caller
// holds mu.
func (inst *Service) holdForWidening(t *task, rec *callRec, req wireCall, need needE, mode ModeE) {
	r := &request{key: "req-" + randomHex(8), actor: t.actor, actorInstance: t.actorInstance, task: t,
		plan: req.Reason, wanted: map[uint64]ModeE{req.Instance: mode}, wantedOps: map[uint64][]string{},
		created: time.Now(), share: map[uint64]bool{req.Instance: true}, shareFlag: make(map[uint64]*bool),
		mode: map[uint64]ModeE{req.Instance: mode},
		held: &held{rec: rec, req: req, need: need, mode: mode, extra: inst.deadline().String()}}
	if need == needOperation {
		r.wantedOps[req.Instance] = []string{req.Operation}
	}
	rec.heldBy = r
	inst.grantEvent(trail.GrantEventRequested, "coordinator", "a call outside the grant: "+req.Operation, nil, r)
	inst.requests[r.key] = r
	inst.requestOrder = append(inst.requestOrder, r.key)
}

// pending lists the undecided requests, oldest first, expiring stale ones.
// The caller holds mu.
func (inst *Service) pending() (out []*request) {
	for _, k := range inst.requestOrder {
		r := inst.requests[k]
		inst.expireRequest(r)
		if r.state == reqStatePending {
			out = append(out, r)
		}
	}
	return
}

// testWiden approves a widening of a test task in place.
func (inst *Service) testWiden(r *request) (rep wireGrantReply) {
	rep.V = wireVersion
	for _, m := range r.mode {
		if m != ModeObserve && m != ModeAct {
			rep.Reason = "a test grant takes observe or act; suggest needs the person's proposal surface"
			return
		}
	}
	inst.mu.Lock()
	route := inst.approve(r)
	t := r.task
	inst.mu.Unlock()
	if route != nil {
		inst.routeHeld(route)
	}
	rep.Ok, rep.Task, rep.Handle, rep.Phase = true, t.id, t.handle, reqStateApproved.String()
	return
}

// approve applies the person's decision. The caller holds mu; a held call
// is routed after it is released.
func (inst *Service) approve(r *request) (route *held) {
	if r.state != reqStatePending {
		return
	}
	event := trail.GrantEventWidened
	if r.task == nil {
		t := inst.newTask(r.actor, r.actorInstance, r.conversation, r.plan, r.destinations, r.calls, r.deadline, false)
		r.task = t
		inst.tasks[t.handle] = t
		event = trail.GrantEventApproved
	}
	t := r.task
	// A widening's destinations join the task's.
	for _, d := range r.destinations {
		if !slices.Contains(t.destinations, d) {
			t.destinations = append(t.destinations, d)
		}
	}
	for k, share := range r.share {
		if !share {
			continue
		}
		m := r.mode[k]
		e := t.entries[k]
		if e == nil {
			info, open := inst.openInstance(k)
			if !open {
				continue
			}
			e = &entry{instance: k, app: info.App, alias: info.Alias, mode: m}
			t.entries[k] = e
			inst.startTurnAt(t, k)
			inst.attach(k)
		} else if m > e.mode {
			e.mode = m
		}
		if ops := r.wantedOps[k]; len(ops) > 0 && len(e.ops) > 0 {
			for _, op := range ops {
				if !slices.Contains(e.ops, op) {
					e.ops = append(e.ops, op)
				}
			}
		}
	}
	if time.Now().After(t.deadline) || (r.held != nil && r.held.need == needDeadline) {
		t.deadline = time.Now().Add(inst.deadline())
	}
	if r.held != nil && r.held.need == needBudget {
		t.callsBudget += DefaultCallBudget / 4
	}
	inst.addLaunches(t, r.launches)
	if r.desktopShare && r.desktop > t.desktop {
		t.desktop = r.desktop
	}
	r.state = reqStateApproved
	inst.grantEvent(event, inst.decider(), "", t, nil)
	route = r.held
	return
}

// reject applies the person's refusal. The caller holds mu.
func (inst *Service) reject(r *request) {
	if r.state != reqStatePending {
		return
	}
	r.state, r.why = reqStateRejected, "the person declined"
	inst.grantEvent(trail.GrantEventRefused, "person", r.why, nil, r)
	if r.held != nil {
		r.held.rec.outcome = phaseOutcome(opwire.PhaseRejected, "the person declined the widening")
		r.held.rec.heldBy = nil
	}
}

// routeHeld re-runs a call whose widening the person approved, off the
// render goroutine.
func (inst *Service) routeHeld(h *held) {
	go func() {
		inst.mu.Lock()
		h.rec.heldBy = nil
		t := inst.taskOf(h.rec)
		if t == nil {
			inst.mu.Unlock()
			return
		}
		out, spec, e, _, _ := inst.check(t, h.req)
		h.rec.spec = spec
		if out.Phase == opwire.PhaseProposed {
			h.rec.req, h.rec.entry = h.req, e
			h.rec.proposal = &proposal{confirm: spec.Effect == app.OperationEffectConsequential,
				expects: inst.expectsFor(t, h.req, spec)}
		}
		if out.Phase != opwire.PhaseUnspecified {
			h.rec.outcome = out
			inst.mu.Unlock()
			inst.record(t, h.rec, "dispatch", out)
			return
		}
		t.callsUsed++
		inst.mu.Unlock()
		inst.route(t, h.rec, h.req, spec, e)
	}()
}

// taskOf finds the task a call belongs to. The caller holds mu.
func (inst *Service) taskOf(rec *callRec) (t *task) {
	for _, cand := range inst.tasks {
		if cand.keys[rec.key] == rec {
			return cand
		}
	}
	return
}

func (inst *Service) openInstance(key uint64) (info opwire.InstanceInfo, ok bool) {
	if inst.cfg.Host == nil {
		return
	}
	for _, i := range inst.cfg.Host.OpsInstances() {
		if i.Key == key {
			return i, true
		}
	}
	return
}

func (inst *Service) attach(key uint64) {
	if inst.cfg.Host != nil {
		inst.cfg.Host.OpsAttach(key, true)
	}
}

// holder returns the task other than t that holds key in suggest or act
// mode: one task at a time does (ADR-0269 §SD4). The caller holds mu.
func (inst *Service) holder(key uint64, t *task) (other *task) {
	for _, cand := range inst.tasks {
		if cand == t || cand.revoked != "" {
			continue
		}
		if e := cand.entries[key]; e != nil && e.mode >= ModeSuggest {
			return cand
		}
	}
	return
}

// endTask revokes a task: the epoch moves, its queued calls expire, its
// windows detach. Instances are told after mu is released. by is who ended
// it — "person", "coordinator" or "host" — for the grant's record.
func (inst *Service) endTask(t *task, why string, by string) {
	inst.mu.Lock()
	if t.revoked != "" {
		inst.mu.Unlock()
		return
	}
	t.revoked = why
	inst.grantEvent(trail.GrantEventEnded, by, why, t, nil)
	t.epoch++
	queued := make(map[uint64][]string)
	for k := range t.entries {
		queued[k] = t.queuedOn(k)
	}
	for _, r := range inst.requests {
		if r.task == t && r.state == reqStatePending {
			r.state, r.why = reqStateExpired, "the task ended"
		}
	}
	inst.discardProposals(t, 0, "the task ended")
	// The windows it opened pass to the person (ADR-0269 §SD6).
	for k := range t.launched {
		if t.entries[k] != nil {
			inst.leftBy[k] = t.id
		}
	}
	inst.mu.Unlock()
	for k, ids := range queued {
		if inst.cfg.Host == nil {
			break
		}
		if len(ids) > 0 {
			inst.cfg.Host.OpsExpire(k, ids, why)
		}
		inst.cfg.Host.OpsAttach(k, false)
	}
}

// detachEntry removes one instance from a task.
func (inst *Service) detachEntry(t *task, key uint64, why string) {
	inst.mu.Lock()
	e := t.entries[key]
	delete(t.entries, key)
	ids := t.queuedOn(key)
	inst.discardProposals(t, key, why)
	inst.mu.Unlock()
	if e == nil || inst.cfg.Host == nil {
		return
	}
	inst.cfg.Host.OpsExpire(key, ids, why)
	inst.cfg.Host.OpsAttach(key, false)
}

// instanceClosed ends the tasks whose coordinator closed and detaches a
// closed window from every task (ADR-0269 §SD6).
func (inst *Service) instanceClosed(msg *app.Msg) {
	ev, err := buscodec.Decode[instanceclosed.InstanceClosed](msg.Payload)
	if err != nil {
		return
	}
	inst.mu.Lock()
	var ends, detaches []*task
	for _, t := range inst.tasks {
		if t.revoked != "" {
			continue
		}
		if string(t.actor) == ev.AppId && t.actorInstance == ev.InstanceKey {
			ends = append(ends, t)
		} else if t.entries[ev.InstanceKey] != nil {
			detaches = append(detaches, t)
		}
	}
	inst.mu.Unlock()
	for _, t := range ends {
		inst.endTask(t, "the coordinator closed", "host")
	}
	for _, t := range detaches {
		inst.detachEntry(t, ev.InstanceKey, "the window closed")
	}
}

// AwaitGrant polls a request key until the person decides or ctx ends.
func (inst *Client) AwaitGrant(ctx context.Context, key string) (g Grant, err error) {
	for {
		var out Outcome
		out, err = inst.Status(ctx, "", key, MaxStatusWait)
		if err != nil {
			return
		}
		switch out.Phase {
		case reqStateApproved.String():
			g = Grant{Task: out.Task, Handle: out.Handle}
			return
		case reqStatePending.String():
			if err = ctx.Err(); err != nil {
				return
			}
			continue
		default:
			err = &RefusedError{Reason: out.Phase + ": " + out.Reason}
			return
		}
	}
}

// asked is a grant request as the event record takes it, for a request the
// host refuses before it becomes one the person sees.
func asked(msg *app.Msg, req wireGrantRequest) (r *request) {
	r = &request{actor: msg.Sender, actorInstance: msg.SenderInstance, conversation: req.Conversation, plan: req.Plan,
		destinations: req.Destinations, launches: req.Launches, wanted: make(map[uint64]ModeE), desktop: ParseMode(req.Desktop)}
	for _, e := range req.Entries {
		r.wanted[e.Instance] = ParseMode(e.Mode)
	}
	return
}
