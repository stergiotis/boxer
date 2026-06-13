package cpu

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

// TopoKindE enumerates the object kinds in a [Topology] tree, mirroring the
// subset of hwloc object types we read from sysfs.
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

// suffix returns the lstopo-style cache-type suffix ("d", "i", "").
func (inst CacheTypeE) suffix() (s string) {
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
// Zero value is not used directly — construct via [ReadTopology].
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
		return "L" + strconv.FormatInt(int64(inst.CacheLevel), 10) + inst.CacheType.suffix() +
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
// from sysfs. Unlike [Snapshot] it carries no time-varying fields and is not
// part of the sampling path — call [ReadTopology] once.
type Topology struct {
	// Root is the Machine object; its subtree is the full hierarchy.
	Root *TopoObject
	// LogicalCount is the number of online logical CPUs (PU leaves).
	LogicalCount int32
}

// TopologyOptions configures [ReadTopology].
type TopologyOptions struct {
	// Sys is the sysfs reader. A nil reader defaults to sysfs.New("")
	// (root "/sys"), matching [Options.Sys].
	Sys *sysfs.Reader
}

// ReadTopology reads the CPU package / NUMA / cache / core / thread hierarchy
// from sysfs and returns it as a [Topology]. It is a one-shot structural read:
// the result does not change while the machine is running, so callers read it
// once (e.g. at startup) rather than on a sampling cadence.
//
// Sources, all under the reader's root ("/sys"):
//   - devices/system/cpu/online                       — the online CPU set
//   - devices/system/cpu/cpuN/topology/{physical_package_id,core_id,thread_siblings_list}
//   - devices/system/cpu/cpuN/cache/indexN/{level,type,size,shared_cpu_list}
//   - devices/system/node/nodeN/cpulist               — NUMA grouping
//
// Caches and NUMA nodes are optional: in a container or a stripped kernel the
// respective directories may be absent, and the corresponding levels are
// simply omitted from the tree (cores then attach directly to their package).
//
// Limitation: NUMA nodes are nested within their package by CPU-set
// intersection. On the common topologies (one package per NUMA node, or
// sub-NUMA clustering within a package) this is correct; a NUMA node that
// spans multiple packages (legacy node-interleave) is not modelled in v1.
func ReadTopology(opts TopologyOptions) (topo Topology, err error) {
	sys := opts.Sys
	if sys == nil {
		sys = sysfs.New("")
	}

	var onlineRaw string
	onlineRaw, err = sys.ReadString("devices/system/cpu/online")
	if err != nil {
		err = eb.Build().Errorf("read online CPU set: %w", err)
		return
	}
	online := parseCPUSet(onlineRaw)
	if len(online) == 0 {
		err = eb.Build().Str("online", onlineRaw).Errorf("no online CPUs")
		return
	}

	// cpu id -> node id, and node id -> local RAM bytes; both nil when the
	// NUMA sysfs tree is absent.
	numaByCPU, nodeMem := readNUMA(sys)

	b := newTopoBuilder()
	for _, c := range online {
		var steps []topoStep
		steps, err = readCPUSteps(sys, c, numaByCPU, nodeMem)
		if err != nil {
			err = eb.Build().Int32("cpu", c).Errorf("read cpu topology: %w", err)
			return
		}
		b.insert(steps)
	}

	topo.Root = b.root
	topo.LogicalCount = int32(len(online))
	return
}

// topoStep is one rung of a single CPU's containment chain, from widest
// (package) to narrowest (the PU itself). Steps are emitted already ordered
// outermost-first by readCPUSteps.
type topoStep struct {
	kind       TopoKindE
	osIndex    int32
	level      uint8
	ctype      CacheTypeE
	size       uint64
	mem        uint64
	freqPolicy *FreqPolicy
	// key uniquely identifies the object among its siblings: it folds the
	// covered CPU set together with the cache discriminators so two CPUs that
	// share, say, an L3 dedupe to the same node while L1d and L1i (same set,
	// same level) stay distinct.
	key string
}

// readCPUSteps builds the outermost-first containment chain for one logical
// CPU: Package, [NUMANode], caches (level-descending), Core, PU. The fixed
// order respects physical CPU-set inclusion on standard topologies.
func readCPUSteps(sys *sysfs.Reader, cpu int32, numaByCPU map[int32]int32, nodeMem map[int32]uint64) (steps []topoStep, err error) {
	base := "devices/system/cpu/cpu" + strconv.FormatInt(int64(cpu), 10)

	pkgID, err := readInt32(sys, base+"/topology/physical_package_id")
	if err != nil {
		return
	}
	pkgSet, err := sys.ReadString(base + "/topology/package_cpus_list")
	if err != nil {
		// Fallback to core_siblings_list (older kernels).
		pkgSet, err = sys.ReadString(base + "/topology/core_siblings_list")
		if err != nil {
			return
		}
	}
	coreID, err := readInt32(sys, base+"/topology/core_id")
	if err != nil {
		return
	}
	threadSet, err := sys.ReadString(base + "/topology/thread_siblings_list")
	if err != nil {
		return
	}

	steps = append(steps, topoStep{
		kind: TopoKindPackage, osIndex: pkgID,
		key: "pkg:" + setKey(pkgSet),
	})

	if numaByCPU != nil {
		if node, ok := numaByCPU[cpu]; ok {
			steps = append(steps, topoStep{
				kind: TopoKindNUMANode, osIndex: node, mem: nodeMem[node],
				key: "numa:" + strconv.FormatInt(int64(node), 10),
			})
		}
	}

	caches, err := readCaches(sys, base+"/cache")
	if err != nil {
		return
	}
	for _, ca := range caches {
		steps = append(steps, ca)
	}

	steps = append(steps, topoStep{
		kind: TopoKindCore, osIndex: coreID,
		key: "core:" + setKey(threadSet),
	})
	steps = append(steps, topoStep{
		kind: TopoKindPU, osIndex: cpu, freqPolicy: readPUFreqPolicy(sys, cpu),
		key: "pu:" + strconv.FormatInt(int64(cpu), 10),
	})
	return
}

// readCaches reads cpuN/cache/index* and returns cache steps sorted
// level-descending (L3 outermost) so they slot into the containment chain
// between NUMA and Core. Same-level caches are ordered Unified, Data,
// Instruction for determinism.
func readCaches(sys *sysfs.Reader, cacheDir string) (steps []topoStep, err error) {
	names, lerr := sys.ListDir(cacheDir)
	if lerr != nil {
		// No cache directory (container / stripped kernel): not an error.
		return nil, nil
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "index") {
			continue
		}
		idx := cacheDir + "/" + name
		levelStr, e := sys.ReadString(idx + "/level")
		if e != nil {
			continue // not a real cache leaf (e.g. uevent)
		}
		level, e := strconv.ParseUint(strings.TrimSpace(levelStr), 10, 8)
		if e != nil {
			continue
		}
		typeStr, _ := sys.ReadString(idx + "/type")
		sizeStr, _ := sys.ReadString(idx + "/size")
		sharedStr, e := sys.ReadString(idx + "/shared_cpu_list")
		if e != nil {
			continue
		}
		ctype := parseCacheType(typeStr)
		steps = append(steps, topoStep{
			kind:  TopoKindCache,
			level: uint8(level),
			ctype: ctype,
			size:  parseCacheSize(sizeStr),
			// Include level+type in the key so L1d and L1i (identical CPU
			// set) remain distinct nodes.
			key: "cache:" + strconv.FormatUint(level, 10) + ctype.suffix() + ":" + setKey(sharedStr),
		})
	}
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].level != steps[j].level {
			return steps[i].level > steps[j].level // L3 before L2 before L1
		}
		return steps[i].ctype < steps[j].ctype // Unified, Data, Instruction
	})
	return
}

