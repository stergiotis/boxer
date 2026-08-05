package capmapvocab_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

func TestAllMembsHaveNonZeroIds(t *testing.T) {
	require.NotEmpty(t, capmapvocab.AllMembs)
	for _, m := range capmapvocab.AllMembs {
		assert.NotZero(t, m.GetId().Value(), "membership %q must have a non-zero id", m.GetNaturalKey())
	}
}

func TestAllMembsHaveUniqueIds(t *testing.T) {
	seen := make(map[uint64]string, len(capmapvocab.AllMembs))
	for _, m := range capmapvocab.AllMembs {
		id := m.GetId().Value()
		name := string(m.GetNaturalKey())
		if prev, dup := seen[id]; dup {
			t.Fatalf("membership id %d is shared by %q and %q", id, prev, name)
		}
		seen[id] = name
	}
}

// AllMembs must actually enumerate the registry. A membership registered but
// left out of the slice is invisible to every invariant below, so the guard
// would silently stop covering it.
func TestAllMembsCoversTheRegistry(t *testing.T) {
	registered := 0
	for range capmapvocab.NkRegistry.IterateAll() {
		registered++
	}
	assert.Equal(t, registered, len(capmapvocab.AllMembs),
		"AllMembs must list every membership registered in NkRegistry")
}

// The load-bearing invariant of ADR-0168 §SD6.
//
// capmap facts and runtime facts share one table, so a membership id minted by
// two vocabularies would not fail to compile or to write — it would make two
// unrelated facts wear the same tag, and every query over either would be
// quietly wrong. Nothing but this test stands between the tag-value allocation
// and that outcome.
func TestTagValuesAreDisjointFromOtherVocabularies(t *testing.T) {
	ours := make(map[uint64]string, len(capmapvocab.AllMembs))
	for _, m := range capmapvocab.AllMembs {
		ours[m.GetId().Value()] = string(m.GetNaturalKey())
	}

	for _, other := range []struct {
		name  string
		membs []registry.RegisteredNaturalKey
	}{
		{"keelson/runtime/vocab", vocab.AllMembs},
		{"keelson/vdd", []registry.RegisteredNaturalKey{vdd.MembParent, vdd.MembChild, vdd.MembNaturalKey}},
	} {
		require.NotEmpty(t, other.membs, "%s exposed no memberships to compare against, so this test proves nothing", other.name)
		for _, m := range other.membs {
			if name, clash := ours[m.GetId().Value()]; clash {
				t.Fatalf("membership id %d is minted by both capmapvocab %q and %s %q",
					m.GetId().Value(), name, other.name, m.GetNaturalKey())
			}
		}
	}
}

// The base is the allocation, so it is pinned rather than left to drift: the
// package comment, ADR-0168 §SD6 and this constant have to agree, and a change
// here is a change to what rows already in the table mean.
func TestTagValueBaseIsTheAllocatedOne(t *testing.T) {
	assert.Equal(t, uint32(16), uint32(capmapvocab.TagValueBase))
	assert.Equal(t, uint32(16), capmapvocab.MembersTagValue.GetTagValue().Value(),
		"offset 0 of the base is what the memberships hang from")
}

// Ordering is append-only because ids follow declaration order and written
// rows carry them. Pinning the first and last entries makes an insertion in
// the middle — the change that would silently renumber everything after it —
// fail here rather than in a corpus somebody already ingested.
func TestMembershipOrderIsAppendOnly(t *testing.T) {
	require.GreaterOrEqual(t, len(capmapvocab.AllMembs), 2)
	assert.Equal(t, "capmap-kind-capability", string(capmapvocab.AllMembs[0].GetNaturalKey()))
	assert.Equal(t, "capmap-relation-ncd", string(capmapvocab.AllMembs[len(capmapvocab.AllMembs)-1].GetNaturalKey()))
}
