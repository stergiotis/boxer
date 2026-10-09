package trail

import (
	"context"
	"iter"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// ForwarderI is the seam a downstream carrier adapter implements (ADR-0296
// §SD6): it is handed audit-event rows that are durable on the server,
// never buffered ones. Delivery is at least once — the prompt path and
// the backstop can both hand over the same row — and the receiving side
// deduplicates by the row's id. An error is counted and logged; the
// backstop is what sends the rows again.
type ForwarderI interface {
	Forward(ctx context.Context, events []*TrailEntity) error
}

// DefaultForwardQueue bounds the batches the prompt path holds for a
// forwarder that has not taken them yet; past it a batch is dropped from
// the prompt path, counted, and left to the backstop.
const DefaultForwardQueue = 64

// recentForwarded bounds the row ids the backstop skips because the prompt
// path forwarded them lately.
const recentForwarded = 1 << 16

// WithForwarder attaches f (ADR-0296 §SD6). The prompt path hands f each
// batch of audit events as the server takes it, on a goroutine of the
// recorder's own, behind a queue of DefaultForwardQueue batches. The
// backstop is [Recorder.ForwardWindow], which the caller schedules.
func WithForwarder(f ForwarderI) RecorderOption {
	return func(inst *Recorder) { inst.forwarder = f }
}

// forwardState is the prompt path's bookkeeping: the entities of audit
// events written but not yet durable, by row id (under mu); the queue the
// flush goroutine hands durable batches to; and the ids forwarded lately,
// for the backstop to skip (under recentMu).
type forwardState struct {
	pending map[uint64]*TrailEntity
	queue   chan []*TrailEntity
	done    chan struct{}

	recentMu  sync.Mutex
	recent    map[uint64]struct{}
	recentSeq []uint64
	recentAt  int
}

func newForwardState() (fs *forwardState) {
	return &forwardState{pending: map[uint64]*TrailEntity{}, queue: make(chan []*TrailEntity, DefaultForwardQueue), done: make(chan struct{}),
		recent: make(map[uint64]struct{}, recentForwarded), recentSeq: make([]uint64, recentForwarded)}
}

// remember keeps id among the lately forwarded, evicting the oldest.
func (inst *forwardState) remember(ids []uint64) {
	inst.recentMu.Lock()
	defer inst.recentMu.Unlock()
	for _, id := range ids {
		if _, ok := inst.recent[id]; ok {
			continue
		}
		if old := inst.recentSeq[inst.recentAt]; old != 0 {
			delete(inst.recent, old)
		}
		inst.recentSeq[inst.recentAt] = id
		inst.recentAt = (inst.recentAt + 1) % len(inst.recentSeq)
		inst.recent[id] = struct{}{}
	}
}

func (inst *forwardState) isRecent(id uint64) (yes bool) {
	inst.recentMu.Lock()
	defer inst.recentMu.Unlock()
	_, yes = inst.recent[id]
	return
}

// keep notes an audit event's entity until the store says it is durable
// or discarded. The caller holds mu.
func (inst *Recorder) keepForForward(ent *TrailEntity) {
	if inst.fwd == nil {
		return
	}
	inst.fwd.pending[ent.ID] = ent
}

// recorderObserver is the observer every store rows are built into gets:
// it relays to the caller's observer, and feeds the prompt path.
type recorderObserver struct {
	rec   *Recorder
	inner recordstore.WriteObserverI[uint64, time.Time]
}

func (inst recorderObserver) Committed(w recordstore.WrittenKey[uint64, time.Time]) {
	if inst.inner != nil {
		inst.inner.Committed(w)
	}
}

func (inst recorderObserver) Durable(ctx context.Context, batch recordstore.BatchIdT, keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	if inst.inner == nil {
		inst.rec.promptForward(keys)
		return
	}
	// Both consume the sequence: materialise once.
	var ws []recordstore.WrittenKey[uint64, time.Time]
	for w := range keys {
		ws = append(ws, w)
	}
	inst.inner.Durable(ctx, batch, func(yield func(recordstore.WrittenKey[uint64, time.Time]) bool) {
		for _, w := range ws {
			if !yield(w) {
				return
			}
		}
	})
	inst.rec.promptForward(func(yield func(recordstore.WrittenKey[uint64, time.Time]) bool) {
		for _, w := range ws {
			if !yield(w) {
				return
			}
		}
	})
}

func (inst recorderObserver) Discarded(keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	if inst.inner == nil {
		inst.rec.dropForward(keys)
		return
	}
	var ws []recordstore.WrittenKey[uint64, time.Time]
	for w := range keys {
		ws = append(ws, w)
	}
	inst.inner.Discarded(func(yield func(recordstore.WrittenKey[uint64, time.Time]) bool) {
		for _, w := range ws {
			if !yield(w) {
				return
			}
		}
	})
	inst.rec.dropForward(func(yield func(recordstore.WrittenKey[uint64, time.Time]) bool) {
		for _, w := range ws {
			if !yield(w) {
				return
			}
		}
	})
}

// storeObserver is the observer for a store rows are built into: nil when
// neither a caller's observer nor a forwarder wants one.
func (inst *Recorder) storeObserver() (o recordstore.WriteObserverI[uint64, time.Time]) {
	if inst.observer == nil && inst.fwd == nil {
		return nil
	}
	return recorderObserver{rec: inst, inner: inst.observer}
}

// promptForward hands the audit events among keys to the forward
// goroutine. Called on the flush goroutine, outside mu.
func (inst *Recorder) promptForward(keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	if inst.fwd == nil {
		return
	}
	var batch []*TrailEntity
	inst.mu.Lock()
	for w := range keys {
		if ent, ok := inst.fwd.pending[w.Key]; ok {
			delete(inst.fwd.pending, w.Key)
			batch = append(batch, ent)
		}
	}
	inst.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	select {
	case inst.fwd.queue <- batch:
	default:
		inst.forwardDropped.Add(uint64(len(batch)))
		inst.log.Warn().Int("events", len(batch)).Msg("trail: the forwarder has not taken earlier batches; this one is left to the backstop")
	}
}

// dropForward forgets the audit events among keys: they never became
// durable, so there is nothing to forward.
func (inst *Recorder) dropForward(keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	if inst.fwd == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for w := range keys {
		delete(inst.fwd.pending, w.Key)
	}
}

// forwardLoop hands queued batches to the forwarder until the queue is
// closed and drained.
func (inst *Recorder) forwardLoop() {
	defer close(inst.fwd.done)
	for batch := range inst.fwd.queue {
		inst.forward(context.Background(), batch)
	}
}

// forward is one hand-over: on success the ids are remembered for the
// backstop to skip; on failure they are counted and left to it.
func (inst *Recorder) forward(ctx context.Context, batch []*TrailEntity) (ok bool) {
	if err := inst.forwarder.Forward(ctx, batch); err != nil {
		inst.forwardDropped.Add(uint64(len(batch)))
		inst.log.Warn().Err(err).Int("events", len(batch)).Msg("trail: the forwarder refused a batch; the backstop sends it again")
		return false
	}
	ids := make([]uint64, len(batch))
	for i, e := range batch {
		ids[i] = e.ID
	}
	inst.fwd.remember(ids)
	inst.forwarded.Add(uint64(len(batch)))
	return true
}

// ForwardWindow is the backstop (ADR-0296 §SD6): it reads the audit events
// with a timestamp in [from, to) from the read store, oldest first, and
// hands them to the forwarder in batches of at most page rows, on the
// caller's goroutine, skipping ids the prompt path forwarded lately. The
// caller chooses the windows: each should reach back past the longest
// outage the recorder holds rows through, because a batch held through
// one lands with its old timestamps. n is what was handed over. Nothing
// without a forwarder or a store.
func (inst *Recorder) ForwardWindow(ctx context.Context, from, to time.Time, page int) (n int, err error) {
	if inst == nil || inst.fwd == nil {
		return 0, nil
	}
	if page <= 0 {
		page = 500
	}
	var batch []*TrailEntity
	send := func() (serr error) {
		if len(batch) == 0 {
			return nil
		}
		if !inst.forward(ctx, batch) {
			return eh.Errorf("trail: forward %d events", len(batch))
		}
		n += len(batch)
		batch = batch[:0]
		return nil
	}
	for ent, serr := range inst.scanEvents(ctx, windowPredicate(from, to)) {
		if serr != nil {
			return n, eh.Errorf("trail: backstop: %w", serr)
		}
		if ent == nil || !ent.AuditEvent.Has || inst.fwd.isRecent(ent.ID) {
			continue
		}
		batch = append(batch, ent)
		if len(batch) >= page {
			if err = send(); err != nil {
				return
			}
		}
	}
	err = send()
	return
}
