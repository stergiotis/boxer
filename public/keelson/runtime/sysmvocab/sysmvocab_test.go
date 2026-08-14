package sysmvocab_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
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

// The base is the allocation, so it is pinned rather than left to drift: the
// package comment, ADR-0184 §SD4 and this constant have to agree, and a change
// here is a change to what rows already in the table mean.
func TestTagValueBaseIsTheAllocatedOne(t *testing.T) {
	assert.Equal(t, uint32(32), uint32(sysmvocab.TagValueBase))
	assert.Equal(t, uint32(32), sysmvocab.MembersTagValue.GetTagValue().Value(),
		"offset 0 of the base is what the memberships hang from")
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

// The whole name-to-id table, written down.
//
// A membership id is its registration ordinal, so declaring a new membership
// anywhere but at the end renumbers every one after it. That change compiles,
// vets, writes and reads — it just makes rows already in `boxer.facts` mean
// something else, because the id is all a row carries. Pinning only the ends
// catches a prepend and an append but not the insertion in the middle, which is
// the one an author actually makes when they put a new field "with the others".
//
// Updating this table is therefore a deliberate act: appending a line is
// ordinary, and changing a line that is already here means every store holding
// metric history must be re-ingested.
func TestMembershipIdsAreGoldenPinned(t *testing.T) {
	golden := []struct {
		name string
		id   uint64
	}{
		{"sysm-kind-cpu", 3098476543630901248},
		{"sysm-kind-cpu-info", 3098476543630901249},
		{"sysm-kind-mem", 3098476543630901250},
		{"sysm-cpu-host", 3098476543630901251},
		{"sysm-cpu-info-host", 3098476543630901252},
		{"sysm-mem-host", 3098476543630901253},
		{"sysm-cpu-total-pct", 3098476543630901254},
		{"sysm-cpu-per-core-pct", 3098476543630901255},
		{"sysm-cpu-per-core-freq-mhz", 3098476543630901256},
		{"sysm-cpu-load-avg1", 3098476543630901257},
		{"sysm-cpu-load-avg5", 3098476543630901258},
		{"sysm-cpu-load-avg15", 3098476543630901259},
		{"sysm-cpu-usage-watts", 3098476543630901260},
		{"sysm-cpu-active-cpus", 3098476543630901261},
		{"sysm-cpu-model-name", 3098476543630901262},
		{"sysm-cpu-logical-cores", 3098476543630901263},
		{"sysm-mem-total-bytes", 3098476543630901264},
		{"sysm-mem-free-bytes", 3098476543630901265},
		{"sysm-mem-available-bytes", 3098476543630901266},
		{"sysm-mem-buffers-bytes", 3098476543630901267},
		{"sysm-mem-cached-bytes", 3098476543630901268},
		{"sysm-mem-swap-total-bytes", 3098476543630901269},
		{"sysm-mem-swap-free-bytes", 3098476543630901270},
		{"sysm-mem-used-bytes", 3098476543630901271},
		{"sysm-mem-swap-used-bytes", 3098476543630901272},
		{"sysm-mem-arc-size-bytes", 3098476543630901273},
		{"sysm-mem-arc-min-bytes", 3098476543630901274},
		{"sysm-sensitive", 3098476543630901275},
	}
	require.Len(t, sysmvocab.AllMembs, len(golden),
		"a membership was added or removed — append its line to the golden table, and re-ingest if any line changed")
	for i, want := range golden {
		got := sysmvocab.AllMembs[i]
		assert.Equalf(t, want.name, string(got.GetNaturalKey()), "position %d", i)
		assert.Equalf(t, want.id, got.GetId().Value(), "id of %q", want.name)
	}
}
