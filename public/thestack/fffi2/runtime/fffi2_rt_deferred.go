package runtime

// DeferredBlockScope captures opcode sequences into keyed blocks.
// During capture, it redirects Fffi2.SendIntermediate to write into
// a temporary buffer. Each block's bytes contain complete framed messages
// that the Rust interpreter can replay via begin_consume_message.

import (
	"bytes"
	"encoding/binary"
	"sort"
	"sync"
	"sync/atomic"
)

// Buffer-sizing constants for the hinted constructor. See ADR-0049
// "Per-call-site buffer-size memoization for FFFI2 deferred-block scopes".
const (
	// deferredDataBufFloor is the lower bound on the initial dataBuf
	// capacity even when the per-kind hint is zero (cold start) or has
	// decayed to a very small value. 4 KiB is large enough that the
	// smallest scopes (tooltip targets at ~0.5 KiB observed) start
	// without a re-grow on their first opcode write, and small enough
	// that the cold-start allocation cost is bounded.
	deferredDataBufFloor = 4 * 1024

	// deferredTempBufInitialCap is the initial capacity of the per-cycle
	// capture buffer. ADR-0049 left it un-hinted in M1 and said to
	// "promote to a hint in a follow-up if profiling shows it's worth
	// it". Profiling says it isn't: a hint was built and measured on
	// 2026-08-19 and moved nothing, because the large capture buffers
	// are not this one — DockAreaFluid.Tab captures each tab body into
	// its own detached bytes.Buffer, which never touches a scope. Once
	// scopes are pooled this buffer also keeps whatever capacity it
	// grew to, which covers the cold path well enough. See the
	// 2026-08-19 Update to ADR-0049.
	deferredTempBufInitialCap = 4 * 1024

	// deferredPooledBufferCeiling bounds what a scope may carry back
	// into its pool. A scope whose dataBuf or tempBuf grew past it is
	// dropped for the collector instead, so one pathological frame
	// cannot pin an outsized allocation for the pool's lifetime
	// (golang/go#23199).
	//
	// It sits far above the working set deliberately. The same pool in
	// fffi2/typed shipped with a ceiling *below* its working set and
	// therefore discarded every buffer that mattered — measured at 47.8 %
	// of all bytes allocated before it was raised. A ceiling is insurance
	// against an outlier, not a sizing mechanism; put it under the
	// steady state and it becomes the bug it was meant to prevent.
	deferredPooledBufferCeiling = 4 * 1024 * 1024

	// deferredHintDecayShift drives the slow exponential decay of the
	// hint when the observed high-water mark is below the current
	// hint:  next = old - ((old - observed) >> deferredHintDecayShift).
	// Shift=5 ≈ 1/32 per frame ≈ 30-frame half-life — fast enough to
	// recover from a transient fat-frame spike within ~half a second
	// at 60 Hz, slow enough that frame-to-frame jitter does not
	// destabilise the hint into oscillation.
	deferredHintDecayShift = 5
)

// ScopeHint is the per-kind handle a deferred-block scope is constructed
// against: the size hint that pre-sizes its buffers, and the pool that
// recycles them between frames. One instance per IDL deferred-block kind,
// obtained from [RegisterScopeHint].
//
// The two halves work together. The hint sizes a *cold* buffer — the first
// scope of a kind after start-up, or after the pool has been drained by a
// collection — and the pool removes the per-frame allocation entirely for
// every scope after that. Neither subsumes the other: without the hint a
// cold scope re-grows from the floor, and without the pool every frame pays
// a fresh `make` at the hinted size, which ADR-0049 recorded as the cost it
// was leaving on the table.
type ScopeHint struct {
	// kind is the IDL deferred-block name (e.g. "cells", "tabBody").
	kind string
	// hint is the smoothed high-water mark of dataBuf in wire bytes; see
	// [DeferredBlockScope.ReleaseWithHint] for the update rule.
	hint atomic.Uint64
	// pool holds released *DeferredBlockScope values of this kind.
	// Per-kind rather than global so a scope built for one kind's
	// working size is never handed to a kind with a different one.
	pool sync.Pool
}

// registry of ScopeHints. Kept as a flat slice rather than a map because
// the working set is small (one entry per IDL deferred-block kind, ≤16 in
// practice) and the snapshot path wants a stable iteration order keyed by
// kind.
var (
	scopeHintsMu sync.RWMutex
	scopeHints   []*ScopeHint
)

