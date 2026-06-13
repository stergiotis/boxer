package audit

import (
	"sync"
	"testing"
)

type recordingSink struct {
	mu   sync.Mutex
	recs []AuditRecord
}

func (s *recordingSink) Record(rec AuditRecord) {
	s.mu.Lock()
	s.recs = append(s.recs, rec)
	s.mu.Unlock()
}

func (s *recordingSink) count() (n int) {
	s.mu.Lock()
	n = len(s.recs)
	s.mu.Unlock()
	return
}

// TestAsyncSink_ForwardsAllThenClose checks the happy path: with a buffer
// large enough for the burst, every record reaches the inner sink and Close
// drains synchronously.
func TestAsyncSink_ForwardsAllThenClose(t *testing.T) {
	inner := &recordingSink{}
	s := NewAsyncSink(inner, 0)
	const n = 500
	for i := 0; i < n; i++ {
		s.Record(AuditRecord{Subject: "x"})
	}
	s.Close()
	if got := inner.count(); got != n {
		t.Fatalf("inner received %d, want %d", got, n)
	}
	if d := s.Dropped(); d != 0 {
		t.Fatalf("dropped %d, want 0", d)
	}
}

type blockingSink struct{ gate chan struct{} }

func (s *blockingSink) Record(_ AuditRecord) { <-s.gate }

// TestAsyncSink_DropsOnOverflow checks that a slow inner sink causes
// best-effort drops rather than blocking the producer (the bus Request path).
func TestAsyncSink_DropsOnOverflow(t *testing.T) {
	gate := make(chan struct{})
	inner := &blockingSink{gate: gate}
	s := NewAsyncSink(inner, 1)
	const n = 100
	for i := 0; i < n; i++ {
		s.Record(AuditRecord{Subject: "x"}) // must never block
	}
	if s.Dropped() == 0 {
		t.Fatal("expected overflow drops with a tiny buffer and a blocked sink, got 0")
	}
	close(gate)
	s.Close()
}

// TestAsyncSink_RecordAfterCloseNoPanic ensures a Record racing Close cannot
// send on a closed channel (the data channel is never closed).
func TestAsyncSink_RecordAfterCloseNoPanic(t *testing.T) {
	s := NewAsyncSink(&recordingSink{}, 0)
	s.Close()
	s.Record(AuditRecord{Subject: "x"})
}

// TestAsyncSink_NilInner tolerates a nil inner sink (records discarded).
func TestAsyncSink_NilInner(t *testing.T) {
	s := NewAsyncSink(nil, 0)
	s.Record(AuditRecord{Subject: "x"})
	s.Close()
}
