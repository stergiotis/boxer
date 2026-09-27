//go:build !(js || wasip1)

package application

// yieldToOtherGoroutines is a no-op natively: a frame's blocking reads on
// the client pipe park the render goroutine, which is when the scheduler
// runs everything else. See yield_wasm.go for the browser case.
func yieldToOtherGoroutines() {}
