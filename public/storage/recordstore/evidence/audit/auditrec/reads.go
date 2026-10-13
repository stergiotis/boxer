package auditrec

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// Window bounds a read: timestamps in [From, To) — a zero bound is open —
// at most Limit events (zero: no limit), oldest first unless Newest.
type Window struct {
	From, To time.Time
	Limit    int
	Newest   bool
}

// EventsBySubject reads the stored audit events of one data subject in w,
// of WithNode's node only, each id once. The SQL narrows by the order
// column and a has() over the u64 section's values; the exact match on
// Subject is made after decoding. Subject 0 — no subject — is refused.
// Buffered rows are not seen until a flush.
func (inst *Recorder[B]) EventsBySubject(ctx context.Context, subject uint64, w Window) (events []audit.Event, err error) {
	if subject == 0 {
		return nil, eh.Errorf("auditrec: subject 0 is no subject")
	}
	if inst == nil {
		return nil, nil
	}
	pred := and(inst.windowPredicate(w.From, w.To), inst.nodePredicate(), "has("+inst.cols.U64Array+", "+strconv.FormatUint(subject, 10)+")")
	return inst.events(ctx, pred, w, func(e *audit.AuditEvent) bool { return e.Subject == subject })
}

// EventsByPrincipal reads the stored audit events of one principal in w,
// as EventsBySubject does by subject.
func (inst *Recorder[B]) EventsByPrincipal(ctx context.Context, principal string, w Window) (events []audit.Event, err error) {
	if inst == nil {
		return nil, nil
	}
	pred := and(inst.windowPredicate(w.From, w.To), inst.nodePredicate(), "has("+inst.cols.StringArray+", "+marshalling.EscapeString(principal)+")")
	return inst.events(ctx, pred, w, func(e *audit.AuditEvent) bool { return e.Principal.Has && e.Principal.Val == principal })
}

// events scans under pred, keeps what keep accepts of the node's events,
// each id once, and stops at the limit.
func (inst *Recorder[B]) events(ctx context.Context, pred string, w Window, keep func(*audit.AuditEvent) bool) (events []audit.Event, err error) {
	inst.readMu.Lock()
	defer inst.readMu.Unlock()
	if inst.st == nil {
		return nil, nil
	}
	if inst.stClosed {
		return nil, ErrClosed
	}
	seen := make(map[uint64]struct{})
	for e, serr := range inst.st.ScanEvents(ctx, recordstore.ScanOpts{ExtraPredicate: pred, Descending: w.Newest}) {
		if serr != nil {
			return nil, eh.Errorf("auditrec: read events: %w", serr)
		}
		if !inst.ofNode(&e.Row) || !keep(&e.Row) {
			continue
		}
		if _, dup := seen[e.Id]; dup {
			continue
		}
		seen[e.Id] = struct{}{}
		events = append(events, e)
		if w.Limit > 0 && len(events) >= w.Limit {
			break
		}
	}
	return
}

// VerifySeals is law L34 over the run named run as its rows read back,
// and law L32 over its anchored seals: the run's seals chain from the
// stream's start, every row a seal names is there with the digest the seal
// names, no row is named twice, and each anchored ordinal holds the digest
// of the n-th seal. A row or a seal read twice — a batch stored twice
// after a lost acknowledgement — counts once. broken is the first break,
// zero when the stream holds; unnamed the run's rows no seal names;
// unanchored the anchored ordinals the stream does not reproduce, none
// without an anchor store. Buffered rows are not seen until a flush.
func (inst *Recorder[B]) VerifySeals(ctx context.Context, run string) (broken evidence.SealBreak, unnamed []uint64, unanchored []uint64, err error) {
	if inst == nil {
		return
	}
	inst.readMu.Lock()
	defer inst.readMu.Unlock()
	if inst.st == nil {
		return
	}
	if inst.stClosed {
		err = ErrClosed
		return
	}
	var seals []evidence.Seal
	sealIds := make(map[uint64]struct{})
	present := make(map[uint64]string)
	opts := recordstore.ScanOpts{ExtraPredicate: "has(" + inst.cols.Symbol + ", " + marshalling.EscapeString(run) + ")"}
	for r, serr := range inst.st.ScanRows(ctx, opts) {
		if serr != nil {
			return broken, nil, nil, eh.Errorf("auditrec: read the run's rows: %w", serr)
		}
		if r.Origin.Run != run {
			continue
		}
		if r.Seal.Has {
			if _, dup := sealIds[r.Id]; !dup {
				sealIds[r.Id] = struct{}{}
				seals = append(seals, r.Seal.Val.Seal)
			}
			continue
		}
		if r.Item == nil {
			return broken, nil, nil, eb.Build().Uint64("row", r.Id).Errorf("auditrec: a row without its canonical item")
		}
		present[r.Id] = inst.scheme.RowDigest(r.Item)
	}
	broken, unnamed = evidence.VerifySeals(inst.scheme, "", seals, present)
	if broken.Why != evidence.SealBreakNone || inst.cfg.anchors == nil {
		return
	}
	ordered, _ := evidence.OrderSeals(seals)
	rows, aerr := inst.cfg.anchors.Read(ctx, inst.cfg.cell, inst.cfg.sealScope(run))
	if aerr != nil {
		return broken, unnamed, nil, eh.Errorf("auditrec: read the stream's anchors: %w", aerr)
	}
	unanchored = evidence.VerifySealAnchors(ordered, anchor.HeadsOf(rows, inst.scheme.Name()))
	return
}

func timeLit(t time.Time) string {
	return "fromUnixTimestamp64Nano(" + strconv.FormatInt(t.UTC().UnixNano(), 10) + ")"
}

// windowPredicate narrows a scan to rows in [from, to); a zero bound is
// open.
func (inst *Recorder[B]) windowPredicate(from, to time.Time) string {
	var parts []string
	if !from.IsZero() {
		parts = append(parts, inst.cols.Order+" >= "+timeLit(from))
	}
	if !to.IsZero() {
		parts = append(parts, inst.cols.Order+" < "+timeLit(to))
	}
	return strings.Join(parts, " AND ")
}

// nodePredicate narrows a scan to rows that may be WithNode's node's;
// ofNode makes the exact match after decoding. Without a node every row
// is the recorder's.
func (inst *Recorder[B]) nodePredicate() string {
	if !inst.cfg.node.Has {
		return ""
	}
	return "has(" + inst.cols.Symbol + ", " + marshalling.EscapeString(inst.cfg.node.Val) + ")"
}

func (inst *Recorder[B]) ofNode(row *audit.AuditEvent) bool {
	return !inst.cfg.node.Has || (row.Node.Has && row.Node.Val == inst.cfg.node.Val)
}

func and(preds ...string) string {
	var parts []string
	for _, p := range preds {
		if p != "" {
			parts = append(parts, "("+p+")")
		}
	}
	return strings.Join(parts, " AND ")
}
