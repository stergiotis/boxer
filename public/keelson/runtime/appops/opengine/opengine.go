// Package opengine is one instance's side of the app operations contract
// (ADR-0269 §SD4): the queue a command waits in, the per-resource revisions,
// the snapshot queries read, and the command log. It makes no imzero2 call;
// the window host drives it at fixed points of each frame:
//
//	frame N+1, on the render goroutine
//	BeginFrame     the write-back of frame N has landed: changed resources
//	               are bumped with the person as writer; commands applied
//	               in frame N are now rendered
//	ApplyQueued    queued commands, in order, each re-checked: expected
//	               revisions, the person editing, availability
//	TakeSnapshot   what queries read until the next frame (as_of N+1)
//	  … the app's Frame runs; gestures it routes through its catalog are
//	    applied with the person as writer …
//	EndFrame       changes the app's own frame logic made are bumped with
//	               the app as writer
//
// Submit, Status and Cancel are safe from any goroutine; everything else
// runs on the render goroutine.
package opengine

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// DefaultKeepCalls bounds how many finished calls the engine remembers for
// Status.
const DefaultKeepCalls = 4096

// DefaultKeepLog bounds the command log.
const DefaultKeepLog = 1024

// LogEntry is one change in the command log: a command, a gesture, or a
// change the engine observed (a write-back, the app's frame logic).
type LogEntry struct {
	Frame  uint64
	Writer string
	// Op is the operation, empty for an observed change.
	Op        string
	CallId    string
	Phase     opwire.PhaseE
	Reason    string
	Resources []string
	Revisions map[string]uint64
}

type call struct {
	id      string
	spec    app.OperationSpec
	args    []byte
	expects map[string]uint64
	call    app.OperationCall
	outcome opwire.Outcome
	// before holds the written resources' values before the command, and
	// after their revisions after it: what undo checks and puts back.
	before map[string]any
	after  map[string]uint64
	// undone says how an undo of the call ended; empty until one ran.
	undone string
}

type snapshot struct {
	asOf uint64
	view app.OperationsSnapshotI
	revs map[string]uint64
}

// Engine serves one instance's catalog.
type Engine struct {
	catalog *app.OperationsCatalog
	h       app.OperationsHandlerI

	mu       sync.Mutex
	queue    []*call
	undos    []string
	calls    map[string]*call
	order    []string
	attached int
	log      []LogEntry
	logHead  int

	snap atomic.Pointer[snapshot]

	// Render goroutine only.
	frame       uint64
	started     bool
	values      map[string]any
	revs        map[string]uint64
	writers     map[string]string
	toBeDrawn   []*call
	resourceSet []string
}

// New serves catalog through h.
func New(catalog *app.OperationsCatalog, h app.OperationsHandlerI) (inst *Engine) {
	inst = &Engine{
		catalog: catalog, h: h,
		calls:   make(map[string]*call),
		values:  make(map[string]any),
		revs:    make(map[string]uint64),
		writers: make(map[string]string),
	}
	for _, r := range catalog.Resources {
		inst.resourceSet = append(inst.resourceSet, r.Name)
	}
	return
}

// Catalog is the catalog the engine serves.
func (inst *Engine) Catalog() (c *app.OperationsCatalog) { return inst.catalog }

// Submit takes a call off the render goroutine. A command is queued and
// answers accepted; a query or an external read is answered from the latest
// snapshot.
func (inst *Engine) Submit(op string, req opwire.CallRequest) (out opwire.Outcome) {
	spec, ok := inst.catalog.Lookup(op)
	if !ok {
		out = opwire.Outcome{Phase: opwire.PhaseRefused, Reason: "no such operation"}
		return
	}
	if spec.Class != app.OperationClassCommand {
		out = inst.query(spec, req)
		return
	}
	c := &call{id: req.CallId, spec: spec, args: req.Args, expects: req.Expects,
		call:    app.OperationCall{Writer: req.Writer, Key: req.Key, Reason: req.Reason},
		outcome: opwire.Outcome{Phase: opwire.PhaseAccepted}}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.calls[c.id]; dup {
		out = opwire.Outcome{Phase: opwire.PhaseRefused, Reason: "call id reused"}
		return
	}
	inst.queue = append(inst.queue, c)
	inst.remember(c)
	out = c.outcome
	return
}

