//go:build llm_generated_opus48

package imztop

import (
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// buildTopoLayout converts a static [cpu.Topology] into a treemap [layout.Node]
// tree plus a map from each PU leaf node to its logical CPU id. The map is the
// hook the live-load coloring uses (imztop_panel_topology.go): the tree is
// built once and never mutated — only the per-frame load slice changes — so
// the treemap widget's drill-in state, which keys off node identity, stays
// stable across frames.
//
// Every PU leaf is given Size:1 so the squarified layout weights all hardware
// threads equally; interior nodes (packages, NUMA nodes, caches, cores) derive
// their size from their children via [layout.Node.TotalSize].
func buildTopoLayout(topo cpu.Topology) (root *layout.Node, nodeCPU map[*layout.Node]int32) {
	nodeCPU = make(map[*layout.Node]int32)
	if topo.Root == nil {
		return &layout.Node{Name: "Machine"}, nodeCPU
	}
	var conv func(o *cpu.TopoObject) *layout.Node
	conv = func(o *cpu.TopoObject) (n *layout.Node) {
		n = &layout.Node{Name: o.Label()}
		if o.Kind == cpu.TopoKindPU {
			n.Size = 1
			nodeCPU[n] = o.OSIndex
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
