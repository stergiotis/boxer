// Package sysmvocab is the system-metrics leeway natural-key vocabulary: the
// memberships that tag a metric sample in `boxer.facts` (ADR-0184 §SD4).
//
// It mirrors [github.com/stergiotis/boxer/public/gov/capmapvocab] and
// [github.com/stergiotis/boxer/public/keelson/runtime/vocab], which do the same
// job for the competence corpus and for runtime facts in the same table. What
// keeps the three apart is the tag value below.
//
// The DTOs written against these names live in
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts]; the ids are
// resolved into a generated record store at generation time by
// [github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen].
//
// # Tag value 32, and why the number is written down
//
// This package owns **tag value 32**, allocated by ADR-0184 §SD4 under the rule
// [github.com/stergiotis/boxer/public/gov/capmapvocab] states: a new vocabulary
// takes the next unused multiple of 16 and owns the even offsets up to the
// following multiple. 32 here means 32, 34 … 46. Bases 1 (keelson vdd) and 2
// (keelson runtime) are grandfathered; 16 is capmap's.
//
// Allocation is by *base* rather than by number because a base reserves an
// open-ended range: the tag-value registry mints `base + tv` for any even `tv`,
// so taking merely the next free integer would put a new vocabulary inside an
// existing one's growth path.
//
// TestTagValuesAreDisjointFromOtherVocabularies is what enforces it. A
// collision would not be a compile error — it would be two unrelated facts
// wearing the same membership id, and every query over either would be quietly
// wrong.
//
// # Raw counters only
//
// Every membership here names something the scraper read, never something
// derived from it. Rates, windows and EWMAs are consumer-side views
// (ADR-0090 §SD3), so a stored sample stays interpretable without knowing what
// window a writer had in mind.
package sysmvocab

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching the runtime's and capmap's. It requires the tag value handed to
// Begin be even; the base is what places the result.
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for sysmetrics membership names. It matches
// the other vocabularies sharing the table, so a query joining metric samples to
// runtime facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueBase is this vocabulary's reserved base — see the package comment for
// why allocation is by base rather than by single value.
const TagValueBase = 32

// TagValueRegistry allocates tag values for sysmetrics membership categories.
// It lives in its own scope so it cannot collide with the keelson, runtime or
// capmap registries, which are different vocabularies in the same table.
var TagValueRegistry = registry.MustNewTagValueRegistry(
	identifier.TagValue(TagValueBase), NamingStyle, 4, Contract,
)

// MembersTagValue is the tag value rooted at offset 0 of [TagValueRegistry],
// covering every membership registered below.
var MembersTagValue = TagValueRegistry.MustBegin("sysmMembers", 0).End()

// NkRegistry is the natural-key registry for sysmetrics memberships. Every
// Memb* constant below lives in it. Sized for the domains ADR-0184 phases in
// after cpu and mem — psi, net, disk, battery, gpu, then the fan-out and
// topology domains — so the common case does not re-hash.
var NkRegistry = registry.MustNewNaturalKeyRegistry(
	MembersTagValue.GetTagValue(), 128, NamingStyle, identifier.UntaggedId(0), Contract,
)