func (inst *Engine) query(spec app.OperationSpec, req opwire.CallRequest) (out opwire.Outcome) {
	s := inst.snap.Load()
	if s == nil {
		out = opwire.Outcome{Phase: opwire.PhaseRefused, Reason: "the window has not drawn yet"}
		return
	}
	result, err := s.view.Query(spec.Name, req.Args)
	out = outcomeOfError(err)
	if out.Phase == opwire.PhaseUnspecified {
		out = opwire.Outcome{Phase: opwire.PhaseCompleted, Result: result}
	}
	out.AsOf = s.asOf
	out.Revisions = pick(s.revs, spec.Reads)
	return
}

// outcomeOfError maps a handler's error; a nil error maps to the zero
// outcome.
func outcomeOfError(err error) (out opwire.Outcome) {
	if err == nil {
		return
	}
	var refusal *app.OperationRefusal
	if errors.As(err, &refusal) {
		out.Reason = refusal.Reason
		out.Phase = opwire.PhaseRefused
		if refusal.Conflict {
			out.Phase = opwire.PhaseConflict
		}
		return
	}
	out = opwire.Outcome{Phase: opwire.PhaseFailed, Reason: err.Error()}
	return
}

func pick(revs map[string]uint64, names []string) (out map[string]uint64) {
	if len(names) == 0 {
		return
	}
	out = make(map[string]uint64, len(names))
	for _, n := range names {
		out[n] = revs[n]
	}
	return
}

// remember records a call for Status; the caller holds mu.
func (inst *Engine) remember(c *call) {
	inst.calls[c.id] = c
	inst.order = append(inst.order, c.id)
	for len(inst.order) > DefaultKeepCalls {
		old := inst.order[0]
		if oc, ok := inst.calls[old]; ok && !oc.outcome.Phase.Final() {
			break
		}
		delete(inst.calls, old)
		inst.order = inst.order[1:]
	}
}

// Status returns a call's outcome by call id.
func (inst *Engine) Status(id string) (out opwire.Outcome, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	c, ok := inst.calls[id]
	if ok {
		out = c.outcome
	}
	return
}

// Cancel withdraws a queued command; a call past the queue keeps its phase,
// which is returned.
func (inst *Engine) Cancel(id string) (out opwire.Outcome, ok bool) {
	return inst.withdraw(id, opwire.PhaseCancelled, "withdrawn by cancel")
}

// Expire ends queued commands — every one when ids is empty — because their
// task or instance went away.
func (inst *Engine) Expire(ids []string, reason string) {
	inst.mu.Lock()
	queued := slices.Clone(inst.queue)
	inst.mu.Unlock()
	for _, c := range queued {
		if len(ids) == 0 || slices.Contains(ids, c.id) {
			inst.withdraw(c.id, opwire.PhaseExpired, reason)
		}
	}
}

func (inst *Engine) withdraw(id string, phase opwire.PhaseE, reason string) (out opwire.Outcome, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	c, ok := inst.calls[id]
	if !ok {
		return
	}
	if c.outcome.Phase == opwire.PhaseAccepted {
		c.outcome = opwire.Outcome{Phase: phase, Reason: reason}
		inst.queue = slices.DeleteFunc(inst.queue, func(q *call) bool { return q.id == id })
	}
	out = c.outcome
	return
}

// SetAttached counts the tasks attached to the instance; while any is, the
// host repaints at a bounded interval so a queued command does not wait for
// the person to move the mouse.
func (inst *Engine) SetAttached(attached bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if attached {
		inst.attached++
	} else if inst.attached > 0 {
		inst.attached--
	}
}

// Busy reports whether the instance needs frames soon: a task is attached,
// or a command or an undo is queued.
func (inst *Engine) Busy() (busy bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	busy = inst.attached > 0 || len(inst.queue) > 0 || len(inst.undos) > 0
	return
}

// --- render goroutine ---------------------------------------------------

// BeginFrame runs once the previous frame's write-back has landed.
func (inst *Engine) BeginFrame() {
	inst.frame++
	if len(inst.toBeDrawn) > 0 {
		inst.mu.Lock()
		for _, c := range inst.toBeDrawn {
			if c.outcome.Phase == opwire.PhaseApplied {
				c.outcome.Phase = opwire.PhaseRendered
			}
		}
		inst.mu.Unlock()
		inst.toBeDrawn = inst.toBeDrawn[:0]
	}
	if !inst.started {
		for _, r := range inst.resourceSet {
			inst.values[r] = inst.h.ResourceValue(r)
		}
		inst.started = true
		return
	}
	if changed := inst.observe(opwire.WriterPerson, inst.resourceSet); len(changed) > 0 {
		inst.appendLog(LogEntry{Frame: inst.frame, Writer: opwire.WriterPerson, Phase: opwire.PhaseApplied,
			Resources: changed, Revisions: pick(inst.revs, changed)})
	}
}

