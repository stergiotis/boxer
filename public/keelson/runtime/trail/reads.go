package trail

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
)

// EventsByPrincipal reads the audit events of one principal with a
// timestamp in [from, to) — a zero bound is open — oldest first, at most
// limit of them (zero is no limit), each row once. The reads are the
// shared writer's (ADR-0330 §SD7): the SQL narrows by the time column and
// a has() over the string section's values; the exact match on the
// Principal slot is made after decoding, so a row that carries the same
// string in another slot is read and discarded. Each entity carries the
// row's origin and audit event. Nil without a store; buffered rows are not
// seen until a flush (ADR-0296 §SD5).
func (inst *Recorder) EventsByPrincipal(ctx context.Context, principal string, from, to time.Time, limit int) (ents []*TrailEntity, err error) {
	if inst == nil {
		return nil, nil
	}
	events, err := inst.rec.EventsByPrincipal(ctx, principal, auditrec.Window{From: from, To: to, Limit: limit})
	return entitiesOf(events), err
}

// EventsBySubject reads the audit events of one data subject with a
// timestamp in [from, to), oldest first, at most limit of them, as
// EventsByPrincipal does by principal. Subject 0 — no subject — is refused,
// since it names every event that has none.
func (inst *Recorder) EventsBySubject(ctx context.Context, subject uint64, from, to time.Time, limit int) (ents []*TrailEntity, err error) {
	if subject == 0 {
		return nil, eh.Errorf("trail: subject 0 is no subject")
	}
	if inst == nil {
		return nil, nil
	}
	events, err := inst.rec.EventsBySubject(ctx, subject, auditrec.Window{From: from, To: to, Limit: limit})
	return entitiesOf(events), err
}

// entitiesOf is the read events as trail entities: the origin and the
// audit event each row carries.
func entitiesOf(events []audit.Event) (ents []*TrailEntity) {
	if len(events) == 0 {
		return nil
	}
	ents = make([]*TrailEntity, 0, len(events))
	for _, e := range events {
		ents = append(ents, &TrailEntity{ID: e.Id, Ts: e.At, Origin: option.Some(e.Origin), AuditEvent: option.Some(e.Row)})
	}
	return
}

// ForwardWindow is the backstop (ADR-0296 §SD6), the shared writer's
// (ADR-0330 §SD7): it reads the audit events with a timestamp in [from,
// to), each row once, oldest first, and hands them to the forwarder in
// batches of at most page rows, on the caller's goroutine, skipping ids
// the prompt path forwarded lately. The caller chooses the windows: each
// should reach back past the longest outage the recorder holds rows
// through, because a batch held through one lands with its old
// timestamps. n is what was handed over. Nothing without a forwarder or a
// store.
func (inst *Recorder) ForwardWindow(ctx context.Context, from, to time.Time, page int) (n int, err error) {
	if inst == nil {
		return 0, nil
	}
	return inst.rec.ForwardWindow(ctx, from, to, page)
}
