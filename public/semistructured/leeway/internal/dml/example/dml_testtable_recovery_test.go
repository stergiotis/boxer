package example

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// collectIds reads the plain entity-id column (field 0, Uint64) of every row
// across the returned record batches, accounting for sliced records.
func collectIds(t *testing.T, recs []arrow.RecordBatch) []uint64 {
	t.Helper()
	var ids []uint64
	for _, r := range recs {
		col, ok := r.Column(0).(*array.Uint64)
		require.Truef(t, ok, "column 0 should be Uint64, got %T", r.Column(0))
		for i := 0; i < int(r.NumRows()); i++ {
			ids = append(ids, col.Value(i))
		}
	}
	return ids
}

// Regression for review F-1: TransferRecords must preserve records that were
// flushed into inst.records by an intervening RollbackEntity. The previous
// implementation used slices.Grow+copy, which (with the universal
// TransferRecords(nil) call) copied zero buffered records, silently dropping —
// and leaking — every entity committed before the rollback.
//
// Sequence: commit e1; begin+rollback e2 (this flushes e1 into inst.records);
// commit e3. TransferRecords must then return both e1 and e3.
func TestTransferRecordsPreservesPreRollbackEntities(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()

	e := NewInEntityTesttable(pool, 4)

	// e1 — fully committed.
	e.BeginEntity().SetId(1).SetTimestamp(ts)
	e.GetSectionSpecial().BeginAttribute("e1").AddToCoContainers(1, 2).EndAttribute()
	require.NoError(t, e.CommitEntity())

	// e2 — begun then rolled back. RollbackEntity flushes the builder (which
	// still holds e1's committed row) into inst.records as a side effect.
	e.BeginEntity().SetId(2).SetTimestamp(ts)
	require.NoError(t, e.RollbackEntity())

	// e3 — fully committed into the (now empty) live builder.
	e.BeginEntity().SetId(3).SetTimestamp(ts)
	e.GetSectionSpecial().BeginAttribute("e3").AddToCoContainers(3, 4).EndAttribute()
	require.NoError(t, e.CommitEntity())

	recs, err := e.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)

	ids := collectIds(t, recs)
	require.ElementsMatch(t, []uint64{1, 3}, ids,
		"both the pre-rollback (e1) and post-rollback (e3) entities must survive TransferRecords")
}

// Regression for review F-3: RollbackEntity must clear entity-level errors so
// it can serve as a recovery mechanism. Previously a failed CommitEntity left
// an error permanently in inst.errs (the generated clearErrors had zero
// callers on the entity), so every subsequent commit failed forever — rollback
// reset the state machine but could not actually recover the instance.
func TestRollbackClearsEntityErrorsAndRecovers(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()

	e := NewInEntityTesttable(pool, 4)

	// Poison the entity-level error buffer without touching arrow builders
	// mid-attribute: a second BeginEntity is an illegal state transition and
	// appends to inst.errs.
	e.BeginEntity().SetId(10).SetTimestamp(ts)
	e.BeginEntity() // illegal: already InEntity -> entity-level error appended
	e.GetSectionSpecial().BeginAttribute("bad").AddToCoContainers(1, 2).EndAttribute()
	require.Error(t, e.CommitEntity(), "commit must fail while the entity carries the poison error")

	// Recover.
	require.NoError(t, e.RollbackEntity())

	// A clean entity must now commit — this is the assertion that fails before
	// the fix (the stale error survives the rollback).
	e.BeginEntity().SetId(11).SetTimestamp(ts)
	e.GetSectionSpecial().BeginAttribute("good").AddToCoContainers(3, 4).EndAttribute()
	require.NoError(t, e.CommitEntity(), "after rollback the instance must accept new entities")

	recs, err := e.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)

	ids := collectIds(t, recs)
	require.ElementsMatch(t, []uint64{11}, ids,
		"only the recovered entity should be present; the poisoned one was rolled back")
}
