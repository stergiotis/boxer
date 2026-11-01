package audit

import (
	"strconv"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	evaudit "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// A bus request is recorded as an audit event (ADR-0296's Update of
// 2026-10-30): the domain and action below, the bus subject as a
// reference, the result as the outcome and the measurements as
// attributes. These names are what the readers select and decode it by —
// the audit-event view and the app center's fold — so they are spelled
// once, here.
const (
	// BusDomain is the domain of a bus request's row. It is the host's:
	// the audit subject refuses an app's row in it.
	BusDomain = "bus"
	// BusActionRequest is the action of one request.
	BusActionRequest = "request"
	// BusRefSubject is the reference type of the bus subject requested.
	BusRefSubject = "subject"
	// BusAttrLatencyMs, BusAttrRequestBytes and BusAttrResponseBytes are
	// the measurements, decimal; each is absent when zero, as the kind it
	// replaces wrote them.
	BusAttrLatencyMs     = "latency-ms"
	BusAttrRequestBytes  = "request-bytes"
	BusAttrResponseBytes = "response-bytes"
)

// Outcome is the audit event's outcome of a result: ok, denied, timeout
// and error as they are, an unspecified result failed.
func (inst AuditResultE) Outcome() (o string) {
	switch inst {
	case AuditResultOk:
		return evaudit.OutcomeOk
	case AuditResultDenied:
		return evaudit.OutcomeDenied
	case AuditResultTimeout:
		return evaudit.OutcomeTimeout
	case AuditResultError:
		return evaudit.OutcomeError
	default:
		return evaudit.OutcomeFailed
	}
}

// EventOf is the audit event one audited request is recorded as. It names
// no principal and no data subject: a bus request is the host's
// observation that a request crossed the bus, its origin the requester's
// app and window, which the bus took from the client (OriginBy host). The
// retention class is the trail's own. The row passes Validate.
func EventOf(rec AuditRecord) (e evaudit.AuditEvent) {
	e = evaudit.AuditEvent{
		Domain: BusDomain, Action: BusActionRequest, Outcome: rec.Result.Outcome(),
		PrincipalBy: evaudit.PrincipalByNone, OriginBy: evidence.OriginByHost.String(),
		Retention: evaudit.RetentionTrail,
	}
	if rec.Subject != "" {
		e.RefTypes, e.RefValues = []string{BusRefSubject}, []string{evaudit.Bounded(rec.Subject)}
	}
	attr := func(k string, v uint32) {
		if v > 0 {
			e.AttrKeys, e.AttrValues = append(e.AttrKeys, k), append(e.AttrValues, strconv.FormatUint(uint64(v), 10))
		}
	}
	attr(BusAttrLatencyMs, rec.LatencyMs)
	attr(BusAttrRequestBytes, rec.RequestSizeB)
	attr(BusAttrResponseBytes, rec.ResponseSizeB)
	return
}
