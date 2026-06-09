//go:build llm_generated_opus47

package fsmview

import (
	"fmt"
	"hash/fnv"
	"iter"
	"time"

	"github.com/hishamk/statetrooper"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// EdgeKey identifies a single transition from From to To. Comparable so it
// works as a map key over arbitrary state types.
type EdgeKey[T comparable] struct {
	From T
	To   T
}

// Transition is a single recorded state change, returned by
// [Machine.History] / [Machine.LastTransition]. Defined locally rather
// than re-exporting [statetrooper.Transition] so the public widget API
// doesn't leak the underlying FSM library type — callers can keep
// using the widget after a future swap without recompiling their code.
//
// At is the wall-clock time the transition fired (zero-value when the
// underlying FSM was configured with maxHistory=0).
type Transition[T comparable] struct {
	From     T
	To       T
	At       time.Time
	Metadata map[string]string
}

// StateColorFn returns the IDS palette colour for a given state, with
// isCurrent flagging the active state. Override via [WithStateColor]; the
// default scheme lights the active state with AccentDefault and keeps the
// rest in NeutralSubtle.
type StateColorFn[T comparable] func(state T, isCurrent bool) styletokens.RGBA8

// Machine is the visualization-aware wrapper around [statetrooper.FSM]. It
// owns the FSM (one source of truth for Current/Transition) plus a local
// mirror of the rule graph (statetrooper's ruleset is unexported, so the
// widget can't enumerate transitions from the FSM alone).
//
// Construct via [NewMachine] + [Option]; declare valid transitions via
// [Machine.AddRule]; drive runtime state with [Machine.Transition].
type Machine[T comparable] struct {
	fsm        *statetrooper.FSM[T]
	rules      map[T][]T
	order      []T
	known      map[T]struct{}
	labelFn    func(T) string
	colorFn    StateColorFn[T]
	edgeLabel  map[EdgeKey[T]]string
	nodeId     map[T]uint64
}

// Option configures a Machine at construction.
type Option[T comparable] func(inst *Machine[T])

// WithLabel sets a human-readable label for each state. Defaults to
// [fmt.Sprint], which is fine for string / int states and produces "{a b}"
// for struct states (callers should override for structs).
func WithLabel[T comparable](fn func(T) string) Option[T] {
	return func(inst *Machine[T]) {
		inst.labelFn = fn
	}
}

// WithStateOrder pins the display order used by the level-2 table. States
// not listed here appear after the listed ones in insertion order (i.e.
// the order they first show up via AddRule). Useful when alphabetic order
// hides the FSM's natural flow.
func WithStateOrder[T comparable](order []T) Option[T] {
	return func(inst *Machine[T]) {
		inst.order = append(inst.order[:0], order...)
		for _, s := range order {
			inst.known[s] = struct{}{}
		}
	}
}

// WithStateColor overrides the default per-state colour scheme. Callers
// typically use this to flag domain-specific severity (error states red,
// terminal states muted, etc.) on top of the isCurrent emphasis.
func WithStateColor[T comparable](fn StateColorFn[T]) Option[T] {
	return func(inst *Machine[T]) {
		inst.colorFn = fn
	}
}

// NewMachine constructs a Machine with the given initial state. maxHistory
// caps the [statetrooper.FSM] transition history; pass 0 to disable
// history tracking. Options apply in order; later options win.
func NewMachine[T comparable](initial T, maxHistory int, opts ...Option[T]) *Machine[T] {
	const expectedStates = 8
	inst := &Machine[T]{
		fsm:       statetrooper.NewFSM(initial, maxHistory),
		rules:     make(map[T][]T, expectedStates),
		known:     make(map[T]struct{}, expectedStates),
		edgeLabel: make(map[EdgeKey[T]]string, expectedStates),
		nodeId:    make(map[T]uint64, expectedStates),
		labelFn:   func(s T) string { return fmt.Sprint(s) },
		colorFn:   defaultStateColor[T],
	}
	inst.observe(initial)
	for _, opt := range opts {
		opt(inst)
	}
	return inst
}

// AddRule mirrors [statetrooper.FSM.AddRule] and remembers the rule for
// graph/table enumeration. Returns the receiver for chaining.
func (inst *Machine[T]) AddRule(from T, to ...T) *Machine[T] {
	inst.fsm.AddRule(from, to...)
	inst.rules[from] = append(inst.rules[from], to...)
	inst.observe(from)
	for _, t := range to {
		inst.observe(t)
	}
	return inst
}

// EdgeLabel attaches a display label to one transition. Used by the level-2
// graph view (egui_graphs edge labels) and the table view (transition
// triggers column). Empty label clears.
func (inst *Machine[T]) EdgeLabel(from, to T, label string) *Machine[T] {
	k := EdgeKey[T]{From: from, To: to}
	if label == "" {
		delete(inst.edgeLabel, k)
		return inst
	}
	inst.edgeLabel[k] = label
	return inst
}

// Transition delegates to [statetrooper.FSM.Transition]. The widget reads
// the new state on the next frame via [Machine.Current]; no event fires.
// Wraps statetrooper's TransitionError via [eh.Errorf] so the boxer
// stack-tracer surfaces this package on the way out.
func (inst *Machine[T]) Transition(target T) error {
	_, err := inst.fsm.Transition(target, nil)
	if err != nil {
		return eh.Errorf("fsmview: transition: %w", err)
	}
	return nil
}

// Mirror drives the machine to target like [Machine.Transition], but never
// fails. Use it when the machine is a passive mirror of state derived
// elsewhere (e.g. a per-frame projection of external data): the source of
// truth lives outside, so refusing an "illegal" edge would only wedge the
// mirror — a memoryless producer re-proposes the same target next frame and
// the machine never catches up. When target is reachable by a declared
// rule, Mirror behaves exactly like Transition (records history, returns
// declared=true). When it is not, Mirror still moves there and records the
// transition, but returns declared=false so the caller can log the
// unexpected edge as a diagnostic.
//
// The undeclared edge is taught to the underlying FSM (so the move records
// in history and CanTransition stays honest) and target is registered as a
// known node, but the edge is deliberately NOT added to the drawn rule graph
// ([Machine.Edges]): the graph keeps only the edges the domain declared via
// [Machine.AddRule], and surprises surface in the logs / history instead of
// as phantom arrows. A same-state call is a no-op (no self-loop is recorded).
//
// Prefer [Machine.Transition] when the machine itself is authoritative and
// an illegal edge is a real error the caller must handle.
func (inst *Machine[T]) Mirror(target T) (declared bool) {
	return inst.MirrorWithMetadata(target, nil)
}

// MirrorWithMetadata behaves exactly like [Machine.Mirror] — a never-failing
// move suited to a passive mirror of externally-derived state — but attaches md
// to the recorded transition. Consumers of [Machine.History] /
// [Machine.LastTransition] (notably the History view) can then show *why* the
// move happened, e.g. {"reason": "<builder rejection>"} for a validity mirror.
// md may be nil (equivalent to Mirror). A same-state call is a no-op and records
// nothing, matching Mirror's no-self-loop contract.
func (inst *Machine[T]) MirrorWithMetadata(target T, md map[string]string) (declared bool) {
	cur := inst.Current()
	if cur == target {
		return true
	}
	declared = inst.fsm.CanTransition(target)
	if !declared {
		inst.observe(target)
		inst.fsm.AddRule(cur, target)
	}
	_, _ = inst.fsm.Transition(target, md)
	return declared
}

// Current returns the current FSM state.
func (inst *Machine[T]) Current() T {
	return inst.fsm.CurrentState()
}

// CanTransition reports whether the given target is reachable in one step
// from the current state.
func (inst *Machine[T]) CanTransition(target T) bool {
	return inst.fsm.CanTransition(target)
}

// Label returns the configured display label for a state.
func (inst *Machine[T]) Label(state T) string {
	return inst.labelFn(state)
}

// Color returns the IDS palette colour for a state, factoring in whether
// it is the current one.
func (inst *Machine[T]) Color(state T) styletokens.RGBA8 {
	return inst.colorFn(state, state == inst.Current())
}

// States iterates the known states in display order — pinned states first
// (via [WithStateOrder]), then any states observed via AddRule that weren't
// pinned, in observation order.
func (inst *Machine[T]) States() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, s := range inst.order {
			if !yield(s) {
				return
			}
		}
	}
}

