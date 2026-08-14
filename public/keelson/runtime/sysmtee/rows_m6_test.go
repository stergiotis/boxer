package sysmtee

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTopology is a two-core, four-thread machine with an L3 over two L2s.
//
//	Machine
//	└── Package P#0
//	    └── NUMANode #0 (16 GiB)
//	        └── L3 (8 MiB, Unified)
//	            ├── L2 (512 KiB, Data) ── Core P#0 ── PU P#0, PU P#1
//	            └── L2 (512 KiB, Data) ── Core P#1 ── PU P#2, PU P#3
func testTopology() *sysmsnap.Topology {
	pu := func(idx int32, withFreq bool) *sysmsnap.TopoObject {
		o := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindPU, OSIndex: idx}
		if withFreq {
			o.FreqPolicy = &sysmsnap.FreqPolicy{MinMHz: 800, MaxMHz: 4200, Governor: "performance", Driver: "amd-pstate"}
		}
		return o
	}
	core := func(idx int32, pus ...*sysmsnap.TopoObject) *sysmsnap.TopoObject {
		return &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCore, OSIndex: idx, Children: pus}
	}
	l2 := func(child *sysmsnap.TopoObject) *sysmsnap.TopoObject {
		return &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCache, OSIndex: -1, CacheLevel: 2,
			CacheType: sysmsnap.CacheTypeData, CacheSizeBytes: 512 << 10, Children: []*sysmsnap.TopoObject{child}}
	}
	l3 := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCache, OSIndex: -1, CacheLevel: 3,
		CacheType: sysmsnap.CacheTypeUnified, CacheSizeBytes: 8 << 20,
		Children: []*sysmsnap.TopoObject{
			l2(core(0, pu(0, true), pu(1, true))),
			l2(core(1, pu(2, true), pu(3, false))),
		}}
	numa := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindNUMANode, OSIndex: 0, MemBytes: 16 << 30,
		Children: []*sysmsnap.TopoObject{l3}}
	pkg := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindPackage, OSIndex: 0,
		Children: []*sysmsnap.TopoObject{numa}}
	return &sysmsnap.Topology{
		Root:         &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindMachine, OSIndex: -1, Children: []*sysmsnap.TopoObject{pkg}},
		LogicalCount: 4,
	}
}

// countNodes is an independent oracle for the walk: the row must carry exactly
// as many nodes as the tree has, counted a different way.
func countNodes(o *sysmsnap.TopoObject) int {
	if o == nil {
		return 0
	}
	n := 1
	for _, c := range o.Children {
		n += countNodes(c)
	}
	return n
}

func TestTopologyRow_FlattensEveryNode(t *testing.T) {
	topo := testTopology()
	row, err := topologyRow("box1", topo, testTs)
	require.NoError(t, err)

	want := countNodes(topo.Root)
	assert.Equal(t, 12, want, "the fixture is Machine+Package+NUMA+L3+2*(L2+Core+2*PU)")
	require.Len(t, row.Node, want)
	for _, n := range []int{len(row.Parent), len(row.NodeKind), len(row.OSIndex),
		len(row.CacheLevel), len(row.CacheType), len(row.CacheSizeBytes), len(row.MemBytes),
		len(row.FreqPresent), len(row.FreqMinMHz), len(row.FreqMaxMHz),
		len(row.FreqGovernor), len(row.FreqDriver)} {
		assert.Equal(t, want, n, "every array must have one element per node")
	}
	assert.EqualValues(t, 4, row.LogicalCount)
}

