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
		{"sysm-kind-psi", 3098476543630901276},
		{"sysm-psi-host", 3098476543630901277},
		{"sysm-psi-cpu-some-avg10", 3098476543630901278},
		{"sysm-psi-cpu-some-avg60", 3098476543630901279},
		{"sysm-psi-cpu-some-avg300", 3098476543630901280},
		{"sysm-psi-cpu-some-total-us", 3098476543630901281},
		{"sysm-psi-cpu-full-avg10", 3098476543630901282},
		{"sysm-psi-cpu-full-avg60", 3098476543630901283},
		{"sysm-psi-cpu-full-avg300", 3098476543630901284},
		{"sysm-psi-cpu-full-total-us", 3098476543630901285},
		{"sysm-psi-memory-some-avg10", 3098476543630901286},
		{"sysm-psi-memory-some-avg60", 3098476543630901287},
		{"sysm-psi-memory-some-avg300", 3098476543630901288},
		{"sysm-psi-memory-some-total-us", 3098476543630901289},
		{"sysm-psi-memory-full-avg10", 3098476543630901290},
		{"sysm-psi-memory-full-avg60", 3098476543630901291},
		{"sysm-psi-memory-full-avg300", 3098476543630901292},
		{"sysm-psi-memory-full-total-us", 3098476543630901293},
		{"sysm-psi-io-some-avg10", 3098476543630901294},
		{"sysm-psi-io-some-avg60", 3098476543630901295},
		{"sysm-psi-io-some-avg300", 3098476543630901296},
		{"sysm-psi-io-some-total-us", 3098476543630901297},
		{"sysm-psi-io-full-avg10", 3098476543630901298},
		{"sysm-psi-io-full-avg60", 3098476543630901299},
		{"sysm-psi-io-full-avg300", 3098476543630901300},
		{"sysm-psi-io-full-total-us", 3098476543630901301},
		{"sysm-psi-available", 3098476543630901302},
		{"sysm-kind-net", 3098476543630901303},
		{"sysm-net-host", 3098476543630901304},
		{"sysm-net-name", 3098476543630901305},
		{"sysm-net-index", 3098476543630901306},
		{"sysm-net-hardware-addr", 3098476543630901307},
		{"sysm-net-up", 3098476543630901308},
		{"sysm-net-running", 3098476543630901309},
		{"sysm-net-rx-bytes", 3098476543630901310},
		{"sysm-net-tx-bytes", 3098476543630901311},
		{"sysm-net-rx-bytes-per-sec", 3098476543630901312},
		{"sysm-net-tx-bytes-per-sec", 3098476543630901313},
		{"sysm-kind-disk-mount", 3098476543630901314},
		{"sysm-disk-mount-host", 3098476543630901315},
		{"sysm-disk-mount-device", 3098476543630901316},
		{"sysm-disk-mount-point", 3098476543630901317},
		{"sysm-disk-mount-fs-type", 3098476543630901318},
		{"sysm-disk-mount-block-name", 3098476543630901319},
		{"sysm-disk-mount-real", 3098476543630901320},
		{"sysm-disk-mount-total-bytes", 3098476543630901321},
		{"sysm-disk-mount-free-bytes", 3098476543630901322},
		{"sysm-disk-mount-used-bytes", 3098476543630901323},
		{"sysm-disk-mount-used-pct", 3098476543630901324},
		{"sysm-kind-disk-io", 3098476543630901325},
		{"sysm-disk-io-host", 3098476543630901326},
		{"sysm-disk-io-name", 3098476543630901327},
		{"sysm-disk-io-read-bytes-per-sec", 3098476543630901328},
		{"sysm-disk-io-write-bytes-per-sec", 3098476543630901329},
		{"sysm-disk-io-busy-pct", 3098476543630901330},
		{"sysm-kind-battery", 3098476543630901331},
		{"sysm-battery-host", 3098476543630901332},
		{"sysm-battery-name", 3098476543630901333},
		{"sysm-battery-type", 3098476543630901334},
		{"sysm-battery-percent", 3098476543630901335},
		{"sysm-battery-state", 3098476543630901336},
		{"sysm-battery-power-watts", 3098476543630901337},
		{"sysm-battery-seconds-to-full", 3098476543630901338},
		{"sysm-battery-seconds-to-empty", 3098476543630901339},
		{"sysm-ac-adapter-name", 3098476543630901340},
		{"sysm-ac-adapter-online", 3098476543630901341},
		{"sysm-kind-gpu", 3098476543630901342},
		{"sysm-gpu-host", 3098476543630901343},
		{"sysm-gpu-vendor", 3098476543630901344},
		{"sysm-gpu-index", 3098476543630901345},
		{"sysm-gpu-name", 3098476543630901346},
		{"sysm-gpu-pci-id", 3098476543630901347},
		{"sysm-gpu-busy-pct", 3098476543630901348},
		{"sysm-gpu-memory-used-bytes", 3098476543630901349},
		{"sysm-gpu-memory-total-bytes", 3098476543630901350},
		{"sysm-gpu-power-watts", 3098476543630901351},
		{"sysm-gpu-temp-c", 3098476543630901352},
		{"sysm-gpu-freq-mhz", 3098476543630901353},
		{"sysm-kind-proc", 3098476543630901354},
		{"sysm-proc-host", 3098476543630901355},
		{"sysm-proc-pid", 3098476543630901356},
		{"sysm-proc-ppid", 3098476543630901357},
		{"sysm-proc-name", 3098476543630901358},
		{"sysm-proc-state", 3098476543630901359},
		{"sysm-proc-cpu-pct", 3098476543630901360},
		{"sysm-proc-rss-bytes", 3098476543630901361},
		{"sysm-proc-vm-size-bytes", 3098476543630901362},
		{"sysm-proc-num-threads", 3098476543630901363},
		{"sysm-proc-nice", 3098476543630901364},
		{"sysm-proc-priority", 3098476543630901365},
		{"sysm-proc-kernel-thread", 3098476543630901366},
		{"sysm-proc-started-at-ms", 3098476543630901367},
		{"sysm-proc-component", 3098476543630901368},
		{"sysm-proc-cgroup-unit", 3098476543630901369},
		{"sysm-kind-proc-cmd", 3098476543630901370},
		{"sysm-proc-cmd-host", 3098476543630901371},
		{"sysm-proc-cmd-pid", 3098476543630901372},
		{"sysm-proc-cmd-line", 3098476543630901373},
		{"sysm-proc-cmd-user", 3098476543630901374},
		{"sysm-proc-cmd-uid", 3098476543630901375},
		{"sysm-proc-cmd-gid", 3098476543630901376},
		{"sysm-kind-socket", 3098476543630901377},
		{"sysm-socket-host", 3098476543630901378},
		{"sysm-socket-proto", 3098476543630901379},
		{"sysm-socket-addr", 3098476543630901380},
		{"sysm-socket-port", 3098476543630901381},
		{"sysm-socket-inode", 3098476543630901382},
		{"sysm-socket-uid", 3098476543630901383},
		{"sysm-socket-pid", 3098476543630901384},
	}
	require.Len(t, sysmvocab.AllMembs, len(golden),
		"a membership was added or removed — append its line to the golden table, and re-ingest if any line changed")
	for i, want := range golden {
		got := sysmvocab.AllMembs[i]
		assert.Equalf(t, want.name, string(got.GetNaturalKey()), "position %d", i)
		assert.Equalf(t, want.id, got.GetId().Value(), "id of %q", want.name)
	}
}
