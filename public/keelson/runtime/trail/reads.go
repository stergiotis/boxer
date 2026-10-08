package trail

import (
	"context"
	_ "embed"
	"iter"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The facts DDL the store was generated against, embedded so the read
// helpers take their physical column names from it rather than spelling
// them: a change of encoding hints regenerates them (as the watchbill store
// does for its claim SQL).
//
//go:embed facts_ddl_clickhouse.out.sql
var factsDDL string

// valueColumns maps a section name to the quoted physical name of its
// value column.
var valueColumns = valueColumnsOf(factsDDL)

var valueColumnPattern = regexp.MustCompile(`"tv:([A-Za-z0-9]+):value:[^"]*"`)

func valueColumnsOf(ddl string) (cols map[string]string) {
	cols = make(map[string]string, 16)
	for _, m := range valueColumnPattern.FindAllStringSubmatch(ddl, -1) {
		cols[m[1]] = m[0]
	}
	return
}

// valueColumn is the quoted physical value column of a section; it panics
// on an unknown section, which only a typo here can cause.
func valueColumn(section string) (name string) {
	name, ok := valueColumns[section]
	if !ok {
		panic("trail: no value column for section " + section)
	}
	return
}

func timeLit(t time.Time) (lit string) {
	return "fromUnixTimestamp64Nano(" + strconv.FormatInt(t.UTC().UnixNano(), 10) + ")"
}

// windowPredicate narrows a scan to rows whose timestamp is in [from, to);
// a zero bound is open.
func windowPredicate(from, to time.Time) (pred string) {
	var parts []string
	if !from.IsZero() {
		parts = append(parts, TrailColOrder+" >= "+timeLit(from))
	}
	if !to.IsZero() {
		parts = append(parts, TrailColOrder+" < "+timeLit(to))
	}
	return strings.Join(parts, " AND ")
}

func andPredicates(preds ...string) (pred string) {
	var parts []string
	for _, p := range preds {
		if p != "" {
			parts = append(parts, "("+p+")")
		}
	}
	return strings.Join(parts, " AND ")
}

// EventsByPrincipal reads the audit events of one principal with a
// timestamp in [from, to) — a zero bound is open — oldest first, at most
// limit of them (zero is no limit). The SQL narrows by the time column and
// a has() over the string section's values; the exact match on the
// Principal slot is made after decoding, so a row that carries the same
// string in another slot is read and discarded. Nil without a store;
// buffered rows are not seen until a flush (ADR-0296 §SD5).
func (inst *Recorder) EventsByPrincipal(ctx context.Context, principal string, from, to time.Time, limit int) (ents []*TrailEntity, err error) {
	pred := andPredicates(windowPredicate(from, to), "has("+valueColumn("stringArray")+", "+marshalling.EscapeString(principal)+")")
	return inst.events(ctx, pred, limit, func(e *AuditEvent) bool { return e.Principal.Has && e.Principal.Val == principal })
}

// EventsBySubject reads the audit events of one data subject with a
// timestamp in [from, to), oldest first, at most limit of them, as
// EventsByPrincipal does by principal. Subject 0 — no subject — is refused,
// since it names every event that has none.
func (inst *Recorder) EventsBySubject(ctx context.Context, subject uint64, from, to time.Time, limit int) (ents []*TrailEntity, err error) {
	if subject == 0 {
		return nil, eh.Errorf("trail: subject 0 is no subject")
	}
	pred := andPredicates(windowPredicate(from, to), "has("+valueColumn("u64Array")+", "+strconv.FormatUint(subject, 10)+")")
	return inst.events(ctx, pred, limit, func(e *AuditEvent) bool { return e.Subject == subject })
}

// events scans audit events under pred, keeps those keep accepts, and
// stops at limit.
func (inst *Recorder) events(ctx context.Context, pred string, limit int, keep func(*AuditEvent) bool) (ents []*TrailEntity, err error) {
	if inst == nil {
		return nil, nil
	}
	inst.readMu.Lock()
	defer inst.readMu.Unlock()
	if inst.read == nil {
		return nil, nil
	}
	seq := inst.read.ScanAuditEvent(ctx, recordstore.ScanOpts{ExtraPredicate: pred})
	for ent, serr := range seq {
		if serr != nil {
			return nil, eh.Errorf("trail: read events: %w", serr)
		}
		if ent == nil || !ent.AuditEvent.Has || !keep(&ent.AuditEvent.Val) {
			continue
		}
		ents = append(ents, ent)
		if limit > 0 && len(ents) >= limit {
			break
		}
	}
	return
}

// scanEvents is the iterator form over the read store under pred, for
// the forwarder's backstop.
func (inst *Recorder) scanEvents(ctx context.Context, pred string) (seq iter.Seq2[*TrailEntity, error]) {
	return func(yield func(*TrailEntity, error) bool) {
		if inst == nil {
			return
		}
		inst.readMu.Lock()
		defer inst.readMu.Unlock()
		if inst.read == nil {
			return
		}
		for ent, serr := range inst.read.ScanAuditEvent(ctx, recordstore.ScanOpts{ExtraPredicate: pred}) {
			if !yield(ent, serr) {
				return
			}
		}
	}
}
