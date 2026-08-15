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
// This package claims **tag value 2178312**, the fourth of the width-32 class
// (ADR-0183 D0). ADR-0184 §SD4 allocated base 32 for it a day earlier, under
// the base-allocation rule the re-key replaced: a width-32 tag holds about
// 4.3e9 ids, so one claimed value is a vocabulary's whole allocation and there
// is no growth path to reserve room beside.
//
// The claim goes through [github.com/stergiotis/boxer/public/identity/tagmint],
// which refuses a value another package already claimed, and the committed
// assignment tables are compared across the repo (ADR-0183 D1). A collision
// would not be a compile error — it would be two unrelated facts wearing the
// same membership id, and every query over either would be quietly wrong.
//
// # Raw counters only
//
// Every membership here names something the scraper read, never something
// derived from it. Rates, windows and EWMAs are consumer-side views
// (ADR-0090 §SD3), so a stored sample stays interpretable without knowing what
// window a writer had in mind.
package sysmvocab

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching the runtime's and capmap's. It requires a tag value from the
// vocabulary width class and each membership's ordinal declared in source.
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for sysmetrics membership names. It matches
// the other vocabularies sharing the table, so a query joining metric samples to
// runtime facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from (ADR-0183 D0). The
// fourth of the class.
//
// It was base 32, allocated by ADR-0184 §SD4 a day before this regime landed,
// under the hand-picked scheme the claim replaces. The re-key moves every
// sysmetrics id; the tee had written none that outlive the change.
var TagValueClaim = tagmint.MustClaim("sysmetrics", 2178312, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
const MaxExpectedMemberships = 1 << 16

// NkRegistry is the natural-key registry for sysmetrics memberships. Every
// Memb* constant below lives in it. The size is a capacity hint only — an id
// is what its registration declares — set with room for the topology domain
// ADR-0184 M6 still adds.
var NkRegistry = registry.MustNewNaturalKeyRegistry(
	TagValueClaim, 192, NamingStyle, Contract,
)

// Membership constants for `boxer.facts` rows carrying system-metric samples.
//
// Each states its ordinal: the number beside the name is the id's body, and
// rows already written carry it. A new membership takes the next unused
// ordinal and may be declared anywhere — the registry refuses a repeat, and
// nothing moves because of where it sits (ADR-0183 D0). Changing an ordinal
// already here is the breaking act, and the assignment golden is what makes
// it visible in review.
var (
	// Kinds. The row's attribute value carries the kind label for readability;
	// the membership id is what identifies which kind the row is. A generated
	// store's Scan<Kind> filters on a kind's own memberships and does not need
	// these — they exist so hand-written SQL can select one kind without
	// enumerating its attributes.
	MembKindCpu     = NkRegistry.MustBegin("sysmKindCpu", 0).End()
	MembKindCpuInfo = NkRegistry.MustBegin("sysmKindCpuInfo", 1).End()
	MembKindMem     = NkRegistry.MustBegin("sysmKindMem", 2).End()

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
	MembCpuHost     = NkRegistry.MustBegin("sysmCpuHost", 3).End()
	MembCpuInfoHost = NkRegistry.MustBegin("sysmCpuInfoHost", 4).End()
	MembMemHost     = NkRegistry.MustBegin("sysmMemHost", 5).End()

	// CPU sample. Percentages are whole numbers in [0,100] as the collector
	// reports them; frequencies are MHz; the load averages are the kernel's own
	// 1/5/15-minute figures and not recomputed here.
	MembCpuTotalPct       = NkRegistry.MustBegin("sysmCpuTotalPct", 6).End()
	MembCpuPerCorePct     = NkRegistry.MustBegin("sysmCpuPerCorePct", 7).End()
	MembCpuPerCoreFreqMhz = NkRegistry.MustBegin("sysmCpuPerCoreFreqMhz", 8).End()
	MembCpuLoadAvg1       = NkRegistry.MustBegin("sysmCpuLoadAvg1", 9).End()
	MembCpuLoadAvg5       = NkRegistry.MustBegin("sysmCpuLoadAvg5", 10).End()
	MembCpuLoadAvg15      = NkRegistry.MustBegin("sysmCpuLoadAvg15", 11).End()
	// Package power over the most recent sample interval, from the RAPL energy
	// counter. Absent rather than zero where RAPL is unavailable, so "no reading"
	// and "idle" stay distinguishable.
	MembCpuUsageWatts = NkRegistry.MustBegin("sysmCpuUsageWatts", 12).End()
	// The cgroup v2 effective cpuset, as logical CPU indices. Absent where the
	// cgroup file is.
	MembCpuActiveCpus = NkRegistry.MustBegin("sysmCpuActiveCpus", 13).End()

	// CPU descriptor — read once per host rather than per tick (ADR-0184 §SD3).
	MembCpuModelName    = NkRegistry.MustBegin("sysmCpuModelName", 14).End()
	MembCpuLogicalCores = NkRegistry.MustBegin("sysmCpuLogicalCores", 15).End()

	// Memory sample. Every figure is absolute bytes — the collector scales the
	// kB lines of /proc/meminfo — so nothing downstream has to know which unit a
	// given field arrived in.
	MembMemTotalBytes     = NkRegistry.MustBegin("sysmMemTotalBytes", 16).End()
	MembMemFreeBytes      = NkRegistry.MustBegin("sysmMemFreeBytes", 17).End()
	MembMemAvailableBytes = NkRegistry.MustBegin("sysmMemAvailableBytes", 18).End()
	MembMemBuffersBytes   = NkRegistry.MustBegin("sysmMemBuffersBytes", 19).End()
	MembMemCachedBytes    = NkRegistry.MustBegin("sysmMemCachedBytes", 20).End()
	MembMemSwapTotalBytes = NkRegistry.MustBegin("sysmMemSwapTotalBytes", 21).End()
	MembMemSwapFreeBytes  = NkRegistry.MustBegin("sysmMemSwapFreeBytes", 22).End()
	// Used and SwapUsed are the collector's own derivations, kept because they
	// encode which fallback it applied (Available vs Free) — a reader cannot
	// recover that from the raw fields alone.
	MembMemUsedBytes     = NkRegistry.MustBegin("sysmMemUsedBytes", 23).End()
	MembMemSwapUsedBytes = NkRegistry.MustBegin("sysmMemSwapUsedBytes", 24).End()
	// ZFS ARC, absent unless the collector was built with it enabled and the
	// arcstats file is present.
	MembMemArcSizeBytes = NkRegistry.MustBegin("sysmMemArcSizeBytes", 25).End()
	MembMemArcMinBytes  = NkRegistry.MustBegin("sysmMemArcMinBytes", 26).End()

	// Sensitivity, declared before it has a writer (ADR-0090 §SD8, ADR-0184
	// §SD4). It tags attributes a later "untrusted" switch would mask at one
	// policy point — process command lines and usernames, which arrive with the
	// proc domain. Registered now so that when those rows are first written
	// they are already tagged, rather than there being a span of stored rows
	// the switch cannot see.
	MembSensitive = NkRegistry.MustBegin("sysmSensitive", 27).End()

	// --- ADR-0184 M4: the remaining scalar and per-item domains. ---
	//
	// Grouped after the block above for reading, not for the ids: each
	// registration below states its own ordinal, so where it sits changes
	// nothing.

	// PSI. Every figure is the kernel's own — the avg windows are already
	// percentages and the totals are cumulative microseconds, so neither is
	// recomputed here. `full` is all-zero for cpu on most kernels; it is stored
	// as read rather than dropped, because "the kernel reported zero" and "we
	// decided not to store it" are different facts.
	MembKindPsi = NkRegistry.MustBegin("sysmKindPsi", 28).End()
	MembPsiHost = NkRegistry.MustBegin("sysmPsiHost", 29).End()

	MembPsiCpuSomeAvg10   = NkRegistry.MustBegin("sysmPsiCpuSomeAvg10", 30).End()
	MembPsiCpuSomeAvg60   = NkRegistry.MustBegin("sysmPsiCpuSomeAvg60", 31).End()
	MembPsiCpuSomeAvg300  = NkRegistry.MustBegin("sysmPsiCpuSomeAvg300", 32).End()
	MembPsiCpuSomeTotalUs = NkRegistry.MustBegin("sysmPsiCpuSomeTotalUs", 33).End()
	MembPsiCpuFullAvg10   = NkRegistry.MustBegin("sysmPsiCpuFullAvg10", 34).End()
	MembPsiCpuFullAvg60   = NkRegistry.MustBegin("sysmPsiCpuFullAvg60", 35).End()
	MembPsiCpuFullAvg300  = NkRegistry.MustBegin("sysmPsiCpuFullAvg300", 36).End()
	MembPsiCpuFullTotalUs = NkRegistry.MustBegin("sysmPsiCpuFullTotalUs", 37).End()

	MembPsiMemorySomeAvg10   = NkRegistry.MustBegin("sysmPsiMemorySomeAvg10", 38).End()
	MembPsiMemorySomeAvg60   = NkRegistry.MustBegin("sysmPsiMemorySomeAvg60", 39).End()
	MembPsiMemorySomeAvg300  = NkRegistry.MustBegin("sysmPsiMemorySomeAvg300", 40).End()
	MembPsiMemorySomeTotalUs = NkRegistry.MustBegin("sysmPsiMemorySomeTotalUs", 41).End()
	MembPsiMemoryFullAvg10   = NkRegistry.MustBegin("sysmPsiMemoryFullAvg10", 42).End()
	MembPsiMemoryFullAvg60   = NkRegistry.MustBegin("sysmPsiMemoryFullAvg60", 43).End()
	MembPsiMemoryFullAvg300  = NkRegistry.MustBegin("sysmPsiMemoryFullAvg300", 44).End()
	MembPsiMemoryFullTotalUs = NkRegistry.MustBegin("sysmPsiMemoryFullTotalUs", 45).End()

	MembPsiIoSomeAvg10   = NkRegistry.MustBegin("sysmPsiIoSomeAvg10", 46).End()
	MembPsiIoSomeAvg60   = NkRegistry.MustBegin("sysmPsiIoSomeAvg60", 47).End()
	MembPsiIoSomeAvg300  = NkRegistry.MustBegin("sysmPsiIoSomeAvg300", 48).End()
	MembPsiIoSomeTotalUs = NkRegistry.MustBegin("sysmPsiIoSomeTotalUs", 49).End()
	MembPsiIoFullAvg10   = NkRegistry.MustBegin("sysmPsiIoFullAvg10", 50).End()
	MembPsiIoFullAvg60   = NkRegistry.MustBegin("sysmPsiIoFullAvg60", 51).End()
	MembPsiIoFullAvg300  = NkRegistry.MustBegin("sysmPsiIoFullAvg300", 52).End()
	MembPsiIoFullTotalUs = NkRegistry.MustBegin("sysmPsiIoFullTotalUs", 53).End()

	// Available distinguishes "kernel built without CONFIG_PSI" from "no
	// pressure". Without it every unsupported host would read as a perfectly
	// unstalled one.
	MembPsiAvailable = NkRegistry.MustBegin("sysmPsiAvailable", 54).End()

	// Network, one array element per interface. See the DTO for the alignment
	// contract the parallel arrays carry.
	MembKindNet = NkRegistry.MustBegin("sysmKindNet", 55).End()
	MembNetHost = NkRegistry.MustBegin("sysmNetHost", 56).End()

	MembNetName         = NkRegistry.MustBegin("sysmNetName", 57).End()
	MembNetIndex        = NkRegistry.MustBegin("sysmNetIndex", 58).End()
	MembNetHardwareAddr = NkRegistry.MustBegin("sysmNetHardwareAddr", 59).End()
	MembNetUp           = NkRegistry.MustBegin("sysmNetUp", 60).End()
	MembNetRunning      = NkRegistry.MustBegin("sysmNetRunning", 61).End()
	MembNetRxBytes      = NkRegistry.MustBegin("sysmNetRxBytes", 62).End()
	MembNetTxBytes      = NkRegistry.MustBegin("sysmNetTxBytes", 63).End()
	// The per-second rates are the collector's own derivation and are stored
	// alongside the raw counters rather than left to the reader: they
	// compensate for counter wrap on 32-bit virtual NICs, which a consumer
	// differencing the cumulative fields cannot detect after the fact.
	MembNetRxBytesPerSec = NkRegistry.MustBegin("sysmNetRxBytesPerSec", 64).End()
	MembNetTxBytesPerSec = NkRegistry.MustBegin("sysmNetTxBytesPerSec", 65).End()

	// Filesystem capacity, one array element per mount entry.
	MembKindDiskMount = NkRegistry.MustBegin("sysmKindDiskMount", 66).End()
	MembDiskMountHost = NkRegistry.MustBegin("sysmDiskMountHost", 67).End()

	MembDiskMountDevice     = NkRegistry.MustBegin("sysmDiskMountDevice", 68).End()
	MembDiskMountPoint      = NkRegistry.MustBegin("sysmDiskMountPoint", 69).End()
	MembDiskMountFsType     = NkRegistry.MustBegin("sysmDiskMountFsType", 70).End()
	MembDiskMountBlockName  = NkRegistry.MustBegin("sysmDiskMountBlockName", 71).End()
	MembDiskMountReal       = NkRegistry.MustBegin("sysmDiskMountReal", 72).End()
	MembDiskMountTotalBytes = NkRegistry.MustBegin("sysmDiskMountTotalBytes", 73).End()
	MembDiskMountFreeBytes  = NkRegistry.MustBegin("sysmDiskMountFreeBytes", 74).End()
	MembDiskMountUsedBytes  = NkRegistry.MustBegin("sysmDiskMountUsedBytes", 75).End()
	MembDiskMountUsedPct    = NkRegistry.MustBegin("sysmDiskMountUsedPct", 76).End()

	// Block-device I/O, one array element per device. A separate kind from the
	// mount table because the two lists have independent lengths — one entity
	// per aligned group keeps every array in a row the same length.
	MembKindDiskIo = NkRegistry.MustBegin("sysmKindDiskIo", 77).End()
	MembDiskIoHost = NkRegistry.MustBegin("sysmDiskIoHost", 78).End()

	MembDiskIoName             = NkRegistry.MustBegin("sysmDiskIoName", 79).End()
	MembDiskIoReadBytesPerSec  = NkRegistry.MustBegin("sysmDiskIoReadBytesPerSec", 80).End()
	MembDiskIoWriteBytesPerSec = NkRegistry.MustBegin("sysmDiskIoWriteBytesPerSec", 81).End()
	MembDiskIoBusyPct          = NkRegistry.MustBegin("sysmDiskIoBusyPct", 82).End()

	// Power supplies. Batteries and mains adapters are two independently
	// lengthed groups within one kind; each group's own arrays are aligned.
	MembKindBattery = NkRegistry.MustBegin("sysmKindBattery", 83).End()
	MembBatteryHost = NkRegistry.MustBegin("sysmBatteryHost", 84).End()

	MembBatteryName    = NkRegistry.MustBegin("sysmBatteryName", 85).End()
	MembBatteryType    = NkRegistry.MustBegin("sysmBatteryType", 86).End()
	MembBatteryPercent = NkRegistry.MustBegin("sysmBatteryPercent", 87).End()
	// State is the normalized kernel charge state, stored as its numeric code
	// rather than its label so the stored value cannot drift with a rename.
	MembBatteryState      = NkRegistry.MustBegin("sysmBatteryState", 88).End()
	MembBatteryPowerWatts = NkRegistry.MustBegin("sysmBatteryPowerWatts", 89).End()
	// The two remaining-time fields carry the collector's -1 sentinel for
	// "unknown or not in that state", which is why they are signed.
	MembBatterySecondsToFull  = NkRegistry.MustBegin("sysmBatterySecondsToFull", 90).End()
	MembBatterySecondsToEmpty = NkRegistry.MustBegin("sysmBatterySecondsToEmpty", 91).End()

	MembAcAdapterName   = NkRegistry.MustBegin("sysmAcAdapterName", 92).End()
	MembAcAdapterOnline = NkRegistry.MustBegin("sysmAcAdapterOnline", 93).End()

	// GPUs, one array element per device across all vendors.
	MembKindGpu = NkRegistry.MustBegin("sysmKindGpu", 94).End()
	MembGpuHost = NkRegistry.MustBegin("sysmGpuHost", 95).End()

	MembGpuVendor  = NkRegistry.MustBegin("sysmGpuVendor", 96).End()
	MembGpuIndex   = NkRegistry.MustBegin("sysmGpuIndex", 97).End()
	MembGpuName    = NkRegistry.MustBegin("sysmGpuName", 98).End()
	MembGpuPciId   = NkRegistry.MustBegin("sysmGpuPciId", 99).End()
	MembGpuBusyPct = NkRegistry.MustBegin("sysmGpuBusyPct", 100).End()
	// Memory, power, temperature and clock are 0 where the vendor exposes no
	// accounting for them; the collector does not distinguish that from a
	// genuine zero, so neither can a reader.
	MembGpuMemoryUsedBytes  = NkRegistry.MustBegin("sysmGpuMemoryUsedBytes", 101).End()
	MembGpuMemoryTotalBytes = NkRegistry.MustBegin("sysmGpuMemoryTotalBytes", 102).End()
	MembGpuPowerWatts       = NkRegistry.MustBegin("sysmGpuPowerWatts", 103).End()
	MembGpuTempC            = NkRegistry.MustBegin("sysmGpuTempC", 104).End()
	MembGpuFreqMhz          = NkRegistry.MustBegin("sysmGpuFreqMhz", 105).End()

	// --- ADR-0184 M5: the per-tick tables. ---

	// The process table, column-major like the M4 per-item domains. Nothing
	// here identifies a human or reveals what a process was invoked with — see
	// the sysmProcCmd* block for why that is a separate kind.
	MembKindProc = NkRegistry.MustBegin("sysmKindProc", 106).End()
	MembProcHost = NkRegistry.MustBegin("sysmProcHost", 107).End()

	MembProcPid = NkRegistry.MustBegin("sysmProcPid", 108).End()
	// PPID plus PID is what makes the table a forest rather than a list; a
	// process is only interpretable relative to its parent.
	MembProcPpid = NkRegistry.MustBegin("sysmProcPpid", 109).End()
	// Name is /proc/[pid]/comm — the kernel's own 15-character truncation, not
	// the command line.
	MembProcName = NkRegistry.MustBegin("sysmProcName", 110).End()
	// State is the single-letter Linux state (R/S/D/Z/T/I/…), stored as the
	// letter because that is what every kernel document and every operator
	// calls it.
	MembProcState = NkRegistry.MustBegin("sysmProcState", 111).End()
	// CPUPercent is per-CPU: a process pegging one core reads 100, one pegging
	// N cores reads N*100. It is not clamped, and a reader that clamps it loses
	// exactly the processes worth looking at.
	MembProcCpuPct       = NkRegistry.MustBegin("sysmProcCpuPct", 112).End()
	MembProcRssBytes     = NkRegistry.MustBegin("sysmProcRssBytes", 113).End()
	MembProcVmSizeBytes  = NkRegistry.MustBegin("sysmProcVmSizeBytes", 114).End()
	MembProcNumThreads   = NkRegistry.MustBegin("sysmProcNumThreads", 115).End()
	MembProcNice         = NkRegistry.MustBegin("sysmProcNice", 116).End()
	MembProcPriority     = NkRegistry.MustBegin("sysmProcPriority", 117).End()
	MembProcKernelThread = NkRegistry.MustBegin("sysmProcKernelThread", 118).End()
	// StartedAt is what makes a pid unambiguous over time: pids are reused, so
	// (pid, startedAt) is the identity a history query needs.
	MembProcStartedAtMs = NkRegistry.MustBegin("sysmProcStartedAtMs", 119).End()
	// The two ADR-0126 topology marks: the cooperative BOXER_COMPONENT value
	// and the kernel-maintained systemd unit that corroborates it.
	MembProcComponent  = NkRegistry.MustBegin("sysmProcComponent", 120).End()
	MembProcCgroupUnit = NkRegistry.MustBegin("sysmProcCgroupUnit", 121).End()

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
	MembKindProcCmd = NkRegistry.MustBegin("sysmKindProcCmd", 122).End()
	MembProcCmdHost = NkRegistry.MustBegin("sysmProcCmdHost", 123).End()
	// Pid repeats here because this kind's arrays are aligned among themselves,
	// not with the sysmProc* kind's — the two are separate entities and a
	// reader joins them on pid.
	MembProcCmdPid  = NkRegistry.MustBegin("sysmProcCmdPid", 124).End()
	MembProcCmdLine = NkRegistry.MustBegin("sysmProcCmdLine", 125).End()
	MembProcCmdUser = NkRegistry.MustBegin("sysmProcCmdUser", 126).End()
	MembProcCmdUid  = NkRegistry.MustBegin("sysmProcCmdUid", 127).End()
	MembProcCmdGid  = NkRegistry.MustBegin("sysmProcCmdGid", 128).End()

	// Listening sockets (ADR-0126 observed topology). The collector samples on
	// its own slower cadence and consecutive bundles repeat one snapshot, so
	// the tee writes a row only when the collection stamp advances.
	MembKindSocket = NkRegistry.MustBegin("sysmKindSocket", 129).End()
	MembSocketHost = NkRegistry.MustBegin("sysmSocketHost", 130).End()

	MembSocketProto = NkRegistry.MustBegin("sysmSocketProto", 131).End()
	// Addr is an IP literal for inet sockets and a filesystem or @abstract path
	// for unix ones; Port is 0 for unix.
	MembSocketAddr = NkRegistry.MustBegin("sysmSocketAddr", 132).End()
	MembSocketPort = NkRegistry.MustBegin("sysmSocketPort", 133).End()
	// Inode is the join key the fd walk attributes pids by, kept so an
	// unattributed row can still be correlated later.
	MembSocketInode = NkRegistry.MustBegin("sysmSocketInode", 134).End()
	MembSocketUid   = NkRegistry.MustBegin("sysmSocketUid", 135).End()
	// Pid is 0 where the owning process's fd table was unreadable. Partial over
	// absent: the row is published anyway (ADR-0126 §SD3), so a zero here means
	// "not attributed", not "owned by pid 0".
	MembSocketPid = NkRegistry.MustBegin("sysmSocketPid", 136).End()

	// --- ADR-0184 M6: the CPU containment tree. ---
	//
	// The tree is stored as an adjacency list: a pre-order walk numbers the
	// nodes and each carries its parent's number. Parallel arrays cannot hold a
	// recursive shape directly, and the alternatives are worse — a serialized
	// blob would put the structure beyond SQL entirely, which is the opposite
	// of what modelling it as facts is for. NodeIdx plus ParentIdx reconstruct
	// the tree exactly, and a recursive CTE walks it.
	MembKindTopology = NkRegistry.MustBegin("sysmKindTopology", 137).End()
	MembTopologyHost = NkRegistry.MustBegin("sysmTopologyHost", 138).End()

	// NodeIdx is stored rather than left implicit in array position: the moment
	// a query filters the arrays — "just the PUs" — position is lost and the
	// parent references would dangle.
	MembTopoNodeIdx = NkRegistry.MustBegin("sysmTopoNodeIdx", 139).End()
	// ParentIdx is -1 for the root, which is the only node without one.
	MembTopoParentIdx = NkRegistry.MustBegin("sysmTopoParentIdx", 140).End()
	// Kind is the hwloc-style name (Machine/Package/NUMANode/Cache/Core/PU),
	// stored as the name rather than its enum ordinal so a row stays readable
	// and cannot drift if the enum is reordered.
	MembTopoKind = NkRegistry.MustBegin("sysmTopoKind", 141).End()
	// OSIndex is the kernel's own id for the object and is -1 for Machine and
	// Cache, which have no single id.
	MembTopoOsIndex = NkRegistry.MustBegin("sysmTopoOsIndex", 142).End()

	// Cache attributes, meaningful only on Cache nodes.
	MembTopoCacheLevel     = NkRegistry.MustBegin("sysmTopoCacheLevel", 143).End()
	MembTopoCacheType      = NkRegistry.MustBegin("sysmTopoCacheType", 144).End()
	MembTopoCacheSizeBytes = NkRegistry.MustBegin("sysmTopoCacheSizeBytes", 145).End()

	// Node-local RAM, meaningful only on NUMANode nodes.
	MembTopoMemBytes = NkRegistry.MustBegin("sysmTopoMemBytes", 146).End()

	// The cpufreq policy, meaningful only on PU nodes. Present is carried
	// separately because a PU whose cpufreq read failed and a PU with a policy
	// reporting zeroes are otherwise the same row.
	MembTopoFreqPresent  = NkRegistry.MustBegin("sysmTopoFreqPresent", 147).End()
	MembTopoFreqMinMhz   = NkRegistry.MustBegin("sysmTopoFreqMinMhz", 148).End()
	MembTopoFreqMaxMhz   = NkRegistry.MustBegin("sysmTopoFreqMaxMhz", 149).End()
	MembTopoFreqGovernor = NkRegistry.MustBegin("sysmTopoFreqGovernor", 150).End()
	MembTopoFreqDriver   = NkRegistry.MustBegin("sysmTopoFreqDriver", 151).End()

	// LogicalCount is the collector's own count of online PU leaves. It is
	// stored rather than derived from the node arrays because it is what the
	// collector observed, and a mismatch between the two is itself a finding.
	MembTopoLogicalCount = NkRegistry.MustBegin("sysmTopoLogicalCount", 152).End()
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
