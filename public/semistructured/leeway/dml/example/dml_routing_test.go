package example

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

type routingRow struct {
	id, ts, partitioningKey, routingKey uint64
	naturalKey, classification, kind    string
}

func writeRoutingRows(t *testing.T, rows []routingRow, perColumn bool) []arrow.RecordBatch {
	t.Helper()
	e := NewInEntityRouting(memory.NewGoAllocator(), len(rows))
	for _, r := range rows {
		e.BeginEntity().SetTimestamp(r.ts)
		if perColumn {
			// Deliberately not in declaration order.
			e.SetKind(r.kind).
				SetNaturalKey(r.naturalKey).
				SetRoutingKey(r.routingKey).
				SetClassification(r.classification).
				SetIdColumn(r.id).
				SetPartitioningKey(r.partitioningKey)
		} else {
			e.SetId(r.id, r.naturalKey).
				SetRouting(r.partitioningKey, r.routingKey, r.classification, r.kind)
		}
		e.GetSectionSymbol().BeginAttribute("v").AddMembershipLowCardRef(1).EndAttribute()
		require.NoError(t, e.CommitEntity())
	}
	recs, err := e.TransferRecords(nil)
	require.NoError(t, err)
	return recs
}

// TestPlainColumnSettersMatchGroupedSetter checks that the per-column setters,
// called in any order, write the same rows as the grouped positional setters.
func TestPlainColumnSettersMatchGroupedSetter(t *testing.T) {
	rows := []routingRow{
		{id: 1, ts: 10, partitioningKey: 100, routingKey: 200, naturalKey: "a", classification: "public", kind: "event"},
		{id: 2, ts: 20, partitioningKey: 101, routingKey: 201, naturalKey: "b", classification: "internal", kind: "fact"},
	}
	recsGrouped := writeRoutingRows(t, rows, false)
	defer releaseAll(recsGrouped)
	recsColumn := writeRoutingRows(t, rows, true)
	defer releaseAll(recsColumn)
	assertEquivalent(t, recsGrouped, recsColumn)
}

// TestPlainColumnSettersPartialAndOverride checks that an unset column keeps
// its zero value, a later setter call overrides an earlier one, and the
// per-column and grouped setters mix freely within one entity.
func TestPlainColumnSettersPartialAndOverride(t *testing.T) {
	pool := memory.NewGoAllocator()

	eA := NewInEntityRouting(pool, 1)
	eA.BeginEntity().SetId(7, "").SetTimestamp(3).SetRouting(0, 0, "c", "k")
	eA.GetSectionSymbol().BeginAttribute("v").EndAttribute()
	require.NoError(t, eA.CommitEntity())
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityRouting(pool, 1)
	eB.BeginEntity().SetTimestamp(3).SetRouting(9, 9, "x", "k").
		SetPartitioningKey(0).SetRoutingKey(0).SetClassification("c").SetIdColumn(7)
	eB.GetSectionSymbol().BeginAttribute("v").EndAttribute()
	require.NoError(t, eB.CommitEntity())
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}

// TestPlainColumnSetterOutsideEntity checks that a per-column setter called
// outside an entity frame is an invalid state transition, as the grouped
// setter's is.
func TestPlainColumnSetterOutsideEntity(t *testing.T) {
	e := NewInEntityRouting(memory.NewGoAllocator(), 1)
	e.SetKind("k")
	require.Error(t, e.CheckErrors())
}
