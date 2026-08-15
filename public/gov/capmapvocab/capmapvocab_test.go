package capmapvocab_test

import (
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

// The base is the allocation, so it is pinned rather than left to drift: the
// package comment, ADR-0168 §SD6 and this constant have to agree, and a change
// here is a change to what rows already in the table mean.
func TestTagValueBaseIsTheAllocatedOne(t *testing.T) {
	assert.Equal(t, uint32(16), uint32(capmapvocab.TagValueBase))
	assert.Equal(t, uint32(16), capmapvocab.MembersTagValue.GetTagValue().Value(),
		"offset 0 of the base is what the memberships hang from")
}

// The whole name-to-id table, written down.
//
// A membership id is its registration ordinal, so declaring a new membership
// anywhere but at the end renumbers every one after it. That change compiles,
// vets, writes and reads — it just makes rows already in `boxer.facts` mean
// something else, because the id is all a row carries. Pinning only the ends
// (which this test used to do) catches a prepend and an append but not the
// insertion in the middle, which is the one an author actually makes when they
// put a new field "with the others".
//
// Updating this table is therefore a deliberate act: appending a line is
// ordinary, and changing a line that is already here means every store holding
// the corpus must be re-ingested.
func TestMembershipIdsAreGoldenPinned(t *testing.T) {
	golden := []struct {
		name string
		id   uint64
	}{
		{"capmap-kind-competence", 2738188573441261568},
		{"capmap-kind-relation", 2738188573441261569},
		{"capmap-competence-slug", 2738188573441261570},
		{"capmap-competence-name", 2738188573441261571},
		{"capmap-competence-abbrev", 2738188573441261572},
		{"capmap-competence-synopsis", 2738188573441261573},
		{"capmap-competence-domain", 2738188573441261574},
		{"capmap-competence-catalog", 2738188573441261575},
		{"capmap-competence-owner", 2738188573441261576},
		{"capmap-competence-level", 2738188573441261577},
		{"capmap-competence-vault-path", 2738188573441261578},
		{"capmap-competence-maturity", 2738188573441261579},
		{"capmap-competence-pain", 2738188573441261580},
		{"capmap-competence-section", 2738188573441261581},
		{"capmap-competence-lifecycle-by", 2738188573441261582},
		{"capmap-competence-lifecycle-at", 2738188573441261583},
		{"capmap-relation-source", 2738188573441261584},
		{"capmap-relation-target", 2738188573441261585},
		{"capmap-relation-target-text", 2738188573441261586},
		{"capmap-relation-kind", 2738188573441261587},
		{"capmap-relation-resolution", 2738188573441261588},
		{"capmap-relation-section", 2738188573441261589},
		{"capmap-relation-ncd", 2738188573441261590},
		{"capmap-competence-tag", 2738188573441261591},
	}
	require.Len(t, capmapvocab.AllMembs, len(golden),
		"a membership was added or removed — append its line to the golden table, and re-ingest if any line changed")
	for i, want := range golden {
		got := capmapvocab.AllMembs[i]
		assert.Equalf(t, want.name, string(got.GetNaturalKey()), "position %d", i)
		assert.Equalf(t, want.id, got.GetId().Value(), "id of %q", want.name)
	}
}
