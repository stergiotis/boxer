//go:build llm_generated_opus47

package h3

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ErrClosed is returned by [Runtime.AcquireE] after the runtime has been
// closed.
var ErrClosed = eh.New("h3: runtime closed")

// ErrHandleReleased signals a use-after-release on a [Handle].
var ErrHandleReleased = eh.New("h3: handle already released")

// ErrNoWasmBytes is returned by [NewRuntime] when the embedded wasm slice
// is empty (the build pipeline has not yet populated h3.wasm).
var ErrNoWasmBytes = eh.New("h3: embedded wasm is empty")

// ErrExportNotFound is returned by [NewRuntime] when the instantiated
// module is missing one of the h3_* ABI exports; the build artifact is
// stale or not the expected bridge.
var ErrExportNotFound = eh.New("h3: required wasm export missing")

// ErrAllocReturnedZero is surfaced when ext_alloc returns a null guest
// offset; the guest allocator is exhausted or the module is in a bad state.
var ErrAllocReturnedZero = eh.New("h3: wasm ext_alloc returned null offset")

// ErrMemoryOOB signals that a wasm linear-memory read or write crossed the
// guest memory bounds — a bug in this package, not in the caller.
var ErrMemoryOOB = eh.New("h3: wasm memory access out of bounds")

// ErrGrowProtocol is returned by variable-arity bulk calls when the
// one-retry growth loop does not settle after the second attempt; the
// guest reported `needed` incorrectly.
var ErrGrowProtocol = eh.New("h3: variable-arity grow protocol did not settle in one retry")