// The adjacency list has to be reconstructable, or storing the tree as data
// bought nothing. Rebuilding it and comparing against the original is the only
// assertion that actually proves the encoding lossless in shape.
func TestTopologyRow_ReconstructsTheTree(t *testing.T) {
	topo := testTopology()
	row, err := topologyRow("box1", topo, testTs)
	require.NoError(t, err)

	// Exactly one root, and it is node 0.
	roots := 0
	for i, p := range row.Parent {
		if p == -1 {
			roots++
			assert.EqualValues(t, 0, row.Node[i], "the root must be the first node of a pre-order walk")
		}
	}
	assert.Equal(t, 1, roots)

	// Every non-root parent must reference a node that exists and precedes it —
	// the property a recursive CTE relies on to terminate.
	byIdx := make(map[uint32]int, len(row.Node))
	for i, n := range row.Node {
		_, dup := byIdx[n]
		require.False(t, dup, "node index %d repeats", n)
		byIdx[n] = i
	}
	for i, p := range row.Parent {
		if p == -1 {
			continue
		}
		pos, ok := byIdx[uint32(p)]
		require.True(t, ok, "node %d references a parent that is not in the row", row.Node[i])
		assert.Less(t, pos, i, "a pre-order walk emits a parent before its children")
	}

	// Rebuild the child lists and compare the shape against the source.
	children := make(map[int32][]int, len(row.Node))
	for i, p := range row.Parent {
		children[p] = append(children[p], i)
	}
	var walk func(pos int, obj *sysmsnap.TopoObject)
	walk = func(pos int, obj *sysmsnap.TopoObject) {
		assert.Equal(t, obj.Kind.String(), row.NodeKind[pos])
		assert.Equal(t, obj.OSIndex, row.OSIndex[pos])
		kids := children[int32(row.Node[pos])]
		require.Len(t, kids, len(obj.Children), "child count at %s", obj.Kind)
		for i, c := range obj.Children {
			walk(kids[i], c)
		}
	}
	walk(0, topo.Root)
}

func TestTopologyRow_CarriesTheKindSpecificAttributes(t *testing.T) {
	row, err := topologyRow("box1", testTopology(), testTs)
	require.NoError(t, err)

	find := func(kind string, pred func(i int) bool) int {
		for i, k := range row.NodeKind {
			if k == kind && (pred == nil || pred(i)) {
				return i
			}
		}
		t.Fatalf("no %s node found", kind)
		return -1
	}

	numa := find("NUMANode", nil)
	assert.EqualValues(t, 16<<30, row.MemBytes[numa])

	l3 := find("Cache", func(i int) bool { return row.CacheLevel[i] == 3 })
	assert.EqualValues(t, 8<<20, row.CacheSizeBytes[l3])
	assert.Equal(t, "Unified", row.CacheType[l3])

	l2 := find("Cache", func(i int) bool { return row.CacheLevel[i] == 2 })
	assert.Equal(t, "Data", row.CacheType[l2])

	// Non-cache nodes must not read as caches. CacheTypeUnified is the enum's
	// zero value, so without the kind gate every package, core and PU in the
	// tree would claim to be a unified cache.
	machine := find("Machine", nil)
	assert.Empty(t, row.CacheType[machine])
	assert.EqualValues(t, 0, row.CacheLevel[machine])
}

// A PU whose cpufreq read failed and a PU reporting a zero policy are the same
// row without the presence flag.
func TestTopologyRow_FreqPolicyPresenceIsExplicit(t *testing.T) {
	row, err := topologyRow("box1", testTopology(), testTs)
	require.NoError(t, err)

	withFreq, without := 0, 0
	for i, k := range row.NodeKind {
		if k != "PU" {
			assert.EqualValues(t, 0, row.FreqPresent[i], "only PUs carry a cpufreq policy")
			continue
		}
		if row.FreqPresent[i] == 1 {
			withFreq++
			assert.EqualValues(t, 4200, row.FreqMaxMHz[i])
			assert.Equal(t, "performance", row.FreqGovernor[i])
		} else {
			without++
			assert.Empty(t, row.FreqGovernor[i])
		}
	}
	assert.Equal(t, 3, withFreq)
	assert.Equal(t, 1, without, "the fixture has one PU without a policy")
}

// A nil child would shift every subsequent index and silently re-parent the
// rest of the tree, so it is skipped rather than emitted.
func TestTopologyRow_SkipsNilChildren(t *testing.T) {
	row, err := topologyRow("box1", &sysmsnap.Topology{
		LogicalCount: 1,
		Root: &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindMachine, OSIndex: -1,
			Children: []*sysmsnap.TopoObject{
				nil,
				{Kind: sysmsnap.TopoKindPU, OSIndex: 0},
			}},
	}, testTs)
	require.NoError(t, err)
	require.Len(t, row.Node, 2)
	assert.Equal(t, "PU", row.NodeKind[1])
	assert.EqualValues(t, 0, row.Parent[1], "the surviving child still points at the root")
}

func TestTopologyRow_RefusesARootlessTree(t *testing.T) {
	_, err := topologyRow("box1", &sysmsnap.Topology{}, testTs)
	require.Error(t, err)
	_, err = topologyRow("box1", nil, testTs)
	require.Error(t, err)
}
