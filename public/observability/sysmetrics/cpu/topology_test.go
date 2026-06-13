package cpu_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

// mustWrite writes content to path, creating parent directories.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func writeCacheIndex(t *testing.T, cacheDir, index, level, ctype, size, shared string) {
	t.Helper()
	dir := filepath.Join(cacheDir, index)
	mustWrite(t, filepath.Join(dir, "level"), level+"\n")
	mustWrite(t, filepath.Join(dir, "type"), ctype+"\n")
	mustWrite(t, filepath.Join(dir, "size"), size+"\n")
	mustWrite(t, filepath.Join(dir, "shared_cpu_list"), shared+"\n")
}

// writeTopoFixture lays out an 8-PU machine: 1 package, 1 NUMA node, two L3
// groups (0-3 / 4-7), 4 cores × 2 SMT threads, per-core L2 + L1d + L1i.
// Mirrors this repo's dual-CCX shape at small scale.
func writeTopoFixture(t *testing.T) (sysRoot string) {
	t.Helper()
	sysRoot = filepath.Join(t.TempDir(), "sys")
	cpuBase := filepath.Join(sysRoot, "devices/system/cpu")
	mustWrite(t, filepath.Join(cpuBase, "online"), "0-7\n")

	l3Set := func(i int) string {
		if i < 4 {
			return "0-3"
		}
		return "4-7"
	}
	for i := range 8 {
		core := i / 2
		pair := fmt.Sprintf("%d-%d", core*2, core*2+1)
		topo := filepath.Join(cpuBase, fmt.Sprintf("cpu%d/topology", i))
		mustWrite(t, filepath.Join(topo, "physical_package_id"), "0\n")
		mustWrite(t, filepath.Join(topo, "core_id"), strconv.Itoa(core)+"\n")
		mustWrite(t, filepath.Join(topo, "package_cpus_list"), "0-7\n")
		mustWrite(t, filepath.Join(topo, "thread_siblings_list"), pair+"\n")

		cache := filepath.Join(cpuBase, fmt.Sprintf("cpu%d/cache", i))
		writeCacheIndex(t, cache, "index0", "1", "Data", "32K", pair)
		writeCacheIndex(t, cache, "index1", "1", "Instruction", "32K", pair)
		writeCacheIndex(t, cache, "index2", "2", "Unified", "512K", pair)
		writeCacheIndex(t, cache, "index3", "3", "Unified", "8192K", l3Set(i))
		// A non-cache entry that must be ignored (no level file).
		mustWrite(t, filepath.Join(cache, "uevent"), "DEVTYPE=cache\n")

		freq := filepath.Join(cpuBase, fmt.Sprintf("cpu%d/cpufreq", i))
		mustWrite(t, filepath.Join(freq, "scaling_min_freq"), "1000000\n")
		mustWrite(t, filepath.Join(freq, "scaling_max_freq"), "4000000\n")
		mustWrite(t, filepath.Join(freq, "scaling_governor"), "schedutil\n")
		mustWrite(t, filepath.Join(freq, "scaling_driver"), "acpi-cpufreq\n")
	}
	mustWrite(t, filepath.Join(sysRoot, "devices/system/node/node0/cpulist"), "0-7\n")
	mustWrite(t, filepath.Join(sysRoot, "devices/system/node/node0/meminfo"),
		"Node 0 MemTotal:       8000000 kB\nNode 0 MemFree:        4000000 kB\n")
	return
}

// collectPUs returns every PU object's OSIndex in the subtree.
func collectPUs(o *cpu.TopoObject) (pus []int32) {
	if o.Kind == cpu.TopoKindPU {
		pus = append(pus, o.OSIndex)
		return
	}
	for _, c := range o.Children {
		pus = append(pus, collectPUs(c)...)
	}
	return
}

// findKind reports whether any object in the subtree has the given kind.
func findKind(o *cpu.TopoObject, k cpu.TopoKindE) bool {
	if o.Kind == k {
		return true
	}
	for _, c := range o.Children {
		if findKind(c, k) {
			return true
		}
	}
	return false
}

