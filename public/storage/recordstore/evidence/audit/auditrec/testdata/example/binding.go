package example

import (
	"context"
	"iter"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
)

// columns are the physical names the recorder's reads narrow on, read out
// of the generated DDL.
var columns, columnsErr = auditrec.ColumnsFromDDL(ExampleColOrder, auditexampleDDLCreate)

// Store is the example store as the recorder reaches it: batches over
// table, and a store of its own for reads.
type Store struct {
	exec  recordstore.ExecutorI
	table string
	read  *ExampleStore
}

// NewStore binds the table on exec; Table when table is empty.
func NewStore(exec recordstore.ExecutorI, table string) (inst *Store, err error) {
	if columnsErr != nil {
		return nil, columnsErr
	}
	if table == "" {
		table = Table
	}
	return &Store{exec: exec, table: table, read: NewExampleStore(exec, nil, ExampleStoreConfig{Table: table})}, nil
}

// EnsureTable creates the table, and its database, when they are missing.
func (inst *Store) EnsureTable(ctx context.Context) (err error) {
	return inst.read.EnsureTable(ctx)
}

// NewBatch opens a batch over the table.
func (inst *Store) NewBatch(o recordstore.WriteObserverI[uint64, time.Time]) *Batch {
	return &Batch{st: NewExampleStore(inst.exec, nil, ExampleStoreConfig{Table: inst.table, WriteObserver: o})}
}

// Columns are the physical names the recorder's reads narrow on.
func (inst *Store) Columns() auditrec.Columns { return columns }

// ScanEvents reads the audit events under opts with their origins.
func (inst *Store) ScanEvents(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[audit.Event, error] {
	return func(yield func(audit.Event, error) bool) {
		for ent, err := range inst.read.ScanAuditEvent(ctx, opts) {
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
func (inst *Store) ScanRows(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[auditrec.Row, error] {
	return func(yield func(auditrec.Row, error) bool) {
		for ent, err := range inst.read.ScanOrigin(ctx, opts) {
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
			} else if r.Item, err = inst.read.CanonicalItem(ent); err != nil {
				yield(auditrec.Row{}, eh.Errorf("canonical item of row %d: %w", ent.ID, err))
				return
			}
			if !yield(r, nil) {
				return
			}
		}
	}
}

// Close releases the read store.
func (inst *Store) Close() { inst.read.Close() }

// Batch is one batch of rows over the table.
type Batch struct {
	st *ExampleStore
}

// Event buffers one row of o and e under ctx and returns its canonical
// item; on error nothing is buffered.
func (inst *Batch) Event(ctx context.Context, id uint64, at time.Time, key string, o audit.Origin, e audit.AuditEvent) (item []byte, err error) {
	b := inst.st.BeginCtx(ctx, id, at, ExampleEnvelope{NaturalKey: []byte(key)})
	b.AddOrigin(o)
	b.AddAuditEvent(e)
	ent, _ := b.Entity()
	if item, err = inst.st.CanonicalItem(&ent); err != nil {
		_ = b.Rollback()
		return nil, err
	}
	if err = b.Commit(); err != nil {
		return nil, err
	}
	return
}

// Seal buffers the batch's seal row and returns the seal's digest.
func (inst *Batch) Seal(id uint64, at time.Time, key string, o audit.Origin, s auditrec.SealSpec) (digest string, err error) {
	row := SealOf(s.Scheme, s.Stream, s.Prev, s.PrevDigest, s.Rows)
	row.Id = id
	b := inst.st.Begin(id, at, ExampleEnvelope{NaturalKey: []byte(key)})
	b.AddOrigin(o)
	b.AddEvidenceSeal(row)
	if err = b.Commit(); err != nil {
		return "", err
	}
	return row.Digest, nil
}

// Buffered is the rows buffered and not yet flushed.
func (inst *Batch) Buffered() int { return inst.st.Buffered() }

// Flush inserts what is buffered.
func (inst *Batch) Flush(ctx context.Context) (err error) {
	_, err = inst.st.Flush(ctx)
	return
}

// Close discards what is unflushed and releases the batch.
func (inst *Batch) Close() { inst.st.Close() }

var _ auditrec.StoreI[*Batch] = (*Store)(nil)
