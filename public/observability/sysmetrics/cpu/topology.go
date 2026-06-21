package cpu

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// TopologyOptions configures [ReadTopology].
type TopologyOptions struct {
	// Sys is the sysfs reader. A nil reader defaults to sysfs.New("")
	// (root "/sys"), matching [Options.Sys].
	Sys *sysfs.Reader
}

// ReadTopology reads the CPU package / NUMA / cache / core / thread hierarchy
// from sysfs and returns it as a [sysmsnap.Topology]. It is a one-shot
// structural read: the result does not change while the machine is running, so
// callers read it once (e.g. at startup) rather than on a sampling cadence.
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
func ReadTopology(opts TopologyOptions) (topo sysmsnap.Topology, err error) {
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
	kind       sysmsnap.TopoKindE
	osIndex    int32
	level      uint8
	ctype      sysmsnap.CacheTypeE
	size       uint64
	mem        uint64
	freqPolicy *sysmsnap.FreqPolicy
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
		kind: sysmsnap.TopoKindPackage, osIndex: pkgID,
		key: "pkg:" + setKey(pkgSet),
	})

	if numaByCPU != nil {
		if node, ok := numaByCPU[cpu]; ok {
			steps = append(steps, topoStep{
				kind: sysmsnap.TopoKindNUMANode, osIndex: node, mem: nodeMem[node],
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
		kind: sysmsnap.TopoKindCore, osIndex: coreID,
		key: "core:" + setKey(threadSet),
	})
	steps = append(steps, topoStep{
		kind: sysmsnap.TopoKindPU, osIndex: cpu, freqPolicy: readPUFreqPolicy(sys, cpu),
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
			kind:  sysmsnap.TopoKindCache,
			level: uint8(level),
			ctype: ctype,
			size:  parseCacheSize(sizeStr),
			// Include level+type in the key so L1d and L1i (identical CPU
			// set) remain distinct nodes.
			key: "cache:" + strconv.FormatUint(level, 10) + ctype.Suffix() + ":" + setKey(sharedStr),
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
	root     *sysmsnap.TopoObject
	children map[*sysmsnap.TopoObject]map[string]*sysmsnap.TopoObject
}

func newTopoBuilder() (b *topoBuilder) {
	return &topoBuilder{
		root:     &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindMachine, OSIndex: -1},
		children: make(map[*sysmsnap.TopoObject]map[string]*sysmsnap.TopoObject),
	}
}

// insert walks one CPU's outermost-first chain, reusing existing sibling
// nodes by key and creating new ones as needed.
func (b *topoBuilder) insert(steps []topoStep) {
	parent := b.root
	for _, s := range steps {
		idx := b.children[parent]
		if idx == nil {
			idx = make(map[string]*sysmsnap.TopoObject)
			b.children[parent] = idx
		}
		child := idx[s.key]
		if child == nil {
			osIdx := s.osIndex
			if s.kind == sysmsnap.TopoKindCache {
				osIdx = -1
			}
			child = &sysmsnap.TopoObject{
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
func readPUFreqPolicy(sys *sysfs.Reader, cpu int32) (p *sysmsnap.FreqPolicy) {
	base := "devices/system/cpu/cpu" + strconv.FormatInt(int64(cpu), 10) + "/cpufreq/"
	gov, _ := sys.ReadString(base + "scaling_governor")
	drv, _ := sys.ReadString(base + "scaling_driver")
	minKHz := readUintFile(sys, base+"scaling_min_freq")
	maxKHz := readUintFile(sys, base+"scaling_max_freq")
	if gov == "" && drv == "" && minKHz == 0 && maxKHz == 0 {
		return nil
	}
	return &sysmsnap.FreqPolicy{
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

// parseCacheType maps the sysfs cache `type` string to a [sysmsnap.CacheTypeE].
func parseCacheType(s string) (t sysmsnap.CacheTypeE) {
	switch strings.TrimSpace(s) {
	case "Data":
		return sysmsnap.CacheTypeData
	case "Instruction":
		return sysmsnap.CacheTypeInstruction
	default:
		return sysmsnap.CacheTypeUnified
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