// RegisterScopeHint returns the singleton [ScopeHint] for the given
// scope-kind name, allocating it on first call. Idempotent: a second call
// with the same name returns the same pointer, so the codegen can wire
// `runtime.RegisterScopeHint("cells")` once per kind and every scope
// instance of that kind shares one hint and one pool.
//
// Concurrency: safe to call from any goroutine. Registration uses a short
// writer-lock; subsequent hint reads/writes and pool traffic from a scope's
// hot path go through the returned handle with no further synchronisation.
func RegisterScopeHint(kind string) *ScopeHint {
	scopeHintsMu.RLock()
	for _, sh := range scopeHints {
		if sh.kind == kind {
			scopeHintsMu.RUnlock()
			return sh
		}
	}
	scopeHintsMu.RUnlock()

	scopeHintsMu.Lock()
	defer scopeHintsMu.Unlock()
	// Re-check under the writer-lock to avoid a duplicate entry under
	// the racing-registration window between the RUnlock and Lock above.
	for _, sh := range scopeHints {
		if sh.kind == kind {
			return sh
		}
	}
	sh := &ScopeHint{kind: kind}
	scopeHints = append(scopeHints, sh)
	return sh
}

// ScopeHintSnapshot is one entry from [ScopeHintsSnapshot].
type ScopeHintSnapshot struct {
	// Kind is the IDL deferred-block name (e.g. "cells", "tabBody").
	Kind string
	// Bytes is the current hint value — the observed high-water mark
	// for this kind, smoothed by the peak-and-slow-decay update rule
	// applied at each [DeferredBlockScope.ReleaseWithHint] call.
	Bytes uint64
}

