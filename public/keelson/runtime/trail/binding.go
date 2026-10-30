package trail

import (
	"context"
	_ "embed"
	"iter"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
)

// The trail's binding to the shared writer (ADR-0330 §SD4, §SD8): the
// trail store as auditrec reaches it. The envelope is a natural key on
// boxer.facts, so the recorder's key is the row's natural key. Every row
// carries its origin; a row of the trail's own kinds is built by a closure
// the recorder's Write hands a batch, an audit event by the batch's Event.

// The facts DDL the store was generated against, embedded so the reads
// take their physical column names from it rather than spelling them: a
// change of encoding hints regenerates them.
//
//go:embed facts_ddl_clickhouse.out.sql
var factsDDL string

// auditColumns are the physical names the recorder's reads narrow on.
var auditColumns, auditColumnsErr = auditrec.ColumnsFromDDL(TrailColOrder, factsDDL)

// rowContextKey carries an audit event's conversation and delegation from
// Recorder.Event to the batch that builds its row: the shared writer's
// event verb knows only the origin and the event.
type rowContextKey struct{}

// withRowContext is ctx carrying c's conversation and delegation, when c
// has either.
func withRowContext(ctx context.Context, c Context) context.Context {
	if !c.Conversation.Has && !c.Delegation.Has {
		return ctx
	}
	return context.WithValue(ctx, rowContextKey{}, c)
}

// auditStore is the trail store as the recorder reaches it: a batch per
// flush over the facts table, and a store of its own for reads, released
// by Close under mu.
type auditStore struct {
	exec recordstore.ExecutorI

	mu   sync.Mutex
	read *TrailStore
}

func newAuditStore(exec recordstore.ExecutorI, readTable string) (inst *auditStore, err error) {
	if auditColumnsErr != nil {
		return nil, auditColumnsErr
	}
	return &auditStore{exec: exec, read: NewTrailStore(exec, nil, TrailStoreConfig{ReadTable: readTable})}, nil
}

// NewBatch opens a batch over the facts table with o as its observer.
func (inst *auditStore) NewBatch(o recordstore.WriteObserverI[uint64, time.Time]) *auditBatch {
	return &auditBatch{st: NewTrailStore(inst.exec, nil, TrailStoreConfig{WriteObserver: o})}
}

// Columns are the physical names the recorder's reads narrow on.
func (inst *auditStore) Columns() auditrec.Columns { return auditColumns }

// scan runs one of the read store's scans under mu; nothing once closed.
func (inst *auditStore) scan(scan func(st *TrailStore) iter.Seq2[*TrailEntity, error]) iter.Seq2[*TrailEntity, error] {
	return func(yield func(*TrailEntity, error) bool) {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if inst.read == nil {
			return
		}
		for ent, err := range scan(inst.read) {
			if !yield(ent, err) {
				return
			}
		}
	}
}

