package sysmtee

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

const (
	kindTopology   = "sysTopology"
	domainTopology = "topology"
)

// topologyRow flattens the containment tree into an adjacency list.
//
// The walk is pre-order and iterative rather than recursive: the tree comes off
// a bus from another process, so its depth is not something this code gets to
// assume. A malformed or adversarially deep tree should cost memory, which is
// bounded by the row itself, rather than stack.
func topologyRow(host string, topo *sysmsnap.Topology, ts time.Time) (row sysmfacts.SysTopology, err error) {
	if topo == nil || topo.Root == nil {
		err = eh.Errorf("sysmtee: topology has no root")
		return
	}
	nk, err := entityNaturalKey(host, domainTopology)
	if err != nil {
		return
	}
	row = sysmfacts.SysTopology{
		Id:           entityKey(host, domainTopology),
		NaturalKey:   nk,
		Ts:           ts,
		Kind:         kindTopology,
		Host:         host,
		LogicalCount: topo.LogicalCount,
	}

	// Parent index travels with the node so the walk needs no second pass and
	// no parent map: a node is appended, then its children are queued knowing
	// the position it landed at.
	type pending struct {
		obj    *sysmsnap.TopoObject
		parent int32
	}
	queue := []pending{{obj: topo.Root, parent: -1}}
	for len(queue) > 0 {
		cur := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if cur.obj == nil {
			// A nil child would otherwise shift every subsequent index and
			// silently re-parent the rest of the tree.
			continue
		}
		idx := int32(len(row.Node))
		row.Node = append(row.Node, uint32(idx))
		row.Parent = append(row.Parent, cur.parent)
		row.NodeKind = append(row.NodeKind, cur.obj.Kind.String())
		row.OSIndex = append(row.OSIndex, cur.obj.OSIndex)
		row.CacheLevel = append(row.CacheLevel, cur.obj.CacheLevel)
		row.CacheType = append(row.CacheType, cacheTypeLabel(cur.obj.Kind, cur.obj.CacheType))
		row.CacheSizeBytes = append(row.CacheSizeBytes, cur.obj.CacheSizeBytes)
		row.MemBytes = append(row.MemBytes, cur.obj.MemBytes)

		if fp := cur.obj.FreqPolicy; fp != nil {
			row.FreqPresent = append(row.FreqPresent, 1)
			row.FreqMinMHz = append(row.FreqMinMHz, fp.MinMHz)
			row.FreqMaxMHz = append(row.FreqMaxMHz, fp.MaxMHz)
			row.FreqGovernor = append(row.FreqGovernor, fp.Governor)
			row.FreqDriver = append(row.FreqDriver, fp.Driver)
		} else {
			row.FreqPresent = append(row.FreqPresent, 0)
			row.FreqMinMHz = append(row.FreqMinMHz, 0)
			row.FreqMaxMHz = append(row.FreqMaxMHz, 0)
			row.FreqGovernor = append(row.FreqGovernor, "")
			row.FreqDriver = append(row.FreqDriver, "")
		}

		// Pushed in reverse so the stack pops them in discovery order, which is
		// what makes the walk pre-order and the node numbering reproducible for
		// one tree.
		for i := len(cur.obj.Children) - 1; i >= 0; i-- {
			queue = append(queue, pending{obj: cur.obj.Children[i], parent: idx})
		}
	}
	return
}

// cacheTypeLabel renders the sysfs cache type, and only for cache nodes.
//
// The kind gate is what makes the column honest: CacheTypeUnified is the zero
// value of the enum, so without it every node in the tree — packages, cores,
// PUs — would read as a unified cache. Empty says "not a cache" the way the
// other cache lanes cannot, since 0 is a legitimate level and size.
//
// The word is stored rather than the enum's own Suffix ("d"/"i"/""), which is
// an lstopo display form and would collide with that empty.
func cacheTypeLabel(kind sysmsnap.TopoKindE, t sysmsnap.CacheTypeE) string {
	if kind != sysmsnap.TopoKindCache {
		return ""
	}
	switch t {
	case sysmsnap.CacheTypeData:
		return "Data"
	case sysmsnap.CacheTypeInstruction:
		return "Instruction"
	default:
		return "Unified"
	}
}
