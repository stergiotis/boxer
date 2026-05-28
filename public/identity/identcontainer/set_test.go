package identcontainer

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ids(vs ...uint64) *TaggedIdSet {
	s := NewTaggedIdSet()
	for _, v := range vs {
		s.AddMember(identifier.TaggedId(v))
	}
	return s
}

func collect(s *TaggedIdSet) []uint64 {
	var out []uint64
	for id := range s.Iterate() {
		out = append(out, uint64(id))
	}
	return out
}

func TestTaggedIdSetMembership(t *testing.T) {
	s := NewTaggedIdSet()
	assert.True(t, s.IsEmpty())
	assert.Equal(t, uint64(0), s.Cardinality())

	s.AddMember(identifier.TaggedId(7))
	s.AddMember(identifier.TaggedId(7)) // idempotent
	s.AddMember(identifier.TaggedId(42))
	assert.False(t, s.IsEmpty())
	assert.Equal(t, uint64(2), s.Cardinality())
	assert.Equal(t, s.Cardinality(), s.Length())

	s.RemoveMember(identifier.TaggedId(7))
	assert.Equal(t, uint64(1), s.Cardinality())
	assert.Equal(t, []uint64{42}, collect(s))

	s.Clear()
	assert.True(t, s.IsEmpty())
}

func TestTaggedIdSetAlgebra(t *testing.T) {
	// A = {1,2,3,4,5}, B = {4,5,6}; chosen so |A∩B| != |A\B| so the test would
	// catch the historical DifferenceCardinality bug (it returned the
	// intersection count instead of the difference count).
	mk := func() (a *TaggedIdSet, b *TaggedIdSet) { return ids(1, 2, 3, 4, 5), ids(4, 5, 6) }

	a, b := mk()
	assert.Equal(t, uint64(2), a.IntersectionCardinality(b)) // {4,5}
	assert.Equal(t, uint64(3), a.DifferenceCardinality(b))   // {1,2,3}
	assert.Equal(t, uint64(6), a.UnionCard(b))               // {1..6}
	assert.Equal(t, uint64(1), b.DifferenceCardinality(a))   // {6}
	// cardinality queries must not mutate their receiver
	assert.Equal(t, uint64(5), a.Cardinality())
	assert.Equal(t, uint64(3), b.Cardinality())

	a, b = mk()
	a.Intersect(b)
	assert.Equal(t, []uint64{4, 5}, collect(a))

	a, b = mk()
	a.Diff(b)
	assert.Equal(t, []uint64{1, 2, 3}, collect(a))

	a, b = mk()
	a.Union(b)
	assert.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, collect(a))
}

func TestTaggedIdSetMinMaxRank(t *testing.T) {
	s := ids(10, 20, 30, 40)
	assert.Equal(t, identifier.TaggedId(10), s.Min())
	assert.Equal(t, identifier.TaggedId(40), s.Max())
	assert.Equal(t, uint64(3), s.Rank(identifier.TaggedId(30))) // count of members <= 30
}

func TestTaggedIdSetCloneIsolation(t *testing.T) {
	a := ids(1, 2, 3)
	c := a.Clone()
	c.AddMember(identifier.TaggedId(99))
	assert.Equal(t, uint64(3), a.Cardinality()) // original untouched
	assert.Equal(t, uint64(4), c.Cardinality())
}

func TestTaggedIdSetSerializeRoundTrip(t *testing.T) {
	a := ids(1, 5, 9, 1000, 1<<40)
	blob, err := a.Serialize()
	require.NoError(t, err)
	require.NotEmpty(t, blob)

	b := NewTaggedIdSet()
	_, err = b.ReadFrom(bytes.NewReader(blob))
	require.NoError(t, err)
	assert.Equal(t, collect(a), collect(b))
	assert.Equal(t, uint64(0), a.DifferenceCardinality(b))
}
