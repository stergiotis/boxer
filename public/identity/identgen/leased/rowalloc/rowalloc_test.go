package rowalloc

import (
	"context"
	"errors"
	"iter"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// The compare-and-set is pinned to the lease row's position and guarded on
// the value read, and carries rowcas's sequential mode.
func TestUpdateIsPinnedAndGuarded(t *testing.T) {
	sql := updateSQL(Layout{}, 7, lease{block: 3, offset: 1, next: 42}, 1066, "tok")
	assert.Equal(t, "UPDATE boxer.idlease SET next = 1066, holder = 'tok' WHERE tag = 7 AND _block_number = 3 AND _block_offset = 1 AND next = 42 SETTINGS update_parallel_mode='sync'", sql)
}

func TestProvisionCarriesTheLightweightUpdateSettings(t *testing.T) {
	stmts := ProvisionSQL(Layout{Database: "x"})
	require.Len(t, stmts, 3)
	assert.Contains(t, stmts[1], "CREATE TABLE IF NOT EXISTS x.idlease")
	assert.Contains(t, stmts[1], "enable_block_number_column=1, enable_block_offset_column=1")
	assert.True(t, strings.HasPrefix(stmts[2], "ALTER TABLE x.idlease MODIFY SETTING"))
	assert.Contains(t, insertSQL(Layout{}, 7), "async_insert = 0", "async_insert never sits on the claim path")
}

func TestRefusals(t *testing.T) {
	_, err := NewAllocator(nil, Layout{})
	assert.Error(t, err)
	_, err = NewAllocator(&faultExec{}, Layout{Database: "a; DROP"})
	assert.Error(t, err, "the database name lands in SQL text")
	a, err := NewAllocator(&faultExec{}, Layout{})
	require.NoError(t, err)
	ctx := context.Background()
	_, _, err = a.AllocateBlock(ctx, 0, 1, 1)
	assert.Error(t, err, "tag value zero is reserved")
	_, _, err = a.AllocateBlock(ctx, 7, 0, 1)
	assert.Error(t, err)
	_, _, err = a.AllocateBlock(ctx, 7, 2, 1)
	assert.Error(t, err)
}

func localExec(t *testing.T) (exec *chexec.LocalExecutor) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	return
}

type block struct{ lo, hi uint64 }

func requireDisjointAscending(t *testing.T, blocks []block) {
	t.Helper()
	for i, b := range blocks {
		require.Less(t, b.lo, b.hi, "block %d is empty", i)
		require.GreaterOrEqual(t, b.lo, uint64(1), "body 0 is invalid in every stream")
		if i > 0 {
			require.GreaterOrEqual(t, b.lo, blocks[i-1].hi, "block %d overlaps or precedes block %d", i, i-1)
		}
	}
}

// Two allocators over one table, interleaved, on two tags: blocks start at
// body 1, never overlap, increase per tag, and the tags do not share a
// counter. clickhouse local runs one process per statement and locks its
// path, so this is the order the default lane can exercise; the race is
// the integration lane's, against a server.
func TestBlocksAreDisjointAcrossAllocatorsUnderClickhouseLocal(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "leasetest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	require.NoError(t, ProvisionIn(ctx, exec, layout), "idempotent")
	a, err := NewAllocator(exec, layout)
	require.NoError(t, err)
	b, err := NewAllocator(exec, layout)
	require.NoError(t, err)

	var byTag = map[identifier.TagValue][]block{}
	for i, step := range []struct {
		alloc *Allocator
		tag   identifier.TagValue
		size  uint64
	}{{a, 7, 10}, {b, 7, 5}, {a, 8, 3}, {a, 7, 1}, {b, 8, 100}, {b, 7, 10}} {
		lo, hi, aerr := step.alloc.AllocateBlock(ctx, step.tag, 1, step.size)
		require.NoError(t, aerr, "step %d", i)
		assert.Equal(t, step.size, uint64(hi-lo), "step %d: an uncontended lease gets maxSize", i)
		byTag[step.tag] = append(byTag[step.tag], block{uint64(lo), uint64(hi)})
	}
	assert.Equal(t, block{1, 11}, byTag[7][0])
	assert.Equal(t, block{1, 4}, byTag[8][0], "a tag's stream is its own")
	for _, blocks := range byTag {
		requireDisjointAscending(t, blocks)
	}
}

