package imzrt

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/observability/goruntime"
)

// PublishedSnapshot is the read-only frame the renderer consumes. Built once per
// Sampler tick and replaced atomically; slices are owned by the snapshot and
// never mutated after publication, so a concurrent reader sees a coherent view.
//
// Memory series are stored in MiB. The five class bands (objects / idle / stacks
// / metadata / other) partition the mapped total and stack to it; the panel
// computes their running cumulative for the stacked-area draw.
type PublishedSnapshot struct {
	SampledAtUnixMs int64

	// Shared time axis (unix seconds).
	HistTimeUnixSec []float64

	// Heap sawtooth + stacked memory-class bands (MiB).
	HistHeapObjectsMiB []float64 // also the sawtooth "heap in use" line + bottom band
	HistHeapGoalMiB    []float64 // GC trigger target
	HistIdleMiB        []float64 // heap free + unused + released
	HistStacksMiB      []float64 // heap stacks + OS thread stacks
	HistMetadataMiB    []float64 // runtime metadata
	HistOtherMiB       []float64 // other + profiling buckets
	HistTotalMiB       []float64 // total mapped (≈ sum of bands)
	HistGoroutines     []float64

	// Latest scalars for instant readouts and the top bar.
	Goroutines       uint64
	GomaxProcs       uint64
	HeapObjectsBytes uint64
	HeapLiveBytes    uint64
	HeapGoalBytes    uint64
	IdleBytes        uint64
	StacksBytes      uint64
	MetadataBytes    uint64
	OtherBytes       uint64
	TotalMappedBytes uint64
	ReleasedBytes    uint64
	HeapObjectsCount uint64
	GCCyclesTotal    uint64
	GCCyclesForced   uint64
	GOGCPercent      uint64
	GOMemLimitBytes  uint64 // goruntime.MemLimitUnset when GOMEMLIMIT is not set
	CgoCallsTotal    uint64

	// Derived current rates.
	AllocRateBytesPerSec float64
	GCPerSec             float64

	// GC dynamics (M2): rolling history plus current-interval scalars.
	HistPauseP50Ms     []float64 // rolling per-interval p50 GC pause (ms)
	HistPauseP99Ms     []float64 // rolling per-interval p99 GC pause (ms)
	HistPauseMaxMs     []float64 // rolling per-interval max GC pause (ms)
	HistGCPerSec       []float64 // total GC cycles/s
	HistGCForcedPerSec []float64 // forced GC cycles/s
	HistAllocMiBs      []float64 // allocation rate (MiB/s)
	PauseP50Sec        float64
	PauseP99Sec        float64
	PauseMaxSec        float64
	PausesInWindow     uint64
	GCForcedPerSec     float64
	AllocObjPerSec     float64

	// Count of curated runtime metrics absent on this Go version (0 on a current toolchain).
	MissingMetrics int
}

// MemLimitSet reports whether GOMEMLIMIT is configured (not the unset sentinel).
func (inst *PublishedSnapshot) MemLimitSet() (set bool) {
	set = inst.GOMemLimitBytes > 0 && inst.GOMemLimitBytes < goruntime.MemLimitUnset
	return
}

// SamplerOptions configures a Sampler.
type SamplerOptions struct {
	UpdateInterval time.Duration
	HistoryWindow  time.Duration
}

// SamplerI is the public surface a Sampler implements.
type SamplerI interface {
	Start(ctx context.Context)
	Latest() (snap *PublishedSnapshot)
	Pause(p bool)
	IsPaused() (p bool)
	Close() (err error)
}

// Sampler runs a goroutine that periodically reads the Go runtime via a
// goruntime.Collector and publishes a PublishedSnapshot through atomic.Pointer.
// It is a process-wide singleton (ensureSampler): there is exactly one Go runtime
// per process, so one shared history is the correct model, and it keeps recording
// while every window is hidden.
type Sampler struct {
	coll *goruntime.Collector
	work goruntime.Snapshot // reused read buffer; never published (would alias under mutation)

	intervalNs atomic.Int64
	histN      int32

	timeWin      *SlidingWindow[float64]
	objectsWin   *SlidingWindow[float64]
	goalWin      *SlidingWindow[float64]
	idleWin      *SlidingWindow[float64]
	stacksWin    *SlidingWindow[float64]
	metaWin      *SlidingWindow[float64]
	otherWin     *SlidingWindow[float64]
	totalWin     *SlidingWindow[float64]
	goroutineWin *SlidingWindow[float64]

	// GC-dynamics history (M2): rolling pause percentiles (ms), GC rates
	// (cycles/s), and allocation rate (MiB/s).
	pauseP50Win *SlidingWindow[float64]
	pauseP99Win *SlidingWindow[float64]
	pauseMaxWin *SlidingWindow[float64]
	gcTotalWin  *SlidingWindow[float64]
	gcForcedWin *SlidingWindow[float64]
	allocWin    *SlidingWindow[float64]

	// histWork is the reused per-tick windowed pause histogram; prevGCPauses
	// holds the previous tick's cumulative pause histogram for the delta.
	histWork     WindowedHistogram
	prevGCPauses goruntime.Histogram

	// Rate-derivation state. Cumulative counters are differenced against the
	// previous tick; havePrev gates the first tick, where no rate exists yet.
	havePrev         bool
	prevAllocBytes   uint64
	prevAllocObjects uint64
	prevGCCycles     uint64
	prevGCForced     uint64
	prevTimeMs       int64

	latest atomic.Pointer[PublishedSnapshot]
	paused atomic.Bool

	cancel context.CancelFunc
	done   chan struct{}
}

