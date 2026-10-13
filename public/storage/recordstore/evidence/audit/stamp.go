package audit

import (
	"context"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// Stamp is row with the identity on ctx applied, the rule every recorder
// of audit events follows (ADR-0296 §SD2): the principal and the purpose
// come from the call identity's claims and replace whatever row carried;
// without a principal PrincipalBy becomes none and PrincipalVia absent;
// OriginBy is the context's attestation, else def. PrincipalBy and
// PrincipalVia beside a principal stay the emitter's.
func Stamp(ctx context.Context, row AuditEvent, def evidence.OriginByE) (out AuditEvent) {
	out = row
	out.Principal = option.None[string]()
	out.Purpose = option.None[string]()
	if ci, ok := callident.CallIdentityFrom(ctx); ok {
		if ci.Claims.Principal != "" {
			out.Principal = option.Some(ci.Claims.Principal)
		}
		if ci.Claims.Purpose != "" {
			out.Purpose = option.Some(ci.Claims.Purpose)
		}
	}
	if !out.Principal.Has {
		out.PrincipalBy = PrincipalByNone
		out.PrincipalVia = option.None[string]()
	}
	out.OriginBy = def.String()
	if a, ok := evidence.AttestationFrom(ctx); ok {
		out.OriginBy = a.OriginBy.String()
	}
	return
}

// OriginOf is the origin a row recorded under ctx carries. Run is always
// run, the recorder's: it names the seal stream the row belongs to, so a
// call identity naming another run does not move the row out of it. The
// app and the instance are the call identity's when it carries an origin
// naming either, else def's. Id is left as def has it.
func OriginOf(ctx context.Context, run string, def Origin) (o Origin) {
	o = def
	o.Run = run
	if ci, ok := callident.CallIdentityFrom(ctx); ok && (ci.Origin.App != "" || ci.Origin.Instance != 0) {
		o.App, o.Instance = ci.Origin.App, ci.Origin.Instance
	}
	return
}

// Invalid is the row a recorder writes in place of one that failed
// Validate (ADR-0296 §SD4): the recorder's own domain, naming the
// attempted domain and action and the reason, keeping the subject, the
// node, and a principal with its PrincipalBy and PrincipalVia where they
// pass the bounds. OriginBy is row's when it is one, else def. Built from
// bounded values, it passes Validate.
func Invalid(row AuditEvent, verr error, def evidence.OriginByE) (out AuditEvent) {
	out = AuditEvent{
		Domain: TrailDomain, Action: ActionAuditInvalid, Outcome: OutcomeFailed,
		PrincipalBy: PrincipalByNone, OriginBy: row.OriginBy, Subject: row.Subject, Retention: RetentionTrail,
		AttrKeys:   []string{"domain", "action", "reason"},
		AttrValues: []string{Bounded(row.Domain), Bounded(row.Action), Bounded(verr.Error())},
	}
	if _, ok := evidence.ParseOriginBy(out.OriginBy); !ok {
		out.OriginBy = def.String()
	}
	if row.Node.Has && CheckValue("node", row.Node.Val) == nil {
		out.Node = row.Node
	}
	if row.Principal.Has && CheckValue("principal", row.Principal.Val) == nil {
		out.Principal = row.Principal
		switch row.PrincipalBy {
		case PrincipalBySystem, PrincipalByEnv, PrincipalByOs:
			out.PrincipalBy = row.PrincipalBy
		default:
			out.PrincipalBy = PrincipalByEnv
		}
		if row.PrincipalVia.Has && CheckName("principal-via", row.PrincipalVia.Val) == nil {
			out.PrincipalVia = row.PrincipalVia
		}
	}
	return
}

// Gap is the row a recorder writes for rows it had to drop (ADR-0296
// §SD4): the recorder's own domain, the count and the window of the
// dropped rows' timestamps as attributes, under originBy and, when given,
// the recorder's node. It passes Validate.
func Gap(rows int, first, last time.Time, originBy evidence.OriginByE, node option.Option[string]) (out AuditEvent) {
	out = AuditEvent{
		Domain: TrailDomain, Action: ActionAuditGap, Outcome: OutcomeFailed,
		PrincipalBy: PrincipalByNone, OriginBy: originBy.String(), Retention: RetentionTrail,
		AttrKeys:   []string{"rows", "from", "to"},
		AttrValues: []string{strconv.Itoa(rows), first.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano)},
	}
	if node.Has && CheckValue("node", node.Val) == nil {
		out.Node = node
	}
	return
}
