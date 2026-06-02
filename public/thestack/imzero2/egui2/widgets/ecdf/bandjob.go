//go:build llm_generated_opus47

package ecdf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// BandJobState is the lifecycle of a background confidence-band warm-up.
type BandJobState uint8

const (
	// BandJobRunning: the O(n²) critical-value inversion is in flight.
	BandJobRunning BandJobState = iota
	// BandJobDone: the critical value is cached — ecdfbands.BandReady is
	// now true and the band renders cheaply on the next frame.
	BandJobDone
	// BandJobError: the inversion failed or was cancelled; Err carries
	// the reason. Not retried automatically.
	BandJobError
)

// BandJobSnapshot is an immutable view of a warm-up's progress, safe to
// read from the render goroutine. Fraction ∈ [0,1]; EtaMs is -1 until
// estimable, 0 once finished.
type BandJobSnapshot struct {
	State    BandJobState
	Fraction float32
	EtaMs    int64
	Note     string
	Err      error
}

// bandJob is one in-flight (or finished) confidence-band warm-up,
// registered in [bandJobs] under a caller-supplied job key.
type bandJob struct {
	// cancel aborts the background solve. Set once at construction, before
	// the job is published to [bandJobs], so it is safe to read without
	// the mutex. Calling it is idempotent: it drives the inversion to
	// ctx.Err() within one O(n²) eval and (when a task handle is attached)
	// lands that handle in its cancelled/error state.
	cancel context.CancelFunc

	// n, alpha, method are the solve parameters captured at spawn. They are
	// immutable for the life of the job. A caller whose parameters have
	// moved on (typically n advancing as a live digest grows) holds a stale
	// job; [ensureBandWarm] cancels and replaces it rather than reading its
	// now-irrelevant progress.
	n      int
	alpha  float64
	method ecdfbands.BandMethodE

	mu   sync.Mutex
	snap BandJobSnapshot
}

func (j *bandJob) read() (s BandJobSnapshot) {
	j.mu.Lock()
	s = j.snap
	j.mu.Unlock()
	return
}

func (j *bandJob) store(s BandJobSnapshot) {
	j.mu.Lock()
	j.snap = s
	j.mu.Unlock()
}

func (j *bandJob) matches(n int, alpha float64, method ecdfbands.BandMethodE) (ok bool) {
	ok = j.n == n && j.alpha == alpha && j.method == method
	return
}

// bandJobs is the process-global warm-up registry, keyed by the caller's
// job key — one entry per consuming widget instance (the ECDF inspector's
// per-call scope), NOT per (n, α, method). Per-instance keying is what
// makes cancellation safe: closing one inspector aborts exactly its own
// solve and never a band another open inspector is still waiting on. The
// underlying ecdfbands cache (keyed by n / α / method) still deduplicates
// the expensive result across instances — once any inspector's job lands,
// a second inspector on the same parameters finds BandReady true and never
// schedules a job at all; only inspectors that begin warming the same
// parameters concurrently pay a redundant (and harmless) solve.
var bandJobs sync.Map // string(jobKey) -> *bandJob

// ensureBandWarm returns the current snapshot of jobKey's warm-up,
// starting one on first request. Idempotent across frames: the same
// inspector calling it every frame attaches to its existing job rather
// than spawning duplicates. When the parameters registered under jobKey no
// longer match (the digest grew, so n advanced) the stale solve is
// cancelled and a fresh one scheduled for the new parameters.
//
// tasks may be nil — the solve still runs and drives the snapshot, and
// [cancelBandJob] still aborts it; only the keelson task.HandleI
// integration (supervisor audit, global taskmonitor visibility, host
// mount-cancel) is skipped when nil.
func ensureBandWarm(jobKey string, tasks task.TaskApiI, n int, alpha float64, method ecdfbands.BandMethodE) BandJobSnapshot {
	if existing, ok := bandJobs.Load(jobKey); ok {
		j := existing.(*bandJob)
		if j.matches(n, alpha, method) {
			return j.read()
		}
		// Stale parameters under this key: abort the old solve and drop it
		// so the LoadOrStore below schedules a fresh warm-up for the new n.
		j.cancel()
		bandJobs.Delete(jobKey)
	}

	// Already cached (e.g. warmed by another inspector, or by this one before
	// the entry self-evicted on completion): report Done without spawning a
	// redundant — if instant — cache-hit goroutine. Keeps ensureBandWarm
	// idempotent across the self-eviction in runBandWarm.
	if ecdfbands.BandReady(n, alpha, method) {
		return BandJobSnapshot{State: BandJobDone, Fraction: 1, EtaMs: 0}
	}

	ctx, cancel := context.WithCancel(context.Background())
	j := &bandJob{
		cancel: cancel,
		n:      n,
		alpha:  alpha,
		method: method,
		snap: BandJobSnapshot{
			State:    BandJobRunning,
			Fraction: 0,
			EtaMs:    -1,
			Note:     "computing confidence band…",
		},
	}
	actual, loaded := bandJobs.LoadOrStore(jobKey, j)
	if loaded {
		// Lost a race for this key (not expected on the single-threaded
		// render loop). Discard the unused context and attach to the winner
		// rather than leaking a second goroutine.
		cancel()
		return actual.(*bandJob).read()
	}
	go runBandWarm(ctx, jobKey, j, tasks, n, alpha, method)
	return j.read()
}

