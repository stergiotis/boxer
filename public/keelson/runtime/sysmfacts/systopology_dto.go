package sysmfacts

import "time"

// SysTopology is the CPU containment tree of one host, stored as an adjacency
// list: a pre-order walk numbers the nodes, and each node carries its parent's
// number.
//
// # Why an adjacency list
//
// The tree is the one shape in this vocabulary that parallel arrays cannot hold
// directly, and it is recursive, so no fixed nesting expresses it either. The
// alternative — a serialized blob in one column — would put the structure
// beyond SQL entirely, which is the opposite of what modelling metrics as facts
// is for. Node and Parent reconstruct the tree exactly, and a recursive CTE
// walks it.
//
// Node is stored rather than left implicit in array position because the moment
// a query filters the arrays — "just the PUs" — position is lost and the parent
// references would dangle.
//
// # Written once per host
//
// The topology is static: the collector reads it once from sysfs and stamps the
// same value onto every snapshot. The tee writes it on first sight of a host,
// like [SysCpuInfo]. Every kind here is append-shaped, so a re-write after a
// restart is harmless rather than something to guard against.
//
// Index i of every array describes the same node — see [SysNet] for the
// alignment contract.
type SysTopology struct {
	_ struct{} `kind:"sysTopology"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindTopology,symbol"`
	Host string `lw:"sysmTopologyHost,symbol"`

	Node []uint32 `lw:"sysmTopoNodeIdx,u32Array"`
	// Parent is -1 for the root, the only node without one.
	Parent []int32 `lw:"sysmTopoParentIdx,i32Array"`
	// NodeKind is the hwloc-style name (Machine/Package/NUMANode/Cache/Core/PU),
	// stored as the name rather than the enum ordinal so a row stays readable
	// and cannot drift if the enum is reordered.
	NodeKind []string `lw:"sysmTopoKind,symbolArray"`
	// OSIndex is the kernel's own id for the object: -1 for Machine and Cache,
	// which have no single id.
	OSIndex []int32 `lw:"sysmTopoOsIndex,i32Array"`

	// Cache attributes, meaningful only where NodeKind is "Cache".
	CacheLevel     []uint8  `lw:"sysmTopoCacheLevel,u8Array,ct=u8h"`
	CacheType      []string `lw:"sysmTopoCacheType,symbolArray"`
	CacheSizeBytes []uint64 `lw:"sysmTopoCacheSizeBytes,u64Array"`

	// Node-local RAM, meaningful only where NodeKind is "NUMANode".
	MemBytes []uint64 `lw:"sysmTopoMemBytes,u64Array"`

	// The cpufreq policy, meaningful only where NodeKind is "PU". FreqPresent is
	// carried because a PU whose cpufreq read failed and a PU with a policy
	// reporting zeroes are otherwise the same row.
	FreqPresent  []uint8  `lw:"sysmTopoFreqPresent,u8Array,ct=u8h"`
	FreqMinMHz   []uint32 `lw:"sysmTopoFreqMinMhz,u32Array"`
	FreqMaxMHz   []uint32 `lw:"sysmTopoFreqMaxMhz,u32Array"`
	FreqGovernor []string `lw:"sysmTopoFreqGovernor,symbolArray"`
	FreqDriver   []string `lw:"sysmTopoFreqDriver,symbolArray"`

	// LogicalCount is the collector's own count of online PU leaves, stored
	// rather than derived from the arrays because it is what the collector
	// observed — a mismatch between the two is itself a finding.
	LogicalCount int32 `lw:"sysmTopoLogicalCount,i32Array,unit"`
}
