package chstore

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// leaseRetryInterval is how long a store mints process-local ids after a
// lease failed before it tries to lease again.
const leaseRetryInterval = time.Minute

// RowIds configures where a Store's row ids come from (ADR-0397 §SD2). The
// zero value is the default: blocks of vocab.IdStreamFactsRow leased through
// the lease table beside the facts table (ADR-0319 §SD3).
//
// A deployment with an id authority of its own sets Allocator, and Stream
// when the ids are to carry a tag it claimed. Whatever the source, a write
// made while no block can be had still lands, under the process-local
// stream vocab.IdStreamFactsRowLocal.
type RowIds struct {
	// Allocator reserves the blocks the store mints from. It must hand out,
	// for Stream's tag value, blocks that overlap no block of any other
	// writer of the table: that is all that makes an id unique. The store
	// does not close it. Nil selects the lease table, which setup then
	// provisions; with an allocator set, setup leaves the lease table alone.
	Allocator identgen.AllocatorI
	// Stream is the claimed tag the ids carry; the zero value is
	// vocab.IdStreamFactsRow. A stream other than that one is the
	// deployment's decision in the way a claim is (ADR-0319 §SD2): rows
	// written under one value keep it.
	Stream tagmint.ClaimedTagValue
	// BlockSize is how many ids one reservation covers (ADR-0319 §SD4);
	// zero is rowalloc.DefaultBlockSize.
	BlockSize uint64
}

// rowIds are the two streams a store mints row ids from (ADR-0397 §SD2).
//
// leased draws blocks of the configured stream from the configured
// allocator — by default vocab.IdStreamFactsRow through the lease table
// beside the facts table (ADR-0319 §SD3) — so an id is unique across every
// process writing to the server and across runs. Building it reserves
// nothing: the first write reserves the first block.
//
// local counts under vocab.IdStreamFactsRowLocal in process. A write takes
// it while no block can be had — the lease table is missing, the server or
// the authority refused — so the row still lands; its id is unique only
// within the process, and its tag says so.
type rowIds struct {
	leased *leased.Sequence
	local  *leased.Sequence
	// source names where the leased blocks come from, for the log.
	source string
	// retryAt is the time, in Unix nanoseconds, before which no lease is
	// attempted after one failed.
	retryAt atomic.Int64
}

// stream is the claimed tag the leased ids carry.
func (inst RowIds) stream() (stream tagmint.ClaimedTagValue) {
	stream = inst.Stream
	if !stream.IsValid() {
		stream = vocab.IdStreamFactsRow
	}
	return
}

func (inst *rowIds) init(exec recordstore.ExecutorI, layout rowalloc.Layout, cfg RowIds) (err error) {
	stream := cfg.stream()
	if stream.Value() == vocab.IdStreamFactsRowLocal.Value() {
		return eb.Build().Str("stream", stream.Name()).Errorf("row id stream: the process-local stream cannot be the leased one; leased ids need a tag of their own")
	}
	blockSize := cfg.BlockSize
	if blockSize == 0 {
		blockSize = rowalloc.DefaultBlockSize
	}
	alloc := cfg.Allocator
	inst.source = "the configured allocator"
	if alloc == nil {
		var rows *rowalloc.Allocator
		rows, err = rowalloc.NewAllocator(exec, layout)
		if err != nil {
			return eh.Errorf("row id lease allocator: %w", err)
		}
		alloc = rows
		inst.source = layout.Table()
	}
	inst.leased, err = leased.NewSequence(alloc, stream.Value(), blockSize)
	if err != nil {
		return eh.Errorf("row id stream: %w", err)
	}
	inst.local, err = leased.NewSequence(memalloc.NewAllocator(), vocab.IdStreamFactsRowLocal.Value(), rowalloc.DefaultBlockSize)
	if err != nil {
		return eh.Errorf("process-local row id stream: %w", err)
	}
	return
}

// idColumn is the physical name of the facts envelope's row id.
const idColumn = "id:id:u64:47::0:"

// seedLease gives the stream tagValue a lease row in layout when it has
// none, continuing after the largest id of the stream that relation holds
// (ADR-0397 §SD2, rowalloc.Seed). The ids of one stream are one range of
// the id column — the tag sits above the body — so the largest is one
// aggregate over that range; it reads the whole id column, which is why it
// runs at setup, once per lost lease row, and never on a write.
func seedLease(ctx context.Context, exec recordstore.ExecutorI, layout rowalloc.Layout, relation string, tagValue identifier.TagValue) (seededAt uint64, err error) {
	tag := tagValue.GetTag()
	lo, hi := tag.ComposeId(1), tag.ComposeId(tag.GetMaxPossibleIdIncl())
	sql := "SELECT max(`" + idColumn + "`) AS m FROM " + relation +
		" WHERE `" + idColumn + "` >= " + strconv.FormatUint(lo.Value(), 10) + " AND `" + idColumn + "` <= " + strconv.FormatUint(hi.Value(), 10)
	return rowalloc.Seed(ctx, exec, layout, tagValue, func(ctx context.Context) (maxBody uint64, err error) {
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			if qerr != nil {
				return 0, qerr
			}
			if col, ok := rec.Column(0).(*array.Uint64); ok && rec.NumRows() > 0 {
				if largest := identifier.TaggedId(col.Value(0)); largest != 0 {
					maxBody = largest.RemoveTag().Value()
				}
			} else if !ok {
				err = eb.Build().Str("relation", relation).Errorf("the largest row id did not come back as a UInt64")
			}
			rec.Release()
		}
		return
	})
}

// leaseLayout is the lease table this store leases through when its
// configuration names no allocator: the one in the facts table's database.
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
		log.Warn().Err(lerr).Str("source", inst.ids.source).Dur("retryIn", leaseRetryInterval).
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
