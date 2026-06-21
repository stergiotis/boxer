package sysmsnap

import "strconv"

// TopoKindE enumerates the object kinds in a [Topology] tree, mirroring the
// subset of hwloc object types the cpu collector reads from sysfs.
type TopoKindE uint8

const (
	// TopoKindMachine is the synthetic root spanning every online CPU.
	TopoKindMachine TopoKindE = iota
	// TopoKindPackage is a physical package (socket); physical_package_id.
	TopoKindPackage
	// TopoKindNUMANode is a NUMA node (/sys/devices/system/node/nodeN).
	TopoKindNUMANode
	// TopoKindCache is a CPU cache level (L1/L2/L3, data/instruction/unified).
	TopoKindCache
	// TopoKindCore is a physical core; core_id within a package.
	TopoKindCore
	// TopoKindPU is a processing unit — one logical CPU / hardware thread.
	TopoKindPU
)

// String returns the short hwloc-style name of the kind.
func (inst TopoKindE) String() (s string) {
	switch inst {
	case TopoKindMachine:
		return "Machine"
	case TopoKindPackage:
		return "Package"
	case TopoKindNUMANode:
		return "NUMANode"
	case TopoKindCache:
		return "Cache"
	case TopoKindCore:
		return "Core"
	case TopoKindPU:
		return "PU"
	default:
		return "Unknown"
	}
}

// CacheTypeE distinguishes the sysfs cache `type` values.
type CacheTypeE uint8

const (
	// CacheTypeUnified is a combined data+instruction cache ("Unified").
	CacheTypeUnified CacheTypeE = iota
	// CacheTypeData is a data cache ("Data").
	CacheTypeData
	// CacheTypeInstruction is an instruction cache ("Instruction").
	CacheTypeInstruction
)

// Suffix returns the lstopo-style cache-type suffix ("d", "i", "").
func (inst CacheTypeE) Suffix() (s string) {
	switch inst {
	case CacheTypeData:
		return "d"
	case CacheTypeInstruction:
		return "i"
	default:
		return ""
	}
}

// TopoObject is one node in the static CPU containment tree. The tree is
// uniform (every node is a TopoObject) so it maps 1:1 onto a renderer's
// node type; Kind discriminates which of the optional fields are meaningful.
//
// Zero value is not used directly — the cpu collector's topology reader
// populates it.
type TopoObject struct {
	Kind TopoKindE

	// OSIndex is the kernel-reported id for the object: physical_package_id
	// (Package), NUMA node id (NUMANode), core_id (Core), or the logical CPU
	// id (PU). It is -1 for Machine and Cache, which have no single id.
	OSIndex int32

	// CacheLevel / CacheType / CacheSizeBytes are populated only when
	// Kind == TopoKindCache.
	CacheLevel     uint8
	CacheType      CacheTypeE
	CacheSizeBytes uint64

	// MemBytes is the node-local RAM in bytes (MemTotal from the node's
	// meminfo); set only for Kind == TopoKindNUMANode, 0 otherwise.
	MemBytes uint64

	// FreqPolicy is the cpufreq scaling policy; set only for Kind ==
	// TopoKindPU, nil otherwise.
	FreqPolicy *FreqPolicy

	// Children are the contained objects, in discovery order.
	Children []*TopoObject
}

// FreqPolicy is a CPU's cpufreq scaling policy (governor, driver, and the
// min/max clock the governor may select), read once from cpuN/cpufreq.
type FreqPolicy struct {
	MinMHz   uint32
	MaxMHz   uint32
	Governor string
	Driver   string
}

// Label returns a human-readable lstopo-style label, e.g. "Package P#0",
// "L3 (32 MiB)", "Core P#5", "PU P#11".
func (inst *TopoObject) Label() (s string) {
	switch inst.Kind {
	case TopoKindMachine:
		return "Machine"
	case TopoKindPackage:
		return "Package P#" + strconv.FormatInt(int64(inst.OSIndex), 10)
	case TopoKindNUMANode:
		return "NUMANode #" + strconv.FormatInt(int64(inst.OSIndex), 10)
	case TopoKindCache:
		return "L" + strconv.FormatInt(int64(inst.CacheLevel), 10) + inst.CacheType.Suffix() +
			" (" + cacheSizeLabel(inst.CacheSizeBytes) + ")"
	case TopoKindCore:
		return "Core P#" + strconv.FormatInt(int64(inst.OSIndex), 10)
	case TopoKindPU:
		return "PU P#" + strconv.FormatInt(int64(inst.OSIndex), 10)
	default:
		return inst.Kind.String()
	}
}

// Topology is a static snapshot of the CPU containment hierarchy, read once
// from sysfs. Unlike [CPUSnapshot] it carries no time-varying fields and is
// not part of the sampling path — the cpu collector reads it once.
type Topology struct {
	// Root is the Machine object; its subtree is the full hierarchy.
	Root *TopoObject
	// LogicalCount is the number of online logical CPUs (PU leaves).
	LogicalCount int32
}

// cacheSizeLabel renders a cache size in binary units (KiB/MiB), preferring
// the largest unit that divides evenly.
func cacheSizeLabel(bytes uint64) (s string) {
	switch {
	case bytes == 0:
		return "?"
	case bytes%(1<<20) == 0:
		return strconv.FormatUint(bytes>>20, 10) + " MiB"
	case bytes%(1<<10) == 0:
		return strconv.FormatUint(bytes>>10, 10) + " KiB"
	default:
		return strconv.FormatUint(bytes, 10) + " B"
	}
}