// Edges iterates all declared transitions (from, to, label). Label is empty
// when no [Machine.EdgeLabel] was set for that edge.
func (inst *Machine[T]) Edges() iter.Seq2[EdgeKey[T], string] {
	return func(yield func(EdgeKey[T], string) bool) {
		for _, from := range inst.order {
			for _, to := range inst.rules[from] {
				k := EdgeKey[T]{From: from, To: to}
				if !yield(k, inst.edgeLabel[k]) {
					return
				}
			}
		}
	}
}

// NodeId returns a stable u64 id for a state, suitable for c.GraphNode.
// Uses FNV-1a over the state's fmt.Sprint representation so two states with
// the same label collide — but that's also true on the visualization side
// (same label looks like the same state to the user). For domain states
// distinguished only by an internal field, callers should override
// WithLabel to make the representation unique.
func (inst *Machine[T]) NodeId(state T) uint64 {
	if id, ok := inst.nodeId[state]; ok {
		return id
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(inst.labelFn(state)))
	id := h.Sum64()
	if id == 0 {
		id = 1
	}
	inst.nodeId[state] = id
	return id
}

// HistoryLen returns the number of transitions currently retained by the
// underlying FSM (capped by maxHistory passed to NewMachine).
func (inst *Machine[T]) HistoryLen() int {
	return len(inst.fsm.Transitions())
}