func TestReadTopology_Hierarchy(t *testing.T) {
	sysRoot := writeTopoFixture(t)
	topo, err := cpu.ReadTopology(cpu.TopologyOptions{Sys: sysfs.New(sysRoot)})
	require.NoError(t, err)

	require.NotNil(t, topo.Root)
	assert.Equal(t, cpu.TopoKindMachine, topo.Root.Kind)
	assert.Equal(t, int32(8), topo.LogicalCount)
	assert.Equal(t, "Machine", topo.Root.Label())

	// Machine → Package P#0
	require.Len(t, topo.Root.Children, 1)
	pkg := topo.Root.Children[0]
	assert.Equal(t, cpu.TopoKindPackage, pkg.Kind)
	assert.Equal(t, int32(0), pkg.OSIndex)
	assert.Equal(t, "Package P#0", pkg.Label())

	// Package → NUMANode #0
	require.Len(t, pkg.Children, 1)
	numa := pkg.Children[0]
	assert.Equal(t, cpu.TopoKindNUMANode, numa.Kind)
	assert.Equal(t, "NUMANode #0", numa.Label())
	assert.Equal(t, uint64(8000000)*1024, numa.MemBytes, "per-node MemTotal, in bytes")

	// NUMANode → two L3 caches (the CCX split)
	require.Len(t, numa.Children, 2)
	l3 := numa.Children[0]
	assert.Equal(t, cpu.TopoKindCache, l3.Kind)
	assert.Equal(t, uint8(3), l3.CacheLevel)
	assert.Equal(t, cpu.CacheTypeUnified, l3.CacheType)
	assert.Equal(t, uint64(8<<20), l3.CacheSizeBytes)
	assert.Equal(t, "L3 (8 MiB)", l3.Label())

	// L3 → two L2 caches (one per core)
	require.Len(t, l3.Children, 2)
	l2 := l3.Children[0]
	assert.Equal(t, uint8(2), l2.CacheLevel)
	assert.Equal(t, "L2 (512 KiB)", l2.Label())

	// L2 → L1d → L1i → Core (linear nest of equal-set caches)
	require.Len(t, l2.Children, 1)
	l1d := l2.Children[0]
	assert.Equal(t, uint8(1), l1d.CacheLevel)
	assert.Equal(t, cpu.CacheTypeData, l1d.CacheType)
	assert.Equal(t, "L1d (32 KiB)", l1d.Label())

	require.Len(t, l1d.Children, 1)
	l1i := l1d.Children[0]
	assert.Equal(t, cpu.CacheTypeInstruction, l1i.CacheType)
	assert.Equal(t, "L1i (32 KiB)", l1i.Label())

	// L1i → Core P#0 → {PU P#0, PU P#1}
	require.Len(t, l1i.Children, 1)
	core := l1i.Children[0]
	assert.Equal(t, cpu.TopoKindCore, core.Kind)
	assert.Equal(t, int32(0), core.OSIndex)
	assert.Equal(t, "Core P#0", core.Label())

	require.Len(t, core.Children, 2)
	assert.Equal(t, cpu.TopoKindPU, core.Children[0].Kind)
	assert.Equal(t, int32(0), core.Children[0].OSIndex)
	assert.Equal(t, "PU P#0", core.Children[0].Label())
	assert.Equal(t, int32(1), core.Children[1].OSIndex)
	if fp := core.Children[0].FreqPolicy; assert.NotNil(t, fp, "PU should carry a cpufreq policy") {
		assert.Equal(t, uint32(1000), fp.MinMHz)
		assert.Equal(t, uint32(4000), fp.MaxMHz)
		assert.Equal(t, "schedutil", fp.Governor)
		assert.Equal(t, "acpi-cpufreq", fp.Driver)
	}

	// Every logical CPU appears exactly once as a PU leaf.
	pus := collectPUs(topo.Root)
	assert.ElementsMatch(t, []int32{0, 1, 2, 3, 4, 5, 6, 7}, pus)
}

