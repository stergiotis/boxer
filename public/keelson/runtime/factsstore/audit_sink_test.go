//go:build llm_generated_opus47

package factsstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

func TestAsAuditSink_FunnelsToWriteAudit(t *testing.T) {
	s := NewInMemoryFactsStore()
	sink := AsAuditSink(s)
	rec := audit.AuditRecord{
		AppId:         "play",
		Subject:       "ch.query.boxer",
		Result:        audit.AuditResultOk,
		LatencyMs:     7,
		RequestSizeB:  100,
		ResponseSizeB: 4096,
		Ts:            time.Now(),
	}
	sink.Record(rec)
	rows := s.AuditRows()
	assert.Len(t, rows, 1)
	assert.Equal(t, "play", string(rows[0].AppId))
	assert.Equal(t, "ch.query.boxer", rows[0].Subject)
	assert.Equal(t, "ok", rows[0].Result)
	assert.Equal(t, uint32(7), rows[0].LatencyMs)
	assert.Equal(t, uint32(100), rows[0].RequestSizeB)
	assert.Equal(t, uint32(4096), rows[0].ResponseSizeB)
}

func TestAsAuditSink_DeniedResult(t *testing.T) {
	s := NewInMemoryFactsStore()
	sink := AsAuditSink(s)
	sink.Record(audit.AuditRecord{Subject: "x.y", Result: audit.AuditResultDenied})
	rows := s.AuditRows()
	assert.Equal(t, "denied", rows[0].Result)
}