// History iterates the recorded transitions in chronological order
// (oldest first). Length is capped by maxHistory passed to [NewMachine];
// when the cap is hit, the oldest entry is evicted, not the call.
func (inst *Machine[T]) History() iter.Seq[Transition[T]] {
	return func(yield func(Transition[T]) bool) {
		for _, t := range inst.fsm.Transitions() {
			if !yield(toTransition(t)) {
				return
			}
		}
	}
}

// HistoryReverse iterates the recorded transitions newest-first. Useful
// when rendering an "activity feed" or computing the time-since-last-
// transition without walking the whole slice.
func (inst *Machine[T]) HistoryReverse() iter.Seq[Transition[T]] {
	return func(yield func(Transition[T]) bool) {
		ts := inst.fsm.Transitions()
		for i := len(ts) - 1; i >= 0; i-- {
			if !yield(toTransition(ts[i])) {
				return
			}
		}
	}
}

// LastTransition returns the most recent recorded transition, or ok=false
// when no transitions have been recorded (fresh machine, or maxHistory=0).
func (inst *Machine[T]) LastTransition() (t Transition[T], ok bool) {
	ts := inst.fsm.Transitions()
	if len(ts) == 0 {
		return
	}
	t = toTransition(ts[len(ts)-1])
	ok = true
	return
}

// toTransition converts statetrooper's leaky pointer-timestamp into the
// value-typed widget-facing Transition. Nil timestamp (impossible in
// practice given how statetrooper writes it, but defensive) collapses to
// time.Time{}.
func toTransition[T comparable](t statetrooper.Transition[T]) Transition[T] {
	var at time.Time
	if t.Timestamp != nil {
		at = *t.Timestamp
	}
	return Transition[T]{
		From:     t.FromState,
		To:       t.ToState,
		At:       at,
		Metadata: t.Metadata,
	}
}

// observe records a state as known and assigns it a position in the display
// order if it wasn't pinned via [WithStateOrder].
func (inst *Machine[T]) observe(s T) {
	if _, ok := inst.known[s]; ok {
		return
	}
	inst.known[s] = struct{}{}
	inst.order = append(inst.order, s)
}

// defaultStateColor lights the active state with AccentDefault and uses
// NeutralSubtle for the rest. ADR-0031 §SD2 forbids using AccentDefault as
// a background for large surfaces, but a chip-sized highlight is fine.
func defaultStateColor[T comparable](_ T, isCurrent bool) styletokens.RGBA8 {
	if isCurrent {
		return styletokens.AccentDefault
	}
	return styletokens.NeutralSubtle
}
