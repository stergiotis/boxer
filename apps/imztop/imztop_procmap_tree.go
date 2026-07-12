package imztop

import (
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// procMetricE selects what a process rectangle's AREA encodes in the Proc Map
// treemap.
type procMetricE uint8

const (
	// procMetricRSS sizes each box by resident memory. The default: RSS is
	// always positive, so every process gets a visible cell (CPU% is 0 for
	// most, which the min-cell cull would hide).
	procMetricRSS procMetricE = iota
	// procMetricCPU sizes each box by the EWMA-smoothed CPU%. Idle processes
	// collapse to the layout's unit-weight floor.
	procMetricCPU
)

var procMetrics = []procMetricE{procMetricRSS, procMetricCPU}

func procMetricLabel(m procMetricE) string {
	if m == procMetricCPU {
		return "CPU%"
	}
	return "RSS"
}

// procCell is the payload hung off every Proc Map layout node: the source
// process (a pointer into the immutable published snapshot slice) plus the
// smoothed CPU% that drives the continuous load tint. A process's own container
// node and its synthetic self-leaf point at the same procCell.
type procCell struct {
	info *sysmsnap.ProcInfo
	cpu  float32 // EWMA-smoothed CPU% — colour tint + optional cell label
}

// procMetricValue returns a leaf's area weight in the active metric. RSS is
// always positive; CPU% can be 0, which layout.Node.TotalSize floors to a unit
// weight so the cell still appears (just minimal).
func procMetricValue(p *sysmsnap.ProcInfo, cpu float32, metric procMetricE) float64 {
	if metric == procMetricCPU {
		return float64(cpu)
	}
	return float64(p.RSSBytes)
}

// buildProcForest derives the parent→child adjacency of a process slice, keyed
// by index into infos. A process whose PPID is absent from the slice (a real
// root like PID 1, or a child orphaned by the sampler's top-N truncation) or
// which is its own parent starts a forest root. Any process left unreachable by
// a PID-reuse cycle is promoted to a root so no process is ever dropped. Mirrors
// buildProcOrder's robustness; returns children[i] = child indices and the
// ordered root indices.
func buildProcForest(infos []sysmsnap.ProcInfo) (children [][]int, roots []int) {
	n := len(infos)
	idxByPID := make(map[uint32]int, n)
	for i := range infos {
		idxByPID[infos[i].PID] = i
	}
	children = make([][]int, n)
	isChild := make([]bool, n)
	for i := range infos {
		if pi, ok := idxByPID[infos[i].PPID]; ok && pi != i {
			children[pi] = append(children[pi], i)
			isChild[i] = true
		}
	}
	for i := range infos {
		if !isChild[i] {
			roots = append(roots, i)
		}
	}
	// Cycle guard: promote any node unreachable from the natural roots. This is
	// only possible via a PID-reuse cycle where every member looks like a child.
	visited := make([]bool, n)
	var mark func(i int)
	mark = func(i int) {
		if visited[i] {
			return
		}
		visited[i] = true
		for _, ci := range children[i] {
			mark(ci)
		}
	}
	for _, r := range roots {
		mark(r)
	}
	for i := range infos {
		if !visited[i] {
			roots = append(roots, i)
			mark(i)
		}
	}
	return
}

// reconcileProcTree rebuilds the Proc Map treemap's node tree from a fresh
// process snapshot, in place under the stable synthetic root. Process nodes are
// pooled by (PID, StartedAt) — the same key the per-process CPU EWMA uses — so
// their pointer identity, which the treemap keys drill state off, survives the
// ~1 Hz rebuilds. A process that has children also gets a synthetic self-leaf
// carrying its OWN weight, because layout.Node.TotalSize ignores an interior
// node's Size (a parent's area is the sum of its children); without the
// self-leaf a heavyweight parent with light children would read as tiny.
//
// Called only when a new sample lands or the area metric changes
// (imztop_panel_procmap.go), never per frame — a hidden dock tab still runs its
// Go body every frame under the DockArea's late cull, so the O(n) rebuild is
// gated on the sample clock, not the frame rate.
func (inst *App) reconcileProcTree(procs []sysmsnap.ProcInfo, smoothed []float32, metric procMetricE) {
	n := len(procs)
	children, roots := buildProcForest(procs)

	// Get-or-create a stable node per process; refresh its label + payload.
	live := make(map[procEWMAKey]bool, n)
	nodeObj := make(map[*layout.Node]*procCell, n)
	nodeOf := make([]*layout.Node, n)
	for i := range procs {
		key := procEWMAKey{PID: procs[i].PID, StartedAt: procs[i].StartedAtUnixMs}
		node := inst.procNodes[key]
		if node == nil {
			node = &layout.Node{}
			inst.procNodes[key] = node
		}
		node.Name = procs[i].Name
		node.Children = node.Children[:0]
		node.Size = 0
		live[key] = true
		nodeOf[i] = node
		cpu := procs[i].CPUPercent
		if i < len(smoothed) {
			cpu = smoothed[i]
		}
		nodeObj[node] = &procCell{info: &procs[i], cpu: cpu}
	}

	// Link + size. A leaf process carries its metric directly; a parent gets a
	// self-leaf (own weight) followed by its child nodes, so the container's
	// area is own + subtree. The attached guard makes PID-reuse cycles finite.
	attached := make([]bool, n)
	var attach func(i int)
	attach = func(i int) {
		attached[i] = true
		node := nodeOf[i]
		kids := children[i]
		if len(kids) == 0 {
			node.Size = procMetricValue(&procs[i], nodeObj[node].cpu, metric)
			return
		}
		self := &layout.Node{Name: procs[i].Name, Size: procMetricValue(&procs[i], nodeObj[node].cpu, metric)}
		nodeObj[self] = nodeObj[node]
		node.Children = append(node.Children, self)
		for _, ci := range kids {
			if attached[ci] {
				continue
			}
			attach(ci)
			node.Children = append(node.Children, nodeOf[ci])
		}
	}
	inst.procRoot.Children = inst.procRoot.Children[:0]
	for _, r := range roots {
		if attached[r] {
			continue
		}
		attach(r)
		inst.procRoot.Children = append(inst.procRoot.Children, nodeOf[r])
	}

	// Evict pooled nodes for processes that vanished this sample.
	for key := range inst.procNodes {
		if !live[key] {
			delete(inst.procNodes, key)
		}
	}
	inst.procNodeObj = nodeObj

	// Heal drill state. Pooled pointers keep a surviving drill path valid, so
	// DrillTo→NavigateTo no-ops (no spurious zoom) when the focused process is
	// unchanged; it re-seats if the process was reparented, resets if it
	// vanished, and drills up one level if it lost all its children.
	if inst.procTreemap != nil && inst.procTreemap.Depth() > 0 {
		focus := inst.procTreemap.Focused()
		if err := inst.procTreemap.DrillTo(focus); err != nil {
			inst.procTreemap.Reset()
		} else if len(focus.Children) == 0 {
			_ = inst.procTreemap.DrillUp(1)
		}
	}
}
