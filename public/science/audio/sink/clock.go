package sink

import (
	"sync"
	"time"
)

// ClockI supplies wall time. The [Null] sink advances its position by it, so
// which clock a sink is handed is what decides whether playback follows the
// process clock or a test's command sequence.
type ClockI interface {
	Now() (t time.Time)
}

// RealClock reads the process clock. It is what production uses and what
// [NewNull] substitutes for a nil clock.
type RealClock struct{}

var _ ClockI = RealClock{}

// Now implements [ClockI].
func (inst RealClock) Now() (t time.Time) { return time.Now() }

// ManualClock is a clock moved by hand, for tests and headless scenes. Its
// zero value is valid and reads the zero [time.Time]; only [ManualClock.Set]
// and [ManualClock.Advance] move it.
//
// It is safe for concurrent use, so one goroutine may drive the clock while
// another polls the sink.
type ManualClock struct {
	mu  sync.Mutex
	now time.Time
}

var _ ClockI = (*ManualClock)(nil)

// NewManualClock returns a clock reading t.
func NewManualClock(t time.Time) (inst *ManualClock) {
	return &ManualClock{now: t}
}

// Now implements [ClockI].
func (inst *ManualClock) Now() (t time.Time) {
	inst.mu.Lock()
	t = inst.now
	inst.mu.Unlock()
	return t
}

// Set moves the clock to t, backwards as well as forwards. A [Null] sink
// projects its position from the clock, so a clock moved backwards moves the
// playhead back with it — never behind the anchor the projection runs from.
func (inst *ManualClock) Set(t time.Time) {
	inst.mu.Lock()
	inst.now = t
	inst.mu.Unlock()
}

// Advance moves the clock by d.
func (inst *ManualClock) Advance(d time.Duration) {
	inst.mu.Lock()
	inst.now = inst.now.Add(d)
	inst.mu.Unlock()
}
