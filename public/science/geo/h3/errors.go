package h3

import (
	"errors"
)

// ErrClosed is returned by [Runtime.Acquire] after the runtime has been
// closed.
var ErrClosed = errors.New("h3: runtime closed")

// ErrHandleReleased signals a use-after-release on a [Handle].
var ErrHandleReleased = errors.New("h3: handle already released")

// ErrNoWasmBytes is returned by [NewRuntime] when the embedded wasm slice
// is empty (the build pipeline has not yet populated h3.wasm).
var ErrNoWasmBytes = errors.New("h3: embedded wasm is empty")

// ErrExportNotFound is returned by [NewRuntime] when the instantiated
// module is missing one of the h3_* ABI exports; the build artifact is
// stale or not the expected bridge.
var ErrExportNotFound = errors.New("h3: required wasm export missing")

// ErrAllocReturnedZero is surfaced when ext_alloc returns a null guest
// offset; the guest allocator is exhausted or the module is in a bad state.
var ErrAllocReturnedZero = errors.New("h3: wasm ext_alloc returned null offset")

// ErrMemoryOOB signals that a wasm linear-memory read or write crossed the
// guest memory bounds — a bug in this package, not in the caller.
var ErrMemoryOOB = errors.New("h3: wasm memory access out of bounds")

// ErrGrowProtocol is returned by variable-arity bulk calls when the
// one-retry growth loop does not settle after the second attempt; the
// guest reported `needed` incorrectly.
var ErrGrowProtocol = errors.New("h3: variable-arity grow protocol did not settle in one retry")

// ErrBadContainmentMode is returned by [Handle.PolygonToCells] when the
// mode argument is outside the [ContainmentModeE] enum.
var ErrBadContainmentMode = errors.New("h3: bad containment mode")

// ErrBadPolygonGeometry is returned by [Handle.PolygonToCells] for a
// malformed polygon (non-monotone ring offsets, rings not closed under
// h3o's acceptance criteria).
var ErrBadPolygonGeometry = errors.New("h3: bad polygon geometry")

// ErrCompactMixedResolution is returned by [Handle.CompactCells] when the
// input set mixes H3 resolutions.
var ErrCompactMixedResolution = errors.New("h3: compact input mixes resolutions")

// ErrCompactDuplicateInput is returned by [Handle.CompactCells] when the
// input set contains duplicate cells.
var ErrCompactDuplicateInput = errors.New("h3: compact input contains duplicates")

// ErrDissolveInvalidCell is returned by [Handle.Dissolve] when the input
// contains a non-H3 cell.
var ErrDissolveInvalidCell = errors.New("h3: dissolve input contains a non-H3 cell")

// ErrDissolveMixedResolution is returned by [Handle.Dissolve] when the
// input set mixes H3 resolutions.
var ErrDissolveMixedResolution = errors.New("h3: dissolve input mixes resolutions")

// ErrDissolveDuplicateInput is returned by [Handle.Dissolve] when the
// input set contains duplicate cells.
var ErrDissolveDuplicateInput = errors.New("h3: dissolve input contains duplicates")
