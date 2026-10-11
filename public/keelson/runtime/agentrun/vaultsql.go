package agentrun

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// SqlBodyStore is BodyStoreI over the generated vault store
// (agentrunstore.BodyRowStore) and the executor it was opened on: the
// vault of ADR-0320 §SD1. The generated store is single-goroutine, so
// every method takes one mutex, and every write flushes before it
// returns. A row is destroyed by a heavyweight mutation that rewrites
// the part before the call returns (§SD4), which this table may take
// and the ledger's never do.
type SqlBodyStore struct {
	mu     sync.Mutex
	exec   recordstore.ExecutorI
	layout agentrunstore.Layout
	st     *agentrunstore.BodyRowStore
}

var _ BodyStoreI = (*SqlBodyStore)(nil)

// NewSqlBodyStore opens the vault store over exec in layout; the caller
// provisions first (agentrunstore.ProvisionBodyIn) and closes afterwards.
func NewSqlBodyStore(exec recordstore.ExecutorI, layout agentrunstore.Layout) (inst *SqlBodyStore) {
	return &SqlBodyStore{exec: exec, layout: layout, st: agentrunstore.NewBodyStoreIn(exec, layout)}
}

// Close releases the generated store.
func (inst *SqlBodyStore) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.st.Close()
}

func (inst *SqlBodyStore) Put(ctx context.Context, bodies []Body) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, b := range bodies {
		row := agentrunstore.BodyRow{ID: b.Run, Ordinal: b.Ordinal, Opening: OpeningText(b.Opening), Body: b.Body, Calls: b.Calls}
		if err = inst.st.Begin(b.Run, recordstore.SeqTs(uint64(b.Ordinal))).AddBodyRow(row).Commit(); err != nil {
			inst.st.DiscardPending()
			return eb.Build().Str("run", b.Run).Uint32("ordinal", b.Ordinal).Errorf("buffer body: %w", err)
		}
	}
	if _, err = inst.st.Flush(ctx); err != nil {
		inst.st.DiscardPending()
		return eh.Errorf("flush bodies: %w", err)
	}
	return
}

func (inst *SqlBodyStore) Get(ctx context.Context, run string, from uint32) (bodies []Body, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	predicate := agentrunstore.BodyRowColKey + " = " + marshalling.EscapeString(run)
	if from > 0 {
		predicate += " AND " + agentrunstore.BodyRowColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(recordstore.SeqTs(uint64(from)).UnixNano(), 10) + ")"
	}
	if bodies, err = inst.scanLocked(ctx, predicate); err != nil {
		err = eb.Build().Str("run", run).Errorf("read bodies: %w", err)
	}
	return
}

func (inst *SqlBodyStore) GetOf(ctx context.Context, runs []string) (bodies []Body, err error) {
	if len(runs) == 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ids := make([]string, 0, len(runs))
	for _, id := range runs {
		ids = append(ids, marshalling.EscapeString(id))
	}
	if bodies, err = inst.scanLocked(ctx, agentrunstore.BodyRowColKey+" IN ("+strings.Join(ids, ", ")+")"); err != nil {
		err = eh.Errorf("read bodies: %w", err)
	}
	return
}

// scanLocked reads the rows a predicate selects; where a run and ordinal
// hold more than one row (an append whose ledger write failed and was
// retried at the same ordinal), every row is returned and the reader
// keeps the one that opens.
func (inst *SqlBodyStore) scanLocked(ctx context.Context, predicate string) (bodies []Body, err error) {
	for ent, serr := range inst.st.ScanBodyRow(ctx, recordstore.ScanOpts{ExtraPredicate: predicate}) {
		if serr != nil {
			return nil, serr
		}
		if ent == nil || !ent.BodyRow.Has {
			continue
		}
		row := ent.BodyRow.Val
		bodies = append(bodies, Body{Run: ent.ID, Ordinal: row.Ordinal, Opening: OpeningOf(row.Opening), Body: row.Body, Calls: row.Calls})
	}
	slices.SortStableFunc(bodies, func(a, b Body) int {
		if c := compareString(a.Run, b.Run); c != 0 {
			return c
		}
		return int(a.Ordinal) - int(b.Ordinal)
	})
	return
}

// Destroy rewrites the run's rows out of the table but those at keep.
func (inst *SqlBodyStore) Destroy(ctx context.Context, run string, keep []uint32) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	where := agentrunstore.BodyRowColKey + " = " + marshalling.EscapeString(run)
	if len(keep) > 0 {
		where += " AND " + agentrunstore.BodyRowColOrder + " NOT IN (" + strings.Join(orderInstants(keep), ", ") + ")"
	}
	return inst.mutateLocked(ctx, run, where)
}

// Erase rewrites the rows at ordinals out of the table.
func (inst *SqlBodyStore) Erase(ctx context.Context, run string, ordinals []uint32) (err error) {
	if len(ordinals) == 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	where := agentrunstore.BodyRowColKey + " = " + marshalling.EscapeString(run) +
		" AND " + agentrunstore.BodyRowColOrder + " IN (" + strings.Join(orderInstants(ordinals), ", ") + ")"
	return inst.mutateLocked(ctx, run, where)
}

// mutateLocked is the deletion that rewrites: ALTER TABLE … DELETE with
// mutations_sync = 2 waits for the part to be rewritten before it
// returns, so the bytes are gone from the table when the erasure is
// reported done (ADR-0320 §SD4), not masked for a later merge as a
// lightweight DELETE leaves them. The replaced part stays on disk as an
// outdated part until the server's cleanup removes it, within seconds
// under the table's settings (agentrunstore.BodyDDLTail).
func (inst *SqlBodyStore) mutateLocked(ctx context.Context, run string, where string) (err error) {
	if err = inst.exec.Exec(ctx, "ALTER TABLE "+inst.layout.BodyTable()+" DELETE WHERE "+where+" SETTINGS mutations_sync = 2"); err != nil {
		return eb.Build().Str("run", run).Errorf("destroy bodies: %w", err)
	}
	return
}

// orderInstants are ordinals as the order column's literals.
func orderInstants(ordinals []uint32) (literals []string) {
	literals = make([]string, 0, len(ordinals))
	for _, o := range ordinals {
		literals = append(literals, "fromUnixTimestamp64Nano("+strconv.FormatInt(recordstore.SeqTs(uint64(o)).UnixNano(), 10)+")")
	}
	return
}
