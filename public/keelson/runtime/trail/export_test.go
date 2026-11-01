package trail

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/auditrectest"
)

// WithMaxBacklogForTest bounds the held rows at rows rather than
// MaxBacklogRows, for tests that need a gap without holding that many.
func WithMaxBacklogForTest(rows int) RecorderOption {
	return func(inst *Recorder) { inst.cfg.maxBacklog = rows }
}

// ConformanceForTest runs auditrec's conformance test over the trail's
// binding on exec, whose facts table is provisioned.
func ConformanceForTest(t *testing.T, exec recordstore.ExecutorI) {
	auditrectest.Conformance(t, func(t *testing.T) auditrec.StoreI[*auditBatch] {
		st, err := newAuditStore(exec, "")
		if err != nil {
			t.Fatal(err)
		}
		return st
	})
}
