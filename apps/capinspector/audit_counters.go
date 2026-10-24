package capinspector

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/appstate"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/httpegress"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
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
	// perCap holds one counter and sliding-window histogram per
	// registered cap (allCapIdsOrdered). Built once by NewCounters and
	// never written afterwards, so concurrent Record / Snapshot calls
	// read it without a lock. Keying by CapId rather than one field per
	// cap keeps a newly registered cap from silently falling into
	// "other" because the switch here was not extended with it.
	perCap map[CapId]*capCounter
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
}

// capCounter is one cap's monotonic count and the sliding-window
// histogram feeding its activity sparkline. Each bucket covers
// sparkBucketWidth wall-clock time; the head advances on
// record/snapshot so old data ages out without a sweeper.
type capCounter struct {
	n    atomic.Uint64
	hist auditHistogram
}

var _ audit.AuditSinkI = (*Counters)(nil)

// NewCounters returns a Counters with a slot for every registered cap.
func NewCounters() (inst *Counters) {
	caps := allCapIdsOrdered()
	inst = &Counters{perCap: make(map[CapId]*capCounter, len(caps))}
	for _, capId := range caps {
		inst.perCap[capId] = &capCounter{}
	}
	return
}

// Tally is the package-singleton Counters the carousel wires via
// MultiSink. Inspector windows read from it directly; no DI needed
// because the inspector is itself a carousel-owned package.
var Tally = NewCounters()

func (inst *Counters) Record(rec audit.AuditRecord) {
	inst.recordAt(rec, time.Now())
}

// recordAt is the time-injectable core of Record. Production calls
// always pass time.Now(); tests override to drive the sliding-window
// histograms without sleeping.
func (inst *Counters) recordAt(rec audit.AuditRecord, now time.Time) {
	inst.total.Add(1)
	// The bus histogram counts every audit row, matching
	// Count(CapBus)'s universal-substrate semantics.
	if bus := inst.perCap[CapBus]; bus != nil {
		bus.hist.record(now)
	}
	capId := classify(rec.Subject)
	cc := inst.perCap[capId]
	if capId == CapBus || cc == nil {
		// CapBus is reserved — no subject family maps there; its
		// number is derived from total in Count below.
		inst.other.Add(1)
		return
	}
	cc.n.Add(1)
	cc.hist.record(now)
}

// Count returns the live row count for one cap. CapBus is special:
// it's the router's universal substrate so the count is everything
// that crossed it (total). The other caps return their classifier
// bucket. Unknown capIds return 0.
func (inst *Counters) Count(capId CapId) (n uint64) {
	if capId == CapBus {
		n = inst.total.Load()
		return
	}
	if cc := inst.perCap[capId]; cc != nil {
		n = cc.n.Load()
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
	if cc := inst.perCap[capId]; cc != nil {
		out = cc.hist.snapshot(now)
	}
	return
}

// Reset clears every counter and sliding-window histogram. Test-only
// helper; the production path never resets — the inspector renders
// monotonic counts and a rolling sparkline.
func (inst *Counters) Reset() {
	for _, cc := range inst.perCap {
		cc.n.Store(0)
		cc.hist.reset()
	}
	inst.other.Store(0)
	inst.total.Store(0)
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
	case strings.HasPrefix(subject, appstate.SubjectPrefix):
		capId = CapAppState
	case strings.HasPrefix(subject, keelsonquery.SubjectPrefix):
		capId = CapKeelsonQuery
	case strings.HasPrefix(subject, adhocSubjectPrefix):
		capId = CapAdhoc
	case strings.HasPrefix(subject, llm.SubjectPrefix):
		capId = CapLLM
	case strings.HasPrefix(subject, httpegress.SubjectPrefix):
		capId = CapHTTP
	case strings.HasPrefix(subject, agent.SubjectPrefix):
		capId = CapAgent
	case isOperationSubject(subject):
		capId = CapAgent
	case strings.HasPrefix(subject, "task."):
		capId = CapTask
	case strings.HasPrefix(subject, "watchbill."):
		capId = CapWatchbill
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

// isOperationSubject reports an app.{alias}.{instance}.op.{name} subject,
// which only the host's dispatcher and window host use (ADR-0269 §SD3).
func isOperationSubject(subject string) (ok bool) {
	_, _, _, ok = opwire.ParseSubject(subject)
	return
}
