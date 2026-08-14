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
// Memb* constant below lives in it. The size is a capacity hint only — ids
// follow registration order and do not move when it changes — set with room
// for the topology domain ADR-0184 M6 still adds.
var NkRegistry = registry.MustNewNaturalKeyRegistry(
	MembersTagValue.GetTagValue(), 192, NamingStyle, identifier.UntaggedId(0), Contract,
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

	// --- ADR-0184 M4: the remaining scalar and per-item domains. ---
	//
	// Appended, never interleaved with the block above: a membership id is its
	// registration ordinal, so inserting among the cpu and mem names would
	// renumber them and silently change what already-stored rows mean.

	// PSI. Every figure is the kernel's own — the avg windows are already
	// percentages and the totals are cumulative microseconds, so neither is
	// recomputed here. `full` is all-zero for cpu on most kernels; it is stored
	// as read rather than dropped, because "the kernel reported zero" and "we
	// decided not to store it" are different facts.
	MembKindPsi = NkRegistry.MustBegin("sysmKindPsi").End()
	MembPsiHost = NkRegistry.MustBegin("sysmPsiHost").End()

	MembPsiCpuSomeAvg10   = NkRegistry.MustBegin("sysmPsiCpuSomeAvg10").End()
	MembPsiCpuSomeAvg60   = NkRegistry.MustBegin("sysmPsiCpuSomeAvg60").End()
	MembPsiCpuSomeAvg300  = NkRegistry.MustBegin("sysmPsiCpuSomeAvg300").End()
	MembPsiCpuSomeTotalUs = NkRegistry.MustBegin("sysmPsiCpuSomeTotalUs").End()
	MembPsiCpuFullAvg10   = NkRegistry.MustBegin("sysmPsiCpuFullAvg10").End()
	MembPsiCpuFullAvg60   = NkRegistry.MustBegin("sysmPsiCpuFullAvg60").End()
	MembPsiCpuFullAvg300  = NkRegistry.MustBegin("sysmPsiCpuFullAvg300").End()
	MembPsiCpuFullTotalUs = NkRegistry.MustBegin("sysmPsiCpuFullTotalUs").End()

	MembPsiMemorySomeAvg10   = NkRegistry.MustBegin("sysmPsiMemorySomeAvg10").End()
	MembPsiMemorySomeAvg60   = NkRegistry.MustBegin("sysmPsiMemorySomeAvg60").End()
	MembPsiMemorySomeAvg300  = NkRegistry.MustBegin("sysmPsiMemorySomeAvg300").End()
	MembPsiMemorySomeTotalUs = NkRegistry.MustBegin("sysmPsiMemorySomeTotalUs").End()
	MembPsiMemoryFullAvg10   = NkRegistry.MustBegin("sysmPsiMemoryFullAvg10").End()
	MembPsiMemoryFullAvg60   = NkRegistry.MustBegin("sysmPsiMemoryFullAvg60").End()
	MembPsiMemoryFullAvg300  = NkRegistry.MustBegin("sysmPsiMemoryFullAvg300").End()
	MembPsiMemoryFullTotalUs = NkRegistry.MustBegin("sysmPsiMemoryFullTotalUs").End()

	MembPsiIoSomeAvg10   = NkRegistry.MustBegin("sysmPsiIoSomeAvg10").End()
	MembPsiIoSomeAvg60   = NkRegistry.MustBegin("sysmPsiIoSomeAvg60").End()
	MembPsiIoSomeAvg300  = NkRegistry.MustBegin("sysmPsiIoSomeAvg300").End()
	MembPsiIoSomeTotalUs = NkRegistry.MustBegin("sysmPsiIoSomeTotalUs").End()
	MembPsiIoFullAvg10   = NkRegistry.MustBegin("sysmPsiIoFullAvg10").End()
	MembPsiIoFullAvg60   = NkRegistry.MustBegin("sysmPsiIoFullAvg60").End()
	MembPsiIoFullAvg300  = NkRegistry.MustBegin("sysmPsiIoFullAvg300").End()
	MembPsiIoFullTotalUs = NkRegistry.MustBegin("sysmPsiIoFullTotalUs").End()

	// Available distinguishes "kernel built without CONFIG_PSI" from "no
	// pressure". Without it every unsupported host would read as a perfectly
	// unstalled one.
	MembPsiAvailable = NkRegistry.MustBegin("sysmPsiAvailable").End()

	// Network, one array element per interface. See the DTO for the alignment
	// contract the parallel arrays carry.
	MembKindNet = NkRegistry.MustBegin("sysmKindNet").End()
	MembNetHost = NkRegistry.MustBegin("sysmNetHost").End()

	MembNetName         = NkRegistry.MustBegin("sysmNetName").End()
	MembNetIndex        = NkRegistry.MustBegin("sysmNetIndex").End()
	MembNetHardwareAddr = NkRegistry.MustBegin("sysmNetHardwareAddr").End()
	MembNetUp           = NkRegistry.MustBegin("sysmNetUp").End()
	MembNetRunning      = NkRegistry.MustBegin("sysmNetRunning").End()
	MembNetRxBytes      = NkRegistry.MustBegin("sysmNetRxBytes").End()
	MembNetTxBytes      = NkRegistry.MustBegin("sysmNetTxBytes").End()
	// The per-second rates are the collector's own derivation and are stored
	// alongside the raw counters rather than left to the reader: they
	// compensate for counter wrap on 32-bit virtual NICs, which a consumer
	// differencing the cumulative fields cannot detect after the fact.
	MembNetRxBytesPerSec = NkRegistry.MustBegin("sysmNetRxBytesPerSec").End()
	MembNetTxBytesPerSec = NkRegistry.MustBegin("sysmNetTxBytesPerSec").End()

	// Filesystem capacity, one array element per mount entry.
	MembKindDiskMount = NkRegistry.MustBegin("sysmKindDiskMount").End()
	MembDiskMountHost = NkRegistry.MustBegin("sysmDiskMountHost").End()

	MembDiskMountDevice     = NkRegistry.MustBegin("sysmDiskMountDevice").End()
	MembDiskMountPoint      = NkRegistry.MustBegin("sysmDiskMountPoint").End()
	MembDiskMountFsType     = NkRegistry.MustBegin("sysmDiskMountFsType").End()
	MembDiskMountBlockName  = NkRegistry.MustBegin("sysmDiskMountBlockName").End()
	MembDiskMountReal       = NkRegistry.MustBegin("sysmDiskMountReal").End()
	MembDiskMountTotalBytes = NkRegistry.MustBegin("sysmDiskMountTotalBytes").End()
	MembDiskMountFreeBytes  = NkRegistry.MustBegin("sysmDiskMountFreeBytes").End()
	MembDiskMountUsedBytes  = NkRegistry.MustBegin("sysmDiskMountUsedBytes").End()
	MembDiskMountUsedPct    = NkRegistry.MustBegin("sysmDiskMountUsedPct").End()

	// Block-device I/O, one array element per device. A separate kind from the
	// mount table because the two lists have independent lengths — one entity
	// per aligned group keeps every array in a row the same length.
	MembKindDiskIo = NkRegistry.MustBegin("sysmKindDiskIo").End()
	MembDiskIoHost = NkRegistry.MustBegin("sysmDiskIoHost").End()

	MembDiskIoName             = NkRegistry.MustBegin("sysmDiskIoName").End()
	MembDiskIoReadBytesPerSec  = NkRegistry.MustBegin("sysmDiskIoReadBytesPerSec").End()
	MembDiskIoWriteBytesPerSec = NkRegistry.MustBegin("sysmDiskIoWriteBytesPerSec").End()
	MembDiskIoBusyPct          = NkRegistry.MustBegin("sysmDiskIoBusyPct").End()

	// Power supplies. Batteries and mains adapters are two independently
	// lengthed groups within one kind; each group's own arrays are aligned.
	MembKindBattery = NkRegistry.MustBegin("sysmKindBattery").End()
	MembBatteryHost = NkRegistry.MustBegin("sysmBatteryHost").End()

	MembBatteryName    = NkRegistry.MustBegin("sysmBatteryName").End()
	MembBatteryType    = NkRegistry.MustBegin("sysmBatteryType").End()
	MembBatteryPercent = NkRegistry.MustBegin("sysmBatteryPercent").End()
	// State is the normalized kernel charge state, stored as its numeric code
	// rather than its label so the stored value cannot drift with a rename.
	MembBatteryState      = NkRegistry.MustBegin("sysmBatteryState").End()
	MembBatteryPowerWatts = NkRegistry.MustBegin("sysmBatteryPowerWatts").End()
	// The two remaining-time fields carry the collector's -1 sentinel for
	// "unknown or not in that state", which is why they are signed.
	MembBatterySecondsToFull  = NkRegistry.MustBegin("sysmBatterySecondsToFull").End()
	MembBatterySecondsToEmpty = NkRegistry.MustBegin("sysmBatterySecondsToEmpty").End()

	MembAcAdapterName   = NkRegistry.MustBegin("sysmAcAdapterName").End()
	MembAcAdapterOnline = NkRegistry.MustBegin("sysmAcAdapterOnline").End()

	// GPUs, one array element per device across all vendors.
	MembKindGpu = NkRegistry.MustBegin("sysmKindGpu").End()
	MembGpuHost = NkRegistry.MustBegin("sysmGpuHost").End()

	MembGpuVendor  = NkRegistry.MustBegin("sysmGpuVendor").End()
	MembGpuIndex   = NkRegistry.MustBegin("sysmGpuIndex").End()
	MembGpuName    = NkRegistry.MustBegin("sysmGpuName").End()
	MembGpuPciId   = NkRegistry.MustBegin("sysmGpuPciId").End()
	MembGpuBusyPct = NkRegistry.MustBegin("sysmGpuBusyPct").End()
	// Memory, power, temperature and clock are 0 where the vendor exposes no
	// accounting for them; the collector does not distinguish that from a
	// genuine zero, so neither can a reader.
	MembGpuMemoryUsedBytes  = NkRegistry.MustBegin("sysmGpuMemoryUsedBytes").End()
	MembGpuMemoryTotalBytes = NkRegistry.MustBegin("sysmGpuMemoryTotalBytes").End()
	MembGpuPowerWatts       = NkRegistry.MustBegin("sysmGpuPowerWatts").End()
	MembGpuTempC            = NkRegistry.MustBegin("sysmGpuTempC").End()
	MembGpuFreqMhz          = NkRegistry.MustBegin("sysmGpuFreqMhz").End()

	// --- ADR-0184 M5: the per-tick tables. ---

	// The process table, column-major like the M4 per-item domains. Nothing
	// here identifies a human or reveals what a process was invoked with — see
	// the sysmProcCmd* block for why that is a separate kind.
	MembKindProc = NkRegistry.MustBegin("sysmKindProc").End()
	MembProcHost = NkRegistry.MustBegin("sysmProcHost").End()

	MembProcPid = NkRegistry.MustBegin("sysmProcPid").End()
	// PPID plus PID is what makes the table a forest rather than a list; a
	// process is only interpretable relative to its parent.
	MembProcPpid = NkRegistry.MustBegin("sysmProcPpid").End()
	// Name is /proc/[pid]/comm — the kernel's own 15-character truncation, not
	// the command line.
	MembProcName = NkRegistry.MustBegin("sysmProcName").End()
	// State is the single-letter Linux state (R/S/D/Z/T/I/…), stored as the
	// letter because that is what every kernel document and every operator
	// calls it.
	MembProcState = NkRegistry.MustBegin("sysmProcState").End()
	// CPUPercent is per-CPU: a process pegging one core reads 100, one pegging
	// N cores reads N*100. It is not clamped, and a reader that clamps it loses
	// exactly the processes worth looking at.
	MembProcCpuPct       = NkRegistry.MustBegin("sysmProcCpuPct").End()
	MembProcRssBytes     = NkRegistry.MustBegin("sysmProcRssBytes").End()
	MembProcVmSizeBytes  = NkRegistry.MustBegin("sysmProcVmSizeBytes").End()
	MembProcNumThreads   = NkRegistry.MustBegin("sysmProcNumThreads").End()
	MembProcNice         = NkRegistry.MustBegin("sysmProcNice").End()
	MembProcPriority     = NkRegistry.MustBegin("sysmProcPriority").End()
	MembProcKernelThread = NkRegistry.MustBegin("sysmProcKernelThread").End()
	// StartedAt is what makes a pid unambiguous over time: pids are reused, so
	// (pid, startedAt) is the identity a history query needs.
	MembProcStartedAtMs = NkRegistry.MustBegin("sysmProcStartedAtMs").End()
	// The two ADR-0126 topology marks: the cooperative BOXER_COMPONENT value
	// and the kernel-maintained systemd unit that corroborates it.
	MembProcComponent  = NkRegistry.MustBegin("sysmProcComponent").End()
	MembProcCgroupUnit = NkRegistry.MustBegin("sysmProcCgroupUnit").End()

	// Process identity and invocation — the ADR-0090 §SD8 sensitive class, in
	// its own kind so that a deployment which does not opt in stores none of
	// it rather than storing it tagged.
	//
	// §SD8 designed a `sensitive` membership carried alongside the attribute's
	// own, on the reasoning that the tag travels with the data. Two things make
	// separation the better shape for the stored form: a component DTO binds
	// one membership per field, so the second tag is unreachable from the
	// generated write path at all; and the masking switch §SD8 defers does not
	// exist, so a tag today is an annotation nothing enforces. A kind a
	// deployment never writes needs no enforcement.
	MembKindProcCmd = NkRegistry.MustBegin("sysmKindProcCmd").End()
	MembProcCmdHost = NkRegistry.MustBegin("sysmProcCmdHost").End()
	// Pid repeats here because this kind's arrays are aligned among themselves,
	// not with the sysmProc* kind's — the two are separate entities and a
	// reader joins them on pid.
	MembProcCmdPid  = NkRegistry.MustBegin("sysmProcCmdPid").End()
	MembProcCmdLine = NkRegistry.MustBegin("sysmProcCmdLine").End()
	MembProcCmdUser = NkRegistry.MustBegin("sysmProcCmdUser").End()
	MembProcCmdUid  = NkRegistry.MustBegin("sysmProcCmdUid").End()
	MembProcCmdGid  = NkRegistry.MustBegin("sysmProcCmdGid").End()

	// Listening sockets (ADR-0126 observed topology). The collector samples on
	// its own slower cadence and consecutive bundles repeat one snapshot, so
	// the tee writes a row only when the collection stamp advances.
	MembKindSocket = NkRegistry.MustBegin("sysmKindSocket").End()
	MembSocketHost = NkRegistry.MustBegin("sysmSocketHost").End()

	MembSocketProto = NkRegistry.MustBegin("sysmSocketProto").End()
	// Addr is an IP literal for inet sockets and a filesystem or @abstract path
	// for unix ones; Port is 0 for unix.
	MembSocketAddr = NkRegistry.MustBegin("sysmSocketAddr").End()
	MembSocketPort = NkRegistry.MustBegin("sysmSocketPort").End()
	// Inode is the join key the fd walk attributes pids by, kept so an
	// unattributed row can still be correlated later.
	MembSocketInode = NkRegistry.MustBegin("sysmSocketInode").End()
	MembSocketUid   = NkRegistry.MustBegin("sysmSocketUid").End()
	// Pid is 0 where the owning process's fd table was unreadable. Partial over
	// absent: the row is published anyway (ADR-0126 §SD3), so a zero here means
	// "not attributed", not "owned by pid 0".
	MembSocketPid = NkRegistry.MustBegin("sysmSocketPid").End()

	// --- ADR-0184 M6: the CPU containment tree. ---
	//
	// The tree is stored as an adjacency list: a pre-order walk numbers the
	// nodes and each carries its parent's number. Parallel arrays cannot hold a
	// recursive shape directly, and the alternatives are worse — a serialized
	// blob would put the structure beyond SQL entirely, which is the opposite
	// of what modelling it as facts is for. NodeIdx plus ParentIdx reconstruct
	// the tree exactly, and a recursive CTE walks it.
	MembKindTopology = NkRegistry.MustBegin("sysmKindTopology").End()
	MembTopologyHost = NkRegistry.MustBegin("sysmTopologyHost").End()

	// NodeIdx is stored rather than left implicit in array position: the moment
	// a query filters the arrays — "just the PUs" — position is lost and the
	// parent references would dangle.
	MembTopoNodeIdx = NkRegistry.MustBegin("sysmTopoNodeIdx").End()
	// ParentIdx is -1 for the root, which is the only node without one.
	MembTopoParentIdx = NkRegistry.MustBegin("sysmTopoParentIdx").End()
	// Kind is the hwloc-style name (Machine/Package/NUMANode/Cache/Core/PU),
	// stored as the name rather than its enum ordinal so a row stays readable
	// and cannot drift if the enum is reordered.
	MembTopoKind = NkRegistry.MustBegin("sysmTopoKind").End()
	// OSIndex is the kernel's own id for the object and is -1 for Machine and
	// Cache, which have no single id.
	MembTopoOsIndex = NkRegistry.MustBegin("sysmTopoOsIndex").End()

	// Cache attributes, meaningful only on Cache nodes.
	MembTopoCacheLevel     = NkRegistry.MustBegin("sysmTopoCacheLevel").End()
	MembTopoCacheType      = NkRegistry.MustBegin("sysmTopoCacheType").End()
	MembTopoCacheSizeBytes = NkRegistry.MustBegin("sysmTopoCacheSizeBytes").End()

	// Node-local RAM, meaningful only on NUMANode nodes.
	MembTopoMemBytes = NkRegistry.MustBegin("sysmTopoMemBytes").End()

	// The cpufreq policy, meaningful only on PU nodes. Present is carried
	// separately because a PU whose cpufreq read failed and a PU with a policy
	// reporting zeroes are otherwise the same row.
	MembTopoFreqPresent  = NkRegistry.MustBegin("sysmTopoFreqPresent").End()
	MembTopoFreqMinMhz   = NkRegistry.MustBegin("sysmTopoFreqMinMhz").End()
	MembTopoFreqMaxMhz   = NkRegistry.MustBegin("sysmTopoFreqMaxMhz").End()
	MembTopoFreqGovernor = NkRegistry.MustBegin("sysmTopoFreqGovernor").End()
	MembTopoFreqDriver   = NkRegistry.MustBegin("sysmTopoFreqDriver").End()

	// LogicalCount is the collector's own count of online PU leaves. It is
	// stored rather than derived from the node arrays because it is what the
	// collector observed, and a mismatch between the two is itself a finding.
	MembTopoLogicalCount = NkRegistry.MustBegin("sysmTopoLogicalCount").End()
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

	MembKindPsi, MembPsiHost,
	MembPsiCpuSomeAvg10, MembPsiCpuSomeAvg60, MembPsiCpuSomeAvg300, MembPsiCpuSomeTotalUs,
	MembPsiCpuFullAvg10, MembPsiCpuFullAvg60, MembPsiCpuFullAvg300, MembPsiCpuFullTotalUs,
	MembPsiMemorySomeAvg10, MembPsiMemorySomeAvg60, MembPsiMemorySomeAvg300, MembPsiMemorySomeTotalUs,
	MembPsiMemoryFullAvg10, MembPsiMemoryFullAvg60, MembPsiMemoryFullAvg300, MembPsiMemoryFullTotalUs,
	MembPsiIoSomeAvg10, MembPsiIoSomeAvg60, MembPsiIoSomeAvg300, MembPsiIoSomeTotalUs,
	MembPsiIoFullAvg10, MembPsiIoFullAvg60, MembPsiIoFullAvg300, MembPsiIoFullTotalUs,
	MembPsiAvailable,

	MembKindNet, MembNetHost,
	MembNetName, MembNetIndex, MembNetHardwareAddr, MembNetUp, MembNetRunning,
	MembNetRxBytes, MembNetTxBytes, MembNetRxBytesPerSec, MembNetTxBytesPerSec,

	MembKindDiskMount, MembDiskMountHost,
	MembDiskMountDevice, MembDiskMountPoint, MembDiskMountFsType, MembDiskMountBlockName,
	MembDiskMountReal, MembDiskMountTotalBytes, MembDiskMountFreeBytes,
	MembDiskMountUsedBytes, MembDiskMountUsedPct,

	MembKindDiskIo, MembDiskIoHost,
	MembDiskIoName, MembDiskIoReadBytesPerSec, MembDiskIoWriteBytesPerSec, MembDiskIoBusyPct,

	MembKindBattery, MembBatteryHost,
	MembBatteryName, MembBatteryType, MembBatteryPercent, MembBatteryState,
	MembBatteryPowerWatts, MembBatterySecondsToFull, MembBatterySecondsToEmpty,
	MembAcAdapterName, MembAcAdapterOnline,

	MembKindGpu, MembGpuHost,
	MembGpuVendor, MembGpuIndex, MembGpuName, MembGpuPciId, MembGpuBusyPct,
	MembGpuMemoryUsedBytes, MembGpuMemoryTotalBytes, MembGpuPowerWatts,
	MembGpuTempC, MembGpuFreqMhz,

	MembKindProc, MembProcHost,
	MembProcPid, MembProcPpid, MembProcName, MembProcState, MembProcCpuPct,
	MembProcRssBytes, MembProcVmSizeBytes, MembProcNumThreads, MembProcNice,
	MembProcPriority, MembProcKernelThread, MembProcStartedAtMs,
	MembProcComponent, MembProcCgroupUnit,

	MembKindProcCmd, MembProcCmdHost,
	MembProcCmdPid, MembProcCmdLine, MembProcCmdUser, MembProcCmdUid, MembProcCmdGid,

	MembKindSocket, MembSocketHost,
	MembSocketProto, MembSocketAddr, MembSocketPort, MembSocketInode,
	MembSocketUid, MembSocketPid,

	MembKindTopology, MembTopologyHost,
	MembTopoNodeIdx, MembTopoParentIdx, MembTopoKind, MembTopoOsIndex,
	MembTopoCacheLevel, MembTopoCacheType, MembTopoCacheSizeBytes,
	MembTopoMemBytes,
	MembTopoFreqPresent, MembTopoFreqMinMhz, MembTopoFreqMaxMhz,
	MembTopoFreqGovernor, MembTopoFreqDriver,
	MembTopoLogicalCount,
}
