//go:build llm_generated_opus47

package progressbar

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const refreshInterval = 250 * time.Millisecond

// DetailFunc is called on each render to format domain-specific status text.
// It receives the current processed count and total (0 if indeterminate).
type DetailFunc func(processed int64, total int64) string

// Bar is a terminal progress bar supporting determinate (known total) and
// indeterminate (total=0, spinner only) modes. It is safe for concurrent use.
//
// The rendering path is ANSI-to-stderr by default (see ansi.go). Tests or
// non-terminal consumers can swap the writer via SetWriter.
type Bar struct {
	total     int64 // 0 = indeterminate
	processed atomic.Int64

	label     string
	detailFn  DetailFunc
	startTime time.Time
	w         io.Writer
	isTTY     bool

	eta *Estimator

	// writeMu serialises every byte written to inst.w. Held by render() and
	// by LogWriter.Write so log lines and bar frames never interleave
	// mid-sequence. See log.go.
	writeMu sync.Mutex

	done    chan struct{}
	stopped chan struct{}
}

// New creates a progress bar. Pass total=0 for indeterminate mode.
// The label describes what is being counted (e.g. "tiles", "pages").
func New(total int64, label string) (bar *Bar) {
	bar = &Bar{
		total:     total,
		label:     label,
		startTime: time.Now(),
		w:         os.Stderr,
		isTTY:     stderrIsTTY(),
		eta:       NewEstimator(),
		done:      make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	return
}

// SetDetail registers a callback that returns domain-specific status text
// appended after the bar on each render. May be called before Start.
func (inst *Bar) SetDetail(fn DetailFunc) {
	inst.detailFn = fn
}

// SetWriter overrides the output writer (default: os.Stderr). Also flips the
// bar out of TTY mode so tests/log-capture receive line-based output.
func (inst *Bar) SetWriter(w io.Writer) {
	inst.w = w
	inst.isTTY = false
}

// Estimator exposes the underlying ETA estimator so callers (e.g. the egui2
// demo) can inspect smoothed rate/trend and the damped vs. raw ETA.
func (inst *Bar) Estimator() *Estimator { return inst.eta }

func (inst *Bar) Start(ctx context.Context) {
	inst.eta.Start(inst.startTime, 0)
	go inst.renderLoop(ctx)
}

func (inst *Bar) Stop() {
	close(inst.done)
	<-inst.stopped
	inst.writeMu.Lock()
	inst.renderLocked()
	inst.finalizeLineLocked()
	inst.writeMu.Unlock()
}

// Tick increments the processed counter by 1.
func (inst *Bar) Tick() {
	inst.processed.Add(1)
}

// Add increments the processed counter by delta.
func (inst *Bar) Add(delta int64) {
	inst.processed.Add(delta)
}

// Processed returns the current count.
func (inst *Bar) Processed() int64 {
	return inst.processed.Load()
}

// Total returns the configured total (0 for indeterminate bars).
func (inst *Bar) Total() int64 {
	return inst.total
}

// Elapsed returns time since the bar was constructed.
func (inst *Bar) Elapsed() time.Duration {
	return time.Since(inst.startTime)
}

func (inst *Bar) renderLoop(ctx context.Context) {
	defer close(inst.stopped)
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-inst.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.render()
		}
	}
}

func (inst *Bar) render() {
	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()
	inst.renderLocked()
}

// renderLocked assumes inst.writeMu is held. Callers that hold the mutex
// for a broader critical section (Stop, LogWriter coordination) use this
// form directly.
func (inst *Bar) renderLocked() {
	n := inst.processed.Load()
	now := time.Now()
	elapsed := now.Sub(inst.startTime)

	inst.eta.Update(now, n)

	detail := ""
	if inst.detailFn != nil {
		detail = inst.detailFn(n, inst.total)
	}

	inst.renderANSI(n, elapsed, detail)
}