// ScanEvents reads the audit events under opts with their origins.
func (inst *auditStore) ScanEvents(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[audit.Event, error] {
	return func(yield func(audit.Event, error) bool) {
		for ent, err := range inst.scan(func(st *TrailStore) iter.Seq2[*TrailEntity, error] { return st.ScanAuditEvent(ctx, opts) }) {
			if err != nil {
				yield(audit.Event{}, err)
				return
			}
			if ent == nil || !ent.AuditEvent.Has {
				continue
			}
			e := audit.Event{Id: ent.ID, At: ent.Ts, Row: ent.AuditEvent.Val}
			if ent.Origin.Has {
				e.Origin = ent.Origin.Val
			}
			if !yield(e, nil) {
				return
			}
		}
	}
}

// ScanRows reads the rows carrying an origin under opts: a seal as the
// seal law reads it, any other row with its canonical item.
func (inst *auditStore) ScanRows(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[auditrec.Row, error] {
	return func(yield func(auditrec.Row, error) bool) {
		for ent, err := range inst.scan(func(st *TrailStore) iter.Seq2[*TrailEntity, error] { return st.ScanOrigin(ctx, opts) }) {
			if err != nil {
				yield(auditrec.Row{}, err)
				return
			}
			if ent == nil || !ent.Origin.Has {
				continue
			}
			r := auditrec.Row{Id: ent.ID, Origin: ent.Origin.Val}
			if ent.EvidenceSeal.Has {
				s := ent.EvidenceSeal.Val
				r.Seal = option.Some(auditrec.StreamSeal{Stream: s.Stream, Seal: s.Seal(ent.ID)})
			} else if r.Item, err = inst.read.CanonicalItem(ent); err != nil { // the scan holds mu, so read is open
				yield(auditrec.Row{}, eb.Build().Uint64("row", ent.ID).Errorf("canonical item of row: %w", err))
				return
			}
			if !yield(r, nil) {
				return
			}
		}
	}
}

// Close releases the read store.
func (inst *auditStore) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.read != nil {
		inst.read.Close()
		inst.read = nil
	}
}

// auditBatch is one batch of trail rows.
type auditBatch struct {
	st *TrailStore
}

// row buffers one row keyed key with its origin and c's conversation and
// delegation, add contributing the row's own component, and returns its
// canonical item; a row whose item cannot be computed is not buffered.
func (inst *auditBatch) row(ctx context.Context, id uint64, at time.Time, key string, o Origin, c Context, add func(b *TrailEntityBuilder)) (item []byte, err error) {
	b := inst.st.BeginCtx(ctx, id, at.UTC(), TrailEnvelope{NaturalKey: []byte(key)})
	o.Id = id
	b.AddOrigin(o)
	if c.Conversation.Has {
		v := c.Conversation.Val
		v.Id = id
		b.AddConversation(v)
	}
	if c.Delegation.Has {
		v := c.Delegation.Val
		v.Id = id
		b.AddDelegation(v)
	}
	add(b)
	ent, mirrored := b.Entity()
	if !mirrored {
		_ = b.Rollback()
		return nil, eh.Errorf("a row written through Raw has no canonical item")
	}
	if item, err = inst.st.CanonicalItem(&ent); err != nil {
		_ = b.Rollback()
		return nil, err
	}
	if err = b.Commit(); err != nil {
		return nil, err
	}
	return
}

// Event buffers one audit event's row, with the conversation and the
// delegation Recorder.Event put on ctx, and returns its canonical item.
func (inst *auditBatch) Event(ctx context.Context, id uint64, at time.Time, key string, o audit.Origin, e audit.AuditEvent) (item []byte, err error) {
	c, _ := ctx.Value(rowContextKey{}).(Context)
	return inst.row(ctx, id, at, key, o, c, func(b *TrailEntityBuilder) { b.AddAuditEvent(e) })
}

// Seal buffers the batch's seal row, built by the store's SealOf, and
// returns the seal's digest.
func (inst *auditBatch) Seal(id uint64, at time.Time, key string, o audit.Origin, s auditrec.SealSpec) (digest string, err error) {
	row := SealOf(s.Scheme, s.Stream, s.Prev, s.PrevDigest, s.Rows)
	row.Id = id
	b := inst.st.Begin(id, at, TrailEnvelope{NaturalKey: []byte(key)})
	b.AddOrigin(o)
	b.AddEvidenceSeal(row)
	if err = b.Commit(); err != nil {
		return "", err
	}
	return row.Digest, nil
}

// Buffered is the rows buffered and not yet flushed.
func (inst *auditBatch) Buffered() int { return inst.st.Buffered() }

// Flush inserts what is buffered.
func (inst *auditBatch) Flush(ctx context.Context) (err error) {
	_, err = inst.st.Flush(ctx)
	return
}

// Close discards what is unflushed and releases the batch.
func (inst *auditBatch) Close() { inst.st.Close() }

var _ auditrec.StoreI[*auditBatch] = (*auditStore)(nil)
