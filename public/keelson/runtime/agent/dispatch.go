package agent

import (
	"crypto/rand"
	"encoding/hex"
	"image"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
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
	// The host renders captures for the capture service (ADR-0281).
	capture.SourceI
	OpsRevisions(key uint64) (revs map[string]uint64, ok bool)
	OpsUndo(key uint64, callId string) (ok bool)
	OpsUndoStatus(key uint64, callId string) (status string, ok bool)
	OpsLogSince(key uint64, seq uint64) (entries []opengine.LogEntry, latest uint64, ok bool)
	OpsOpen(appId app.AppIdT, kind string, cfg []byte) (key uint64, err error)
	// OpsArrange, OpsRaise and OpsPlace queue a change to the desktop for
	// the next frame (ADR-0276 §SD5).
	OpsArrange(command string, keys []uint64) (err error)
	OpsRaise(key uint64) (err error)
	OpsPlace(key uint64, x, y, w, h float32) (err error)
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
	DefaultCallsMin   = 20
	DefaultCallsMax   = 1000
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
	// turn and cause are what the coordinator said the call belongs to and
	// was asked for by (ADR-0277 §SD1); conversation is its conversation
	// when no task names one.
	turn         string
	cause        option.Option[trail.Cause]
	conversation string
	// title and why are the model's title for the call and the reason it
	// gave, bounded, for the dispatch row.
	title string
	why   string
	// actor and actorInstance are the caller of a call that resolved to no
	// task, which has no other record of who asked.
	actor         app.AppIdT
	actorInstance uint64
	spec          app.OperationSpec
	// routed is true once the call reached the instance; until then, or
	// when the dispatcher decided it, outcome is all there is.
	routed  bool
	outcome opwire.Outcome
	// job is a capture's job id; capture what the capture record needs.
	job     string
	capture *captureRec
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
	// pausedAt is, per window, the log sequence of the change that paused
	// the task there, until a turn lifts it: the trail records a pause once
	// and its lift once, not every change between.
	pausedAt map[uint64]uint64
	// launches are the apps the task may open windows of; launched the
	// windows it opened, which pass to the person when it ends.
	launches map[app.AppIdT]*launchEntry
	launched map[uint64]bool
	// desktop is the task's mode over the desktop as a whole; act lets it
	// arrange every window (ADR-0276 §SD4).
	desktop ModeE
	// ceiling is the most the coordinator's settings let the model do
	// (ADR-0280); nil is a coordinator that set none. nextChange is the
	// earliest a paced task's next visible change may land.
	ceiling    *Ceiling
	nextChange time.Time
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

// causeOf is the cause a call states, none when it names no model call.
func causeOf(req wireCall) (cause option.Option[trail.Cause]) {
	return wireCause{Turn: req.Turn, ModelCall: req.ModelCall, ToolCall: req.ToolCall, ToolIndex: req.ToolIndex}.cause()
}

// cause is the cause a request states, none when it names no model call.
func (inst wireCause) cause() (cause option.Option[trail.Cause]) {
	if inst.ModelCall == "" {
		return
	}
	c := trail.Cause{ModelCall: inst.ModelCall, ToolIndex: inst.ToolIndex}
	if inst.ToolCall != "" {
		c.ToolCall = option.Some(inst.ToolCall)
	}
	return option.Some(c)
}

// maxCallTitleRunes and maxCallReasonRunes bound the model's title and
// reason for a call on its row: the coordinator states them, and the row
// keeps them in every retention mode.
const (
	maxCallTitleRunes  = 60
	maxCallReasonRunes = 200
)

// boundLabel is s on one line, at most n runes, cut with an ellipsis.
func boundLabel(s string, n int) (out string) {
	out = strings.Join(strings.Fields(s), " ")
	if r := []rune(out); len(r) > n {
		out = string(r[:n-1]) + "…"
	}
	return
}

// recordAsked writes the action row of a request the model's tool call
// made outside a window — describe, help or list — as one final row. A
// request the coordinator made on its own names no model call and leaves
// none. t is the task a list ran under, nil otherwise.
func (inst *Service) recordAsked(msg *app.Msg, t *task, operation string, key string, conversation string, asked wireCause, appName string, ok bool, reason string) {
	if asked.ModelCall == "" {
		return
	}
	out := phaseOutcome(opwire.PhaseCompleted, "")
	if !ok {
		out = phaseOutcome(opwire.PhaseRefused, reason)
	}
	inst.record(t, &callRec{key: key, app: app.AppIdT(appName), turn: asked.Turn, cause: asked.cause(), conversation: conversation,
		actor: msg.Sender, actorInstance: msg.SenderInstance, spec: app.OperationSpec{Name: operation, Effect: app.OperationEffectNone}}, "final", out)
}

func digest(s string) (d string) {
	sum := blake3.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func phaseOutcome(p opwire.PhaseE, reason string) (out opwire.Outcome) {
	return opwire.Outcome{Phase: p, Reason: reason}
}

// Denial reasons a coordinator reads: [TaskGone] tells them apart.
const (
	reasonHandleInvalid = "the grant handle is not valid"
	reasonTaskEnded     = "the task ended"
	reasonDeadline      = "the task's deadline passed"
)

// TaskGone says a denial's reason means the task no longer exists for the
// coordinator — the handle is unknown or the task ended — so it should ask
// for a new one. A passed deadline is not that: request extends the task.
func TaskGone(reason string) (gone bool) {
	return strings.Contains(reason, reasonHandleInvalid) || strings.Contains(reason, reasonTaskEnded)
}

// resolve finds the task a handle names, for the sender presenting it. The
// caller holds mu.
func (inst *Service) resolve(handle string, msg *app.Msg) (t *task, out opwire.Outcome, ok bool) {
	t, out, ok, _ = inst.resolveLate(handle, msg, false)
	return
}

// resolveLate is resolve that, with lateOk, also accepts a task whose
// deadline passed and says so: a call to it is held for more time, and a
// request may extend it.
func (inst *Service) resolveLate(handle string, msg *app.Msg, lateOk bool) (t *task, out opwire.Outcome, ok bool, late bool) {
	t = inst.tasks[handle]
	switch {
	case t == nil:
		out = phaseOutcome(opwire.PhaseDenied, reasonHandleInvalid)
	case t.actor != msg.Sender || t.actorInstance != msg.SenderInstance:
		out = phaseOutcome(opwire.PhaseDenied, "the grant handle belongs to another instance")
	case t.revoked != "":
		out = phaseOutcome(opwire.PhaseDenied, reasonTaskEnded+": "+t.revoked)
	case time.Now().After(t.deadline):
		late = true
		ok = lateOk
		if !ok {
			out = phaseOutcome(opwire.PhaseDenied, reasonDeadline+"; request_access extends it")
		}
	default:
		ok = true
	}
	return
}

// deadline is a new task's, or a widening's added, time.
func (inst *Service) deadline() (d time.Duration) {
	if d = inst.cfg.Deadline; d <= 0 {
		d = DefaultDeadline
	}
	return
}

// callRange is the range a new task's call budget lies in.
func (inst *Service) callRange() (lo int, hi int) {
	if lo = inst.cfg.CallsMin; lo <= 0 {
		lo = DefaultCallsMin
	}
	if hi = inst.cfg.CallsMax; hi <= 0 {
		hi = DefaultCallsMax
	}
	hi = max(hi, lo)
	return
}

// clampCalls is the budget a new task gets for calls: zero is
// DefaultCallBudget, and either lies in callRange.
func (inst *Service) clampCalls(calls uint32) (n int) {
	lo, hi := inst.callRange()
	n = int(calls)
	if n == 0 {
		n = DefaultCallBudget
	}
	return min(max(n, lo), hi)
}

// newTask builds a task. The caller registers it.
func (inst *Service) newTask(actor app.AppIdT, actorInstance uint64, conversation string, plan string, destinations []string,
	calls uint32, deadline time.Duration, test bool) (t *task) {
	t = &task{
		id: "task-" + randomHex(6), handle: randomHex(16), actor: actor, actorInstance: actorInstance, conversation: conversation,
		plan: plan, entries: make(map[uint64]*entry), destinations: destinations,
		callsBudget: int(calls), deadline: time.Now().Add(inst.deadline()), epoch: 1, created: time.Now(), test: test,
		keys: make(map[string]*callRec), lastRead: make(map[uint64]map[string]uint64), refs: make(map[string]*resultRef),
		turnSeq: make(map[uint64]uint64), readSinceTurn: make(map[uint64]map[string]uint64),
		launches: make(map[app.AppIdT]*launchEntry), launched: make(map[uint64]bool),
	}
	t.callsBudget = inst.clampCalls(calls)
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
	if len(req.Entries) == 0 && len(req.Launches) == 0 && req.Desktop == "" {
		rep.Reason = "a grant names at least one instance, an app to open or the desktop"
		inst.recordGrantRefusal(msg, req, rep.Reason)
		inst.grantEvent(trail.GrantEventRefused, "host", rep.Reason, nil, asked(msg, req))
		return
	}
	t := inst.newTask(msg.Sender, msg.SenderInstance, req.Conversation, req.Plan, req.Destinations, req.Calls,
		time.Duration(req.DeadlineSecs)*time.Second, true)
	if req.Ceiling != nil {
		c := ceilingOfWire(*req.Ceiling).normal()
		t.ceiling = &c
	}
	inst.addLaunches(t, req.Launches)
	if req.Desktop != "" {
		t.desktop = ParseMode(req.Desktop)
		if t.desktop != ModeObserve && t.desktop != ModeAct {
			rep.Reason = "a test grant takes observe or act for the desktop"
			return
		}
	}
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
	inst.grantEvent(trail.GrantEventApproved, "host", "a test grant", t, nil)
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
	t, out, ok, late := inst.resolveLate(req.Handle, msg, true)
	if !ok {
		inst.mu.Unlock()
		inst.record(nil, &callRec{key: req.Key, instance: req.Instance, turn: req.Turn, cause: causeOf(req),
			title: boundLabel(req.Title, maxCallTitleRunes), why: boundLabel(req.Reason, maxCallReasonRunes),
			actor: msg.Sender, actorInstance: msg.SenderInstance}, "dispatch", out)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	if prev, seen := t.keys[req.Key]; seen {
		inst.mu.Unlock()
		rep.Outcome = inst.outcomeOf(t, prev, 0)
		return
	}
	rec := &callRec{key: req.Key, instance: req.Instance, argsDigest: digest(req.Args), created: time.Now(),
		turn: req.Turn, cause: causeOf(req), title: boundLabel(req.Title, maxCallTitleRunes), why: boundLabel(req.Reason, maxCallReasonRunes)}
	if t.test {
		rec.args = req.Args
	}
	t.keys[req.Key] = rec
	var spec app.OperationSpec
	var e *entry
	var need needE
	var mode ModeE
	if late {
		// Past its deadline, a call waits for the person to give the task
		// more time, as a spent budget waits for more calls.
		e, mode, need = t.entries[req.Instance], ModeObserve, needDeadline
		if e != nil {
			mode = e.mode
		}
		out = phaseOutcome(opwire.PhaseInputRequired, reasonDeadline+"; the person is asked for more time")
		if t.test {
			out.Reason = reasonDeadline + "; request_access extends it"
		}
	} else {
		out, spec, e, need, mode = inst.check(t, req)
	}
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
	if spec.Effect != app.OperationEffectNone {
		// A change the person can see: spaced, unless the settings let the
		// model work faster than a person can follow.
		inst.pace(t)
	}
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
		Writer: opwire.WriterTask(t.id), Key: req.Key, Reason: req.Reason, RefData: refData, OnBehalfOf: inst.onBehalfOf(t, e, rec.callId)}
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
	// What a widening may ask of the person is bounded by the ceiling too.
	defer func() {
		if need != 0 && t.ceiling != nil {
			mode = min(mode, t.ceiling.normal().Mode)
		}
	}()
	switch {
	case !ok:
		out = phaseOutcome(opwire.PhaseRefused, "no such operation")
	case !spec.Agents:
		out = phaseOutcome(opwire.PhaseDenied, "the operation is not exposed to agents")
	case t.ceiling.refuseEffect(spec.Effect) != "":
		// Above the ceiling the person set in the coordinator (ADR-0280):
		// refused whatever the grant holds, and not a widening to ask for.
		out = phaseOutcome(opwire.PhaseRefused, t.ceiling.refuseEffect(spec.Effect))
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
	case spec.Effect != app.OperationEffectNone && t.modeOf(e) == ModeSuggest:
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
			if st, ok := inst.captures.Status(rec.job); ok {
				confined := rec.outcome.Confined
				rec.outcome = phaseOutcome(st.Phase, st.Reason)
				rec.outcome.Confined = confined
				if st.Phase.Final() {
					info, _ := inst.captures.Info(rec.job)
					inst.recordCapture(t, rec, info.Decision, info, st.Phase, st.Reason, confined)
				}
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
		st, found := inst.captures.Status(job.job)
		if !found || st.Phase != opwire.PhaseCompleted {
			rep.Reason = "the capture is not complete"
			return
		}
		if job.outcome.Confined && !inst.modelLocal() {
			rep.Reason = "a confined capture stays an artifact handle"
			return
		}
		data, media, err := inst.captures.Bytes(job.job)
		if err != nil {
			rep.Reason = "the capture: " + err.Error()
			return
		}
		// Every capture is untrusted: it shows whatever the window holds.
		rep.Ok, rep.MediaType, rep.Data, rep.Confined = true, media, data, job.outcome.Confined
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
	windows := req.Instances
	if len(windows) == 0 {
		windows = []uint64{req.Instance}
	}
	rec := &callRec{key: req.Key, instance: windows[0], spec: app.OperationSpec{Name: "capture"}, turn: req.Turn, cause: req.cause()}
	t.keys[req.Key] = rec
	var uncovered []string
	for _, w := range windows {
		if t.entries[w] == nil {
			uncovered = append(uncovered, strconv.FormatUint(w, 10))
		}
	}
	e := t.entries[windows[0]]
	inst.mu.Unlock()
	if len(uncovered) > 0 {
		out = phaseOutcome(opwire.PhaseInputRequired, "the grant does not cover window "+strings.Join(uncovered, ", "))
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	rec.app = e.app
	if inst.captures == nil {
		out = phaseOutcome(opwire.PhaseRefused, "no window host renders captures")
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	// The capture service decides and enforces (ADR-0281): the windows
	// must still be in the task when their spans are chosen, a frame later.
	format := capture.FormatE(req.Format)
	if format == "" {
		format = capture.FormatSvg
	}
	creq := capture.Request{Windows: windows, Format: format}
	if c := req.Crop; c != nil {
		r := image.Rect(int(math.Floor(float64(c.X))), int(math.Floor(float64(c.Y))),
			int(math.Ceil(float64(c.X+c.W))), int(math.Ceil(float64(c.Y+c.H))))
		creq.Crop = &r
	}
	inst.mu.Lock()
	epoch := t.epoch
	inst.mu.Unlock()
	covered := func(w uint64) bool {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return t.revoked == "" && t.epoch == epoch && t.entries[w] != nil
	}
	job, decision, err := inst.captures.Capture(creq, capture.Facts{Covered: covered}, func() bool {
		for _, w := range windows {
			if !covered(w) {
				return false
			}
		}
		return true
	})
	// A capture's label is the highest of the windows it draws (ADR-0281
	// §SD6).
	confined := false
	for _, w := range windows {
		if info, ok := inst.openInstance(w); ok && info.Confined {
			confined = true
		}
	}
	rec.capture = &captureRec{format: format, windows: windows}
	if err == nil && decision.Effect != capture.EffectPermit {
		inst.mu.Lock()
		inst.recordCapture(t, rec, decision, capture.Info{}, opwire.PhaseRefused, decision.Reason, confined)
		inst.mu.Unlock()
		out = phaseOutcome(opwire.PhaseRefused, decision.Reason)
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	if err != nil {
		out = phaseOutcome(opwire.PhaseRefused, err.Error())
		inst.settle(t, rec, out, false)
		rep.Outcome = wireOutcomeOf(out, "", "")
		return
	}
	inst.mu.Lock()
	rec.job = job
	rec.outcome = phaseOutcome(opwire.PhaseRunning, "")
	rec.outcome.Confined = confined
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
		inst.recordAsked(msg, nil, "list", req.Key, "", req.wireCause, "", false, out.Reason)
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
	inst.recordAsked(msg, t, "list", req.Key, "", req.wireCause, "", true, "")
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
	// Who stopped it is the coordinator's word: its Stop button is the
	// person's, its model's stop_task its own.
	by, why := "coordinator", "stopped"
	if req.By == "person" {
		by = "person"
	}
	if r := boundLabel(req.Reason, maxCallReasonRunes); r != "" {
		why = r
	}
	inst.endTaskAsked(t, why, by, req.wireCause)
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
