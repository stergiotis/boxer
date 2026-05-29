//go:build llm_generated_opus47

// Package audit defines the bus audit infrastructure per ADR-0026 §SD12 M2.5.
// Every Client.Request call on inprocbus is captured as an AuditRecord and
// routed to a configured AuditSinkI. The records carry the per-request
// fields ADR-0026 §SD6 maps onto runtime.facts KindAudit rows: subject,
// result classification, latency, payload sizes.
//
// AuditSinkI implementations:
//   - InMemoryAuditSink (this package): test/dev recording into a slice.
//   - factsstore.AsAuditSink (runtime/factsstore): adapter routing records
//     into the runtime.facts view; M2.5 buffers them in memory, the future
//     CH-backed FactsStoreI persists them.
package audit

import (
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// AuditResultE classifies the outcome of an audited request.
type AuditResultE uint8

const (
	AuditResultUnspecified AuditResultE = 0
	AuditResultOk          AuditResultE = 1
	// AuditResultDenied is set when the request was rejected by a
	// permission check (ErrPermissionViolation).
	AuditResultDenied AuditResultE = 2
	// AuditResultTimeout is set when Request waited past its deadline.
	AuditResultTimeout AuditResultE = 3
	// AuditResultError is any other failure shape.
	AuditResultError AuditResultE = 4
)

func (inst AuditResultE) String() (s string) {
	switch inst {
	case AuditResultOk:
		s = "ok"
	case AuditResultDenied:
		s = "denied"
	case AuditResultTimeout:
		s = "timeout"
	case AuditResultError:
		s = "error"
	default:
		s = "unspecified"
	}
	return
}

// AuditRecord is one audited request crossing the bus. Populated by the
// bus and handed to the configured AuditSinkI exactly once per Request.
type AuditRecord struct {
	AppId         app.AppIdT
	Subject       string
	Result        AuditResultE
	LatencyMs     uint32
	RequestSizeB  uint32
	ResponseSizeB uint32
	Ts            time.Time
}

// AuditSinkI receives audit records. Sinks should be goroutine-safe; the
// bus may invoke Record from multiple goroutines concurrently.
type AuditSinkI interface {
	Record(rec AuditRecord)
}

// MultiSink fans a single Record call out to every contained sink in
// order. Useful for the carousel pattern where the bus audit feed
// needs to land in both the durable factsstore sink and a sidecar
// counter sink for live UI. A nil entry is silently skipped; an
// empty MultiSink is a valid no-op sink.
type MultiSink []AuditSinkI

var _ AuditSinkI = MultiSink(nil)

func (inst MultiSink) Record(rec AuditRecord) {
	for _, s := range inst {
		if s == nil {
			continue
		}
		s.Record(rec)
	}
}

// AuditFunc adapts a function as an AuditSinkI.
type AuditFunc func(rec AuditRecord)

var _ AuditSinkI = AuditFunc(nil)

func (inst AuditFunc) Record(rec AuditRecord) {
	inst(rec)
}

// InMemoryAuditSink buffers records in a slice. Production hosts use the
// factsstore-backed sink; tests and dev environments use this directly.
type InMemoryAuditSink struct {
	mu      sync.RWMutex
	records []AuditRecord
}

var _ AuditSinkI = (*InMemoryAuditSink)(nil)

func NewInMemoryAuditSink() (inst *InMemoryAuditSink) {
	inst = &InMemoryAuditSink{}
	return
}

func (inst *InMemoryAuditSink) Record(rec AuditRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.records = append(inst.records, rec)
}

// Records returns a snapshot of all recorded entries in insertion order.
func (inst *InMemoryAuditSink) Records() (recs []AuditRecord) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	recs = make([]AuditRecord, len(inst.records))
	copy(recs, inst.records)
	return
}

// Len returns the number of buffered records.
func (inst *InMemoryAuditSink) Len() (n int) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	n = len(inst.records)
	return
}