var _ SamplerI = (*Sampler)(nil)

// NewSampler builds a Sampler. The error return mirrors imztop's sampler surface
// and leaves room for future construction-time validation; it is nil today.
func NewSampler(opts SamplerOptions) (inst *Sampler, err error) {
	if opts.UpdateInterval <= 0 {
		opts.UpdateInterval = 1 * time.Second
	}
	if opts.HistoryWindow <= 0 {
		opts.HistoryWindow = 10 * time.Minute
	}
	histN := max(int32(opts.HistoryWindow/opts.UpdateInterval), 2)

	inst = &Sampler{
		coll:         goruntime.NewCollector(),
		histN:        histN,
		timeWin:      NewSlidingWindow[float64](histN),
		objectsWin:   NewSlidingWindow[float64](histN),
		goalWin:      NewSlidingWindow[float64](histN),
		idleWin:      NewSlidingWindow[float64](histN),
		stacksWin:    NewSlidingWindow[float64](histN),
		metaWin:      NewSlidingWindow[float64](histN),
		otherWin:     NewSlidingWindow[float64](histN),
		totalWin:     NewSlidingWindow[float64](histN),
		goroutineWin: NewSlidingWindow[float64](histN),
		pauseP50Win:  NewSlidingWindow[float64](histN),
		pauseP99Win:  NewSlidingWindow[float64](histN),
		pauseMaxWin:  NewSlidingWindow[float64](histN),
		gcTotalWin:   NewSlidingWindow[float64](histN),
		gcForcedWin:  NewSlidingWindow[float64](histN),
		allocWin:     NewSlidingWindow[float64](histN),
	}
	inst.intervalNs.Store(int64(opts.UpdateInterval))
	return
}

func (inst *Sampler) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.loop(runCtx)
}

func (inst *Sampler) Latest() (snap *PublishedSnapshot) {
	snap = inst.latest.Load()
	return
}

func (inst *Sampler) Pause(p bool) { inst.paused.Store(p) }

func (inst *Sampler) IsPaused() (p bool) {
	p = inst.paused.Load()
	return
}

// IntervalLabel returns the configured tick interval as a short label for the
// top-bar status row.
func (inst *Sampler) IntervalLabel() (out string) {
	out = time.Duration(inst.intervalNs.Load()).String()
	return
}

// SetInterval changes the tick period, clamped to [100ms, 60s]. The next tick
// after the current ticker fires adopts the new period.
func (inst *Sampler) SetInterval(d time.Duration) {
	if d < 100*time.Millisecond {
		d = 100 * time.Millisecond
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	inst.intervalNs.Store(int64(d))
}

func (inst *Sampler) Interval() (d time.Duration) {
	d = time.Duration(inst.intervalNs.Load())
	return
}

func (inst *Sampler) Close() (err error) {
	if inst.cancel != nil {
		inst.cancel()
	}
	if inst.done != nil {
		<-inst.done
	}
	return
}

func (inst *Sampler) loop(ctx context.Context) {
	defer close(inst.done)

	inst.tick()

	cur := time.Duration(inst.intervalNs.Load())
	ticker := time.NewTicker(cur)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.tick()
			next := time.Duration(inst.intervalNs.Load())
			if next != cur {
				ticker.Reset(next)
				cur = next
			}
		}
	}
}

