package sysmvocab_test

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func TestAllMembsHaveNonZeroIds(t *testing.T) {
	require.NotEmpty(t, sysmvocab.AllMembs)
	for _, m := range sysmvocab.AllMembs {
		assert.NotZero(t, m.GetId().Value(), "membership %q must have a non-zero id", m.GetNaturalKey())
	}
}

func TestAllMembsHaveUniqueIds(t *testing.T) {
	seen := make(map[uint64]string, len(sysmvocab.AllMembs))
	for _, m := range sysmvocab.AllMembs {
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
	for range sysmvocab.NkRegistry.IterateAll() {
		registered++
	}
	assert.Equal(t, registered, len(sysmvocab.AllMembs),
		"AllMembs must list every membership registered in NkRegistry")
}

// The load-bearing invariant of ADR-0184 §SD4.
//
// Metric samples, competence facts and runtime facts share one table, so a
// membership id minted by two vocabularies would not fail to compile or to
// write — it would make two unrelated facts wear the same tag, and every query
// over either would be quietly wrong. Nothing but this test stands between the
// tag-value allocation and that outcome.
func TestTagValuesAreDisjointFromOtherVocabularies(t *testing.T) {
	ours := make(map[uint64]string, len(sysmvocab.AllMembs))
	for _, m := range sysmvocab.AllMembs {
		ours[m.GetId().Value()] = string(m.GetNaturalKey())
	}

	for _, other := range []struct {
		name  string
		membs []registry.RegisteredNaturalKey
	}{
		{"keelson/runtime/vocab", vocab.AllMembs},
		{"gov/capmapvocab", capmapvocab.AllMembs},
		{"keelson/vdd", []registry.RegisteredNaturalKey{vdd.MembParent, vdd.MembChild, vdd.MembNaturalKey}},
	} {
		require.NotEmpty(t, other.membs, "%s exposed no memberships to compare against, so this test proves nothing", other.name)
		for _, m := range other.membs {
			if name, clash := ours[m.GetId().Value()]; clash {
				t.Fatalf("membership id %d is minted by both sysmvocab %q and %s %q",
					m.GetId().Value(), name, other.name, m.GetNaturalKey())
			}
		}
	}
}

// The claimed value is the allocation, so it is pinned rather than left to
// drift: the package comment, ADR-0184 §SD4 and this claim have to agree, and
// a change here is a change to what rows already in the table mean.
func TestTagValueClaimIsTheAllocatedOne(t *testing.T) {
	assert.EqualValues(t, 2178312, sysmvocab.TagValueClaim.Value().Value())
	assert.Equal(t, "sysmetrics", sysmvocab.TagValueClaim.Name())
	assert.Equal(t, tagmint.VocabularyTagWidth, sysmvocab.TagValueClaim.Tag().GetTagWidth(),
		"every vcs-managed vocabulary claims from one width class")
}

// TestMembershipNamesRoundTripToTheirDtoSpelling is this vocabulary's share of
// the check ADR-0183 D1 asks for, and it is not ceremonial here: a DTO's `lw:`
// tag spells a membership lowerCamel while the registry stores it lower-spinal,
// and storegen bridges the two by conversion. Several names in this vocabulary
// end in a digit (`sysmCpuLoadAvg1`, `…15`), which is exactly the shape the
// converter is documented to be lossy on in other styles. A regression there
// would not fail generation — it would bake an id under a key no DTO tag
// matches, and the store would read nothing while reporting nothing.
func TestMembershipNamesRoundTripToTheirDtoSpelling(t *testing.T) {
	for _, m := range sysmvocab.AllMembs {
		stored := m.GetNaturalKey()
		camel := naming.ConvertNameStyle(stored, naming.LowerCamelCase)
		back := naming.ConvertNameStyle(camel, naming.LowerSpinalCase)
		assert.Equalf(t, stored, back, "membership %q does not round-trip via its DTO spelling %q", stored, camel)
	}
	// Spelled out for the digit-suffixed cases, so the expectation is readable
	// rather than merely self-consistent.
	assert.EqualValues(t, "sysmCpuLoadAvg1",
		naming.ConvertNameStyle(naming.StylableName("sysm-cpu-load-avg1"), naming.LowerCamelCase))
	assert.EqualValues(t, "sysmCpuLoadAvg15",
		naming.ConvertNameStyle(naming.StylableName("sysm-cpu-load-avg15"), naming.LowerCamelCase))
}

// The snapshot storegen hands the generator must contain every name a DTO can
// spell. This is the same conversion as above, exercised through the real
// entry point rather than restated.
func TestStoregenSnapshotCoversEveryMembership(t *testing.T) {
	ids, err := storegen.MembershipIds(sysmvocab.NkRegistry)
	require.NoError(t, err)
	require.Len(t, ids, len(sysmvocab.AllMembs))
	assert.Equal(t, sysmvocab.MembCpuLoadAvg15.GetId().Value(), ids["sysmCpuLoadAvg15"])
	assert.Equal(t, sysmvocab.MembCpuInfoHost.GetId().Value(), ids["sysmCpuInfoHost"])
	assert.Equal(t, sysmvocab.MembMemArcMinBytes.GetId().Value(), ids["sysmMemArcMinBytes"])
}
