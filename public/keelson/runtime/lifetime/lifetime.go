// Package lifetime groups work that is retired as one, and lets the owner
// wait for it to finish (ADR-0322, proposed).
//
// A [Scope] has two contexts and a drain:
//
//   - The work context, [Scope.Ctx], ends at [Scope.Retire]. It is what a
//     call that should die with the scope runs under: a model call, a
//     query, a wait on the person.
//   - The tail context, [Scope.Tail], survives Retire and ends at
//     [Scope.Abandon]. It is what the record of the work is written under,
//     so a stopped call still has its stop recorded.
//   - Work counts itself in with [Scope.Enter] and out with the leave func
//     it returns. Once the scope is retired and the last piece of work has
//     left, the [Scope.OnDrained] hooks run, in registration order, under
//     the tail context; then [Scope.Done] closes.
//
// Retire refuses new work but not the tails of work already entered, nor the
// hooks: an owner can retire a scope from inside its own teardown and still
// have the closing writes happen. Abandon is the deadline: tails see their
// context end and are expected to return promptly.
//
// A child scope ([New] with a parent) counts as one piece of the parent's
// work until it is done, so a parent drains only after its children have.
// Retiring or abandoning a parent does the same to its children.
//
// Hooks run on a goroutine of their own and tails on whichever goroutine
// entered the work, both concurrently with the scope's owner: what they
// touch must be safe to share. A panicking hook takes the process down.
// Only work that entered is waited for; a goroutine started without Enter
// or Go is invisible to the drain.
package lifetime