// ApplyQueued runs the undos the person asked for, then applies the queued
// commands in order.
func (inst *Engine) ApplyQueued() {
	inst.mu.Lock()
	queue := inst.queue
	inst.queue = nil
	undos := inst.undos
	inst.undos = nil
	inst.mu.Unlock()
	for _, id := range undos {
		inst.undo(id)
	}
	for _, c := range queue {
		out := inst.apply(c)
		inst.mu.Lock()
		if c.outcome.Phase == opwire.PhaseAccepted {
			c.outcome = out
		}
		inst.mu.Unlock()
		if out.Phase == opwire.PhaseApplied {
			inst.toBeDrawn = append(inst.toBeDrawn, c)
		}
		inst.appendLog(LogEntry{Frame: inst.frame, Writer: c.call.Writer, Op: c.spec.Name, CallId: c.id,
			Phase: out.Phase, Reason: out.Reason, Resources: c.spec.Writes, Revisions: out.Revisions})
	}
}

func (inst *Engine) apply(c *call) (out opwire.Outcome) {
	for _, r := range c.spec.Writes {
		exp, ok := c.expects[r]
		if !ok {
			return opwire.Outcome{Phase: opwire.PhaseConflict, Reason: "read first: no revision of " + r + " is known",
				Revisions: pick(inst.revs, c.spec.Writes)}
		}
		if inst.revs[r] != exp {
			return opwire.Outcome{Phase: opwire.PhaseConflict, Reason: r + " moved since it was read",
				Revisions: pick(inst.revs, c.spec.Writes)}
		}
	}
	for _, r := range c.spec.Writes {
		if inst.h.Editing(r) {
			return opwire.Outcome{Phase: opwire.PhaseConflict, Reason: "the person is editing " + r,
				Revisions: pick(inst.revs, c.spec.Writes)}
		}
	}
	if s := inst.snap.Load(); s != nil {
		if ok, reason := s.view.Available(c.spec.Name); !ok {
			return opwire.Outcome{Phase: opwire.PhaseRefused, Reason: reason}
		}
	}
	before := make(map[string]any, len(c.spec.Writes))
	for _, r := range c.spec.Writes {
		before[r] = inst.values[r]
	}
	result, err := inst.h.ApplyCommand(c.call, c.spec.Name, c.args)
	out = outcomeOfError(err)
	if out.Phase != opwire.PhaseUnspecified {
		if out.Phase == opwire.PhaseConflict {
			out.Revisions = pick(inst.revs, c.spec.Writes)
		}
		return
	}
	inst.observe(c.call.Writer, c.spec.Writes)
	c.before, c.after = before, pick(inst.revs, c.spec.Writes)
	out = opwire.Outcome{Phase: opwire.PhaseApplied, AsOf: inst.frame, Revisions: pick(inst.revs, c.spec.Writes), Result: result}
	return
}

// Undo asks for a command to be undone at the next command stage
// (ADR-0269 §SD8). UndoStatus reports how it ended.
func (inst *Engine) Undo(id string) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	c, found := inst.calls[id]
	if !found || c.after == nil || c.undone != "" {
		return
	}
	inst.undos = append(inst.undos, id)
	ok = true
	return
}

// UndoStatus reports how an undo of a call ended; empty while none ran.
func (inst *Engine) UndoStatus(id string) (status string, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	c, ok := inst.calls[id]
	if ok {
		status = c.undone
	}
	return
}

// undo restores each resource a command wrote while its revision is still
// the one the command left; later work by anyone is never overwritten.
func (inst *Engine) undo(id string) {
	inst.mu.Lock()
	c := inst.calls[id]
	inst.mu.Unlock()
	if c == nil || c.after == nil {
		return
	}
	var restored, moved, fixed []string
	for _, r := range c.spec.Writes {
		switch {
		case inst.revs[r] != c.after[r]:
			moved = append(moved, r)
		case !inst.h.Restore(r, c.before[r]):
			fixed = append(fixed, r)
		default:
			restored = append(restored, r)
		}
	}
	inst.observe(opwire.WriterPerson, restored)
	status := "undone"
	switch {
	case len(restored) == 0 && len(moved) > 0:
		status = "not undone: " + strings.Join(moved, ", ") + " moved since"
	case len(restored) == 0:
		status = "not undone: the app cannot restore " + strings.Join(fixed, ", ")
	case len(moved)+len(fixed) > 0:
		status = "partly undone: " + strings.Join(append(moved, fixed...), ", ") + " kept"
	}
	inst.mu.Lock()
	c.undone = status
	inst.mu.Unlock()
	inst.appendLog(LogEntry{Frame: inst.frame, Writer: opwire.WriterPerson, Op: "undo " + c.spec.Name, CallId: id,
		Phase: opwire.PhaseApplied, Reason: status, Resources: restored, Revisions: pick(inst.revs, restored)})
}

