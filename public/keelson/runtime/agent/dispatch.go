package agent

import (
	"crypto/rand"
	"encoding/hex"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
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
	OpsRevisions(key uint64) (revs map[string]uint64, ok bool)
	OpsUndo(key uint64, callId string) (ok bool)
	OpsUndoStatus(key uint64, callId string) (status string, ok bool)
	OpsLogSince(key uint64, seq uint64) (entries []opengine.LogEntry, latest uint64, ok bool)
	OpsOpen(appId app.AppIdT, kind string, cfg []byte) (key uint64, err error)
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
	instance  uint64
	typ       reflect.Type
	data      []byte
	confined  bool
	untrusted bool
	source    string
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
	// args is the model's JSON, kept only under a test grant, for the
	// actions file.
	args string
	// heldBy is the widening a call outside the grant waits on.
	heldBy *request
	// req and entry are what route sent, with the expectations it sent, so
	// a queued command can turn back into a proposal.
	req   wireCall
	entry *entry
	// proposal is set while the call waits on the person: a suggestion, or
	// a consequential command awaiting confirmation (ADR-0269 §SD5).
	proposal *proposal
	created  time.Time
}

// proposal is a command the person accepts or rejects in host chrome.
type proposal struct {
	// confirm marks a consequential command: accepting it is its
	// confirmation, and it is asked for every time.
	confirm bool
	expects map[string]uint64
	// taken marks a proposal the person accepted while it is being routed;
	// the phase moves when the instance has queued it.
	taken bool
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
	conversation  string
	keys          map[string]*callRec
	lastRead      map[uint64]map[string]uint64
	refs          map[string]*resultRef
	// turnSeq is, per window, the log sequence of the task's last turn;
	// readSinceTurn what it read since (ADR-0269 §SD8).
	turnSeq       map[uint64]uint64
	readSinceTurn map[uint64]map[string]uint64
	// launches are the apps the task may open windows of; launched the
	// windows it opened, which pass to the person when it ends.
	launches map[app.AppIdT]*launchEntry
	launched map[uint64]bool
}

