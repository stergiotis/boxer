package seq

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

func bodyOf(id identifier.TaggedId) uint64 {
	_, u := id.Split()
	return uint64(u)
}

// TestBadgerIdSequence_MonotonicAndIgnoresKey checks the sequential generator
// hands out a strictly increasing, correctly-tagged stream and disregards the
// natural key (fresh is always true).
func TestBadgerIdSequence_MonotonicAndIgnoresKey(t *testing.T) {
	genFac, err := NewBadgerIdSequenceGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	const tagVal = identifier.TagValue(3)
	gen, err := genFac.Create(tagVal, 8)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	var prev uint64
	for i := range 10 {
		id, fresh, err := gen.GetId(context.Background(), []byte("ignored")) // natural key must not matter
		require.NoError(t, err)
		require.True(t, fresh)
		require.True(t, id.IsValid())
		require.EqualValues(t, tagVal, id.GetTag().GetValue())
		b := bodyOf(id)
		require.NotZero(t, b, "body 0 is reserved as invalid/NULL")
		if i > 0 {
			require.Greater(t, b, prev, "sequence must be strictly increasing")
		}
		prev = b
	}
}

// TestBadgerIdSequence_PerTagIsolation checks two tags on one store keep
// independent counters and each id carries its own tag.
func TestBadgerIdSequence_PerTagIsolation(t *testing.T) {
	genFac, err := NewBadgerIdSequenceGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	genA, err := genFac.Create(identifier.TagValue(1), 8)
	require.NoError(t, err)
	defer func() { _ = genA.Release() }()
	genB, err := genFac.Create(identifier.TagValue(2), 8)
	require.NoError(t, err)
	defer func() { _ = genB.Release() }()

	a1, _, err := genA.GetId(context.Background(), nil)
	require.NoError(t, err)
	_, _, err = genB.GetId(context.Background(), nil) // B's draw must not advance A
	require.NoError(t, err)
	a2, _, err := genA.GetId(context.Background(), nil)
	require.NoError(t, err)

	require.EqualValues(t, 1, a1.GetTag().GetValue())
	require.Equal(t, bodyOf(a1)+1, bodyOf(a2), "tag A's counter is independent of tag B")
}

// TestBadgerIdSequence_PersistsAcrossReopen checks the counter does not regress
// or repeat after the store is closed and reopened.
func TestBadgerIdSequence_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	const tagVal = identifier.TagValue(4)

	genFac, err := NewBadgerIdSequenceGenerator(dir)
	require.NoError(t, err)
	gen, err := genFac.Create(tagVal, 4)
	require.NoError(t, err)
	var last uint64
	for range 3 {
		id, _, err := gen.GetId(context.Background(), nil)
		require.NoError(t, err)
		last = bodyOf(id)
	}
	require.NoError(t, gen.Release())
	require.NoError(t, genFac.Close())

	genFac2, err := NewBadgerIdSequenceGenerator(dir)
	require.NoError(t, err)
	defer func() { _ = genFac2.Close() }()
	gen2, err := genFac2.Create(tagVal, 4)
	require.NoError(t, err)
	defer func() { _ = gen2.Release() }()
	id, _, err := gen2.GetId(context.Background(), nil)
	require.NoError(t, err)
	require.Greater(t, bodyOf(id), last, "sequence must not regress or repeat across reopen")
}

// TestBadgerIdSequenceGenerator_RejectsInvalidTag checks Create rejects the
// zero (invalid) tag value (ADR-0106: every non-zero uint32 is encodable).
func TestBadgerIdSequenceGenerator_RejectsInvalidTag(t *testing.T) {
	genFac, err := NewBadgerIdSequenceGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	_, err = genFac.Create(identifier.TagValue(0), 8)
	require.Error(t, err)
}
