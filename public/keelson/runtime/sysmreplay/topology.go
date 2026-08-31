package sysmreplay

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// TopologyFrom rebuilds the containment tree from the stored adjacency list.
//
// The tee flattens the tree by a pre-order walk that numbers nodes and gives
// each its parent's number; this is that walk inverted. Node numbers are read
// from the stored column rather than assumed to equal the array position: the
// writer stores them explicitly precisely so a row whose arrays were filtered
// still resolves its parent references.
//
// The structural checks are not defensive padding. A tree that arrives with a
// dangling parent, two roots, or a cycle would otherwise produce a plausible
// forest with nodes silently missing from it, and the caller renders that as
// though it were the machine.
func TopologyFrom(row sysmfacts.SysTopology) (topo *sysmsnap.Topology, err error) {
	n, err := coLen("sysTopology",
		arr{"Node", len(row.Node)}, arr{"Parent", len(row.Parent)},
		arr{"NodeKind", len(row.NodeKind)}, arr{"OSIndex", len(row.OSIndex)},
		arr{"CacheLevel", len(row.CacheLevel)}, arr{"CacheType", len(row.CacheType)},
		arr{"CacheSizeBytes", len(row.CacheSizeBytes)}, arr{"MemBytes", len(row.MemBytes)},
		arr{"FreqPresent", len(row.FreqPresent)}, arr{"FreqMinMHz", len(row.FreqMinMHz)},
		arr{"FreqMaxMHz", len(row.FreqMaxMHz)}, arr{"FreqGovernor", len(row.FreqGovernor)},
		arr{"FreqDriver", len(row.FreqDriver)})
	if err != nil {
		return
	}
	if n == 0 {
		err = eh.Errorf("sysmreplay: sysTopology row carries no nodes")
		return
	}

	objs := make([]*sysmsnap.TopoObject, n)
	byNum := make(map[uint32]int, n)
	for i := range n {
		num := row.Node[i]
		if prev, dup := byNum[num]; dup {
			err = eb.Build().Uint32("node", num).Int("firstPosition", prev).Int("secondPosition", i).Errorf("sysmreplay: sysTopology node number appears twice")
			return
		}
		byNum[num] = i
		obj := &sysmsnap.TopoObject{
			Kind:           parseTopoKind(row.NodeKind[i]),
			OSIndex:        row.OSIndex[i],
			CacheLevel:     row.CacheLevel[i],
			CacheType:      parseCacheType(row.CacheType[i]),
			CacheSizeBytes: row.CacheSizeBytes[i],
			MemBytes:       row.MemBytes[i],
		}
		if u2b(row.FreqPresent[i]) {
			obj.FreqPolicy = &sysmsnap.FreqPolicy{
				MinMHz:   row.FreqMinMHz[i],
				MaxMHz:   row.FreqMaxMHz[i],
				Governor: row.FreqGovernor[i],
				Driver:   row.FreqDriver[i],
			}
		}
		objs[i] = obj
	}

	// Ascending position preserves each parent's original child order: the
	// writer's walk numbers children in discovery order, so appending in
	// position order replays it.
	rootIdx := -1
	for i := range n {
		p := row.Parent[i]
		if p < 0 {
			if rootIdx >= 0 {
				err = eb.Build().Uint32("root", row.Node[rootIdx]).Uint32("otherRoot", row.Node[i]).Errorf("sysmreplay: sysTopology has more than one root")
				return
			}
			rootIdx = i
			continue
		}
		pi, ok := byNum[uint32(p)]
		if !ok {
			err = eb.Build().Uint32("node", row.Node[i]).Int32("parent", p).Errorf("sysmreplay: sysTopology node names a parent that is not in the row")
			return
		}
		if pi == i {
			err = eb.Build().Uint32("node", row.Node[i]).Errorf("sysmreplay: sysTopology node is its own parent")
			return
		}
		objs[pi].Children = append(objs[pi].Children, objs[i])
	}
	if rootIdx < 0 {
		err = eh.Errorf("sysmreplay: sysTopology has no root — every node names a parent")
		return
	}

	// Every node must hang off the root. One parent each plus a single root
	// still admits a detached cycle, which would drop its members silently.
	seen := 1
	stack := []*sysmsnap.TopoObject{objs[rootIdx]}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, ch := range cur.Children {
			seen++
			stack = append(stack, ch)
		}
	}
	if seen != n {
		err = eb.Build().Int("reachable", seen).Int("nodes", n).Errorf("sysmreplay: sysTopology is not a tree — not every node is reachable from the root")
		return
	}

	topo = &sysmsnap.Topology{Root: objs[rootIdx], LogicalCount: row.LogicalCount}
	return
}

// parseTopoKind inverts [sysmsnap.TopoKindE.String]. An unrecognised label
// reads as Machine — the zero value — rather than failing the row: the kind
// only labels a node, and refusing the whole tree over one unknown word would
// lose the structure a newer writer still described correctly.
func parseTopoKind(s string) (kind sysmsnap.TopoKindE) {
	switch s {
	case "Package":
		return sysmsnap.TopoKindPackage
	case "NUMANode":
		return sysmsnap.TopoKindNUMANode
	case "Cache":
		return sysmsnap.TopoKindCache
	case "Core":
		return sysmsnap.TopoKindCore
	case "PU":
		return sysmsnap.TopoKindPU
	default:
		return sysmsnap.TopoKindMachine
	}
}

// parseCacheType inverts the tee's cacheTypeLabel. The empty string means "not
// a cache" there, and maps to the same Unified zero value the enum uses — the
// distinction is carried by Kind, which is why the writer gates the label on it.
func parseCacheType(s string) (t sysmsnap.CacheTypeE) {
	switch s {
	case "Data":
		return sysmsnap.CacheTypeData
	case "Instruction":
		return sysmsnap.CacheTypeInstruction
	default:
		return sysmsnap.CacheTypeUnified
	}
}
