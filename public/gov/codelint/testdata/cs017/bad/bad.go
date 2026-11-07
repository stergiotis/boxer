package bad

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

func work(context.Context) {}

// A bare literal.
func bareLiteral() {
	go func() {}() // want a CS017 finding here
}

// A bare call.
func bareCall() {
	go work(context.Background()) // want a CS017 finding here
}

// The fallback also runs when Go refused, after the scope retired.
func retiredFallback(s *lifetime.Scope) {
	if s != nil && s.Go(work) {
		return
	}
	go work(context.Background()) // want a CS017 finding here
}

// Go counts the function's own run, not a goroutine it starts.
func insideScopeGo(s *lifetime.Scope) {
	s.Go(func(ctx context.Context) {
		go work(ctx) // want a CS017 finding here
	})
}

// lifetime:
//
// An annotation without a reason does not count.
func annotationWithoutReason() {
	go work(context.Background()) // want a CS017 finding here
}

// The deferred close is on a channel the goroutine does not use.
func unrelatedDefer() {
	done := make(chan struct{})
	defer close(done)
	go work(context.Background()) // want a CS017 finding here
}

// The task call sits in a nested literal the goroutine never runs as a task.
func taskInNestedLiteral(api task.TaskApiI) {
	go func() { // want a CS017 finding here
		later := func() { _ = task.RunToCompletion(api, task.SpawnOpts{}, func() error { return nil }) }
		_ = later
	}()
}