// ScopeHintsSnapshot returns the current per-kind wire-byte hints in
// kind-name order. Safe to call from any goroutine (e.g. a pprof HTTP
// handler, a slow-frame logger). The returned slice is owned by the
// caller; the underlying registry is not held across the call.
//
// Per ADR-0049, the hint table doubles as a passive observability
// surface: each entry reports what a single scope of that kind cost
// the FFFI2 wire on its most-expensive recent frame, smoothed to
// damp one-off spikes.
func ScopeHintsSnapshot() []ScopeHintSnapshot {
	scopeHintsMu.RLock()
	out := make([]ScopeHintSnapshot, 0, len(scopeHints))
	for _, sh := range scopeHints {
		out = append(out, ScopeHintSnapshot{
			Kind:  sh.kind,
			Bytes: sh.hint.Load(),
		})
	}
	scopeHintsMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// deferredBlockEntry stores offset/length into the shared data slab
// instead of holding its own []byte allocation.
type deferredBlockEntry struct {
	keyOff uint32
	keyLen uint32
	bufOff uint32
	bufLen uint32
}

// DeferredBlockScope manages capture of deferred opcode blocks.
//
// All block key bytes and opcode bytes are appended to a single
// contiguous slab (dataBuf). Individual blocks are tracked as
// offset+length pairs into the slab, eliminating per-cell allocations.
//
// Lifecycle:
//  1. scope := NewDeferredBlockScope(fffi, endianness)         (or NewDeferredBlockScopeHinted)
//  2. scope.Begin(key...)     <- redirect Fffi2 to capture buffer
//  3. ... widget.Send() ...   <- messages go to capture buffer
//  4. scope.End()             <- restore Fffi2, store buffer
//  5. scope.SpliceInto(r)     <- write all blocks into RetainedFffiBuilder
//  6. scope.ReleaseWithHint() <- (optional) fold high-water back into hint
type DeferredBlockScope struct {
	entries   []deferredBlockEntry
	dataBuf   *bytes.Buffer // single slab for all keys + opcode data
	tempBuf   *bytes.Buffer // capture target (write pointer for Fffi2)
	capturing bool
	endianess binary.ByteOrder

	// scratch for encoding fixed-size keys without binary.Write
	keyScratch [32]byte

	// Fffi2 accessor — the typed package provides this
	getFffi func() FffiCaptureI

	// scope, when non-nil, is the per-kind hint+pool this scope was
	// built against. [DeferredBlockScope.ReleaseWithHint] folds dataBuf's
	// observed high-water mark into its hint and returns the scope to its
	// pool. See ADR-0049.
	scope *ScopeHint

	// released marks a scope that has been handed back to its pool. It
	// exists to keep use-after-release loud: before pooling, Release
	// nil'd the buffers and a stray Begin panicked on a nil map/pointer,
	// whereas a pooled scope would quietly accept the write and corrupt
	// whichever frame owns it next. Checked in Begin and End.
	released bool
}

// FffiCaptureI is the subset of Fffi2 needed for capture mode.
type FffiCaptureI interface {
	BeginCapture(buf *bytes.Buffer, endianness binary.ByteOrder)
	EndCapture()
	// AppendRawToCapture writes the given framed bytes directly into the
	// innermost capture buffer WITHOUT adding a new frame header. Used when
	// a caller has already captured framed messages into a detached buffer
	// and wants to inject them into an active deferred-block capture scope
	// (e.g. the DockArea iter wrapper, which captures each tab body ahead
	// of Send and flushes all bodies in order at Send time).
	AppendRawToCapture(raw []byte)
	// IsCapturing reports whether a deferred-block capture frame is
	// currently active. Used by callers (notably Fetcher.invoke) that
	// must NOT run inside captures — synchronous fetcher requests would
	// be buffered while the response read blocked on the empty pipe.
	IsCapturing() bool
}

// NewDeferredBlockScope creates a new scope with a fixed-size initial
// dataBuf. Equivalent to [NewDeferredBlockScopeHinted] with a nil hint,
// retained for non-codegen callers and backward compatibility.
//
// getFffi returns the current Fffi2 instance.
func NewDeferredBlockScope(
	getFffi func() FffiCaptureI,
	endianness binary.ByteOrder,
) *DeferredBlockScope {
	return NewDeferredBlockScopeHinted(getFffi, endianness, nil)
}

// NewDeferredBlockScopeHinted creates a new scope whose dataBuf is
// pre-sized from the supplied hint (see [RegisterScopeHint] for the
// per-kind atomic). A nil hint behaves like [NewDeferredBlockScope]
// and falls back to the floor capacity.
//
// Pre-sizing eliminates the per-doubling growSlice traffic that
// otherwise dominates the per-frame protocol marshalling cost on
// widgets like c.EndETable's cells block, where dataBuf reliably
// grows to a kind-specific high-water mark every frame. The hint is
// updated by [DeferredBlockScope.ReleaseWithHint] at the end of the
// scope's life. See ADR-0049 for the design rationale.
func NewDeferredBlockScopeHinted(
	getFffi func() FffiCaptureI,
	endianness binary.ByteOrder,
	sh *ScopeHint,
) *DeferredBlockScope {
	if sh != nil {
		if v := sh.pool.Get(); v != nil {
			inst := v.(*DeferredBlockScope)
			// Buffers and entries were already emptied on release; they
			// keep the capacity they grew to, which is the whole point.
			inst.getFffi = getFffi
			inst.endianess = endianness
			inst.scope = sh
			inst.capturing = false
			inst.released = false
			return inst
		}
	}
	cap := deferredDataBufFloor
	if sh != nil {
		if v := int(sh.hint.Load()); v > cap {
			cap = v
		}
	}
	return &DeferredBlockScope{
		entries:   make([]deferredBlockEntry, 0, 64),
		dataBuf:   bytes.NewBuffer(make([]byte, 0, cap)),
		tempBuf:   bytes.NewBuffer(make([]byte, 0, deferredTempBufInitialCap)),
		getFffi:   getFffi,
		endianess: endianness,
		scope:     sh,
	}
}

// Begin starts capturing opcodes for the given key components.
//
// Key components are serialized as their binary representations
// directly into the slab, avoiding binary.Write interface overhead:
//   - uint64 -> 8 bytes LE
//   - uint32 -> 4 bytes LE
//   - string -> 4 bytes LE length + UTF-8 bytes
func (inst *DeferredBlockScope) Begin(keyParts ...any) {
	if inst.released {
		panic("DeferredBlockScope.Begin called after ReleaseWithHint — the scope has been returned to its pool and may already belong to another frame")
	}
	if inst.capturing {
		panic("DeferredBlockScope.Begin called while already capturing — missing End() call")
	}

	// Serialize key directly into dataBuf (no temp keyBuf allocation)
	keyStart := uint32(inst.dataBuf.Len())
	for _, part := range keyParts {
		switch v := part.(type) {
		case uint64:
			inst.endianess.PutUint64(inst.keyScratch[:8], v)
			inst.dataBuf.Write(inst.keyScratch[:8])
		case uint32:
			inst.endianess.PutUint32(inst.keyScratch[:4], v)
			inst.dataBuf.Write(inst.keyScratch[:4])
		case int:
			inst.endianess.PutUint64(inst.keyScratch[:8], uint64(v))
			inst.dataBuf.Write(inst.keyScratch[:8])
		case string:
			inst.endianess.PutUint32(inst.keyScratch[:4], uint32(len(v)))
			inst.dataBuf.Write(inst.keyScratch[:4])
			inst.dataBuf.WriteString(v)
		default:
			panic("unsupported deferred block key type")
		}
	}
	keyLen := uint32(inst.dataBuf.Len()) - keyStart

	// Pre-register entry with key info; bufOff/bufLen filled in End()
	inst.entries = append(inst.entries, deferredBlockEntry{
		keyOff: keyStart,
		keyLen: keyLen,
	})

	// Redirect Fffi2 sends to capture buffer
	inst.tempBuf.Reset()
	inst.getFffi().BeginCapture(inst.tempBuf, inst.endianess)
	inst.capturing = true
}

// End stops capturing and stores the block.
func (inst *DeferredBlockScope) End() {
	if inst.released {
		panic("DeferredBlockScope.End called after ReleaseWithHint — the scope has been returned to its pool and may already belong to another frame")
	}
	if !inst.capturing {
		return
	}

	// Restore Fffi2 to normal send mode
	inst.getFffi().EndCapture()

	// Append captured opcode bytes to the slab
	bufStart := uint32(inst.dataBuf.Len())
	inst.dataBuf.Write(inst.tempBuf.Bytes())
	bufLen := uint32(inst.tempBuf.Len())

	// Update the entry that was pre-registered in Begin()
	e := &inst.entries[len(inst.entries)-1]
	e.bufOff = bufStart
	e.bufLen = bufLen

	inst.capturing = false
}

// BlockCount returns the number of captured blocks.
func (inst *DeferredBlockScope) BlockCount() uint32 {
	return uint32(len(inst.entries))
}

// WriteToFixedKey serializes the block map with a fixed key layout.
// Reads key and buffer data from the slab via offset+length.
//
// Wire format:
//
//	u32: block_count
//	for each block:
//	  [key_bytes]          (fixed size, determined by KeyTypes)
//	  u32: block_byte_length
//	  [u8; block_byte_length]: framed opcode messages
func (inst *DeferredBlockScope) WriteToFixedKey(w *bytes.Buffer) (err error) {
	data := inst.dataBuf.Bytes()
	err = binary.Write(w, inst.endianess, uint32(len(inst.entries)))
	if err != nil {
		return
	}
	for i := range inst.entries {
		e := &inst.entries[i]
		// Write key bytes (from slab)
		_, err = w.Write(data[e.keyOff : e.keyOff+e.keyLen])
		if err != nil {
			return
		}
		// Buffer length + bytes (from slab)
		err = binary.Write(w, inst.endianess, e.bufLen)
		if err != nil {
			return
		}
		_, err = w.Write(data[e.bufOff : e.bufOff+e.bufLen])
		if err != nil {
			return
		}
	}
	return
}

// Reset clears all blocks for reuse across frames.
func (inst *DeferredBlockScope) Reset() {
	inst.entries = inst.entries[:0]
	inst.dataBuf.Reset()
	inst.tempBuf.Reset()
	inst.capturing = false
}

// ReleaseWithHint finalises the scope and folds its observed dataBuf
// high-water mark back into the per-kind hint via a peak-ratchet +
// slow-decay CAS loop:
//
//	observed >= old : next = observed                          (ratchet up)
//	observed <  old : next = old - ((old - observed) >> N)     (decay)
//
// where N = deferredHintDecayShift (≈30-frame half-life at the
// default shift of 5). The buffers are released to GC; the scope
// must not be touched afterwards. Defensive: if [End] was forgotten
// (capturing == true), EndCapture is invoked to restore Fffi2
// routing so the next frame does not write into a buffer that has
// been handed to GC.
//
// Idempotent in the sense that calling on a scope without a hint
// (scope == nil) collapses to a no-op cleanup. See ADR-0049.
func (inst *DeferredBlockScope) ReleaseWithHint() {
	if inst.capturing {
		inst.getFffi().EndCapture()
		inst.capturing = false
	}
	sh := inst.scope
	if sh != nil && inst.dataBuf != nil {
		FoldSizeHint(&sh.hint, uint64(inst.dataBuf.Len()))
	}

	// Recycle, unless an outlier frame grew a buffer past the ceiling.
	// Emptying rather than dropping is what removes the per-frame
	// allocation: both buffers and the entries slice keep their capacity
	// for the next scope of this kind.
	if sh != nil && inst.dataBuf != nil && inst.tempBuf != nil &&
		inst.dataBuf.Cap() <= deferredPooledBufferCeiling &&
		inst.tempBuf.Cap() <= deferredPooledBufferCeiling {
		inst.entries = inst.entries[:0]
		inst.dataBuf.Reset()
		inst.tempBuf.Reset()
		inst.getFffi = nil
		inst.scope = nil
		inst.released = true
		sh.pool.Put(inst)
		return
	}

	inst.entries = nil
	inst.dataBuf = nil
	inst.tempBuf = nil
	inst.getFffi = nil
	inst.scope = nil
	inst.released = true
}
