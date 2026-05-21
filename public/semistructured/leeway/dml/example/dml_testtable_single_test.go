//go:build llm_generated_opus47

package example

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func releaseAll(recs []arrow.RecordBatch) {
	for _, r := range recs {
		r.Release()
	}
}

func assertEquivalent(t *testing.T, recsA, recsB []arrow.RecordBatch) {
	t.Helper()
	require.Equal(t, len(recsA), len(recsB), "record-batch count differs")
	for i := range recsA {
		require.Truef(t, array.RecordEqual(recsA[i], recsB[i]),
			"record %d differs:\nmanual=%s\nsingle=%s", i, recsA[i], recsB[i])
	}
}

// BeginAttributeSingle(spc, a1, a2).EndAttribute() must produce the same
// Arrow record as BeginAttribute(spc).AddToCoContainers(a1, a2).EndAttribute()
// — that is its sole purpose. Once attribute, plain entity.
func TestBeginAttributeSingleSingleAttribute(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()

	eA := NewInEntityTesttable(pool, 1)
	eA.BeginEntity().SetId(42).SetTimestamp(ts)
	eA.GetSectionSpecial().BeginAttribute("hello").AddToCoContainers(7, 9).EndAttribute()
	require.NoError(t, eA.CommitEntity())
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityTesttable(pool, 1)
	eB.BeginEntity().SetId(42).SetTimestamp(ts)
	eB.GetSectionSpecial().BeginAttributeSingle("hello", 7, 9).EndAttribute()
	require.NoError(t, eB.CommitEntity())
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}

// Multi-attribute per entity, multi-entity. Stresses container-length flushing
// across repeated BeginAttributeSingle calls.
func TestBeginAttributeSingleMultipleAttributes(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()

	type attr struct {
		spc  string
		ary1 uint32
		ary2 uint32
	}
	entities := [][]attr{
		{{"a", 1, 10}, {"b", 2, 20}, {"c", 3, 30}},
		{{"x", 100, 200}},
		{},
		{{"y", 7, 11}, {"z", 8, 12}},
	}

	eA := NewInEntityTesttable(pool, len(entities))
	for i, ents := range entities {
		eA.BeginEntity().SetId(uint64(i + 1)).SetTimestamp(ts)
		sec := eA.GetSectionSpecial()
		for _, a := range ents {
			sec.BeginAttribute(a.spc).AddToCoContainers(a.ary1, a.ary2).EndAttribute()
		}
		require.NoError(t, eA.CommitEntity())
	}
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityTesttable(pool, len(entities))
	for i, ents := range entities {
		eB.BeginEntity().SetId(uint64(i + 1)).SetTimestamp(ts)
		sec := eB.GetSectionSpecial()
		for _, a := range ents {
			sec.BeginAttributeSingle(a.spc, a.ary1, a.ary2).EndAttribute()
		}
		require.NoError(t, eB.CommitEntity())
	}
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}

// "multi" section mixes a scalar (name), a HomogenousArray column (vals),
// and a Set column (tags). BeginAttributeSingle must compose across both
// non-scalar subtypes in one call, producing the same Arrow record as the
// explicit BeginAttribute + AddToCoContainers chain.
func TestBeginAttributeSingleMixedHomogenousArrayAndSet(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()

	eA := NewInEntityTesttable(pool, 1)
	eA.BeginEntity().SetId(1).SetTimestamp(ts)
	eA.GetSectionMulti().BeginAttribute("a").AddToCoContainers(7, 100).EndAttribute()
	eA.GetSectionMulti().BeginAttribute("b").AddToCoContainers(8, 200).EndAttribute()
	require.NoError(t, eA.CommitEntity())
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityTesttable(pool, 1)
	eB.BeginEntity().SetId(1).SetTimestamp(ts)
	eB.GetSectionMulti().BeginAttributeSingle("a", 7, 100).EndAttribute()
	eB.GetSectionMulti().BeginAttributeSingle("b", 8, 200).EndAttribute()
	require.NoError(t, eB.CommitEntity())
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}

// BeginAttributeSingle must return *InAttr so callers can still chain
// memberships. Verify the chain produces the same record as going through
// BeginAttribute + AddToCoContainers + AddMembership... .
func TestBeginAttributeSingleChainsMembership(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()
	const lcRef uint64 = 13
	hcPtr := []byte("entity.field.0")

	eA := NewInEntityTesttable(pool, 1)
	eA.BeginEntity().SetId(1).SetTimestamp(ts)
	eA.GetSectionSpecial().
		BeginAttribute("hello").
		AddToCoContainers(7, 9).
		AddMembershipMixedLowCardRef(lcRef, hcPtr).
		EndAttribute()
	require.NoError(t, eA.CommitEntity())
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityTesttable(pool, 1)
	eB.BeginEntity().SetId(1).SetTimestamp(ts)
	eB.GetSectionSpecial().
		BeginAttributeSingle("hello", 7, 9).
		AddMembershipMixedLowCardRef(lcRef, hcPtr).
		EndAttribute()
	require.NoError(t, eB.CommitEntity())
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}
