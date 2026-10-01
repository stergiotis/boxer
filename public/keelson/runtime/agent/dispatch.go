package agent

import (
	"crypto/rand"
	"encoding/hex"
	"reflect"
	"slices"
	"strconv"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// The dispatcher (ADR-0269 §SD6): every call is checked against the task's
// grant before it reaches an instance, and the outcome is recorded.

// HostI is what the dispatcher asks of the window host directly; calls
// themselves travel on the operation subjects. *windowhost.Inst implements
// it.
type HostI interface {
	OpsInstances() (out []opwire.InstanceInfo)
	OpsRenderGoroutine() (id uint64)
	OpsStatus(key uint64, callId string) (out opwire.Outcome, ok bool)
	OpsCancel(key uint64, callId string) (out opwire.Outcome, ok bool)
	OpsExpire(key uint64, ids []string, reason string)
	OpsAttach(key uint64, attached bool) (ok bool)
	OpsCapture(key uint64) (job string, err error)
	OpsCaptureStatus(job string) (st opwire.CaptureStatus, ok bool)
}

// ModeE is how far a task may act in one instance (ADR-0269 §SD5).
type ModeE uint8

const (
	ModeUnspecified ModeE = 0
	// ModeObserve reads and captures; anything else asks to raise the mode.
	ModeObserve ModeE = 1
	// ModeSuggest turns commands into proposals the person accepts.
	ModeSuggest ModeE = 2
	// ModeAct applies commands, undoable in the UI.
	ModeAct ModeE = 3
)

var AllModes = []ModeE{ModeObserve, ModeSuggest, ModeAct}

func (inst ModeE) String() (s string) {
	switch inst {
	case ModeObserve:
		s = "observe"
	case ModeSuggest:
		s = "suggest"
	case ModeAct:
		s = "act"
	default:
		s = "unspecified"
	}
	return
}

// ParseMode is the inverse of String; unknown names are ModeUnspecified.
func ParseMode(s string) (m ModeE) {
	for _, c := range AllModes {
		if c.String() == s {
			return c
		}
	}
	return
}

// Defaults for a grant that names none.
const (
	DefaultCallBudget = 200
	DefaultDeadline   = 30 * time.Minute
	// MaxStatusWait bounds how long status waits for a final phase.
	MaxStatusWait = 5 * time.Second
	// keepRecords bounds the in-process action record.
	keepRecords = 4096
)

type entry struct {
	instance uint64
	app      app.AppIdT
	alias    string
	mode     ModeE
	ops      []string
}

// covers reports whether the entry names op; an entry naming none covers
// every operation its app exposes to agents.
func (inst *entry) covers(op string) (ok bool) {
	return len(inst.ops) == 0 || slices.Contains(inst.ops, op)
}

type resultRef struct {
	instance uint64
	typ      reflect.Type
	data     []byte
}

type callRec struct {
	key      string
	callId   string
	instance uint64
	app      app.AppIdT
	spec     app.OperationSpec
	// routed is true once the call reached the instance; until then, or
	// when the dispatcher decided it, outcome is all there is.
	routed  bool
	outcome opwire.Outcome
	// job is a capture's job id.
	job string
	// ref is the result reference minted for the call's result.
	ref        string
	finalNoted bool
	argsDigest string
}

type task struct {
	id            string
	handle        string
	actor         app.AppIdT
	actorInstance uint64
	plan          string
	entries       map[uint64]*entry
	destinations  []string
	callsBudget   int
	callsUsed     int
	deadline      time.Time
	epoch         uint64
	revoked       string
	created       time.Time
	test          bool
	keys          map[string]*callRec
	lastRead      map[uint64]map[string]uint64
	refs          map[string]*resultRef
}

func randomHex(n int) (s string) {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func digest(s string) (d string) {
	sum := blake3.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func phaseOutcome(p opwire.PhaseE, reason string) (out opwire.Outcome) {
	return opwire.Outcome{Phase: p, Reason: reason}
}

// resolve finds the task a handle names, for the sender presenting it. The
// caller holds mu.
func (inst *Service) resolve(handle string, msg *app.Msg) (t *task, out opwire.Outcome, ok bool) {
	t = inst.tasks[handle]
	switch {
	case t == nil:
		out = phaseOutcome(opwire.PhaseDenied, "the grant handle is not valid")
	case t.actor != msg.Sender || t.actorInstance != msg.SenderInstance:
		out = phaseOutcome(opwire.PhaseDenied, "the grant handle belongs to another instance")
	case t.revoked != "":
		out = phaseOutcome(opwire.PhaseDenied, "the task ended: "+t.revoked)
	case time.Now().After(t.deadline):
		out = phaseOutcome(opwire.PhaseDenied, "the task's deadline passed")
	default:
		ok = true
	}
	return
}

// grant creates a test grant (ADR-0269 §SD6 "Test grants").
func (inst *Service) grant(msg *app.Msg) (rep wireGrantReply) {
	rep.V = wireVersion
	req, err := decode[wireGrantRequest](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if !inst.cfg.TestGrants {
		rep.Reason = "a grant needs the person's approval in host chrome; this host issues test grants only, and they are off"
		return
	}
	if inst.cfg.Host == nil {
		rep.Reason = "no window host"
		return
	}
	if len(req.Entries) == 0 {
		rep.Reason = "a grant names at least one instance"
		return
	}
	open := make(map[uint64]opwire.InstanceInfo)
	for _, info := range inst.cfg.Host.OpsInstances() {
		open[info.Key] = info
	}
	t := &task{
		id: "task-" + randomHex(6), handle: randomHex(16), actor: msg.Sender, actorInstance: msg.SenderInstance,
		plan: req.Plan, entries: make(map[uint64]*entry), destinations: req.Destinations,
		callsBudget: int(req.Calls), deadline: time.Now().Add(DefaultDeadline), epoch: 1, created: time.Now(), test: true,
		keys: make(map[string]*callRec), lastRead: make(map[uint64]map[string]uint64), refs: make(map[string]*resultRef),
	}
	if t.callsBudget == 0 {
		t.callsBudget = DefaultCallBudget
	}
	if req.DeadlineSecs > 0 {
		t.deadline = time.Now().Add(time.Duration(req.DeadlineSecs) * time.Second)
	}
	for _, e := range req.Entries {
		info, isOpen := open[e.Instance]
		if !isOpen {
			rep.Reason = "no open window by the key " + strconv.FormatUint(e.Instance, 10)
			return
		}
		mode := ParseMode(e.Mode)
		if mode != ModeObserve && mode != ModeAct {
			rep.Reason = "a test grant takes observe or act; suggest needs the person's proposal surface"
			return
		}
		t.entries[e.Instance] = &entry{instance: e.Instance, app: info.App, alias: info.Alias, mode: mode, ops: e.Operations}
	}
	inst.mu.Lock()
	inst.tasks[t.handle] = t
	inst.mu.Unlock()
	for k := range t.entries {
		inst.cfg.Host.OpsAttach(k, true)
	}
	inst.log.Info().Str("task", t.id).Str("actor", string(t.actor)).Int("entries", len(t.entries)).Msg("agent: test grant issued")
	rep.Ok, rep.Task, rep.Handle = true, t.id, t.handle
	return
}

// call checks one call against the grant and routes it.
func (inst *Service) call(msg *app.Msg) (rep wireCallReply) {
	rep.V = wireVersion
	req, err := decode[wireCall](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok = true
	if req.Key == "" {
		rep.Outcome = wireOutcomeOf(phaseOutcome(opwire.PhaseRefused, "every call carries a key"), "", "")
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		inst.record(nil, &callRec{key: req.Key, instance: req.Instance}, "dispatch", out)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	if prev, seen := t.keys[req.Key]; seen {
		inst.mu.Unlock()
		rep.Outcome = inst.outcomeOf(t, prev, 0)
		return
	}
	rec := &callRec{key: req.Key, instance: req.Instance, argsDigest: digest(req.Args)}
	t.keys[req.Key] = rec
	out, spec, e := inst.check(t, req)
	rec.spec = spec
	if e != nil {
		rec.app = e.app
	}
	if out.Phase != opwire.PhaseUnspecified {
		rec.outcome = out
		inst.mu.Unlock()
		inst.record(t, rec, "dispatch", out)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	t.callsUsed++
	expects := make(map[string]uint64, len(spec.Writes))
	for _, r := range spec.Writes {
		if v, given := req.Expects[r]; given {
			expects[r] = v
		} else if v, read := t.lastRead[req.Instance][r]; read {
			expects[r] = v
		}
	}
	inst.nextCall++
	rec.callId = t.id + "-" + strconv.FormatUint(inst.nextCall, 10)
	alias := e.alias
	inst.mu.Unlock()

	args, err := encodeArgs(spec, req.Args)
	if err != nil {
		out = phaseOutcome(opwire.PhaseRefused, "the arguments do not fit the schema: "+err.Error())
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	creq := opwire.CallRequest{V: opwire.WireVersion, CallId: rec.callId, Args: args, Expects: expects,
		Writer: opwire.WriterTask(t.id), Key: req.Key, Reason: req.Reason}
	payload, err := buscodec.Encode(creq)
	if err != nil {
		out = phaseOutcome(opwire.PhaseFailed, "encode: "+err.Error())
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	raw, err := inst.busClient.RequestWithTimeout(opwire.Subject(alias, req.Instance, spec.Name), payload, DefaultTimeout)
	if err != nil {
		out = phaseOutcome(opwire.PhaseFailed, "the instance did not answer: "+err.Error())
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	creply, err := buscodec.Decode[opwire.CallReply](raw)
	if err != nil {
		out = phaseOutcome(opwire.PhaseFailed, "undecodable reply")
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	inst.settle(t, rec, creply.Outcome, spec.Class == app.OperationClassCommand && creply.Outcome.Phase == opwire.PhaseAccepted)
	inst.record(t, rec, "dispatch", creply.Outcome)
	inst.mu.Lock()
	rep.Outcome = inst.outcomeOf(t, rec, 0)
	inst.mu.Unlock()
	return
}

// check decides a call at the dispatcher, or returns the zero outcome when
// it may be routed. The caller holds mu.
func (inst *Service) check(t *task, req wireCall) (out opwire.Outcome, spec app.OperationSpec, e *entry) {
	e = t.entries[req.Instance]
	if e == nil {
		out = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover this instance")
		return
	}
	m, ok := inst.cfg.Registry.LookupManifest(e.app)
	if !ok || m.Operations == nil {
		out = phaseOutcome(opwire.PhaseRefused, "the instance's app serves no catalog")
		return
	}
	spec, ok = m.Operations.Lookup(req.Operation)
	switch {
	case !ok:
		out = phaseOutcome(opwire.PhaseRefused, "no such operation")
	case !spec.Agents:
		out = phaseOutcome(opwire.PhaseDenied, "the operation is not exposed to agents")
	case !e.covers(spec.Name):
		out = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover this operation")
	case spec.Effect != app.OperationEffectNone && e.mode == ModeObserve:
		out = phaseOutcome(opwire.PhaseInputRequired, "observe mode: the person would have to raise the mode")
	case spec.Effect == app.OperationEffectConsequential:
		out = phaseOutcome(opwire.PhaseInputRequired, "a consequential command needs the person's confirmation")
	case t.callsUsed >= t.callsBudget:
		out = phaseOutcome(opwire.PhaseInputRequired, "the task's call budget is spent")
	}
	return
}

// encodeArgs validates the model's JSON against the declared argument type
// and re-encodes it as CBOR for the instance.
func encodeArgs(spec app.OperationSpec, js string) (args []byte, err error) {
	v, err := opjson.Decode([]byte(js), spec.Args)
	if err != nil || spec.Args == nil {
		return
	}
	args, err = buscodec.Encode(v.Interface())
	return
}

// settle records what the instance answered: read revisions for a query,
// a result reference for a result.
func (inst *Service) settle(t *task, rec *callRec, out opwire.Outcome, routed bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	rec.routed = routed
	rec.outcome = out
	inst.absorb(t, rec, out)
}

// absorb takes in an outcome: revisions a query read, and the result. The
// caller holds mu.
func (inst *Service) absorb(t *task, rec *callRec, out opwire.Outcome) {
	if rec.spec.Class != app.OperationClassCommand && out.Phase == opwire.PhaseCompleted && len(out.Revisions) > 0 {
		read := t.lastRead[rec.instance]
		if read == nil {
			read = make(map[string]uint64)
			t.lastRead[rec.instance] = read
		}
		for r, v := range out.Revisions {
			read[r] = v
		}
	}
	if rec.spec.Class == app.OperationClassCommand && (out.Phase == opwire.PhaseApplied || out.Phase == opwire.PhaseRendered) {
		// A command's own writes are what the task last saw of them.
		read := t.lastRead[rec.instance]
		if read == nil {
			read = make(map[string]uint64)
			t.lastRead[rec.instance] = read
		}
		for r, v := range out.Revisions {
			read[r] = v
		}
	}
	if rec.ref == "" && len(out.Result) > 0 && rec.spec.Result != nil {
		rec.ref = "ref-" + randomHex(8)
		t.refs[rec.ref] = &resultRef{instance: rec.instance, typ: rec.spec.Result, data: out.Result}
	}
}

// outcomeOf returns a call's current outcome, asking the host while a
// routed call is not final and waiting up to wait for it to become final.
// The caller holds mu; it is released while waiting.
func (inst *Service) outcomeOf(t *task, rec *callRec, wait time.Duration) (w wireOutcome) {
	deadline := time.Now().Add(wait)
	for {
		if rec.job != "" && rec.outcome.Phase == opwire.PhaseRunning && inst.cfg.Host != nil {
			if st, ok := inst.cfg.Host.OpsCaptureStatus(rec.job); ok {
				rec.outcome = phaseOutcome(st.Phase, st.Reason)
			}
		} else if rec.routed && !rec.outcome.Phase.Final() && inst.cfg.Host != nil {
			if out, ok := inst.cfg.Host.OpsStatus(rec.instance, rec.callId); ok {
				rec.outcome = out
				inst.absorb(t, rec, out)
			}
		}
		final := rec.outcome.Phase.Final()
		if final && !rec.finalNoted {
			rec.finalNoted = true
			inst.mu.Unlock()
			inst.record(t, rec, "final", rec.outcome)
			inst.mu.Lock()
		}
		if final || !time.Now().Before(deadline) {
			break
		}
		inst.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		inst.mu.Lock()
	}
	w = wireOutcomeOf(rec.outcome, rec.ref, rec.job)
	return
}

func wireOutcomeOf(out opwire.Outcome, ref string, job string) (w wireOutcome) {
	return wireOutcome{Phase: out.Phase.String(), Reason: out.Reason, AsOf: out.AsOf, Revisions: out.Revisions, ResultRef: ref, Job: job}
}

func (inst *Service) status(msg *app.Msg) (rep wireCallReply) {
	rep.V = wireVersion
	req, err := decode[wireStatus](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok = true
	inst.mu.Lock()
	defer inst.mu.Unlock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	rec, seen := t.keys[req.Key]
	if !seen {
		rep.Outcome = wireOutcomeOf(phaseOutcome(opwire.PhaseRefused, "no call by that key"), "", "")
		return
	}
	wait := min(time.Duration(req.WaitMs)*time.Millisecond, MaxStatusWait)
	rep.Outcome = inst.outcomeOf(t, rec, wait)
	return
}

func (inst *Service) cancel(msg *app.Msg) (rep wireCallReply) {
	rep.V = wireVersion
	req, err := decode[wireCancel](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok = true
	inst.mu.Lock()
	defer inst.mu.Unlock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	rec, seen := t.keys[req.Key]
	if !seen {
		rep.Outcome = wireOutcomeOf(phaseOutcome(opwire.PhaseRefused, "no call by that key"), "", "")
		return
	}
	if rec.routed && rec.outcome.Phase == opwire.PhaseAccepted && inst.cfg.Host != nil {
		if o, found := inst.cfg.Host.OpsCancel(rec.instance, rec.callId); found {
			rec.outcome = o
		}
	}
	rep.Outcome = inst.outcomeOf(t, rec, 0)
	return
}

func (inst *Service) read(msg *app.Msg) (rep wireReadReply) {
	rep.V = wireVersion
	req, err := decode[wireRead](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		rep.Reason = out.Reason
		return
	}
	ref := t.refs[req.Ref]
	var job *callRec
	if ref == nil {
		for _, rec := range t.keys {
			if rec.job != "" && rec.job == req.Ref {
				job = rec
			}
		}
	}
	inst.mu.Unlock()
	switch {
	case ref != nil:
		ptr := reflect.New(ref.typ)
		if err = buscodec.Default().Decode(ref.data, ptr.Interface()); err != nil {
			rep.Reason = "undecodable result"
			return
		}
		var js []byte
		js, err = opjson.Encode(ptr.Elem().Interface())
		if err != nil {
			rep.Reason = err.Error()
			return
		}
		rep.Ok, rep.MediaType, rep.Text = true, "application/json", string(js)
	case job != nil && inst.cfg.Host != nil:
		st, found := inst.cfg.Host.OpsCaptureStatus(job.job)
		if !found || st.Phase != opwire.PhaseCompleted {
			rep.Reason = "the capture is not complete"
			return
		}
		rep.Ok, rep.MediaType, rep.Path = true, st.MediaType, st.Path
	default:
		rep.Reason = "no result or artifact by that reference in this task"
	}
	return
}

func (inst *Service) capture(msg *app.Msg) (rep wireCallReply) {
	rep.V = wireVersion
	req, err := decode[wireCapture](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok = true
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
	rec := &callRec{key: req.Key, instance: req.Instance, spec: app.OperationSpec{Name: "capture"}}
	t.keys[req.Key] = rec
	e := t.entries[req.Instance]
	inst.mu.Unlock()
	if e == nil {
		out = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover this instance")
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	rec.app = e.app
	job, err := inst.cfg.Host.OpsCapture(req.Instance)
	if err != nil {
		out = phaseOutcome(opwire.PhaseRefused, err.Error())
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	inst.mu.Lock()
	rec.job = job
	rec.outcome = phaseOutcome(opwire.PhaseRunning, "")
	rep.Outcome = inst.outcomeOf(t, rec, 0)
	inst.mu.Unlock()
	inst.record(t, rec, "dispatch", rec.outcome)
	return
}

func (inst *Service) list(msg *app.Msg) (rep wireListReply) {
	rep.V = wireVersion
	req, err := decode[wireHandle](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	inst.mu.Unlock()
	if !ok {
		rep.Reason = out.Reason
		return
	}
	for _, info := range inst.cfg.Host.OpsInstances() {
		e := t.entries[info.Key]
		if e == nil {
			continue
		}
		rep.Instances = append(rep.Instances, wireInstance{Instance: info.Key, App: string(info.App), Title: info.Title, Mode: e.mode.String(), Ops: info.Ops})
	}
	rep.Ok = true
	return
}

// detach removes one instance from the task: its queued calls expire.
func (inst *Service) detach(msg *app.Msg) (rep wireAck) {
	rep.V = wireVersion
	req, err := decode[wireHandle](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		rep.Reason = out.Reason
		return
	}
	e := t.entries[req.Instance]
	delete(t.entries, req.Instance)
	ids := t.queuedOn(req.Instance)
	inst.mu.Unlock()
	if e == nil {
		rep.Reason = "the grant does not cover this instance"
		return
	}
	inst.cfg.Host.OpsExpire(req.Instance, ids, "detached")
	inst.cfg.Host.OpsAttach(req.Instance, false)
	rep.Ok = true
	return
}

// stop ends the task: the epoch moves, queued calls expire, the grant is
// revoked and keeps its id for the record.
func (inst *Service) stop(msg *app.Msg) (rep wireAck) {
	rep.V = wireVersion
	req, err := decode[wireHandle](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		rep.Reason = out.Reason
		return
	}
	t.revoked = "stopped"
	t.epoch++
	queued := make(map[uint64][]string)
	for k := range t.entries {
		queued[k] = t.queuedOn(k)
	}
	inst.mu.Unlock()
	for k, ids := range queued {
		if len(ids) > 0 {
			inst.cfg.Host.OpsExpire(k, ids, "task stopped")
		}
		inst.cfg.Host.OpsAttach(k, false)
	}
	rep.Ok = true
	return
}

// queuedOn lists the task's routed calls on an instance that may still be
// queued. The caller holds mu.
func (inst *task) queuedOn(instance uint64) (ids []string) {
	for _, rec := range inst.keys {
		if rec.instance == instance && rec.routed && rec.outcome.Phase == opwire.PhaseAccepted {
			ids = append(ids, rec.callId)
		}
	}
	return
}
