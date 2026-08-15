package capmapvocab_test

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
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

// The claimed value is the allocation, so it is pinned rather than left to
// drift: the package comment, ADR-0168 §SD6 and this claim have to agree, and
// a change here is a change to what rows already in the table mean.
func TestTagValueClaimIsTheAllocatedOne(t *testing.T) {
	assert.EqualValues(t, 2178311, capmapvocab.TagValueClaim.Value().Value())
	assert.Equal(t, "capmap", capmapvocab.TagValueClaim.Name())
	assert.Equal(t, tagmint.VocabularyTagWidth, capmapvocab.TagValueClaim.Tag().GetTagWidth(),
		"every vcs-managed vocabulary claims from one width class")
}
