package imztop

import (
	"github.com/dustin/go-humanize"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// buildTopoLayout converts a static [sysmsnap.Topology] into a treemap [layout.Node]
// tree plus a map from every layout node back to its source [sysmsnap.TopoObject].
// That map drives both the live tint (PU leaves → logical CPU via OSIndex) and
// the hover-detail panel (any node → its kind and fields). The tree is built
// once and never mutated — only the per-frame load/freq slices change — so the
// treemap widget's drill-in state, which keys off node identity, stays stable
// across frames.
//
// Every PU leaf is given Size:1 so the squarified layout weights all hardware
// threads equally; interior nodes derive their size from their children via
// [layout.Node.TotalSize].
func buildTopoLayout(topo sysmsnap.Topology) (root *layout.Node, nodeObj map[*layout.Node]*sysmsnap.TopoObject) {
	nodeObj = make(map[*layout.Node]*sysmsnap.TopoObject)
	if topo.Root == nil {
		return &layout.Node{Name: "Machine"}, nodeObj
	}
	var conv func(o *sysmsnap.TopoObject) *layout.Node
	conv = func(o *sysmsnap.TopoObject) (n *layout.Node) {
		name := o.Label()
		// lstopo-style: show the NUMA node's local RAM size in the box label.
		if o.Kind == sysmsnap.TopoKindNUMANode && o.MemBytes > 0 {
			name += " · " + humanize.IBytes(o.MemBytes)
		}
		n = &layout.Node{Name: name}
		nodeObj[n] = o
		if o.Kind == sysmsnap.TopoKindPU {
			n.Size = 1
			return
		}
		n.Children = make([]*layout.Node, 0, len(o.Children))
		for _, child := range o.Children {
			n.Children = append(n.Children, conv(child))
		}
		return
	}
	root = conv(topo.Root)
	return
}

// countKind returns the number of [sysmsnap.TopoObject]s of kind k in the subtree
// rooted at o (inclusive). Drives hover summaries like "shared by N threads".
func countKind(o *sysmsnap.TopoObject, k sysmsnap.TopoKindE) (n int) {
	if o == nil {
		return 0
	}
	if o.Kind == k {
		n = 1
	}
	for _, child := range o.Children {
		n += countKind(child, k)
	}
	return
}

// puIndexes returns the logical-CPU ids of every PU leaf in the subtree rooted
// at o, used to aggregate live load/frequency over a core / cache / package.
func puIndexes(o *sysmsnap.TopoObject) (ids []int32) {
	if o == nil {
		return nil
	}
	if o.Kind == sysmsnap.TopoKindPU {
		return []int32{o.OSIndex}
	}
	for _, child := range o.Children {
		ids = append(ids, puIndexes(child)...)
	}
	return
}
