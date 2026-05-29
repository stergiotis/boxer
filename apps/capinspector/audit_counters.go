//go:build llm_generated_opus47

package capinspector

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

// Counters is the audit-sink-shaped per-capability counter the
// inspector reads to render live activity on cap nodes. Bus Request
// calls land here via the carousel's MultiSink fan-out; classify()
// maps the subject to a CapId; Record increments the matching
// counter atomically (no lock).
//
// Counts are monotonic since process start — Phase 2 design keeps
// the data flat; a future sliding-window variant can wrap this with
// a periodic tick that resets stale buckets.
type Counters struct {
	run     atomic.Uint64
	facts   atomic.Uint64
	bus     atomic.Uint64
	fs      atomic.Uint64
	persist atomic.Uint64
	task    atomic.Uint64
	// other accumulates anything that doesn't match a known cap
	// prefix — surfaced in the inspector as "unclassified" so new
	// subject families show up loud rather than silently increment
	// the bus total.
	other atomic.Uint64
	// total counts every recorded audit row regardless of
	// classification; the bus cap node's display formula is
	// total - (other + per-cap) so "bus" reads as "everything the
	// router moved" rather than "rows the classifier punted".
	total atomic.Uint64
	// Per-cap sliding-window histograms feeding the in-box activity
	// sparkline. Each bucket covers sparkBucketWidth wall-clock time;
	// the head advances on record/snapshot so old data ages out
	// without a sweeper. Bus is the universal substrate so its
	// histogram increments on every record, mirroring Count(CapBus).
	runHist     auditHistogram
	factsHist   auditHistogram
	busHist     auditHistogram
	fsHist      auditHistogram
	persistHist auditHistogram
	taskHist    auditHistogram
}

var _ audit.AuditSinkI = (*Counters)(nil)

// Tally is the package-singleton Counters the carousel wires via
// MultiSink. Inspector windows read from it directly; no DI needed
// because the inspector is itself a carousel-owned package.
var Tally = &Counters{}

func (inst *Counters) Record(rec audit.AuditRecord) {
	inst.recordAt(rec, time.Now())
}

// recordAt is the time-injectable core of Record. Production calls
// always pass time.Now(); tests override to drive the sliding-window
// histograms without sleeping.
func (inst *Counters) recordAt(rec audit.AuditRecord, now time.Time) {
	inst.total.Add(1)
	// busHist counts every audit row, matching Count(CapBus)'s
	// universal-substrate semantics.
	inst.busHist.record(now)
	switch classify(rec.Subject) {
	case CapFs:
		inst.fs.Add(1)
		inst.fsHist.record(now)
	case CapPersist:
		inst.persist.Add(1)
		inst.persistHist.record(now)
	case CapTask:
		inst.task.Add(1)
		inst.taskHist.record(now)
	case CapFacts:
		inst.facts.Add(1)
		inst.factsHist.record(now)
	case CapRun:
		inst.run.Add(1)
		inst.runHist.record(now)
	case CapBus:
		// Reserved — no subject family currently maps here; the bus
		// cap's number is derived from total in Count below.
	default:
		inst.other.Add(1)
	}
}

// Count returns the live row count for one cap. CapBus is special:
// it's the router's universal substrate so the count is everything
// that crossed it (total). The other caps return their classifier
// bucket. Unknown capIds return 0.
func (inst *Counters) Count(capId CapId) (n uint64) {
	switch capId {
	case CapRun:
		n = inst.run.Load()
	case CapFacts:
		n = inst.facts.Load()
	case CapBus:
		n = inst.total.Load()
	case CapFs:
		n = inst.fs.Load()
	case CapPersist:
		n = inst.persist.Load()
	case CapTask:
		n = inst.task.Load()
	}
	return
}

// Snapshot returns the sliding-window sparkline buckets for capId,
// oldest-to-newest. Unknown capIds return a zero array. The renderer
// uses this for the per-cap activity bar inside each cap node.
func (inst *Counters) Snapshot(capId CapId) (out SparkSnapshot) {
	return inst.snapshotAt(capId, time.Now())
}

// snapshotAt is the time-injectable core of Snapshot. Tests drive it
// with explicit timestamps; production passes time.Now.
func (inst *Counters) snapshotAt(capId CapId, now time.Time) (out SparkSnapshot) {
	switch capId {
	case CapRun:
		out = inst.runHist.snapshot(now)
	case CapFacts:
		out = inst.factsHist.snapshot(now)
	case CapBus:
		out = inst.busHist.snapshot(now)
	case CapFs:
		out = inst.fsHist.snapshot(now)
	case CapPersist:
		out = inst.persistHist.snapshot(now)
	case CapTask:
		out = inst.taskHist.snapshot(now)
	}
	return
}

// Reset clears every counter and sliding-window histogram. Test-only
// helper; the production path never resets — the inspector renders
// monotonic counts and a rolling sparkline.
func (inst *Counters) Reset() {
	inst.run.Store(0)
	inst.facts.Store(0)
	inst.bus.Store(0)
	inst.fs.Store(0)
	inst.persist.Store(0)
	inst.task.Store(0)
	inst.other.Store(0)
	inst.total.Store(0)
	inst.runHist.reset()
	inst.factsHist.reset()
	inst.busHist.reset()
	inst.fsHist.reset()
	inst.persistHist.reset()
	inst.taskHist.reset()
}

// classify maps a bus subject to a CapId. The match is the lowest
// common denominator: a single prefix lookup. Subjects that don't
// match any known family return an empty CapId — the caller (Record)
// then increments the "other" bucket so unmapped activity surfaces.
//
// The classifier is intentionally hand-rolled rather than driven by
// the Registry.AppFilter predicates: AppFilter operates on
// SubjectFilter values (Pattern + Direction) while this needs to
// classify a concrete subject string. Same intent, different shape.
func classify(subject string) (capId CapId) {
	switch {
	case strings.HasPrefix(subject, "fs."):
		capId = CapFs
	case strings.HasPrefix(subject, "runtime.persist."):
		capId = CapPersist
	case strings.HasPrefix(subject, "task."):
		capId = CapTask
	case strings.HasPrefix(subject, "runtime.facts."):
		// Reserved — no subject lands here today (factsstore writes
		// happen out-of-band, not via the bus). Kept for the read-
		// path queries when they land as bus requests.
		capId = CapFacts
	case strings.HasPrefix(subject, "runtime.heartbeat") ||
		strings.HasPrefix(subject, "runtime.run"):
		capId = CapRun
	}
	return
}
