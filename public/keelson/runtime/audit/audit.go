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
	"sync/atomic"
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

// AsyncSink decouples Record from a slow inner sink so the bus Request that
// produced the record is never blocked on the sink's work. The motivating
// case: factsstore.AsAuditSink over a ClickHouse-backed store issues one
// synchronous HTTP insert per audit row, and the bus calls Record inside
// Client.Request's deferred path on the caller's goroutine — so without this
// every audited Request would pay a full CH round-trip. Record enqueues into
// a bounded buffer and returns immediately; a background goroutine forwards
// to the inner sink. On overflow the record is dropped (the audit trail is
// best-effort, matching logbridge) and Dropped() is bumped. Close drains the
// buffer and stops the goroutine.
type AsyncSink struct {
	inner   AuditSinkI
	ch      chan AuditRecord
	dropped atomic.Uint64
	stop    chan struct{}
	done    chan struct{}
	closed  atomic.Bool
}

var _ AuditSinkI = (*AsyncSink)(nil)

// DefaultAsyncBuffer bounds the in-flight audit record buffer when
// NewAsyncSink is called with buffer <= 0.
const DefaultAsyncBuffer = 4096

// NewAsyncSink wraps inner with a background forwarder. buffer <= 0 selects
// DefaultAsyncBuffer. A nil inner is tolerated (records are accepted and
// discarded) so callers don't branch on "do I have a durable sink".
func NewAsyncSink(inner AuditSinkI, buffer int) (s *AsyncSink) {
	if buffer <= 0 {
		buffer = DefaultAsyncBuffer
	}
	s = &AsyncSink{
		inner: inner,
		ch:    make(chan AuditRecord, buffer),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go s.loop()
	return
}

func (inst *AsyncSink) Record(rec AuditRecord) {
	if inst.closed.Load() {
		return
	}
	// Never blocks: enqueue if there is room, abandon if we're stopping,
	// drop otherwise. The data channel is never closed (Close signals via
	// stop), so a Record racing Close cannot send on a closed channel.
	select {
	case inst.ch <- rec:
	case <-inst.stop:
	default:
		inst.dropped.Add(1)
	}
}

func (inst *AsyncSink) loop() {
	defer close(inst.done)
	for {
		select {
		case rec := <-inst.ch:
			if inst.inner != nil {
				inst.inner.Record(rec)
			}
		case <-inst.stop:
			// Drain whatever is buffered, then exit.
			for {
				select {
				case rec := <-inst.ch:
					if inst.inner != nil {
						inst.inner.Record(rec)
					}
				default:
					return
				}
			}
		}
	}
}

// Close stops accepting records, drains the buffer into the inner sink, and
// waits for the forwarder goroutine to exit. Idempotent.
func (inst *AsyncSink) Close() {
	if inst.closed.Swap(true) {
		return
	}
	close(inst.stop)
	<-inst.done
}

// Dropped returns the number of records discarded because the buffer was
// full. A persistently growing value means the inner sink cannot keep up
// with the audited-request rate; raise the buffer or speed up the sink.
func (inst *AsyncSink) Dropped() (n uint64) {
	n = inst.dropped.Load()
	return
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
