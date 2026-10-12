package attention

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// The audit vocabulary (ADR-0296): what the person did to the queue
// (ADR-0324 §SD4), to the waiting scale (ADR-0323 §SD5) and to their
// status (ADR-0328 §SD1), each with the person as its author, or the host
// for an Away it set for idle time.
const (
	auditDomain       = "attention"
	auditActionLevel  = "level"
	auditActionStatus = "status"
	auditRefItem      = "attention-item"
	auditRefConv      = "conversation"
	auditRefApp       = "app"
	auditAttrKind     = "kind"
	auditAttrLevel    = "level"
	auditAttrScope    = "scope"
	auditScopePerson  = "person"
	auditScopeConvers = "conversation"
)

// AttachTrail records the person's acknowledgements, shelves, changes on
// the waiting scale and changes of status on rec. A recorder without a
// backend records nothing.
func AttachTrail(svc *Service, rec *trail.Recorder, log zerolog.Logger) {
	if svc == nil || rec == nil {
		return
	}
	a := &auditor{rec: rec, log: log}
	svc.Queue.SetOnAct(a.act)
	svc.Levels.SetOnWaitingChange(a.level)
	if svc.Presence != nil {
		svc.Presence.SetOnChange(a.status)
	}
}

type auditor struct {
	rec *trail.Recorder
	log zerolog.Logger
}

func (inst *auditor) act(act ActE, it Item, at time.Time) { inst.write(actRow(act, it), at) }

func (inst *auditor) level(ch Change) { inst.write(levelRow(ch), time.Now()) }

// status records a change of the person's status (ADR-0328 §SD1): by the
// person, or by the host for idle time.
func (inst *auditor) status(ch StatusChange) {
	row := trail.AuditEvent{Domain: auditDomain, Action: auditActionStatus, Outcome: trail.OutcomeOk, Retention: trail.RetentionTrail}
	attr(&row, "from", ch.From.String())
	attr(&row, "to", ch.To.String())
	by := "person"
	if ch.ByIdle {
		by = "idle"
	}
	attr(&row, "by", by)
	inst.write(row, ch.At)
}

func actRow(act ActE, it Item) (row trail.AuditEvent) {
	row = trail.AuditEvent{Domain: auditDomain, Action: act.String(), Outcome: trail.OutcomeOk, Retention: trail.RetentionTrail}
	ref(&row, auditRefItem, it.Key)
	ref(&row, auditRefConv, it.Conversation)
	ref(&row, auditRefApp, string(it.App))
	attr(&row, auditAttrKind, it.Kind)
	return
}

func levelRow(ch Change) (row trail.AuditEvent) {
	row = trail.AuditEvent{Domain: auditDomain, Action: auditActionLevel, Outcome: trail.OutcomeOk, Retention: trail.RetentionTrail}
	ref(&row, auditRefConv, ch.Conversation)
	ref(&row, auditRefApp, string(ch.Key.App))
	attr(&row, auditAttrKind, ch.Key.Kind)
	if ch.Key.Effect != 0 {
		attr(&row, "effect", ch.Key.Effect.String())
	}
	scope := auditScopePerson
	if ch.Conversation != "" {
		scope = auditScopeConvers
	}
	attr(&row, auditAttrScope, scope)
	lvl := "default"
	if ch.Level != LevelUnspecified {
		lvl = ch.Level.String()
	}
	attr(&row, auditAttrLevel, lvl)
	return
}

func (inst *auditor) write(row trail.AuditEvent, at time.Time) {
	if !inst.rec.Durable() {
		return
	}
	c := trail.Context{Origin: inst.rec.OriginOf(ServiceAppId, 0)}
	if err := inst.rec.Event(context.Background(), at.UTC(), c, row); err != nil {
		inst.log.Warn().Err(err).Str("action", row.Action).Msg("attention: write the audit event")
	}
	inst.rec.FlushSoon()
}

func ref(row *trail.AuditEvent, ty string, v string) {
	if v != "" && len(row.RefTypes) < trail.MaxRefs {
		row.RefTypes, row.RefValues = append(row.RefTypes, ty), append(row.RefValues, clip(v, trail.MaxValueRunes-1))
	}
}

func attr(row *trail.AuditEvent, k string, v string) {
	if v != "" && len(row.AttrKeys) < trail.MaxAttrs {
		row.AttrKeys, row.AttrValues = append(row.AttrKeys, k), append(row.AttrValues, clip(v, trail.MaxValueRunes-1))
	}
}
