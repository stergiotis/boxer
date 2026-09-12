package engine

import (
	"runtime"
	"sync"
)

// MinParallel is the range length below which [Engine.ParallelFor] runs
// inline: goroutine fan-out costs more than a few thousand cheap iterations.
const MinParallel = 4096

// ReduceChunk is the fixed chunk length of [Engine.ReduceFloat64]. It is a
// constant, not a function of the worker count, so the fold order — and
// therefore the bits of the result — do not change with the machine.
const ReduceChunk = 4096

// Engine carries the worker count and reusable scratch for the sweeps.
type Engine struct {
	workers int
	next    []bool
	sparse  []int32
	partial []float64
}

// New returns an engine with the given worker count; zero or less means
// runtime.GOMAXPROCS(0).
func New(workers int) *Engine {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	return &Engine{workers: workers}
}

// Workers is the fan-out width.
func (e *Engine) Workers() int { return e.workers }

// ParallelFor runs body over [0, n) split into up to Workers contiguous
// chunks, one goroutine each; below [MinParallel] or with one worker it runs
// inline. body must write only to state indexed within [lo, hi).
func (e *Engine) ParallelFor(n int, body func(worker, lo, hi int)) {
	if e.workers <= 1 || n < MinParallel || n < e.workers {
		body(0, 0, n)
		return
	}
	chunk := (n + e.workers - 1) / e.workers
	var wg sync.WaitGroup
	for w, lo := 0, 0; lo < n; w, lo = w+1, lo+chunk {
		hi := min(lo+chunk, n)
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			body(w, lo, hi)
		}(w, lo, hi)
	}
	wg.Wait()
}

// ReduceFloat64 sums body over [0, n) with a summation order independent of
// the worker count: body is evaluated per [ReduceChunk]-sized chunk, chunk
// partials are stored at their index, and the partials are folded in chunk
// order. body(lo, hi) must return the chunk's own sum, folded in index order.
func (e *Engine) ReduceFloat64(n int, body func(lo, hi int) float64) float64 {
	chunks := (n + ReduceChunk - 1) / ReduceChunk
	if chunks <= 1 {
		return body(0, n)
	}
	if cap(e.partial) < chunks {
		e.partial = make([]float64, chunks)
	}
	partial := e.partial[:chunks]
	e.ParallelFor(chunks, func(_, clo, chi int) {
		for c := clo; c < chi; c++ {
			lo := c * ReduceChunk
			hi := min(lo+ReduceChunk, n)
			partial[c] = body(lo, hi)
		}
	})
	var sum float64
	for _, p := range partial {
		sum += p
	}
	return sum
}

// ChunkedJobs runs body over jobs [0, n) in fixed chunks of size chunk that
// workers claim in any order; the caller folds per-chunk outputs in chunk
// index order to keep the result independent of scheduling. body receives
// the chunk index and its job range.
func (e *Engine) ChunkedJobs(n, chunk int, body func(chunkIdx, lo, hi int)) {
	chunks := (n + chunk - 1) / chunk
	if e.workers <= 1 || chunks <= 1 {
		for c := range chunks {
			body(c, c*chunk, min((c+1)*chunk, n))
		}
		return
	}
	var next atomicCounter
	var wg sync.WaitGroup
	for range min(e.workers, chunks) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				c := next.claim()
				if c >= chunks {
					return
				}
				body(c, c*chunk, min((c+1)*chunk, n))
			}
		}()
	}
	wg.Wait()
}

// Fanout runs body once per index in [0, k), each on its own goroutine when
// the engine has more than one worker; body(w) must write only to state it
// owns for index w.
func (e *Engine) Fanout(k int, body func(w int)) {
	if e.workers <= 1 || k <= 1 {
		for w := range k {
			body(w)
		}
		return
	}
	var wg sync.WaitGroup
	for w := range k {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			body(w)
		}(w)
	}
	wg.Wait()
}
