package nanopass

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// nanopass_profile.go is ADR-0192: opt-in wall-clock attribution for one Run,
// at the granularity of a single pass invocation inside the pass tree.
//
// It exists because a Sequence hands each child the previous child's output
// *string*, so every pass re-parses from text and the pipeline's cost is
// superlinear in expression complexity. A caller timing Pass.Run learns only
// the total; the actionable fact is which child of a composite spent it, and
// whether it changed anything for the price.

// StepCost is one pass invocation's measured cost within a single
// [Pass.RunProfiled], with the invocations it made in turn.
//
// The tree mirrors the shape of the pass tree that actually executed, which is
// not always [Pass.Children]: a Conditional whose predicate was false has no
// child node here, and a composite reached through a route other than
// applyWithProps contributes none. What is present ran; what ran may be absent
// only in that last case, which no combinator in this package does.
type StepCost struct {
	// Name is the invoked pass's Name.
	Name string
	// Dur is wall-clock time spent inside this invocation, inclusive of every
	// child. Self time is Dur minus the sum of the children's Dur.
	Dur time.Duration
	// Iters counts fixed-point iterations — how many times the body's ApplyFunc
	// ran before converging (or before maxIter was exhausted). Zero for a pass
	// with no fixpoint loop of its own; note that a converging loop always runs
	// one iteration more than it rewrote, to observe that nothing changed.
	Iters int
	// Changed reports whether this invocation's output body differs from its
	// input. False when the pass errored and when it discarded its output
	// (the analytical-pass contract).
	Changed bool
	// Err is the error this invocation returned; nil otherwise. A failing child
	// aborts its parent, so a tree carrying an error is a partial one.
	Err error
	// Children are the invocations this one made, in the order they ran.
	Children []StepCost
}

// SelfDur is Dur less the time attributed to children — the invocation's own
// work. For a leaf it equals Dur; for a Sequence it is the combinator's
// overhead, which is normally near zero.
func (inst StepCost) SelfDur() (d time.Duration) {
	d = inst.Dur
	for _, ch := range inst.Children {
		d -= ch.Dur
	}
	if d < 0 {
		d = 0
	}
	return
}

// Walk visits the tree depth-first in execution order, passing each node its
// depth (0 for the receiver). It is the shape every consumer of a cost tree
// wants: a flat, indentable list that preserves the order things ran in.
func (inst StepCost) Walk(visit func(step StepCost, depth int)) {
	var rec func(s StepCost, depth int)
	rec = func(s StepCost, depth int) {
		visit(s, depth)
		for _, ch := range s.Children {
			rec(ch, depth+1)
		}
	}
	rec(inst, 0)
}

// RunProfiled is [Pass.Run] with per-invocation cost attribution: same
// extraction, same application, same integration, byte-identical output, plus
// the tree of what each pass in the tree cost.
//
// cost is the receiver's own invocation, so cost.Children are the composite's
// members. It is populated even when err is non-nil — a partial tree ending at
// the pass that failed is usually the interesting one. It is zero-valued only
// when the failure was in env extraction, before any pass ran.
//
// The profile is of THIS run. Nothing is cached and nothing is amortised, so a
// first run in a process carries the cold ANTLR DFA cost (ADR-0084) that no
// later one pays.
func (p Pass) RunProfiled(sql string) (result string, cost StepCost, err error) {
	e, body, err := env.Extract(sql)
	if err != nil {
		err = eh.Errorf("RunProfiled %s: %w", p.Name, err)
		return
	}
	rec := &costRecorder{}
	attachRecorder(e, rec)
	defer detachRecorder(e)

	newBody, applyErr := p.applyWithProps(e, body)
	cost = rec.root
	if applyErr != nil {
		err = applyErr
		return
	}
	if IsDiscardOutput(newBody) {
		newBody = body
	}
	result, err = e.Integrate(newBody)
	if err != nil {
		err = eh.Errorf("RunProfiled %s: %w", p.Name, err)
	}
	return
}

// costRecorder accumulates one run's invocation tree. It is written only from
// the goroutine driving that run: the combinators recurse synchronously, so the
// open frames form a stack and no locking is needed. A pass that called back
// into the tree from a goroutine it spawned would corrupt that stack; none
// does, and this is the assumption to revisit if one ever should.
type costRecorder struct {
	stack []costFrame
	root  StepCost
}

// costFrame is one open invocation: what enter recorded and leave completes.
type costFrame struct {
	name     string
	start    time.Time
	iters    int
	children []StepCost
}

func (inst *costRecorder) enter(name string) {
	inst.stack = append(inst.stack, costFrame{name: name, start: time.Now()})
}

// leave closes the innermost frame and files it under its parent, or as the
// root when it was the outermost.
func (inst *costRecorder) leave(before string, after string, err error) {
	n := len(inst.stack) - 1
	if n < 0 {
		return
	}
	f := inst.stack[n]
	inst.stack = inst.stack[:n]
	node := StepCost{
		Name:     f.name,
		Dur:      time.Since(f.start),
		Iters:    f.iters,
		Changed:  err == nil && after != before && !IsDiscardOutput(after),
		Err:      err,
		Children: f.children,
	}
	if n == 0 {
		inst.root = node
		return
	}
	parent := &inst.stack[n-1]
	parent.children = append(parent.children, node)
}

// countIteration attributes one fixpoint iteration to the innermost open
// frame — the pass whose ApplyFunc the loop is calling.
func (inst *costRecorder) countIteration() {
	if n := len(inst.stack); n > 0 {
		inst.stack[n-1].iters++
	}
}

// recorders associates a live profiled run with the environment that run
// threads through the pass tree.
//
// The environment is the handle because it is the only value every combinator
// already passes down: ApplyFunc's signature is fixed and shared by every pass
// in the repository, so an observer cannot travel as an argument, and the
// alternative — a field on env.Environment — would oblige a package that models
// SQL regions to define what a pass invocation is (ADR-0192, option O2).
//
// Keyed by pointer identity. env.Extract mints a fresh Environment per Run, so
// two concurrent profiled runs never collide.
var recorders sync.Map // map[*env.Environment]*costRecorder

// liveRecorders is the fast-path guard: zero means no run anywhere in the
// process is being profiled, so applyWithProps skips the map entirely and the
// execute path pays one relaxed atomic load per pass invocation.
var liveRecorders atomic.Int64

func attachRecorder(e *env.Environment, rec *costRecorder) {
	recorders.Store(e, rec)
	liveRecorders.Add(1)
}

func detachRecorder(e *env.Environment) {
	recorders.Delete(e)
	liveRecorders.Add(-1)
}

// recorderFor returns the recorder collecting this environment's run, or nil
// when the run is not profiled.
func recorderFor(e *env.Environment) (rec *costRecorder) {
	if liveRecorders.Load() == 0 || e == nil {
		return
	}
	if v, ok := recorders.Load(e); ok {
		rec, _ = v.(*costRecorder)
	}
	return
}