// launchEntry is one app a task may open windows of (ADR-0269 §SD6).
type launchEntry struct {
	mode  ModeE
	count int
	used  int
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

// newTask builds a task. The caller registers it.
func (inst *Service) newTask(actor app.AppIdT, actorInstance uint64, conversation string, plan string, destinations []string,
	calls uint32, deadline time.Duration, test bool) (t *task) {
	t = &task{
		id: "task-" + randomHex(6), handle: randomHex(16), actor: actor, actorInstance: actorInstance, conversation: conversation,
		plan: plan, entries: make(map[uint64]*entry), destinations: destinations,
		callsBudget: int(calls), deadline: time.Now().Add(DefaultDeadline), epoch: 1, created: time.Now(), test: test,
		keys: make(map[string]*callRec), lastRead: make(map[uint64]map[string]uint64), refs: make(map[string]*resultRef),
		turnSeq: make(map[uint64]uint64), readSinceTurn: make(map[uint64]map[string]uint64),
		launches: make(map[app.AppIdT]*launchEntry), launched: make(map[uint64]bool),
	}
	if t.callsBudget == 0 {
		t.callsBudget = DefaultCallBudget
	}
	if deadline > 0 {
		t.deadline = time.Now().Add(deadline)
	}
	return
}

// testGrant issues a grant without the person (ADR-0269 §SD6 "Test
// grants"), on the headless host behind TestGrantsEnv.
func (inst *Service) testGrant(msg *app.Msg, req wireGrantRequest) (rep wireGrantReply) {
	rep.V = wireVersion
	if inst.cfg.Host == nil {
		rep.Reason = "no window host"
		return
	}
	if len(req.Entries) == 0 && len(req.Launches) == 0 {
		rep.Reason = "a grant names at least one instance or an app to open"
		inst.recordGrantRefusal(msg, req, rep.Reason)
		return
	}
	t := inst.newTask(msg.Sender, msg.SenderInstance, req.Conversation, req.Plan, req.Destinations, req.Calls,
		time.Duration(req.DeadlineSecs)*time.Second, true)
	inst.addLaunches(t, req.Launches)
	for _, e := range req.Entries {
		info, isOpen := inst.openInstance(e.Instance)
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
	for k := range t.entries {
		inst.startTurnAt(t, k)
	}
	inst.mu.Unlock()
	for k := range t.entries {
		inst.attach(k)
	}
	inst.log.Info().Str("task", t.id).Str("actor", string(t.actor)).Int("entries", len(t.entries)).Msg("agent: test grant issued")
	rep.Ok, rep.Task, rep.Handle, rep.Phase = true, t.id, t.handle, reqStateApproved.String()
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
	rec := &callRec{key: req.Key, instance: req.Instance, argsDigest: digest(req.Args), created: time.Now()}
	if t.test {
		rec.args = req.Args
	}
	t.keys[req.Key] = rec
	out, spec, e, need, mode := inst.check(t, req)
	rec.spec = spec
	if e != nil {
		rec.app = e.app
	}
	if out.Phase == opwire.PhaseInputRequired && need != 0 && !t.test {
		inst.holdForWidening(t, rec, req, need, mode)
	}
	if out.Phase == opwire.PhaseProposed {
		if _, aerr := encodeArgs(spec, req.Args); aerr != nil {
			out = schemaRefusal(spec, aerr)
		} else {
			t.callsUsed++
			rec.req, rec.entry = req, e
			rec.proposal = &proposal{confirm: spec.Effect == app.OperationEffectConsequential,
				expects: inst.expectsFor(t, req, spec)}
		}
	}
	if out.Phase != opwire.PhaseUnspecified {
		rec.outcome = out
		inst.mu.Unlock()
		inst.record(t, rec, "dispatch", out)
		rep.Outcome = wireOutcomeOf(out, "", "")
		// A held call is not final; the caller polls status.
		rep.Outcome.Held = rec.heldBy != nil
		return
	}
	t.callsUsed++
	inst.mu.Unlock()
	inst.route(t, rec, req, spec, e)
	inst.mu.Lock()
	rep.Outcome = inst.outcomeOf(t, rec, 0)
	inst.mu.Unlock()
	return
}

// expectsFor is what a command expects of the resources it writes: what
// the call names, else what the task last read. The caller holds mu.
func (inst *Service) expectsFor(t *task, req wireCall, spec app.OperationSpec) (expects map[string]uint64) {
	expects = make(map[string]uint64, len(spec.Writes))
	for _, r := range spec.Writes {
		if v, given := req.Expects[r]; given {
			expects[r] = v
		} else if v, read := t.lastRead[req.Instance][r]; read {
			expects[r] = v
		}
	}
	return
}

// route sends a checked call to its instance and settles what it answers.
func (inst *Service) route(t *task, rec *callRec, req wireCall, spec app.OperationSpec, e *entry) {
	inst.mu.Lock()
	expects := inst.expectsFor(t, req, spec)
	inst.nextCall++
	rec.callId = t.id + "-" + strconv.FormatUint(inst.nextCall, 10)
	rec.req, rec.entry = req, e
	rec.req.Expects = expects
	alias := e.alias
	inst.mu.Unlock()

	args, err := encodeArgs(spec, req.Args)
	if err != nil {
		out := schemaRefusal(spec, err)
		inst.settle(t, rec, out, false)
		inst.record(t, rec, "dispatch", out)
		return
	}
	refData, why := inst.resolveRefs(t, spec, req)
	if why != "" {
		out := phaseOutcome(opwire.PhaseRefused, why)
		inst.settle(t, rec, out, false)
		inst.record(t, rec, "dispatch", out)
		return
	}
	creq := opwire.CallRequest{V: opwire.WireVersion, CallId: rec.callId, Args: args, Expects: expects,
		Writer: opwire.WriterTask(t.id), Key: req.Key, Reason: req.Reason, RefData: refData, OnBehalfOf: inst.onBehalfOf(t, e)}
	payload, err := buscodec.Encode(creq)
	if err != nil {
		out := phaseOutcome(opwire.PhaseFailed, "encode: "+err.Error())
		inst.settle(t, rec, out, false)
		inst.record(t, rec, "dispatch", out)
		return
	}
	raw, err := inst.busClient.RequestWithTimeout(opwire.Subject(alias, req.Instance, spec.Name), payload, DefaultTimeout)
	if err != nil {
		out := phaseOutcome(opwire.PhaseFailed, "the instance did not answer: "+err.Error())
		inst.settle(t, rec, out, false)
		inst.record(t, rec, "dispatch", out)
		return
	}
	creply, err := buscodec.Decode[opwire.CallReply](raw)
	if err != nil {
		out := phaseOutcome(opwire.PhaseFailed, "undecodable reply")
		inst.settle(t, rec, out, false)
		inst.record(t, rec, "dispatch", out)
		return
	}
	inst.settle(t, rec, creply.Outcome, spec.Class == app.OperationClassCommand && creply.Outcome.Phase == opwire.PhaseAccepted)
	inst.record(t, rec, "dispatch", creply.Outcome)
}

// check decides a call at the dispatcher, or returns the zero outcome when
// it may be routed. For an input_required outcome, need says what a
// widening would add and mode the mode it would take. The caller holds mu.
func (inst *Service) check(t *task, req wireCall) (out opwire.Outcome, spec app.OperationSpec, e *entry, need needE, mode ModeE) {
	e = t.entries[req.Instance]
	appId := app.AppIdT("")
	if e != nil {
		appId = e.app
	} else if info, open := inst.openInstance(req.Instance); open {
		appId = info.App
	} else {
		out = phaseOutcome(opwire.PhaseRefused, "no open window by that key")
		return
	}
	m, ok := inst.cfg.Registry.LookupManifest(appId)
	if !ok || m.Operations == nil {
		out = phaseOutcome(opwire.PhaseRefused, "the instance's app serves no catalog")
		return
	}
	spec, ok = m.Operations.Lookup(req.Operation)
	mode = ModeObserve
	if ok && spec.Effect != app.OperationEffectNone {
		mode = ModeAct
	}
	switch {
	case !ok:
		out = phaseOutcome(opwire.PhaseRefused, "no such operation")
	case !spec.Agents:
		out = phaseOutcome(opwire.PhaseDenied, "the operation is not exposed to agents")
	case e == nil:
		out, need = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover this instance; the person is asked"), needInstance
	case !e.covers(spec.Name):
		out, need, mode = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover this operation; the person is asked"), needOperation, e.mode
	case spec.Effect != app.OperationEffectNone && e.mode == ModeObserve:
		out, need = phaseOutcome(opwire.PhaseInputRequired, "observe mode: the person is asked to raise the mode"), needMode
	case spec.Effect != app.OperationEffectNone && inst.holder(req.Instance, t) != nil:
		out = phaseOutcome(opwire.PhaseRefused, "busy: another task holds the instance")
	case spec.Effect != app.OperationEffectNone && inst.isPaused(t, req.Instance):
		ch, _ := inst.pausedBy(t, req.Instance)
		out = phaseOutcome(opwire.PhaseRefused, pausedReason(ch, t))
	case t.callsUsed >= t.callsBudget:
		out, need, mode = phaseOutcome(opwire.PhaseInputRequired, "the task's call budget is spent; the person is asked"), needBudget, e.mode
	case spec.Effect == app.OperationEffectConsequential && t.test:
		out = phaseOutcome(opwire.PhaseInputRequired, "a consequential command needs the person's confirmation")
	case spec.Effect == app.OperationEffectConsequential:
		out = phaseOutcome(opwire.PhaseProposed, "a consequential command: the person confirms it")
	case spec.Effect != app.OperationEffectNone && e.mode == ModeSuggest:
		out = phaseOutcome(opwire.PhaseProposed, "suggest mode: the person accepts or rejects it")
	}
	if t.test {
		// Nobody answers a test grant's widening.
		out.Reason = strings.TrimSuffix(strings.TrimSuffix(out.Reason, "; the person is asked"), ": the person is asked to raise the mode")
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

// schemaRefusal refuses arguments that do not fit the operation's type, with
// the schema they have to fit.
func schemaRefusal(spec app.OperationSpec, err error) (out opwire.Outcome) {
	out = phaseOutcome(opwire.PhaseRefused, "the arguments do not fit the schema: "+err.Error())
	if s, serr := opjson.Schema(spec.Args); serr == nil {
		out.Remedy = &opwire.Remedy{ArgsSchema: string(s)}
	}
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
		since := t.readSinceTurn[rec.instance]
		if since == nil {
			since = make(map[string]uint64)
			t.readSinceTurn[rec.instance] = since
		}
		for r, v := range out.Revisions {
			read[r] = v
			since[r] = out.Seq
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
		t.refs[rec.ref] = &resultRef{instance: rec.instance, typ: rec.spec.Result, data: out.Result,
			confined: out.Confined, untrusted: rec.spec.Untrusted,
			source: "window " + strconv.FormatUint(rec.instance, 10) + " · " + string(rec.app) + " · " + rec.spec.Name}
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
				confined := rec.outcome.Confined
				rec.outcome = phaseOutcome(st.Phase, st.Reason)
				rec.outcome.Confined = confined
			}
		} else if rec.routed && !rec.outcome.Phase.Final() && inst.cfg.Host != nil {
			if out, ok := inst.cfg.Host.OpsStatus(rec.instance, rec.callId); ok {
				rec.outcome = out
				inst.absorb(t, rec, out)
			}
		}
		final := rec.outcome.Phase.Final() || (rec.outcome.Phase == opwire.PhaseInputRequired && rec.heldBy == nil)
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
	w.Held = rec.heldBy != nil && rec.outcome.Phase == opwire.PhaseInputRequired
	w.Confined = rec.outcome.Confined
	return
}

func wireOutcomeOf(out opwire.Outcome, ref string, job string) (w wireOutcome) {
	w.Phase, w.Reason, w.AsOf, w.Revisions, w.ResultRef, w.Job = out.Phase.String(), out.Reason, out.AsOf, out.Revisions, ref, job
	if out.Remedy != nil {
		w.Remedy = &wireRemedy{Destinations: out.Remedy.Destinations, ArgsSchema: out.Remedy.ArgsSchema}
	}
	return
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
	wait := min(time.Duration(req.WaitMs)*time.Millisecond, MaxStatusWait)
	if req.Handle == "" {
		// A request's key: answered without a grant, to the requester only.
		deadline := time.Now().Add(wait)
		for {
			rep.Outcome = inst.requestStatus(req.Key, msg)
			if rep.Outcome.Phase != reqStatePending.String() || !time.Now().Before(deadline) {
				return
			}
			inst.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			inst.mu.Lock()
		}
	}
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
	if rec.proposal != nil && rec.outcome.Phase == opwire.PhaseProposed {
		rec.outcome = phaseOutcome(opwire.PhaseCancelled, "withdrawn by cancel")
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
	case ref != nil && ref.confined && !inst.modelLocal():
		// Confined content the coordinator's model may not see stays a
		// handle it can pass and never read (ADR-0269 §SD7).
		rep.Ok, rep.Confined, rep.DataHandle, rep.Source = true, true, req.Ref, ref.source
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
		rep.Ok, rep.MediaType, rep.Text, rep.Confined = true, "application/json", string(js), ref.confined
		rep.Untrusted, rep.Source = ref.untrusted, ref.source
		if ref.untrusted {
			inst.taint(t)
		}
	case job != nil && inst.cfg.Host != nil:
		st, found := inst.cfg.Host.OpsCaptureStatus(job.job)
		if !found || st.Phase != opwire.PhaseCompleted {
			rep.Reason = "the capture is not complete"
			return
		}
		if job.outcome.Confined && !inst.modelLocal() {
			rep.Reason = "a confined capture stays an artifact handle"
			return
		}
		// Every capture is untrusted: it shows whatever the window holds.
		rep.Ok, rep.MediaType, rep.Path, rep.Confined = true, st.MediaType, st.Path, job.outcome.Confined
		rep.Untrusted, rep.Source = true, "window "+strconv.FormatUint(job.instance, 10)+" · capture"
		inst.taint(t)
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
	info, _ := inst.openInstance(req.Instance)
	inst.mu.Lock()
	rec.job = job
	rec.outcome = phaseOutcome(opwire.PhaseRunning, "")
	rec.outcome.Confined = info.Confined
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
	tainted := false
	for _, info := range inst.cfg.Host.OpsInstances() {
		e := t.entries[info.Key]
		if e == nil {
			continue
		}
		title := info.Title
		if info.Confined {
			title = ""
		} else {
			tainted = true
		}
		load, reason := loadOf(info)
		rep.Instances = append(rep.Instances, wireInstance{Instance: info.Key, App: string(info.App), Title: title,
			Mode: e.mode.String(), Ops: info.Ops, Confined: info.Confined, Load: load.String(), LoadReason: reason})
	}
	if tainted {
		// Titles are untrusted text (ADR-0269 §SD7).
		inst.taint(t)
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
	covered := ok && t.entries[req.Instance] != nil
	inst.mu.Unlock()
	if !ok {
		rep.Reason = out.Reason
		return
	}
	if !covered {
		rep.Reason = "the grant does not cover this instance"
		return
	}
	inst.detachEntry(t, req.Instance, "detached")
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
	inst.mu.Unlock()
	if !ok {
		rep.Reason = out.Reason
		return
	}
	inst.endTask(t, "stopped")
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
