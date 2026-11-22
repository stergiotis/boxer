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
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowintern"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
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

// RowIdGenerator is the facts table's id generator (ADR-0397 §SD2, ADR-0405
// §SD3): the one source every writer of the table mints its row ids from —
// the hand-written store, and every generated store over the table, which is
// handed it as its configuration's Keys.
//
// It mints from two streams. leased draws blocks of the configured stream
// from the configured allocator — by default vocab.IdStreamFactsRow through
// the lease table beside the facts table (ADR-0319 §SD3) — so an id is
// unique across every process writing to the server and across runs.
// Building it reserves nothing: the first id reserves the first block.
//
// local counts under vocab.IdStreamFactsRowLocal in process. An id is taken
// from it while no block can be had — the lease table is missing, the
// server or the authority refused — so the row still lands; its id is
// unique only within the process, and its tag says so. A failed lease is
// logged and not tried again for a minute.
//
// It is safe for concurrent use. Take a store's with [Store.RowIds], or
// build one for a process that has no such store with [NewRowIdGenerator].
type RowIdGenerator struct {
	leased *leased.Sequence
	local  *leased.Sequence
	// source names where the leased blocks come from, for the log.
	source string
	// retryAt is the time, in Unix nanoseconds, before which no lease is
	// attempted after one failed.
	retryAt atomic.Int64
	// exec and database reach the id directory beside the lease table, for
	// the keyed generators ([RowIdGenerator.Keyed]).
	exec     recordstore.ExecutorI
	database string
}

var _ identifier.IdGeneratorI = (*RowIdGenerator)(nil)

// Keyed is the facts table's keyed id generator for scope (ADR-0405 §SD2,
// §SD3): the same natural key resolves to the same id of the facts row
// stream, from every process and across restarts, through the id directory
// beside the lease table. It is what a writer whose rows belong to an
// entity that recurs — a metric series, a worker's run — mints from; a
// writer whose every row is its own thing mints from the generator itself.
//
// scope keeps the natural keys of two writers apart. New keys take ids
// from the leased stream only, never the process-local one: a key first
// seen while no block can be leased fails to resolve, as the insert of its
// row would on the same server.
func (inst *RowIdGenerator) Keyed(scope string) (keyed *rowintern.Internalizer, err error) {
	if inst.exec == nil {
		return nil, eh.Errorf("chstore: keyed id generator: the row id generator reaches no server")
	}
	keyed, err = rowintern.New(inst.exec, rowintern.Layout{Database: inst.database}, scope, inst.leased)
	if err != nil {
		err = eh.Errorf("chstore: keyed id generator: %w", err)
	}
	return
}

// KeyedIdsOf is the facts table's keyed generator for scope beside a facts
// store, as [RowIdsOf] is the sequential one: nil when store is not the
// ClickHouse-backed one. A scope that is not a name is the caller's
// mistake and panics.
func KeyedIdsOf(store factsstore.FactsStoreI, scope string) identifier.IdGeneratorI {
	st, ok := store.(*Store)
	if !ok || st == nil {
		return nil
	}
	keyed, err := st.RowIds().Keyed(scope)
	if err != nil {
		panic("chstore.KeyedIdsOf: " + err.Error())
	}
	return keyed
}

// NewKeyedIdGenerator is the facts table's keyed generator for scope over
// exec, for a process that holds no [Store]: [NewRowIdGenerator] and
// [RowIdGenerator.Keyed] in one.
func NewKeyedIdGenerator(exec recordstore.ExecutorI, cfg Config, scope string) (keyed *rowintern.Internalizer, err error) {
	rows, err := NewRowIdGenerator(exec, cfg)
	if err != nil {
		return
	}
	return rows.Keyed(scope)
}

// NewKeyedIdLookup is the reader's half of the facts table's keyed
// generator for scope over exec: it looks natural keys up in the id
// directory and gives none an id, holds no lease and writes nothing.
func NewKeyedIdLookup(exec recordstore.ExecutorI, cfg Config, scope string) (lookup *rowintern.Internalizer, err error) {
	if cfg.Database == "" {
		return nil, eh.Errorf("chstore: keyed id lookup: cfg requires Database")
	}
	lookup, err = rowintern.NewReadOnly(exec, rowintern.Layout{Database: cfg.Database}, scope, cfg.RowIds.stream().Value())
	if err != nil {
		err = eh.Errorf("chstore: keyed id lookup: %w", err)
	}
	return
}

