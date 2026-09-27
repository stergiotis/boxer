//go:build js || wasip1

package application

import "runtime"

// yieldRounds is how many times Step hands the scheduler to other goroutines
// around a frame. A few rounds let a wake-up chain — an expired timer, a
// job's completion, the bus handler it triggers — finish inside one frame.
const yieldRounds = 4

// yieldToOtherGoroutines exists because, under a browser host, the render
// goroutine never parks: its reads are answered synchronously by the host's
// shim, and between exports the runtime is suspended. Goroutines other than
// the render goroutine — task producers, query lanes, bus handlers — would
// then run only if something in the frame happened to block. Natively a
// read on the pipe parks the goroutine and the scheduler runs the others;
// this is that yield made explicit (ADR-0263 SD1).
func yieldToOtherGoroutines() {
	for range yieldRounds {
		runtime.Gosched()
	}
}
