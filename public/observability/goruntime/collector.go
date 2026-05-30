package goruntime

import (
	"math"
	"runtime/metrics"
)

// MemLimitUnset is the value runtime/metrics reports for /gc/gomemlimit:bytes
// when GOMEMLIMIT is not configured (math.MaxInt64). Callers treat a
// [Snapshot.GOMemLimit] equal to this as "no soft memory limit set".
const MemLimitUnset uint64 = math.MaxInt64

// Histogram mirrors metrics.Float64Histogram: Counts has len(Buckets)-1 entries
// and is cumulative since process start. The zero value is an empty, safe-to-read
// histogram.
type Histogram struct {
	Buckets []float64
	Counts  []uint64
}

// Snapshot is a point-in-time read of the Go runtime's metrics. Callers own the
// value and reuse it across [Collector.Read] calls; in steady state Read performs
// no allocation (the histogram slice backings are reused via append-to-truncated).
// A scalar field is zero when its backing metric is absent on the running Go
// version — indistinguishable from a genuine zero, which is acceptable for the
// metrics here (a real runtime never has zero goroutines or zero mapped memory).
type Snapshot struct {
	// Memory classes (bytes). These partition TotalMapped; the dashboard bands
	// them as objects / idle (free+unused+released) / stacks / metadata / other.
	HeapObjects  uint64
	HeapFree     uint64
	HeapReleased uint64
	HeapUnused   uint64
	HeapStacks   uint64
	OSStacks     uint64
	Metadata     uint64 // summed /memory/classes/metadata/*
	Other        uint64 // /memory/classes/other + /memory/classes/profiling/buckets
	TotalMapped  uint64

	// GC heap accounting.
	HeapGoal         uint64 // /gc/heap/goal: the GC's target heap size for the next cycle
	HeapLive         uint64 // /gc/heap/live, or allocs-frees when that metric is absent
	HeapObjectsCount uint64
	AllocBytes       uint64 // cumulative bytes allocated
	FreeBytes        uint64 // cumulative bytes freed
	AllocObjects     uint64 // cumulative objects allocated
	FreeObjects      uint64 // cumulative objects freed

	// GC cycles (cumulative counts).
	GCCyclesTotal  uint64
	GCCyclesForced uint64

	// Scheduler.
	Goroutines uint64
	GomaxProcs uint64

	// Knobs (read-only; the dashboard never mutates these).
	GOGCPercent uint64
	GOMemLimit  uint64 // MemLimitUnset when GOMEMLIMIT is not configured

	// Misc.
	CgoCalls     uint64  // cumulative cgo calls (go-to-c)
	MutexWaitSec float64 // cumulative seconds goroutines blocked on mutexes

	// Cumulative histograms. Captured here so the GC and Scheduler panels (later
	// milestones) inherit a populated collector; the Heap panel does not read them.
	GCPauses       Histogram // /gc/pauses:seconds
	SchedLatencies Histogram // /sched/latencies:seconds

	// Missing counts curated metrics absent from metrics.All() on this runtime.
	Missing int
}

