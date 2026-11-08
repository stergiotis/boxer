// Package hostwake lets code off the render goroutine ask the imzero2 client
// for a pass now (ADR-0363 §M4, proposed).
//
// The render loop builds a frame only once the client has started a pass, so
// under the reactive cadence (ADR-0062) work that lands between passes — an
// agent's command queued, a background result — waits for the client's
// heartbeat or for input. [Wake] asks for a pass at once. The application that
// launched the client installs the waker with [Set]; until then, and on a
// client that cannot be woken, Wake does nothing. A wake carries no data: the
// pass it brings draws what the render goroutine has.
package hostwake

import "sync/atomic"

var waker atomic.Pointer[func()]

// Set installs the function Wake calls; nil removes it.
func Set(f func()) {
	if f == nil {
		waker.Store(nil)
		return
	}
	waker.Store(&f)
}

// Wake asks the client for a pass. It never blocks, and wakes that arrive
// before the pass they bring are served by that one pass.
func Wake() {
	if f := waker.Load(); f != nil {
		(*f)()
	}
}
