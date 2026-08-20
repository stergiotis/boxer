package ladingvocab_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func TestAllMembsHaveNonZeroIds(t *testing.T) {
	require.NotEmpty(t, ladingvocab.AllMembs)
	for _, m := range ladingvocab.AllMembs {
		assert.NotZero(t, m.GetId().Value(), "membership %q must have a non-zero id", m.GetNaturalKey())
	}
}

func TestAllMembsHaveUniqueIds(t *testing.T) {
	seen := make(map[uint64]string, len(ladingvocab.AllMembs))
	for _, m := range ladingvocab.AllMembs {
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
	for range ladingvocab.NkRegistry.IterateAll() {
		registered++
	}
	assert.Equal(t, registered, len(ladingvocab.AllMembs),
		"AllMembs must list every membership registered in NkRegistry")
}

// The load-bearing invariant. The mount policy record shares `boxer.facts`
// with metric samples, competence facts and runtime facts, so a membership id
// minted by two vocabularies would not fail to compile or to write — it would
// make two unrelated facts wear the same tag, and every query over either
// would be quietly wrong. Nothing but this test stands between the tag-value
// allocation and that outcome.
func TestTagValuesAreDisjointFromOtherVocabularies(t *testing.T) {
	ours := make(map[uint64]string, len(ladingvocab.AllMembs))
	for _, m := range ladingvocab.AllMembs {
		ours[m.GetId().Value()] = string(m.GetNaturalKey())
	}

	for _, other := range []struct {
		name  string
		membs []registry.RegisteredNaturalKey
	}{
		{"keelson/runtime/vocab", vocab.AllMembs},
		{"keelson/runtime/sysmvocab", sysmvocab.AllMembs},
		{"gov/capmapvocab", capmapvocab.AllMembs},
		{"keelson/vdd", []registry.RegisteredNaturalKey{vdd.MembParent, vdd.MembChild, vdd.MembNaturalKey}},
	} {
		require.NotEmpty(t, other.membs, "%s exposed no memberships to compare against, so this test proves nothing", other.name)
		for _, m := range other.membs {
			if name, clash := ours[m.GetId().Value()]; clash {
				t.Fatalf("membership id %d is minted by both ladingvocab %q and %s %q",
					m.GetId().Value(), name, other.name, m.GetNaturalKey())
			}
		}
	}
}

// The claimed value is the allocation, so it is pinned rather than left to
// drift: the package comment, ADR-0198 and this claim have to agree, and a
// change here is a change to what rows already in the tables mean.
func TestTagValueClaimIsTheAllocatedOne(t *testing.T) {
	assert.EqualValues(t, 2178315, ladingvocab.TagValueClaim.Value().Value())
	assert.Equal(t, "lading", ladingvocab.TagValueClaim.Name())
	assert.Equal(t, tagmint.VocabularyTagWidth, ladingvocab.TagValueClaim.Tag().GetTagWidth(),
		"every vcs-managed vocabulary claims from one width class")
}

// A DTO's `lw:` tag spells a membership lowerCamel while the registry stores
// it lower-spinal, and storegen bridges the two by conversion. A regression
// there would not fail generation — it would bake an id under a key no DTO
// tag matches, and the store would read nothing while reporting nothing.
//
// `ladingLine0` is the shape the converter is documented to be lossy on in
// other styles (a digit closing the name), so it is spelled out below rather
// than left to the loop's self-consistency.
func TestMembershipNamesRoundTripToTheirDtoSpelling(t *testing.T) {
	for _, m := range ladingvocab.AllMembs {
		stored := m.GetNaturalKey()
		camel := naming.ConvertNameStyle(stored, naming.LowerCamelCase)
		back := naming.ConvertNameStyle(camel, naming.LowerSpinalCase)
		assert.Equalf(t, stored, back, "membership %q does not round-trip via its DTO spelling %q", stored, camel)
	}
	assert.EqualValues(t, "ladingLine0",
		naming.ConvertNameStyle(naming.StylableName("lading-line0"), naming.LowerCamelCase))
}

// The snapshot storegen hands the generator must contain every name a DTO can
// spell. This is the same conversion as above, exercised through the real
// entry point rather than restated.
func TestStoregenSnapshotCoversEveryMembership(t *testing.T) {
	ids, err := storegen.MembershipIds(ladingvocab.NkRegistry)
	require.NoError(t, err)
	require.Len(t, ids, len(ladingvocab.AllMembs))
	assert.Equal(t, ladingvocab.MembLine0.GetId().Value(), ids["ladingLine0"])
	assert.Equal(t, ladingvocab.MembContentHash.GetId().Value(), ids["ladingContentHash"])
	assert.Equal(t, ladingvocab.MembMountInlineMax.GetId().Value(), ids["ladingMountInlineMax"])
}

// The applied policy on a snapshot's root row and a mount's declared policy
// are separate memberships on purpose (ADR-0198 §SD2): the record is mutable
// runtime state, the root row is not, and a snapshot has to stay
// interpretable after the record changes. Sharing one membership would make
// the two indistinguishable in a query over `boxer.facts`.
func TestAppliedAndDeclaredPolicyAreDistinctMemberships(t *testing.T) {
	for _, pair := range [][2]registry.RegisteredNaturalKey{
		{ladingvocab.MembTtlClass, ladingvocab.MembMountTtlClass},
		{ladingvocab.MembTextRule, ladingvocab.MembMountTextRule},
		{ladingvocab.MembInlineMax, ladingvocab.MembMountInlineMax},
	} {
		assert.NotEqual(t, pair[0].GetId().Value(), pair[1].GetId().Value(),
			"%q and %q must not share an id", pair[0].GetNaturalKey(), pair[1].GetNaturalKey())
	}
}