// SnapshotRevisions returns the revisions as of the latest snapshot; safe
// from any goroutine. A proposal is stale when one it expects has moved.
func (inst *Engine) SnapshotRevisions() (revs map[string]uint64, asOf uint64) {
	if s := inst.snap.Load(); s != nil {
		revs, asOf = maps.Clone(s.revs), s.asOf
	}
	return
}

// TakeSnapshot captures what queries read until the next frame.
func (inst *Engine) TakeSnapshot() {
	inst.snap.Store(&snapshot{asOf: inst.frame, view: inst.h.Snapshot(), revs: maps.Clone(inst.revs)})
}

// Gesture applies a command the person asked for through the app's own UI,
// on the render goroutine, inside the app's Frame (ADR-0269 §SD8 "One
// path"). It is applied at once and logged with the person as writer.
func (inst *Engine) Gesture(op string, args []byte) (result []byte, err error) {
	spec, ok := inst.catalog.Lookup(op)
	if !ok || spec.Class != app.OperationClassCommand {
		err = app.RefuseOperation("no such command")
		return
	}
	result, err = inst.h.ApplyCommand(app.OperationCall{Writer: opwire.WriterPerson}, op, args)
	out := outcomeOfError(err)
	if out.Phase == opwire.PhaseUnspecified {
		inst.observe(opwire.WriterPerson, spec.Writes)
		out = opwire.Outcome{Phase: opwire.PhaseApplied, Revisions: pick(inst.revs, spec.Writes)}
	}
	inst.appendLog(LogEntry{Frame: inst.frame, Writer: opwire.WriterPerson, Op: op, Phase: out.Phase,
		Reason: out.Reason, Resources: spec.Writes, Revisions: out.Revisions})
	return
}

// EndFrame runs after the app's Frame: what changed since the command
// stage, other than through a gesture, is the app's own doing.
func (inst *Engine) EndFrame() {
	if changed := inst.observe(opwire.WriterApp, inst.resourceSet); len(changed) > 0 {
		inst.appendLog(LogEntry{Frame: inst.frame, Writer: opwire.WriterApp, Phase: opwire.PhaseApplied,
			Resources: changed, Revisions: pick(inst.revs, changed)})
	}
}

// Revisions returns the current revisions; render goroutine only.
func (inst *Engine) Revisions() (revs map[string]uint64) { return maps.Clone(inst.revs) }

// Writer returns the last writer of a resource; render goroutine only.
func (inst *Engine) Writer(resource string) (writer string) { return inst.writers[resource] }

// Frame is the engine's frame counter; render goroutine only.
func (inst *Engine) Frame() (frame uint64) { return inst.frame }

// observe bumps the revision of every named resource whose value changed,
// with writer as its writer, and returns those that did.
func (inst *Engine) observe(writer string, names []string) (changed []string) {
	for _, r := range names {
		v := inst.h.ResourceValue(r)
		if same(inst.values[r], v) {
			continue
		}
		inst.values[r] = v
		inst.revs[r]++
		inst.writers[r] = writer
		changed = append(changed, r)
	}
	return
}

// same compares two resource values: with == when the type allows it,
// deeply otherwise.
func same(a any, b any) (eq bool) {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta.Comparable() {
		defer func() {
			if recover() != nil {
				eq = reflect.DeepEqual(a, b)
			}
		}()
		return a == b
	}
	return reflect.DeepEqual(a, b)
}

func (inst *Engine) appendLog(e LogEntry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if len(inst.log) < DefaultKeepLog {
		inst.log = append(inst.log, e)
		return
	}
	inst.log[inst.logHead] = e
	inst.logHead = (inst.logHead + 1) % DefaultKeepLog
}

// Log returns the command log, oldest first.
func (inst *Engine) Log() (entries []LogEntry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	entries = make([]LogEntry, 0, len(inst.log))
	entries = append(entries, inst.log[inst.logHead:]...)
	entries = append(entries, inst.log[:inst.logHead]...)
	return
}
