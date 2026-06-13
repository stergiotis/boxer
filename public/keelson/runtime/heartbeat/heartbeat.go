// Package heartbeat emits periodic liveness ticks into FactsStoreI for
// the current run. Used by the carousel so a crashed process (no
// runtime-stop, no app-lifecycle stop) is distinguishable from a
// clean shutdown by the absence of a recent heartbeat row.
//
// Lifecycle: one call to Start when the run boots; the returned stop
// function cancels the goroutine and waits for any in-flight write to
// settle. The carousel pairs Start with its existing reapOnce
// shutdown hook so SIGINT/SIGTERM both trigger the cancellation.
package heartbeat

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// DefaultInterval is the cadence the carousel uses when callers don't
// override. 30s is short enough that a hang is detectable within a
// minute or two from the audit trail; long enough that the table
// doesn't grow unbounded for a long-running process.
const DefaultInterval = 30 * time.Second

// minInterval guards against pathological inputs (Start called with
// 0 or a negative value falls back to DefaultInterval).
const minInterval = 100 * time.Millisecond

// Inst owns the heartbeat goroutine. Returned by Start so the caller
// can stop the ticker on shutdown; Stop is idempotent and safe to
// call from multiple shutdown paths (defer + signal goroutine).
type Inst struct {
	cancel  context.CancelFunc
	done    chan struct{}
	stopped sync.Once
}

// Start launches a goroutine that writes one HeartbeatRow into store
// every interval until Stop is called or ctx is cancelled. RunId is
// the value to attribute every tick to; an empty RunId or nil store
// is rejected before spawning. The returned *Inst is non-nil exactly
// when err is nil.
//
// Writes that fail are logged at warn level and discarded — the
// heartbeat is best-effort, like every other write on the audit
// trail. The first tick fires at startTime + interval, not at
// startTime; the runtime-start row already captures the boot moment.
func Start(ctx context.Context, store factsstore.FactsStoreI, runId string, interval time.Duration, logger zerolog.Logger) (inst *Inst, err error) {
	if store == nil {
		err = eh.Errorf("heartbeat: store is nil")
		return
	}
	if runId == "" {
		err = eh.Errorf("heartbeat: runId is empty")
		return
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	if interval < minInterval {
		interval = minInterval
	}
	ctx, cancel := context.WithCancel(ctx)
	inst = &Inst{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go inst.tickLoop(ctx, store, runId, interval, logger)
	return
}

// tickLoop runs the heartbeat ticker until ctx is cancelled. One
// write per tick; ctx is checked between ticks so Stop returns
// promptly without waiting a full interval. The runId is captured
// into the closure so the loop has no dependency on the runinfo
// singleton.
func (inst *Inst) tickLoop(ctx context.Context, store factsstore.FactsStoreI, runId string, interval time.Duration, logger zerolog.Logger) {
	defer close(inst.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			_, werr := store.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{
				RunId: runId,
				Ts:    t.UTC(),
			})
			if werr != nil {
				logger.Warn().Err(werr).Str("run_id", runId).Msg("heartbeat: write failed")
			}
		}
	}
}

// Stop cancels the ticker and waits for the in-flight write (if any)
// to drain. Idempotent — multiple shutdown paths can call this; only
// the first call cancels, the rest return immediately.
func (inst *Inst) Stop() {
	if inst == nil {
		return
	}
	inst.stopped.Do(func() {
		inst.cancel()
		<-inst.done
	})
}
