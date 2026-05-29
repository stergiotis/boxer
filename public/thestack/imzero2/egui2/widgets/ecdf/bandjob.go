//go:build llm_generated_opus47

package ecdf

import (
	"context"
	"fmt"
	"math"
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

type bandJob struct {
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

// bandJobKey deduplicates warm-ups across every widget and frame at the
// same granularity ecdfbands caches results, so two ECDF plots sharing
// an (n, α, method) share one background solve. α is keyed by raw bit
// pattern; in the worst case two near-identical α's spawn a second job
// that finds the first's result already cached and returns at once.
type bandJobKey struct {
	n         int
	alphaBits uint64
	method    ecdfbands.BandMethodE
}

// bandJobs is the process-global in-flight (and finished) registry.
var bandJobs sync.Map // bandJobKey -> *bandJob

// ensureBandWarm returns the current snapshot of the (n, α, method)
// warm-up, starting one on first request. Idempotent: calling it every
// frame from every widget attaches to the existing job rather than
// spawning duplicates. tasks may be nil — the solve still runs and
// drives the snapshot; only the keelson task.HandleI integration
// (supervisor audit, global taskmonitor visibility, cancel-on-window-
// close) is skipped when nil.
func ensureBandWarm(tasks task.TaskApiI, n int, alpha float64, method ecdfbands.BandMethodE) BandJobSnapshot {
	key := bandJobKey{n: n, alphaBits: math.Float64bits(alpha), method: method}
	if existing, ok := bandJobs.Load(key); ok {
		return existing.(*bandJob).read()
	}
	j := &bandJob{snap: BandJobSnapshot{
		State:    BandJobRunning,
		Fraction: 0,
		EtaMs:    -1,
		Note:     "computing confidence band…",
	}}
	actual, loaded := bandJobs.LoadOrStore(key, j)
	if loaded {
		// Lost a race for the same key (not expected on the single-
		// threaded render loop, but honoured cheaply).
		return actual.(*bandJob).read()
	}
	go runBandWarm(j, tasks, n, alpha, method)
	return j.read()
}

// runBandWarm performs the warm-up on a background goroutine: spawns a
// keelson task (when tasks != nil), runs the cancellable inversion
// reporting per-eval progress, and lands the result in the ecdfbands
// cache. The render loop observes completion via ecdfbands.BandReady on
// a later frame; the result travels through the cache, not the task's
// Done payload.
func runBandWarm(j *bandJob, tasks task.TaskApiI, n int, alpha float64, method ecdfbands.BandMethodE) {
	var h task.HandleI
	if tasks != nil {
		h, _ = tasks.Spawn(context.Background(), task.SpawnOpts{
			Kind:  "ecdf.band",
			Title: fmt.Sprintf("ECDF confidence band (n=%d)", n),
		})
	}
	ctx := context.Background()
	if h != nil {
		// h.Ctx() cancels on bus-cancel or the host's mount-cancel, so
		// closing the hosting window aborts a long solve for free.
		ctx = h.Ctx()
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

	if err := ecdfbands.WarmBand(ctx, n, alpha, method, onProgress); err != nil {
		note := "confidence band failed"
		if ctx.Err() != nil {
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
}
