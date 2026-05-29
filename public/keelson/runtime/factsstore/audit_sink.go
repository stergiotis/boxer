//go:build llm_generated_opus47

package factsstore

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

// AsAuditSink adapts a FactsStoreI as an audit.AuditSinkI. Each
// AuditRecord is translated to an AuditRow and written via WriteAudit.
// Write errors are swallowed because the bus calls Record synchronously
// inside Request and cannot surface a write failure to the requester —
// production hosts wire a log-forwarding wrapper around the underlying
// store if write failures matter.
func AsAuditSink(s FactsStoreI) (sink audit.AuditSinkI) {
	sink = audit.AuditFunc(func(rec audit.AuditRecord) {
		_, _ = s.WriteAudit(AuditRow{
			AppId:         rec.AppId,
			Subject:       rec.Subject,
			Result:        rec.Result.String(),
			LatencyMs:     rec.LatencyMs,
			RequestSizeB:  rec.RequestSizeB,
			ResponseSizeB: rec.ResponseSizeB,
			Ts:            rec.Ts,
		})
	})
	return
}