// topoBuilder assembles the tree by inserting each CPU's chain and deduping
// nodes that share an identity key with an existing sibling.
type topoBuilder struct {
	root     *TopoObject
	children map[*TopoObject]map[string]*TopoObject
}

func newTopoBuilder() (b *topoBuilder) {
	return &topoBuilder{
		root:     &TopoObject{Kind: TopoKindMachine, OSIndex: -1},
		children: make(map[*TopoObject]map[string]*TopoObject),
	}
}

// insert walks one CPU's outermost-first chain, reusing existing sibling
// nodes by key and creating new ones as needed.
func (b *topoBuilder) insert(steps []topoStep) {
	parent := b.root
	for _, s := range steps {
		idx := b.children[parent]
		if idx == nil {
			idx = make(map[string]*TopoObject)
			b.children[parent] = idx
		}
		child := idx[s.key]
		if child == nil {
			osIdx := s.osIndex
			if s.kind == TopoKindCache {
				osIdx = -1
			}
			child = &TopoObject{
				Kind:           s.kind,
				OSIndex:        osIdx,
				CacheLevel:     s.level,
				CacheType:      s.ctype,
				CacheSizeBytes: s.size,
				MemBytes:       s.mem,
				FreqPolicy:     s.freqPolicy,
			}
			parent.Children = append(parent.Children, child)
			idx[s.key] = child
		}
		parent = child
	}
}