// NewRowIdGenerator is the facts table's id generator for cfg, over exec:
// for a process that writes facts through a generated store and holds no
// [Store]. cfg names the facts database, whose lease table the blocks come
// from, and RowIds as [New] takes it.
func NewRowIdGenerator(exec recordstore.ExecutorI, cfg Config) (inst *RowIdGenerator, err error) {
	if exec == nil {
		return nil, eh.Errorf("chstore: row id generator: nil executor")
	}
	if cfg.Database == "" {
		return nil, eh.Errorf("chstore: row id generator: cfg requires Database")
	}
	inst = &RowIdGenerator{}
	if err = inst.init(exec, rowalloc.Layout{Database: cfg.Database}, cfg.RowIds); err != nil {
		return nil, eh.Errorf("chstore: %w", err)
	}
	return
}

// RowIds is the generator this store mints its row ids from, for the
// generated stores over the same table to mint from too.
func (inst *Store) RowIds() *RowIdGenerator { return &inst.ids }

// RowIdsOf is the facts table's id generator of a facts store, for the code
// that builds a generated store over the same table beside it: store's own
// when it is the ClickHouse-backed one, and nil when it is not — the
// in-memory store reaches no table for another store to write. A generated
// store handed nil refuses to mint, which is the right answer there.
func RowIdsOf(store factsstore.FactsStoreI) identifier.IdGeneratorI {
	if st, ok := store.(*Store); ok && st != nil {
		return st.RowIds()
	}
	return nil
}

// GetId mints the next row id; naturalKey is ignored, as by every
// sequential generator, and fresh is always true.
func (inst *RowIdGenerator) GetId(ctx context.Context, _ []byte) (id identifier.TaggedId, fresh bool, err error) {
	v, err := inst.mint(ctx)
	return identifier.TaggedId(v), err == nil, err
}

// GetUntaggedId is GetId without its tag. The tag is what tells a leased id
// from a process-local one, so prefer GetId.
func (inst *RowIdGenerator) GetUntaggedId(ctx context.Context, naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	id, fresh, err := inst.GetId(ctx, naturalKey)
	return id.RemoveTag(), fresh, err
}

// Release does nothing: a block in hand is used up or burnt with the
// process, as ADR-0319 §SD4 accepts.
func (inst *RowIdGenerator) Release() (err error) { return nil }

// GetTag is the tag of the leased stream. An id minted while no block could
// be leased carries the process-local stream's tag instead.
func (inst *RowIdGenerator) GetTag() identifier.IdTag { return inst.leased.GetTag() }

// mint draws the next row id: leased when a block is in hand or can be
// leased, process-local otherwise. A failed lease is logged and not
// retried for leaseRetryInterval; the id it was wanted for is a local one.
// The lease's round trip runs under ctx, bounded by WriteTimeout when ctx
// carries no deadline of its own.
func (inst *RowIdGenerator) mint(ctx context.Context) (id uint64, err error) {
	now := time.Now().UnixNano()
	if now >= inst.retryAt.Load() {
		lctx, cancel := boundWrite(ctx)
		t, _, lerr := inst.leased.GetId(lctx, nil)
		cancel()
		if lerr == nil {
			id = t.Value()
			return
		}
		inst.retryAt.Store(now + int64(leaseRetryInterval))
		log.Warn().Err(lerr).Str("source", inst.source).Dur("retryIn", leaseRetryInterval).
			Msg("chstore: no block of row ids could be leased; rows get process-local ids until a lease succeeds")
	}
	t, _, err := inst.local.GetId(ctx, nil)
	if err != nil {
		err = eh.Errorf("chstore: mint a process-local row id: %w", err)
		return
	}
	id = t.Value()
	return
}

// stream is the claimed tag the leased ids carry.
func (inst RowIds) stream() (stream tagmint.ClaimedTagValue) {
	stream = inst.Stream
	if !stream.IsValid() {
		stream = vocab.IdStreamFactsRow
	}
	return
}

func (inst *RowIdGenerator) init(exec recordstore.ExecutorI, layout rowalloc.Layout, cfg RowIds) (err error) {
	inst.exec, inst.database = exec, layout.DatabaseName()
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
//
// Every row counts, whatever its kind: every writer of the table mints from
// this stream (ADR-0405 §SD1, §SD6). A row from before that decision whose
// id was a hash can fall into the range and be counted; the stream then
// continues above it, which wastes ids and repeats none.
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

// mintId draws the next row id from the store's generator.
func (inst *Store) mintId(ctx context.Context) (id uint64, err error) {
	return inst.ids.mint(ctx)
}