// A duplicate lease row — the second of two racing first inserts, landing
// after the first already leased — is never the lease: the next block
// continues from the row at the lowest block position.
func TestALateDuplicateRowIsNotTheLease(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "leasetest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	a, err := NewAllocator(exec, layout)
	require.NoError(t, err)
	lo, hi, err := a.AllocateBlock(ctx, 7, 1, 10)
	require.NoError(t, err)
	require.NoError(t, exec.Exec(ctx, insertSQL(layout, 7)))
	lo2, hi2, err := a.AllocateBlock(ctx, 7, 1, 10)
	require.NoError(t, err)
	requireDisjointAscending(t, []block{{uint64(lo), uint64(hi)}, {uint64(lo2), uint64(hi2)}})
	assert.EqualValues(t, 11, lo2)
}

// Lost replies: an update that ran but whose reply was lost, one that never
// ran, and one whose read-back failed. The first is recovered from the
// read-back; the other two return an error, and no later block overlaps
// whatever they may have reserved.
func TestLostRepliesNeverReHandABlock(t *testing.T) {
	ctx := context.Background()
	exec := &faultExec{inner: localExec(t)}
	layout := Layout{Database: "leasetest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	a, err := NewAllocator(exec, layout)
	require.NoError(t, err)

	var blocks []block
	take := func() {
		lo, hi, aerr := a.AllocateBlock(ctx, 7, 1, 10)
		require.NoError(t, aerr)
		blocks = append(blocks, block{uint64(lo), uint64(hi)})
	}
	take()

	exec.updateRunsThenFails = true
	lo, hi, err := a.AllocateBlock(ctx, 7, 1, 10)
	require.NoError(t, err, "the update committed; the read-back says so")
	blocks = append(blocks, block{uint64(lo), uint64(hi)})

	exec.updateFailsWithoutRunning = true
	_, _, err = a.AllocateBlock(ctx, 7, 1, 10)
	assert.ErrorIs(t, err, errInjected)
	take()

	exec.readFailsAfterUpdate = true
	_, _, err = a.AllocateBlock(ctx, 7, 1, 10)
	assert.ErrorIs(t, err, errInjected)
	take()

	requireDisjointAscending(t, blocks)
	assert.EqualValues(t, 41, blocks[len(blocks)-1].lo, "the block behind the failed read-back is burnt")
}

// The leased sequence over the allocator mints tagged ids with a non-zero
// body, from one block until it is spent.
func TestASequenceMintsOverTheLease(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "leasetest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	a, err := NewAllocator(exec, layout)
	require.NoError(t, err)
	const tv identifier.TagValue = 317811
	seq, err := leased.NewSequence(a, tv, 4)
	require.NoError(t, err)
	for want := uint64(1); want <= 6; want++ {
		id, fresh, gerr := seq.GetId(ctx, nil)
		require.NoError(t, gerr)
		assert.True(t, fresh)
		assert.Equal(t, tv, id.GetTag().GetValue())
		assert.Equal(t, want, id.RemoveTag().Value())
	}
}

func TestExhaustion(t *testing.T) {
	ctx := context.Background()
	exec := localExec(t)
	layout := Layout{Database: "leasetest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	a, err := NewAllocator(exec, layout)
	require.NoError(t, err)
	require.NoError(t, exec.Exec(ctx, "INSERT INTO leasetest.idlease (tag, next, holder) VALUES (7, 18446744073709551610, '')"))
	_, _, err = a.AllocateBlock(ctx, 7, 10, 10)
	assert.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)
}

var errInjected = errors.New("injected")

// faultExec wraps an executor and injects one fault per switch, once.
type faultExec struct {
	inner                     recordstore.ExecutorI
	updateRunsThenFails       bool
	updateFailsWithoutRunning bool
	readFailsAfterUpdate      bool
	updated                   bool
}

func (inst *faultExec) Exec(ctx context.Context, sql string) (err error) {
	if strings.HasPrefix(sql, "UPDATE ") {
		switch {
		case inst.updateFailsWithoutRunning:
			inst.updateFailsWithoutRunning = false
			return errInjected
		case inst.updateRunsThenFails:
			inst.updateRunsThenFails = false
			if err = inst.inner.Exec(ctx, sql); err != nil {
				return
			}
			return errInjected
		}
		inst.updated = true
	}
	return inst.inner.Exec(ctx, sql)
}

func (inst *faultExec) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	if inst.readFailsAfterUpdate && inst.updated {
		inst.readFailsAfterUpdate, inst.updated = false, false
		return func(yield func(arrow.RecordBatch, error) bool) { yield(nil, errInjected) }
	}
	inst.updated = false
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *faultExec) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	return inst.inner.InsertArrow(ctx, table, records)
}
