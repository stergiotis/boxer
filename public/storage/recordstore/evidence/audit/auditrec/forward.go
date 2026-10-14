package auditrec

import (
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// recentIds is a bounded set of the ids forwarded lately, the oldest
// evicted first.
type recentIds struct {
	mu   sync.Mutex
	set  map[uint64]struct{}
	ring []uint64
	at   int
}

func newRecentIds(n int) *recentIds {
	return &recentIds{set: make(map[uint64]struct{}, n), ring: make([]uint64, n)}
}

func (inst *recentIds) remember(events []audit.Event) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range events {
		if _, ok := inst.set[e.Id]; ok {
			continue
		}
		if old := inst.ring[inst.at]; old != 0 {
			delete(inst.set, old)
		}
		inst.ring[inst.at] = e.Id
		inst.at = (inst.at + 1) % len(inst.ring)
		inst.set[e.Id] = struct{}{}
	}
}

func (inst *recentIds) has(id uint64) (yes bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, yes = inst.set[id]
	return
}

// forwardPrompt queues a durable batch's events for the forwarder; a full
// queue leaves them, counted, to the backstop. The caller holds flushMu.
func (inst *Recorder[B]) forwardPrompt(events []audit.Event) {
	if inst.queue == nil || len(events) == 0 {
		return
	}
	select {
	case inst.queue <- events:
	default:
		inst.forwardDropped.Add(uint64(len(events)))
		inst.log.Warn().Int("events", len(events)).Msg("auditrec: the forwarder has not taken earlier batches; this one is left to the backstop")
	}
}

// forwardLoop hands queued batches to the forwarder until the queue is
// closed and drained.
func (inst *Recorder[B]) forwardLoop() {
	defer close(inst.fwdDone)
	for events := range inst.queue {
		ctx, cancel := context.WithTimeout(context.Background(), inst.cfg.forwardTimeout)
		inst.forward(ctx, events)
		cancel()
	}
}

// forward is one hand-over: on success the ids are remembered for the
// backstop to skip; on failure they are counted and left to it.
func (inst *Recorder[B]) forward(ctx context.Context, events []audit.Event) (ok bool) {
	if err := inst.cfg.forwarder.Forward(ctx, events); err != nil {
		inst.forwardDropped.Add(uint64(len(events)))
		inst.log.Warn().Err(err).Int("events", len(events)).Msg("auditrec: the forwarder refused a batch; the backstop sends it again")
		return false
	}
	inst.recent.remember(events)
	inst.forwarded.Add(uint64(len(events)))
	return true
}

// ForwardWindow is the backstop: it reads the stored audit events with a
// timestamp in [from, to) — of WithNode's node only, each id once, oldest
// first — and hands them to the forwarder in batches of at most page
// (500 when page is not positive), on the caller's goroutine, skipping ids
// the prompt path forwarded lately. A timestamp is the event's own time,
// so a batch held through an outage lands with old ones: a caller's
// windows reach back past the longest outage. n is what was handed over.
// Nothing without a forwarder or a store.
func (inst *Recorder[B]) ForwardWindow(ctx context.Context, from, to time.Time, page int) (n int, err error) {
	if inst == nil || inst.cfg.forwarder == nil || inst.st == nil {
		return 0, nil
	}
	if page <= 0 {
		page = 500
	}
	var batch []audit.Event
	send := func() error {
		if len(batch) == 0 {
			return nil
		}
		if !inst.forward(ctx, batch) {
			return eb.Build().Int("events", len(batch)).Errorf("auditrec: backstop: the forwarder refused a batch")
		}
		n += len(batch)
		batch = nil
		return nil
	}
	inst.readMu.Lock()
	defer inst.readMu.Unlock()
	if inst.stClosed {
		return 0, ErrClosed
	}
	seen := make(map[uint64]struct{})
	opts := recordstore.ScanOpts{ExtraPredicate: and(inst.windowPredicate(from, to), inst.nodePredicate())}
	for e, serr := range inst.st.ScanEvents(ctx, opts) {
		if serr != nil {
			return n, eh.Errorf("auditrec: backstop: %w", serr)
		}
		if !inst.ofNode(&e.Row) {
			continue
		}
		if _, dup := seen[e.Id]; dup || inst.recent.has(e.Id) {
			continue
		}
		seen[e.Id] = struct{}{}
		batch = append(batch, e)
		if len(batch) >= page {
			if err = send(); err != nil {
				return
			}
		}
	}
	err = send()
	return
}

// CatchUp forwards [from, until) window by window, each at most window
// long (one window when window is not positive), and returns where it got
// to: until, or the start of the window that failed, with the error. n is
// what was handed over. When to run it, and where to keep where it got
// to, are the caller's.
func (inst *Recorder[B]) CatchUp(ctx context.Context, from, until time.Time, window time.Duration) (reached time.Time, n int, err error) {
	reached = from
	for reached.Before(until) {
		if err = ctx.Err(); err != nil {
			return
		}
		to := until
		if window > 0 && to.Sub(reached) > window {
			to = reached.Add(window)
		}
		var k int
		k, err = inst.ForwardWindow(ctx, reached, to, 0)
		n += k
		if err != nil {
			return
		}
		reached = to
	}
	return
}
