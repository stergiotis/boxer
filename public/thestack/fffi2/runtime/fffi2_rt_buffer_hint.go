package runtime

import (
	"bytes"
	"sync"
	"sync/atomic"
)

// Adaptive sizing for pooled byte buffers. ADR-0049 introduced the rule for
// deferred-block scopes ([ScopeHint]); this file holds the parts every other
// FFFI2 buffer pool shares, so the smoothing curve cannot drift between them:
// the fold rule itself ([FoldSizeHint]), the retention ceiling derived from a
// hint ([PooledBufferCeiling]), and [BufferHint] — a hinted pool for DETACHED
// capture buffers, the ones that belong to no scope.
//
// A hint serves one of two jobs, and which one depends on how homogeneous
// its population is. For a [ScopeHint] — one IDL block kind, one working
// size — it sizes COLD buffers as well as bounding retention. For a
// [BufferHint], whose kind can cover regions of wildly different sizes, it
// bounds retention only; see [BufferHint.Acquire].
const (
	// pooledCeilingSlack multiplies a smoothed high-water mark to give the
	// retention ceiling. bytes.Buffer grows by doubling, so the capacity
	// carrying a payload of h bytes is under 2h: a slack of 2 retains the
	// buffer that just carried the working set, while still dropping the
	// outlier that carried several times it.
	pooledCeilingSlack = 2

	// PooledCeilingMin is the floor of the adaptive ceiling: below it the
	// ceiling never goes, so a pool that has only ever seen small buffers
	// does not drop the first big one it is handed.
	//
	// Between this and PooledCeilingMax the ceiling follows the working set
	// in both directions, which is the property a fixed ceiling cannot have:
	// shipped under the steady state it discards every buffer that matters
	// (measured twice — 4 KiB in ADR-0049's 2026-08-18 Update, then 256 KiB
	// in its 2026-08-23 Update), and shipped far above it, it stops being
	// insurance.
	PooledCeilingMin = 256 * 1024

	// PooledCeilingMax is the cap on the adaptive ceiling, and the
	// golang/go#23199 insurance: one pathological frame must not pin an
	// arbitrarily large allocation for the pool's lifetime.
	PooledCeilingMax = 4 * 1024 * 1024

	// detachedBufferFloor is the capacity of a cold [BufferHint] buffer,
	// mirroring deferredDataBufFloor for scopes: large enough that a small
	// region starts without a re-grow, small enough that a frame which finds
	// the pool drained does not allocate the largest region's size once per
	// region. It is the whole cold-sizing story here — see
	// [BufferHint.Acquire] for why the hint stays out of it.
	detachedBufferFloor = 4 * 1024
)

// FoldSizeHint applies the peak-ratchet + slow-decay update rule to one hint.
//
// The rule is:
//
//	observed >= old : next = observed                          (ratchet up)
//	observed <  old : next = old - ((old - observed) >> N)     (decay)
//
// where N = deferredHintDecayShift (≈30-frame half-life at the default shift
// of 5). Shared by every hint in FFFI2 so the two directions cannot drift
// apart between pools. Safe to call from any goroutine.
//
// Fold only observations that are candidates for the quantity being tracked.
// A hint shared by a whole population — fffi2/typed's RetainedFffiBuilder
// pool, whose acquisitions are overwhelmingly small widget builders — is
// dragged to the population's floor if every member folds, and pays an atomic
// write per call to get there. See the participation floor in that pool's
// putInPool.
func FoldSizeHint(h *atomic.Uint64, observed uint64) {
	for {
		old := h.Load()
		var next uint64
		if observed >= old {
			next = observed
		} else {
			next = old - ((old - observed) >> deferredHintDecayShift)
		}
		if next == old || h.CompareAndSwap(old, next) {
			return
		}
	}
}

// PooledBufferCeiling maps a smoothed high-water mark to the retention
// ceiling a pool should apply to a buffer's capacity: the hint times
// pooledCeilingSlack, clamped to [PooledCeilingMin, PooledCeilingMax].
func PooledBufferCeiling(hint uint64) uint64 {
	if hint >= PooledCeilingMax {
		return PooledCeilingMax
	}
	c := hint * pooledCeilingSlack
	if c < PooledCeilingMin {
		return PooledCeilingMin
	}
	if c > PooledCeilingMax {
		return PooledCeilingMax
	}
	return c
}