// cancelBandJob aborts and forgets the warm-up registered under jobKey, if
// any. Idempotent and cheap enough to call every frame an inspector is
// closed: a missing key is a no-op. Cancelling drives the in-flight
// inversion to ctx.Err() within one eval (landing any attached task handle
// in its cancelled state) and removes the registry entry, so a later
// reopen schedules a fresh solve instead of surfacing the cancelled one.
// The shared ecdfbands cache is untouched: a band that already finished
// stays cached and a reopen renders it immediately.
func cancelBandJob(jobKey string) {
	if existing, ok := bandJobs.LoadAndDelete(jobKey); ok {
		existing.(*bandJob).cancel()
	}
}

// runBandWarm performs the warm-up on a background goroutine: spawns a
// keelson task (when tasks != nil), runs the cancellable inversion
// reporting per-eval progress, and lands the result in the ecdfbands
// cache. The render loop observes completion via ecdfbands.BandReady on a
// later frame; the result travels through the cache, not the task's Done
// payload.
//
// ctx is the job's cancellation root (see [ensureBandWarm]); the keelson
// task is parented on it so a widget-driven cancel (window closed /
// inspector retracted, via [cancelBandJob]) propagates to the task handle
// too, while the handle additionally folds in bus-cancel and the host's
// mount-cancel.
func runBandWarm(ctx context.Context, jobKey string, j *bandJob, tasks task.TaskApiI, n int, alpha float64, method ecdfbands.BandMethodE) {
	var h task.HandleI
	if tasks != nil {
		h, _ = tasks.Spawn(ctx, task.SpawnOpts{
			Kind:  "ecdf.band",
			Title: fmt.Sprintf("ECDF confidence band (n=%d)", n),
		})
	}
	solveCtx := ctx
	if h != nil {
		// h.Ctx() is a descendant of ctx that also cancels on bus-cancel
		// and the host's mount-cancel, so closing the hosting window aborts
		// a long solve through either path.
		solveCtx = h.Ctx()
	}

	start := time.Now()
	onProgress := func(done, total int) {
		eta := int64(-1)
		switch {
		case done >= total:
			eta = 0
		case done > 0:
			perEval := time.Since(start) / time.Duration(done)
			eta = (perEval * time.Duration(total-done)).Milliseconds()
		}
		j.store(BandJobSnapshot{
			State:    BandJobRunning,
			Fraction: float32(done) / float32(total),
			EtaMs:    eta,
			Note:     "computing confidence band…",
		})
		if h != nil {
			h.Report(task.ProgressReport{
				Current: uint64(done),
				Total:   uint64(total),
				Unit:    task.UnitSteps,
			})
		}
	}

	if err := ecdfbands.WarmBand(solveCtx, n, alpha, method, onProgress); err != nil {
		note := "confidence band failed"
		if solveCtx.Err() != nil {
			note = "confidence band cancelled"
		}
		j.store(BandJobSnapshot{State: BandJobError, Err: err, Note: note})
		if h != nil {
			_ = h.Error(err, "ecdf band inversion failed")
		}
		return
	}
	j.store(BandJobSnapshot{State: BandJobDone, Fraction: 1, EtaMs: 0})
	if h != nil {
		_ = h.Done(nil)
	}
	// Self-evict on success so the registry entry does not outlive the
	// warm-up it tracked — closing the inspector is no longer the only path
	// that reclaims it, which plugs the leak when a pinned-but-warming
	// inspector simply stops being rendered (row scrolled off, parent panel
	// hidden) and its pinned==false cleanup never runs. Safe because once the
	// ecdfbands cache is populated (above) BandReady is true and the
	// inspector reads the band from the cache, never the snapshot, so the
	// entry is dead weight. CompareAndDelete keys on this exact *bandJob, so a
	// concurrent param-change replace (which stored a different job under
	// jobKey) is left intact. Errored jobs are deliberately NOT evicted: their
	// snapshot must persist so the inspector shows the failure instead of
	// respawning the doomed solve every frame.
	bandJobs.CompareAndDelete(jobKey, j)
}