// Curated runtime/metrics names. Kept as named constants so the Read body reads
// as field assignments rather than string literals, and so the set is greppable.
const (
	mHeapObjects      = "/memory/classes/heap/objects:bytes"
	mHeapFree         = "/memory/classes/heap/free:bytes"
	mHeapReleased     = "/memory/classes/heap/released:bytes"
	mHeapUnused       = "/memory/classes/heap/unused:bytes"
	mHeapStacks       = "/memory/classes/heap/stacks:bytes"
	mOSStacks         = "/memory/classes/os-stacks:bytes"
	mMetaMcacheFree   = "/memory/classes/metadata/mcache/free:bytes"
	mMetaMcacheInuse  = "/memory/classes/metadata/mcache/inuse:bytes"
	mMetaMspanFree    = "/memory/classes/metadata/mspan/free:bytes"
	mMetaMspanInuse   = "/memory/classes/metadata/mspan/inuse:bytes"
	mMetaOther        = "/memory/classes/metadata/other:bytes"
	mOther            = "/memory/classes/other:bytes"
	mProfilingBuckets = "/memory/classes/profiling/buckets:bytes"
	mTotal            = "/memory/classes/total:bytes"

	mHeapGoal         = "/gc/heap/goal:bytes"
	mHeapLive         = "/gc/heap/live:bytes"
	mHeapObjectsCount = "/gc/heap/objects:objects"
	mAllocsBytes      = "/gc/heap/allocs:bytes"
	mFreesBytes       = "/gc/heap/frees:bytes"
	mAllocsObjects    = "/gc/heap/allocs:objects"
	mFreesObjects     = "/gc/heap/frees:objects"

	mGCCyclesTotal  = "/gc/cycles/total:gc-cycles"
	mGCCyclesForced = "/gc/cycles/forced:gc-cycles"

	mGOGC       = "/gc/gogc:percent"
	mGOMemLimit = "/gc/gomemlimit:bytes"

	mGoroutines = "/sched/goroutines:goroutines"
	mGomaxprocs = "/sched/gomaxprocs:threads"

	mCgoCalls  = "/cgo/go-to-c-calls:calls"
	mMutexWait = "/sync/mutex/wait/total:seconds"

	mGCPauses       = "/gc/pauses:seconds"
	mSchedLatencies = "/sched/latencies:seconds"
)

// curatedNames is the full set the collector requests. Order is irrelevant
// (the collector indexes by name) but grouped to mirror the Snapshot layout.
var curatedNames = []string{
	mHeapObjects, mHeapFree, mHeapReleased, mHeapUnused, mHeapStacks, mOSStacks,
	mMetaMcacheFree, mMetaMcacheInuse, mMetaMspanFree, mMetaMspanInuse, mMetaOther,
	mOther, mProfilingBuckets, mTotal,
	mHeapGoal, mHeapLive, mHeapObjectsCount,
	mAllocsBytes, mFreesBytes, mAllocsObjects, mFreesObjects,
	mGCCyclesTotal, mGCCyclesForced,
	mGOGC, mGOMemLimit,
	mGoroutines, mGomaxprocs,
	mCgoCalls, mMutexWait,
	mGCPauses, mSchedLatencies,
}

// Collector reads a curated subset of runtime/metrics into a Snapshot. It owns a
// reusable sample buffer sized to the metrics actually present on this runtime;
// construct once and Read many times.
type Collector struct {
	samples  []metrics.Sample
	idx      map[string]int // metric name -> index into samples (present metrics only)
	haveLive bool           // /gc/heap/live present (else derive live = allocs-frees)
	missing  int
}

// NewCollector builds a Collector over the package's curated metric set,
// intersected with what the running Go version exposes.
func NewCollector() (inst *Collector) {
	inst = newCollectorFromNames(curatedNames)
	return
}

// newCollectorFromNames builds a Collector over an arbitrary name set. Unexported;
// the test uses it to exercise the absent-metric path with a name guaranteed not
// to exist.
func newCollectorFromNames(names []string) (inst *Collector) {
	avail := make(map[string]bool)
	for _, d := range metrics.All() {
		avail[d.Name] = true
	}
	inst = &Collector{idx: make(map[string]int, len(names))}
	for _, n := range names {
		if !avail[n] {
			inst.missing++
			continue
		}
		inst.idx[n] = len(inst.samples)
		inst.samples = append(inst.samples, metrics.Sample{Name: n})
	}
	_, inst.haveLive = inst.idx[mHeapLive]
	return
}