// BufferHint is the sizing + recycling handle for a kind of DETACHED capture
// buffer: one a caller redirects opcodes into via Fffi2.BeginCapture without
// a [DeferredBlockScope] owning it. DockAreaFluid.Tab is the case this exists
// for — each dock tab body is captured into its own buffer every frame, and
// before this it allocated a zero-capacity bytes.Buffer per tab per frame and
// re-grew it by doubling, which an allocation profile on 2026-08-23 put at
// ~1.25 GB per 20 s of run time.
//
// One instance per kind, from [RegisterBufferHint]; hold it in a package-level
// var, not per frame.
type BufferHint struct {
	// kind names the buffer's call site (e.g. "dockTabBody"), so two
	// unrelated working sizes never share a pool.
	kind string
	// hint is the smoothed high-water mark of released buffer lengths in
	// wire bytes; see [FoldSizeHint] for the update rule.
	hint atomic.Uint64
	// pool holds released buffers of this kind.
	pool sync.Pool
}

// registry of BufferHints, kept flat for the same reason scopeHints is: the
// working set is one entry per detached-capture call site.
var (
	bufferHintsMu sync.RWMutex
	bufferHints   []*BufferHint
)

// RegisterBufferHint returns the singleton [BufferHint] for the given kind
// name, allocating it on first call. Idempotent: a second call with the same
// name returns the same pointer, so a package-level var initialised from it
// gives every acquisition of that kind one hint and one pool.
//
// Concurrency: safe from any goroutine. Registration takes a short writer
// lock; Acquire and Release afterwards go through the returned handle with no
// further synchronisation beyond the hint's own atomics and sync.Pool.
func RegisterBufferHint(kind string) *BufferHint {
	bufferHintsMu.RLock()
	for _, bh := range bufferHints {
		if bh.kind == kind {
			bufferHintsMu.RUnlock()
			return bh
		}
	}
	bufferHintsMu.RUnlock()

	bufferHintsMu.Lock()
	defer bufferHintsMu.Unlock()
	// Re-check: another goroutine may have registered between the two locks.
	for _, bh := range bufferHints {
		if bh.kind == kind {
			return bh
		}
	}
	bh := &BufferHint{kind: kind}
	bufferHints = append(bufferHints, bh)
	return bh
}

// Acquire returns an empty buffer for this kind. A pooled buffer keeps the
// capacity it grew to; a COLD one starts at detachedBufferFloor.
//
// Cold buffers are deliberately NOT sized from the hint, unlike a
// [ScopeHint]'s dataBuf. A scope hint is per IDL block kind, so its
// population is one call site with one working size; a BufferHint's kind is
// a call site that may serve wildly different regions — every dock tab of
// every dock area shares "dockTabBody", where a markdown book and a one-line
// loading placeholder are both tab bodies. ADR-0049's 2026-08-19 Update
// measured that shape and rejected it: "one shared hint would size every tab
// to the largest tab, allocating more than the doubling it saves". The hint
// here governs the retention ceiling, where sizing to the largest member is
// exactly right, and nothing else.
//
// The caller must hand the buffer back with [BufferHint.Release] once its
// bytes have been consumed — and not before: Release recycles the buffer,
// so any slice still aliasing it (bytes.Buffer.Bytes()) dangles afterwards.
func (inst *BufferHint) Acquire() (buf *bytes.Buffer) {
	if v := inst.pool.Get(); v != nil {
		buf = v.(*bytes.Buffer)
		buf.Reset()
		return
	}
	return bytes.NewBuffer(make([]byte, 0, detachedBufferFloor))
}

// Release folds the buffer's observed size into this kind's hint and recycles
// it, unless it grew past the ceiling that hint implies — in which case it
// goes to the collector instead, so one outlier frame cannot pin an outsized
// allocation. A nil buffer is a no-op.
func (inst *BufferHint) Release(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	FoldSizeHint(&inst.hint, uint64(buf.Len()))
	if uint64(buf.Cap()) <= PooledBufferCeiling(inst.hint.Load()) {
		buf.Reset()
		inst.pool.Put(buf)
	}
}