// Membership constants for `boxer.facts` rows carrying system-metric samples.
//
// Ordering matters and is append-only: membership ids follow declaration order,
// and rows already written carry them. A new membership goes at the end of its
// group, never in the middle — an insertion renumbers every membership after
// it, which compiles, vets, writes and reads while making stored rows mean
// something else.
var (
	// Kinds. The row's attribute value carries the kind label for readability;
	// the membership id is what identifies which kind the row is. A generated
	// store's Scan<Kind> filters on a kind's own memberships and does not need
	// these — they exist so hand-written SQL can select one kind without
	// enumerating its attributes.
	MembKindCpu     = NkRegistry.MustBegin("sysmKindCpu").End()
	MembKindCpuInfo = NkRegistry.MustBegin("sysmKindCpuInfo").End()
	MembKindMem     = NkRegistry.MustBegin("sysmKindMem").End()

	// The host token, once per kind.
	//
	// It is what makes one plane able to carry many boxes (ADR-0090 §SD1), and
	// it is an attribute as well as part of the natural key because filtering
	// by host is the common query and a `symbol` lane is LowCardinality on disk
	// — where the natural key is a blob a reader would have to parse.
	//
	// One membership per kind rather than one shared `sysmHost` because a
	// generated store declares each membership's kind symbol once per package
	// and refuses two kinds naming the same membership. Sharing needs the
	// reflect path (ADR-0146 D5/D6), which a store does not use. The cost is
	// linear in domains and is paid deliberately.
	MembCpuHost     = NkRegistry.MustBegin("sysmCpuHost").End()
	MembCpuInfoHost = NkRegistry.MustBegin("sysmCpuInfoHost").End()
	MembMemHost     = NkRegistry.MustBegin("sysmMemHost").End()

	// CPU sample. Percentages are whole numbers in [0,100] as the collector
	// reports them; frequencies are MHz; the load averages are the kernel's own
	// 1/5/15-minute figures and not recomputed here.
	MembCpuTotalPct       = NkRegistry.MustBegin("sysmCpuTotalPct").End()
	MembCpuPerCorePct     = NkRegistry.MustBegin("sysmCpuPerCorePct").End()
	MembCpuPerCoreFreqMhz = NkRegistry.MustBegin("sysmCpuPerCoreFreqMhz").End()
	MembCpuLoadAvg1       = NkRegistry.MustBegin("sysmCpuLoadAvg1").End()
	MembCpuLoadAvg5       = NkRegistry.MustBegin("sysmCpuLoadAvg5").End()
	MembCpuLoadAvg15      = NkRegistry.MustBegin("sysmCpuLoadAvg15").End()
	// Package power over the most recent sample interval, from the RAPL energy
	// counter. Absent rather than zero where RAPL is unavailable, so "no reading"
	// and "idle" stay distinguishable.
	MembCpuUsageWatts = NkRegistry.MustBegin("sysmCpuUsageWatts").End()
	// The cgroup v2 effective cpuset, as logical CPU indices. Absent where the
	// cgroup file is.
	MembCpuActiveCpus = NkRegistry.MustBegin("sysmCpuActiveCpus").End()

	// CPU descriptor — read once per host rather than per tick (ADR-0184 §SD3).
	MembCpuModelName    = NkRegistry.MustBegin("sysmCpuModelName").End()
	MembCpuLogicalCores = NkRegistry.MustBegin("sysmCpuLogicalCores").End()

	// Memory sample. Every figure is absolute bytes — the collector scales the
	// kB lines of /proc/meminfo — so nothing downstream has to know which unit a
	// given field arrived in.
	MembMemTotalBytes     = NkRegistry.MustBegin("sysmMemTotalBytes").End()
	MembMemFreeBytes      = NkRegistry.MustBegin("sysmMemFreeBytes").End()
	MembMemAvailableBytes = NkRegistry.MustBegin("sysmMemAvailableBytes").End()
	MembMemBuffersBytes   = NkRegistry.MustBegin("sysmMemBuffersBytes").End()
	MembMemCachedBytes    = NkRegistry.MustBegin("sysmMemCachedBytes").End()
	MembMemSwapTotalBytes = NkRegistry.MustBegin("sysmMemSwapTotalBytes").End()
	MembMemSwapFreeBytes  = NkRegistry.MustBegin("sysmMemSwapFreeBytes").End()
	// Used and SwapUsed are the collector's own derivations, kept because they
	// encode which fallback it applied (Available vs Free) — a reader cannot
	// recover that from the raw fields alone.
	MembMemUsedBytes     = NkRegistry.MustBegin("sysmMemUsedBytes").End()
	MembMemSwapUsedBytes = NkRegistry.MustBegin("sysmMemSwapUsedBytes").End()
	// ZFS ARC, absent unless the collector was built with it enabled and the
	// arcstats file is present.
	MembMemArcSizeBytes = NkRegistry.MustBegin("sysmMemArcSizeBytes").End()
	MembMemArcMinBytes  = NkRegistry.MustBegin("sysmMemArcMinBytes").End()

	// Sensitivity, declared before it has a writer (ADR-0090 §SD8, ADR-0184
	// §SD4). It tags attributes a later "untrusted" switch would mask at one
	// policy point — process command lines and usernames, which arrive with the
	// proc domain. Registered now so that when those rows are first written
	// they are already tagged, rather than there being a span of stored rows
	// the switch cannot see.
	MembSensitive = NkRegistry.MustBegin("sysmSensitive").End()
)

// AllMembs enumerates every registered sysmetrics membership. Tests iterate it
// to assert the invariants that matter — non-zero, unique, and disjoint from
// the other vocabularies sharing the table.
var AllMembs = []registry.RegisteredNaturalKey{
	MembKindCpu, MembKindCpuInfo, MembKindMem,
	MembCpuHost, MembCpuInfoHost, MembMemHost,
	MembCpuTotalPct, MembCpuPerCorePct, MembCpuPerCoreFreqMhz,
	MembCpuLoadAvg1, MembCpuLoadAvg5, MembCpuLoadAvg15,
	MembCpuUsageWatts, MembCpuActiveCpus,
	MembCpuModelName, MembCpuLogicalCores,
	MembMemTotalBytes, MembMemFreeBytes, MembMemAvailableBytes,
	MembMemBuffersBytes, MembMemCachedBytes,
	MembMemSwapTotalBytes, MembMemSwapFreeBytes,
	MembMemUsedBytes, MembMemSwapUsedBytes,
	MembMemArcSizeBytes, MembMemArcMinBytes,
	MembSensitive,
}