// Read fills snap in place from a single metrics.Read. Allocation-free in steady
// state. The returned error is always nil today; it is part of the surface so a
// future validation step does not break callers.
func (inst *Collector) Read(snap *Snapshot) (err error) {
	metrics.Read(inst.samples)

	snap.HeapObjects = inst.u64(mHeapObjects)
	snap.HeapFree = inst.u64(mHeapFree)
	snap.HeapReleased = inst.u64(mHeapReleased)
	snap.HeapUnused = inst.u64(mHeapUnused)
	snap.HeapStacks = inst.u64(mHeapStacks)
	snap.OSStacks = inst.u64(mOSStacks)
	snap.Metadata = inst.sumU64(mMetaMcacheFree, mMetaMcacheInuse, mMetaMspanFree, mMetaMspanInuse, mMetaOther)
	snap.Other = inst.sumU64(mOther, mProfilingBuckets)
	snap.TotalMapped = inst.u64(mTotal)

	snap.HeapGoal = inst.u64(mHeapGoal)
	snap.HeapObjectsCount = inst.u64(mHeapObjectsCount)
	snap.AllocBytes = inst.u64(mAllocsBytes)
	snap.FreeBytes = inst.u64(mFreesBytes)
	snap.AllocObjects = inst.u64(mAllocsObjects)
	snap.FreeObjects = inst.u64(mFreesObjects)
	if inst.haveLive {
		snap.HeapLive = inst.u64(mHeapLive)
	}
	if snap.HeapLive == 0 {
		// /gc/heap/live is absent (older Go) or still zero — it is only updated
		// at the end of a GC cycle, so it reads zero on a fresh process before
		// the first collection. Fall back to allocs-frees: cumulative bytes
		// allocated minus freed is the live byte count, updated continuously,
		// and it is what climbs between collections to drive the heap sawtooth.
		snap.HeapLive = snap.AllocBytes - snap.FreeBytes
	}

	snap.GCCyclesTotal = inst.u64(mGCCyclesTotal)
	snap.GCCyclesForced = inst.u64(mGCCyclesForced)

	snap.Goroutines = inst.u64(mGoroutines)
	snap.GomaxProcs = inst.u64(mGomaxprocs)

	snap.GOGCPercent = inst.u64(mGOGC)
	if _, ok := inst.idx[mGOMemLimit]; ok {
		snap.GOMemLimit = inst.u64(mGOMemLimit)
	} else {
		snap.GOMemLimit = MemLimitUnset
	}

	snap.CgoCalls = inst.u64(mCgoCalls)
	snap.MutexWaitSec = inst.f64(mMutexWait)

	inst.histInto(&snap.GCPauses, mGCPauses)
	inst.histInto(&snap.SchedLatencies, mSchedLatencies)

	snap.Missing = inst.missing
	return
}

func (inst *Collector) u64(name string) (v uint64) {
	if i, ok := inst.idx[name]; ok {
		val := inst.samples[i].Value
		if val.Kind() == metrics.KindUint64 {
			v = val.Uint64()
		}
	}
	return
}

func (inst *Collector) f64(name string) (v float64) {
	if i, ok := inst.idx[name]; ok {
		val := inst.samples[i].Value
		if val.Kind() == metrics.KindFloat64 {
			v = val.Float64()
		}
	}
	return
}

func (inst *Collector) sumU64(names ...string) (v uint64) {
	for _, n := range names {
		v += inst.u64(n)
	}
	return
}

// histInto copies the named cumulative histogram into dst, reusing dst's slice
// backings. An absent or wrong-kind metric yields an empty (truncated) histogram.
func (inst *Collector) histInto(dst *Histogram, name string) {
	i, ok := inst.idx[name]
	if !ok {
		dst.Buckets = dst.Buckets[:0]
		dst.Counts = dst.Counts[:0]
		return
	}
	val := inst.samples[i].Value
	if val.Kind() != metrics.KindFloat64Histogram {
		dst.Buckets = dst.Buckets[:0]
		dst.Counts = dst.Counts[:0]
		return
	}
	h := val.Float64Histogram()
	dst.Buckets = append(dst.Buckets[:0], h.Buckets...)
	dst.Counts = append(dst.Counts[:0], h.Counts...)
}
