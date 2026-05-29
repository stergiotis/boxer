//go:build llm_generated_opus47

package audit

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestAuditResultE_String(t *testing.T) {
	cases := map[AuditResultE]string{
		AuditResultOk:          "ok",
		AuditResultDenied:      "denied",
		AuditResultTimeout:     "timeout",
		AuditResultError:       "error",
		AuditResultUnspecified: "unspecified",
	}
	for r, want := range cases {
		assert.Equal(t, want, r.String())
	}
}

func TestInMemoryAuditSink_RecordsInOrder(t *testing.T) {
	sink := NewInMemoryAuditSink()
	sink.Record(AuditRecord{Subject: "a"})
	sink.Record(AuditRecord{Subject: "b"})
	sink.Record(AuditRecord{Subject: "c"})
	got := sink.Records()
	assert.Equal(t, 3, sink.Len())
	assert.Equal(t, "a", got[0].Subject)
	assert.Equal(t, "b", got[1].Subject)
	assert.Equal(t, "c", got[2].Subject)
}

func TestInMemoryAuditSink_ConcurrentRecords(t *testing.T) {
	sink := NewInMemoryAuditSink()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink.Record(AuditRecord{Subject: "x", Ts: time.Now()})
		}()
	}
	wg.Wait()
	assert.Equal(t, 32, sink.Len())
}

func TestAuditFunc_Adapts(t *testing.T) {
	var got AuditRecord
	var f AuditSinkI = AuditFunc(func(rec AuditRecord) { got = rec })
	want := AuditRecord{AppId: app.AppIdT("test"), Subject: "ch.query.boxer", Result: AuditResultOk}
	f.Record(want)
	assert.Equal(t, want, got)
}