func (inst *Sampler) tick() {
	// Pause freezes the published snapshot. Unlike imztop there is no /proc walk
	// to avoid, but skipping the read also halts the observer-effect entirely
	// while paused, which is the honest thing to do for a self-measuring app.
	if inst.paused.Load() {
		return
	}

	_ = inst.coll.Read(&inst.work)
	w := &inst.work
	nowMs := time.Now().UnixMilli()

	idle := w.HeapFree + w.HeapUnused + w.HeapReleased
	stacks := w.HeapStacks + w.OSStacks

	inst.timeWin.Push(float64(nowMs) / 1000.0)
	inst.objectsWin.Push(mib(w.HeapObjects))
	inst.goalWin.Push(mib(w.HeapGoal))
	inst.idleWin.Push(mib(idle))
	inst.stacksWin.Push(mib(stacks))
	inst.metaWin.Push(mib(w.Metadata))
	inst.otherWin.Push(mib(w.Other))
	inst.totalWin.Push(mib(w.TotalMapped))
	inst.goroutineWin.Push(float64(w.Goroutines))

	var allocRate, allocObjRate, gcRate, gcForcedRate float64
	if inst.havePrev && nowMs > inst.prevTimeMs {
		dt := float64(nowMs-inst.prevTimeMs) / 1000.0
		allocRate = float64(w.AllocBytes-inst.prevAllocBytes) / dt
		allocObjRate = float64(w.AllocObjects-inst.prevAllocObjects) / dt
		gcRate = float64(w.GCCyclesTotal-inst.prevGCCycles) / dt
		gcForcedRate = float64(w.GCCyclesForced-inst.prevGCForced) / dt
	}
	inst.prevAllocBytes = w.AllocBytes
	inst.prevAllocObjects = w.AllocObjects
	inst.prevGCCycles = w.GCCyclesTotal
	inst.prevGCForced = w.GCCyclesForced
	inst.prevTimeMs = nowMs
	inst.havePrev = true

	// Per-interval GC pause distribution from the cumulative histogram delta.
	// On the first tick prevGCPauses is empty, so the delta is all-zero and the
	// percentiles read zero — no NaN, no spurious startup spike.
	WindowDelta(w.GCPauses, inst.prevGCPauses, &inst.histWork)
	pauseP50 := inst.histWork.Quantile(0.50)
	pauseP99 := inst.histWork.Quantile(0.99)
	pauseMax := inst.histWork.Max()
	pausesInWindow := inst.histWork.Total()
	inst.prevGCPauses.Buckets = append(inst.prevGCPauses.Buckets[:0], w.GCPauses.Buckets...)
	inst.prevGCPauses.Counts = append(inst.prevGCPauses.Counts[:0], w.GCPauses.Counts...)

	inst.pauseP50Win.Push(pauseP50 * 1000)
	inst.pauseP99Win.Push(pauseP99 * 1000)
	inst.pauseMaxWin.Push(pauseMax * 1000)
	inst.gcTotalWin.Push(gcRate)
	inst.gcForcedWin.Push(gcForcedRate)
	inst.allocWin.Push(allocRate / bytesPerMiB)

	pub := &PublishedSnapshot{
		SampledAtUnixMs:    nowMs,
		HistTimeUnixSec:    copyFloats(inst.timeWin.Values()),
		HistHeapObjectsMiB: copyFloats(inst.objectsWin.Values()),
		HistHeapGoalMiB:    copyFloats(inst.goalWin.Values()),
		HistIdleMiB:        copyFloats(inst.idleWin.Values()),
		HistStacksMiB:      copyFloats(inst.stacksWin.Values()),
		HistMetadataMiB:    copyFloats(inst.metaWin.Values()),
		HistOtherMiB:       copyFloats(inst.otherWin.Values()),
		HistTotalMiB:       copyFloats(inst.totalWin.Values()),
		HistGoroutines:     copyFloats(inst.goroutineWin.Values()),

		Goroutines:       w.Goroutines,
		GomaxProcs:       w.GomaxProcs,
		HeapObjectsBytes: w.HeapObjects,
		HeapLiveBytes:    w.HeapLive,
		HeapGoalBytes:    w.HeapGoal,
		IdleBytes:        idle,
		StacksBytes:      stacks,
		MetadataBytes:    w.Metadata,
		OtherBytes:       w.Other,
		TotalMappedBytes: w.TotalMapped,
		ReleasedBytes:    w.HeapReleased,
		HeapObjectsCount: w.HeapObjectsCount,
		GCCyclesTotal:    w.GCCyclesTotal,
		GCCyclesForced:   w.GCCyclesForced,
		GOGCPercent:      w.GOGCPercent,
		GOMemLimitBytes:  w.GOMemLimit,
		CgoCallsTotal:    w.CgoCalls,

		AllocRateBytesPerSec: allocRate,
		GCPerSec:             gcRate,

		HistPauseP50Ms:     copyFloats(inst.pauseP50Win.Values()),
		HistPauseP99Ms:     copyFloats(inst.pauseP99Win.Values()),
		HistPauseMaxMs:     copyFloats(inst.pauseMaxWin.Values()),
		HistGCPerSec:       copyFloats(inst.gcTotalWin.Values()),
		HistGCForcedPerSec: copyFloats(inst.gcForcedWin.Values()),
		HistAllocMiBs:      copyFloats(inst.allocWin.Values()),
		PauseP50Sec:        pauseP50,
		PauseP99Sec:        pauseP99,
		PauseMaxSec:        pauseMax,
		PausesInWindow:     pausesInWindow,
		GCForcedPerSec:     gcForcedRate,
		AllocObjPerSec:     allocObjRate,

		MissingMetrics: w.Missing,
	}
	inst.latest.Store(pub)
}

func copyFloats(src []float64) (out []float64) {
	out = make([]float64, len(src))
	copy(out, src)
	return
}
