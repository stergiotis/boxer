package good

import (
	"context"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

func work(context.Context) {}

// The nil-scope fallback, in the then-branch.
func nilThen(s *lifetime.Scope, f func(context.Context)) bool {
	if s == nil {
		go f(context.Background())
		return true
	}
	return s.Go(f)
}

// The nil-scope fallback, after a set scope returned.
func nilAfterReturn(s *lifetime.Scope, f func(context.Context)) {
	if s != nil {
		s.Go(f)
		return
	}
	go f(context.Background())
}

// The nil-scope fallback, in the else-branch, nil on the left.
func nilElse(s *lifetime.Scope, f func(context.Context)) {
	if nil != s {
		s.Go(f)
	} else {
		go f(context.Background())
	}
}

// A wrapper over a scope, with the scope's Go method.
type client struct{ scope *lifetime.Scope }

func (inst *client) Go(f func(ctx context.Context)) (started bool) {
	if inst == nil {
		go f(context.Background())
		return true
	}
	return inst.scope.Go(f)
}

// A literal whose body is a task.
func taskLiteral(api task.TaskApiI) {
	go func() {
		_ = task.RunToCompletion(api, task.SpawnOpts{Kind: "k"}, func() error { return nil })
	}()
}

// A method whose body is a task.
type poller struct{ api task.TaskApiI }

func (inst *poller) poll(ctx context.Context) {
	_ = task.Run(inst.api, ctx, task.SpawnOpts{Kind: "k"}, func(context.Context) error { return nil })
}

func (inst *poller) start() {
	go inst.poll(context.Background())
}

// Work entered into the scope by hand.
func entered(s *lifetime.Scope) {
	leave, ok := s.Enter()
	if !ok {
		return
	}
	go func() {
		defer leave()
		work(s.Ctx())
	}()
}

// Work entered through a wrapper that returns the scope's Enter.
type turn struct{ scope *lifetime.Scope }

func (inst *turn) enterTurn() (leave func(), ok bool) {
	return inst.scope.Enter()
}

func (inst *turn) watch() {
	leave, ok := inst.enterTurn()
	if !ok {
		return
	}
	go func() {
		defer leave()
	}()
}

// A watcher stopped by a deferred cancel on the context it was given.
func boundedByCancel(ctx context.Context) {
	hctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go work(hctx)
}

// A feed stopped by a deferred close of the channel it selects on.
func boundedByClose() {
	stop := make(chan struct{})
	go func() {
		<-stop
	}()
	defer close(stop)
}

var installOnce sync.Once

// install runs once per process, off the frame.
//
// lifetime: process-wide; the install serves every window and outlives each.
func install() {
	installOnce.Do(func() {
		go work(context.Background())
	})
}

// A finding suppressed on its line, with a reason.
func suppressed() {
	go work(context.Background()) //boxer:lint disable=CS017 reason="the fixture shows the suppression"
}
