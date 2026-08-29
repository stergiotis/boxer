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
//
// A value that owns something — a file, a decoder, a device — sets
// [lane.dispose], and the lane is then the only owner: it releases the value
// it replaces, the value a superseded run produced anyway, and whatever it
// holds when it is invalidated or closed. A caller that kept its own pointer
// to such a value would be holding one the lane may close, so it does not.
type lane[T any] struct {
	mu      sync.Mutex
	key     string
	gen     uint64
	running bool
	done    bool
	val     T
	err     error
	cancel  context.CancelFunc
	dispose func(T)
}

// demand returns the state for key, starting run when key is new. busy is
// true while the run is in flight; done and err are meaningful once it is
// not.
func (l *lane[T]) demand(key string, run func(ctx context.Context) (T, error)) (val T, done bool, err error, busy bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key != l.key || (!l.done && !l.running) {
		if run == nil {
			// A poll for a key nobody started yet: report "nothing", start
			// nothing — a nil run is a question, not an order.
			var zero T
			return zero, false, nil, false
		}
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
	l.key, l.running = "", false
	l.clearLocked()
}

// clearLocked drops the held value, releasing it first when it owns
// something. The release runs under the lane's lock and on whichever
// goroutine reached here — the frame's, for a key that changed — so a
// disposer that blocks costs a frame; the ones here close a file or a
// decoder, not a network.
func (l *lane[T]) clearLocked() {
	if l.done && l.dispose != nil {
		l.dispose(l.val)
	}
	l.done = false
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
	l.clearLocked()
	l.key, l.running = key, true
	go func() {
		v, err := run(ctx)
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.gen != gen {
			// Superseded, and nobody will ever be handed this: a value that
			// owns something has to be released here or it leaks. Released
			// whatever the run returned — a value that came back beside an
			// error still holds what it opened.
			if l.dispose != nil {
				l.dispose(v)
			}
			return
		}
		l.val, l.err, l.done, l.running = v, err, true, false
	}()
}

// close cancels whatever is in flight and releases what the lane holds.
func (l *lane[T]) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.key, l.running = "", false
	l.clearLocked()
}
