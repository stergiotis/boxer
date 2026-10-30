package trail

import (
	"context"
	"errors"
	"time"

	busaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
)

// AsAuditSink records each audited bus request through rec as the audit
// event busaudit.EventOf maps it to (ADR-0296's Update of 2026-10-30): the
// origin is the requester's app and window under rec's run, the bus being
// the host's, its originBy host; there is no call identity, so no
// principal. The rows ride rec's buffer and land with its next flush; the
// bus calls Record on the requester's goroutine, so a host puts
// busaudit.NewAsyncSink in front of it. A failure to buffer is logged; the
// bus cannot surface it to the requester.
func AsAuditSink(rec *Recorder) (sink busaudit.AuditSinkI) {
	ctx := evidence.WithAttestation(context.Background(), evidence.Attestation{OriginBy: evidence.OriginByHost})
	return busaudit.AuditFunc(func(r busaudit.AuditRecord) {
		at := r.Ts
		if at.IsZero() {
			at = time.Now()
		}
		err := rec.Event(ctx, at, Context{Origin: rec.OriginOf(r.AppId, r.InstanceKey)}, busaudit.EventOf(r))
		if err != nil && !errors.Is(err, auditrec.ErrClosed) {
			rec.log.Warn().Err(err).Str("subject", r.Subject).Msg("trail: the bus request's audit event could not be buffered")
		}
	})
}
