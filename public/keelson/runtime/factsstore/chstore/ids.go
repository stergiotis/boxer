package chstore

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// leaseRetryInterval is how long a store mints process-local ids after a
// lease failed before it tries to lease again.
const leaseRetryInterval = time.Minute

// rowIds are the two streams a store mints row ids from (ADR-0397 §SD2).
//
// leased draws blocks of vocab.IdStreamFactsRow through the lease table
// beside the facts table (ADR-0319 §SD3), so an id is unique across every
// process writing to the server and across runs. Building it leases
// nothing: the first write leases the first block, and a block lasts
// rowalloc.DefaultBlockSize rows.
//
// local counts under vocab.IdStreamFactsRowLocal in process. A write takes
// it while no block can be leased — the lease table is missing, or the
// server refused the lease — so the row still lands; its id is unique only
// within the process, and its tag says so.
type rowIds struct {
	leased *leased.Sequence
	local  *leased.Sequence
	// retryAt is the time, in Unix nanoseconds, before which no lease is
	// attempted after one failed.
	retryAt atomic.Int64
}

func (inst *rowIds) init(exec recordstore.ExecutorI, layout rowalloc.Layout) (err error) {
	alloc, err := rowalloc.NewAllocator(exec, layout)
	if err != nil {
		return eh.Errorf("row id lease allocator: %w", err)
	}
	inst.leased, err = rowalloc.NewSequence(alloc, vocab.IdStreamFactsRow, 0)
	if err != nil {
		return eh.Errorf("row id stream: %w", err)
	}
	inst.local, err = leased.NewSequence(memalloc.NewAllocator(), vocab.IdStreamFactsRowLocal.Value(), rowalloc.DefaultBlockSize)
	if err != nil {
		return eh.Errorf("process-local row id stream: %w", err)
	}
	return
}

// leaseLayout is the lease table this store leases through: the one in
// the facts table's database.
func (inst *Store) leaseLayout() rowalloc.Layout {
	return rowalloc.Layout{Database: inst.cfg.Database}
}

// mintId draws the next row id: leased when a block is in hand or can be
// leased, process-local otherwise. A failed lease is logged and not
// retried for leaseRetryInterval; the write it happened in goes on with a
// local id. The lease's round trip runs under the write's ctx, bounded by
// WriteTimeout when ctx carries no deadline of its own.
func (inst *Store) mintId(ctx context.Context) (id uint64, err error) {
	now := time.Now().UnixNano()
	if now >= inst.ids.retryAt.Load() {
		lctx, cancel := boundWrite(ctx)
		t, _, lerr := inst.ids.leased.GetId(lctx, nil)
		cancel()
		if lerr == nil {
			id = t.Value()
			return
		}
		inst.ids.retryAt.Store(now + int64(leaseRetryInterval))
		log.Warn().Err(lerr).Str("leaseTable", inst.leaseLayout().Table()).Dur("retryIn", leaseRetryInterval).
			Msg("chstore: no block of row ids could be leased; rows get process-local ids until a lease succeeds")
	}
	t, _, err := inst.ids.local.GetId(ctx, nil)
	if err != nil {
		err = eh.Errorf("chstore: mint a process-local row id: %w", err)
		return
	}
	id = t.Value()
	return
}
