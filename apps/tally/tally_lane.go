package tally

import (
	"context"
	"sync"
)

// lane runs one background computation at a time, keyed by what it is for,
// and hands the render thread the result when it is there. It is the
// smallest shape of play's node lanes: a new key cancels the old run, a
// repeated key is a cache hit, and nothing in here touches the bindings —
// the frame polls.
type lane[T any] struct {
	mu      sync.Mutex
	key     string
	gen     uint64
	running bool
	done    bool
	val     T
	err     error
	cancel  context.CancelFunc
}

// demand returns the state for key, starting run when key is new. busy is
// true while the run is in flight; done and err are meaningful once it is
// not.
func (l *lane[T]) demand(key string, run func(ctx context.Context) (T, error)) (val T, done bool, err error, busy bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key != l.key || (!l.done && !l.running) {
		l.start(key, run)
	}
	return l.val, l.done, l.err, l.running
}

// invalidate forgets the current key so the next demand re-runs it.
func (l *lane[T]) invalidate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.key, l.running, l.done = "", false, false
	var zero T
	l.val, l.err = zero, nil
}

func (l *lane[T]) start(key string, run func(ctx context.Context) (T, error)) {
	if l.cancel != nil {
		l.cancel()
	}
	l.gen++
	gen := l.gen
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.key, l.running, l.done = key, true, false
	var zero T
	l.val, l.err = zero, nil
	go func() {
		v, err := run(ctx)
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.gen != gen {
			return // superseded
		}
		l.val, l.err, l.done, l.running = v, err, true, false
	}()
}

// close cancels whatever is in flight.
func (l *lane[T]) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}