import (
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Scope is a group of work retired as one. Construct with [New]; the
// zero value is not usable.
type Scope struct {
	parent      *Scope
	leaveParent func()

	work       context.Context
	cancelWork context.CancelFunc
	tail       context.Context
	cancelTail context.CancelFunc
	done       chan struct{}

	mu       sync.Mutex
	holds    int
	retired  bool
	draining bool
	hooks    []func(tail context.Context)
	children map[*Scope]struct{}
}

// New is a scope under parent, or a root scope when parent is nil. A child
// of a parent that is already retired is born retired, and done.
func New(parent *Scope) (inst *Scope) {
	inst = &Scope{parent: parent, done: make(chan struct{})}
	workParent, tailParent := context.Background(), context.Background()
	if parent != nil {
		workParent, tailParent = parent.work, parent.tail
	}
	inst.work, inst.cancelWork = context.WithCancel(workParent)
	inst.tail, inst.cancelTail = context.WithCancel(tailParent)
	if parent == nil {
		return
	}
	leave, ok := parent.adopt(inst)
	if !ok {
		inst.retired, inst.draining = true, true
		inst.cancelWork()
		inst.finish()
		return
	}
	inst.leaveParent = leave
	return
}

// adopt counts child in as one piece of work, unless the scope retired. The
// count and the registration are one step, so a Retire never misses a child
// that holds the scope.
func (inst *Scope) adopt(child *Scope) (leave func(), ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.retired {
		return
	}
	inst.holds++
	if inst.children == nil {
		inst.children = make(map[*Scope]struct{})
	}
	inst.children[child] = struct{}{}
	var once sync.Once
	leave = func() { once.Do(inst.leave) }
	ok = true
	return
}

// Ctx is the work context: it ends at Retire.
func (inst *Scope) Ctx() (ctx context.Context) {
	return inst.work
}

// Tail is ctx's values under the scope's tail context: it does not end when
// ctx or the work context does, only at Abandon or once the scope is done.
// A nil ctx stands for context.Background.
func (inst *Scope) Tail(ctx context.Context) (tail context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	return tailCtx{Context: context.WithoutCancel(ctx), tail: inst.tail}
}

// Enter counts one piece of work in. ok is false once the scope retired:
// nothing new starts after Retire. leave counts it out, and may be called
// more than once.
func (inst *Scope) Enter() (leave func(), ok bool) {
	inst.mu.Lock()
	if inst.retired {
		inst.mu.Unlock()
		return func() {}, false
	}
	inst.holds++
	inst.mu.Unlock()
	var once sync.Once
	leave = func() { once.Do(inst.leave) }
	ok = true
	return
}

func (inst *Scope) leave() {
	inst.mu.Lock()
	inst.holds--
	start := inst.startDrainLocked()
	inst.mu.Unlock()
	if start {
		inst.drain()
	}
}

// Go runs f under the work context as one piece of work, unless the scope
// retired. started says whether f runs.
func (inst *Scope) Go(f func(ctx context.Context)) (started bool) {
	leave, ok := inst.Enter()
	if !ok {
		return false
	}
	go func() {
		defer leave()
		f(inst.work)
	}()
	return true
}

// OnDrained registers f to run once the scope is retired and its work has
// left, under the tail context. registered is false once the drain began:
// a hook added that late does not run.
func (inst *Scope) OnDrained(f func(tail context.Context)) (registered bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.draining {
		return false
	}
	inst.hooks = append(inst.hooks, f)
	return true
}

// Retire ends the work context, refuses new work, and retires the children.
// The drain starts once the work already entered has left. Idempotent.
func (inst *Scope) Retire() {
	inst.mu.Lock()
	if inst.retired {
		inst.mu.Unlock()
		return
	}
	inst.retired = true
	children := make([]*Scope, 0, len(inst.children))
	for ch := range inst.children {
		children = append(children, ch)
	}
	start := inst.startDrainLocked()
	inst.mu.Unlock()
	inst.cancelWork()
	for _, ch := range children {
		ch.Retire()
	}
	if start {
		inst.drain()
	}
}

// Abandon retires the scope and ends its tail context and its children's:
// work still writing its tail is told to stop. The drain still completes
// once that work has returned. Idempotent.
func (inst *Scope) Abandon() {
	inst.Retire()
	inst.cancelTail()
}

// IsRetired says whether Retire ran.
func (inst *Scope) IsRetired() (retired bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.retired
}

// Done closes once the scope is retired, its work has left and its hooks
// have returned. A scope with no work and no hooks is done when Retire
// returns.
func (inst *Scope) Done() (done <-chan struct{}) {
	return inst.done
}

// Drain retires the scope and waits until it is done or ctx ends. When ctx
// ends first the scope is abandoned and the error says the drain was cut
// short; Drain does not wait for the abandoned work to return.
func (inst *Scope) Drain(ctx context.Context) (err error) {
	inst.Retire()
	select {
	case <-inst.done:
		return
	case <-ctx.Done():
	}
	inst.Abandon()
	select {
	case <-inst.done:
		// The work returned as the deadline passed: it drained.
		return
	default:
	}
	err = eh.Errorf("lifetime: the scope did not drain in time, abandoned: %w", context.Cause(ctx))
	return
}

// startDrainLocked reports whether the caller is the one to start the
// drain: the scope is retired, nothing holds it, and no drain started.
func (inst *Scope) startDrainLocked() (start bool) {
	if !inst.retired || inst.holds > 0 || inst.draining {
		return false
	}
	inst.draining = true
	return true
}

// drain finishes the scope: at once when no hook is registered, so a scope
// nothing used is done when Retire returns; otherwise on a goroutine of its
// own that runs the hooks first, since the caller may be one that must not
// block.
func (inst *Scope) drain() {
	inst.mu.Lock()
	hooks := inst.hooks
	inst.hooks = nil
	inst.mu.Unlock()
	if len(hooks) == 0 {
		inst.finish()
		return
	}
	go func() {
		for _, f := range hooks {
			f(inst.tail)
		}
		inst.finish()
	}()
}

// finish closes Done, ends the tail context, and leaves the parent.
func (inst *Scope) finish() {
	close(inst.done)
	inst.cancelTail()
	if inst.parent == nil {
		return
	}
	inst.parent.mu.Lock()
	delete(inst.parent.children, inst)
	inst.parent.mu.Unlock()
	if inst.leaveParent != nil {
		inst.leaveParent()
	}
}

// tailCtx carries one context's values under another's lifetime.
type tailCtx struct {
	context.Context
	tail context.Context
}

func (inst tailCtx) Deadline() (deadline time.Time, ok bool) { return inst.tail.Deadline() }
func (inst tailCtx) Done() <-chan struct{}                   { return inst.tail.Done() }
func (inst tailCtx) Err() error                              { return inst.tail.Err() }