// readNUMA returns a cpu-id -> node-id map and a node-id -> local-RAM-bytes
// map, or nil maps when the NUMA sysfs tree is absent (UMA machines or
// stripped kernels). Memory comes from each node's meminfo MemTotal.
func readNUMA(sys *sysfs.Reader) (cpuNode map[int32]int32, nodeMem map[int32]uint64) {
	names, err := sys.ListDir("devices/system/node")
	if err != nil {
		return nil, nil
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "node") {
			continue
		}
		node, perr := strconv.ParseInt(strings.TrimPrefix(name, "node"), 10, 32)
		if perr != nil {
			continue
		}
		if list, rerr := sys.ReadString("devices/system/node/" + name + "/cpulist"); rerr == nil {
			for _, c := range parseCPUSet(list) {
				if cpuNode == nil {
					cpuNode = make(map[int32]int32)
				}
				cpuNode[c] = int32(node)
			}
		}
		if mb := readNodeMemTotal(sys, name); mb > 0 {
			if nodeMem == nil {
				nodeMem = make(map[int32]uint64)
			}
			nodeMem[int32(node)] = mb
		}
	}
	return
}

// readNodeMemTotal parses a node's meminfo for the MemTotal field (reported in
// kB) and returns it in bytes; 0 when the file or field is absent.
func readNodeMemTotal(sys *sysfs.Reader, nodeName string) (bytes uint64) {
	data, err := sys.ReadString("devices/system/node/" + nodeName + "/meminfo")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(data, "\n") {
		_, after, ok := strings.Cut(line, "MemTotal:")
		if !ok {
			continue
		}
		fields := strings.Fields(after)
		if len(fields) >= 1 {
			if kb, perr := strconv.ParseUint(fields[0], 10, 64); perr == nil {
				return kb * 1024
			}
		}
		break
	}
	return 0
}

// readPUFreqPolicy reads cpuN/cpufreq/{scaling_min_freq,scaling_max_freq,
// scaling_governor,scaling_driver}. Returns nil when the CPU exposes no
// cpufreq policy (container, cpufreq disabled, or a non-cpufreq kernel).
func readPUFreqPolicy(sys *sysfs.Reader, cpu int32) (p *FreqPolicy) {
	base := "devices/system/cpu/cpu" + strconv.FormatInt(int64(cpu), 10) + "/cpufreq/"
	gov, _ := sys.ReadString(base + "scaling_governor")
	drv, _ := sys.ReadString(base + "scaling_driver")
	minKHz := readUintFile(sys, base+"scaling_min_freq")
	maxKHz := readUintFile(sys, base+"scaling_max_freq")
	if gov == "" && drv == "" && minKHz == 0 && maxKHz == 0 {
		return nil
	}
	return &FreqPolicy{
		MinMHz:   uint32(minKHz / 1000),
		MaxMHz:   uint32(maxKHz / 1000),
		Governor: strings.TrimSpace(gov),
		Driver:   strings.TrimSpace(drv),
	}
}

// readUintFile reads a sysfs leaf holding a single unsigned integer; 0 on
// error or absence.
func readUintFile(sys *sysfs.Reader, rel string) (v uint64) {
	s, err := sys.ReadString(rel)
	if err != nil {
		return 0
	}
	n, perr := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if perr != nil {
		return 0
	}
	return n
}

// readInt32 reads a sysfs leaf holding a single integer.
func readInt32(sys *sysfs.Reader, rel string) (v int32, err error) {
	s, err := sys.ReadString(rel)
	if err != nil {
		return
	}
	n, perr := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if perr != nil {
		err = eb.Build().Str("path", rel).Str("value", s).Errorf("parse int: %w", perr)
		return
	}
	v = int32(n)
	return
}

// setKey canonicalises a sysfs cpulist (e.g. "0-15", "0,2,4") into a stable
// dedupe key by parsing and re-emitting the sorted CPU ids.
func setKey(cpulist string) (key string) {
	cpus := parseCPUSet(cpulist)
	slices.Sort(cpus)
	var sb strings.Builder
	for i, c := range cpus {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatInt(int64(c), 10))
	}
	return sb.String()
}

// parseCacheType maps the sysfs cache `type` string to a [CacheTypeE].
func parseCacheType(s string) (t CacheTypeE) {
	switch strings.TrimSpace(s) {
	case "Data":
		return CacheTypeData
	case "Instruction":
		return CacheTypeInstruction
	default:
		return CacheTypeUnified
	}
}

// parseCacheSize parses a sysfs cache `size` string ("48K", "1024K", "32M")
// into bytes. An unrecognised value yields 0.
func parseCacheSize(s string) (bytes uint64) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := uint64(1)
	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'M', 'm':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'G', 'g':
		mult = 1 << 30
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
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