func TestReadTopology_NoNUMA_CachesUnderPackage(t *testing.T) {
	sysRoot := filepath.Join(t.TempDir(), "sys")
	cpuBase := filepath.Join(sysRoot, "devices/system/cpu")
	mustWrite(t, filepath.Join(cpuBase, "online"), "0-1\n")
	for i := range 2 {
		topo := filepath.Join(cpuBase, fmt.Sprintf("cpu%d/topology", i))
		mustWrite(t, filepath.Join(topo, "physical_package_id"), "0\n")
		mustWrite(t, filepath.Join(topo, "core_id"), "0\n")
		mustWrite(t, filepath.Join(topo, "package_cpus_list"), "0-1\n")
		mustWrite(t, filepath.Join(topo, "thread_siblings_list"), "0-1\n")
		cache := filepath.Join(cpuBase, fmt.Sprintf("cpu%d/cache", i))
		writeCacheIndex(t, cache, "index0", "2", "Unified", "1024K", "0-1")
	}
	// No devices/system/node tree at all.

	topo, err := cpu.ReadTopology(cpu.TopologyOptions{Sys: sysfs.New(sysRoot)})
	require.NoError(t, err)
	require.Len(t, topo.Root.Children, 1)
	pkg := topo.Root.Children[0]
	assert.False(t, findKind(topo.Root, cpu.TopoKindNUMANode), "no NUMA node expected")
	// Highest object under the package is the L2 cache, not a NUMA node.
	require.Len(t, pkg.Children, 1)
	assert.Equal(t, cpu.TopoKindCache, pkg.Children[0].Kind)
	assert.ElementsMatch(t, []int32{0, 1}, collectPUs(topo.Root))
}

func TestReadTopology_NoCache_CoresUnderPackage(t *testing.T) {
	sysRoot := filepath.Join(t.TempDir(), "sys")
	cpuBase := filepath.Join(sysRoot, "devices/system/cpu")
	mustWrite(t, filepath.Join(cpuBase, "online"), "0-1\n")
	for i := range 2 {
		topo := filepath.Join(cpuBase, fmt.Sprintf("cpu%d/topology", i))
		mustWrite(t, filepath.Join(topo, "physical_package_id"), "0\n")
		mustWrite(t, filepath.Join(topo, "core_id"), strconv.Itoa(i)+"\n")
		mustWrite(t, filepath.Join(topo, "package_cpus_list"), "0-1\n")
		mustWrite(t, filepath.Join(topo, "thread_siblings_list"), strconv.Itoa(i)+"\n")
	}

	topo, err := cpu.ReadTopology(cpu.TopologyOptions{Sys: sysfs.New(sysRoot)})
	require.NoError(t, err)
	require.Len(t, topo.Root.Children, 1)
	pkg := topo.Root.Children[0]
	assert.False(t, findKind(topo.Root, cpu.TopoKindCache), "no cache nodes expected")
	require.Len(t, pkg.Children, 2) // two cores directly under the package
	assert.Equal(t, cpu.TopoKindCore, pkg.Children[0].Kind)
}

func TestReadTopology_MissingOnline_Errors(t *testing.T) {
	sysRoot := filepath.Join(t.TempDir(), "sys")
	require.NoError(t, os.MkdirAll(sysRoot, 0o755))
	_, err := cpu.ReadTopology(cpu.TopologyOptions{Sys: sysfs.New(sysRoot)})
	require.Error(t, err)
}

// sprintTopo renders a topology subtree as an indented outline for eyeballing.
func sprintTopo(o *cpu.TopoObject, depth int, sb *strings.Builder) {
	sb.WriteString(strings.Repeat("  ", depth))
	sb.WriteString(o.Label())
	sb.WriteByte('\n')
	for _, child := range o.Children {
		sprintTopo(child, depth+1, sb)
	}
}

// TestReadTopology_LiveSmoke reads the host's real sysfs (mirrors the live
// /proc smoke in cpu_test.go). Skips where the topology tree is absent
// (non-Linux, stripped containers).
func TestReadTopology_LiveSmoke(t *testing.T) {
	if _, err := os.Stat("/sys/devices/system/cpu/online"); err != nil {
		t.Skipf("no live cpu topology sysfs: %v", err)
	}
	topo, err := cpu.ReadTopology(cpu.TopologyOptions{})
	require.NoError(t, err)
	require.NotNil(t, topo.Root)
	assert.Equal(t, cpu.TopoKindMachine, topo.Root.Kind)

	// Every online logical CPU surfaces exactly once as a PU leaf.
	pus := collectPUs(topo.Root)
	assert.Greater(t, topo.LogicalCount, int32(0))
	assert.Len(t, pus, int(topo.LogicalCount), "PU leaf count must equal LogicalCount")

	var sb strings.Builder
	sprintTopo(topo.Root, 0, &sb)
	t.Logf("live CPU topology:\n%s", sb.String())
}
