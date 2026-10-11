package lifetime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wait = 2 * time.Second

func isClosed(ch <-chan struct{}) (closed bool) {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// A scope with nothing in it is done as soon as it retires, after its hooks.
func TestAnIdleScopeIsDoneAtRetire(t *testing.T) {
	s := New(nil)
	var ran atomic.Bool
	require.True(t, s.OnDrained(func(context.Context) { ran.Store(true) }))
	assert.False(t, isClosed(s.Done()))
	s.Retire()
	require.Eventually(t, func() bool { return isClosed(s.Done()) }, wait, time.Millisecond)
	assert.True(t, ran.Load())
	assert.False(t, s.OnDrained(func(context.Context) {}), "a hook after the drain began does not run")

	unused := New(nil)
	unused.Retire()
	assert.True(t, isClosed(unused.Done()), "a scope nothing used is done when Retire returns")
}

// Retire ends the work context and refuses new work; work already entered
// writes its tail under a context that survives the retire, and the hooks
// run after it left.
func TestTheTailOutlivesRetireAndHooksWaitForIt(t *testing.T) {
	s := New(nil)
	leave, ok := s.Enter()
	require.True(t, ok)
	tail := s.Tail(context.Background())
	var order []string
	s.OnDrained(func(ctx context.Context) {
		assert.NoError(t, ctx.Err(), "the hook runs under a live tail")
		order = append(order, "hook")
	})
	s.Retire()
	assert.ErrorIs(t, s.Ctx().Err(), context.Canceled)
	_, ok = s.Enter()
	assert.False(t, ok, "nothing new starts after Retire")
	assert.NoError(t, tail.Err(), "the tail survives Retire")
	assert.False(t, isClosed(s.Done()))
	order = append(order, "tail")
	leave()
	leave()
	require.Eventually(t, func() bool { return isClosed(s.Done()) }, wait, time.Millisecond)
	assert.Equal(t, []string{"tail", "hook"}, order)
	assert.Error(t, tail.Err(), "the tail ends once the scope is done")
}

// Tail keeps the values of the context it was given.
func TestTailKeepsValues(t *testing.T) {
	type key struct{}
	s := New(nil)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	cancel()
	tail := s.Tail(ctx)
	assert.Equal(t, "v", tail.Value(key{}))
	assert.NoError(t, tail.Err(), "the given context's cancel does not reach the tail")
	derived, cancelDerived := context.WithTimeout(tail, time.Hour)
	defer cancelDerived()
	s.Abandon()
	require.Eventually(t, func() bool { return derived.Err() != nil }, wait, time.Millisecond, "a context derived from the tail ends with it")
}

// A parent drains only after its children; retiring the parent retires them.
func TestAParentWaitsForItsChildren(t *testing.T) {
	parent := New(nil)
	child := New(parent)
	leave, ok := child.Enter()
	require.True(t, ok)
	var closedChild atomic.Bool
	child.OnDrained(func(context.Context) { closedChild.Store(true) })
	parent.Retire()
	assert.True(t, child.IsRetired(), "retire cascades")
	assert.ErrorIs(t, child.Ctx().Err(), context.Canceled)
	time.Sleep(20 * time.Millisecond)
	assert.False(t, isClosed(parent.Done()), "the child's work still runs")
	leave()
	require.Eventually(t, func() bool { return isClosed(parent.Done()) }, wait, time.Millisecond)
	assert.True(t, closedChild.Load())
	assert.True(t, isClosed(child.Done()))
}

// A child of a retired parent is born retired and done.
func TestAChildOfARetiredParentIsDone(t *testing.T) {
	parent := New(nil)
	parent.Retire()
	child := New(parent)
	assert.True(t, child.IsRetired())
	assert.True(t, isClosed(child.Done()))
	_, ok := child.Enter()
	assert.False(t, ok)
}

// Drain waits for the work; a deadline abandons the scope, which tells the
// tail to stop, and reports the cut.
func TestDrainAbandonsAtTheDeadline(t *testing.T) {
	s := New(nil)
	stopped := make(chan struct{})
	require.True(t, s.Go(func(ctx context.Context) {
		<-s.Tail(ctx).Done()
		close(stopped)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Drain(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "abandoned")
	select {
	case <-stopped:
	case <-time.After(wait):
		t.Fatal("the abandoned tail was not told to stop")
	}
	require.Eventually(t, func() bool { return isClosed(s.Done()) }, wait, time.Millisecond)

	quick := New(nil)
	require.True(t, quick.Go(func(ctx context.Context) { <-ctx.Done() }))
	assert.NoError(t, quick.Drain(context.Background()), "work that honours the work context drains")
}
