//go:build llm_generated_opus48

package imztop

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// topoContainerW / topoContainerH size the fixed treemap canvas. The squarify
// widget takes its bounds at construction, so this is a one-time choice rather
// than a per-frame fit; it's wrapped in the tab's ScrollArea so smaller
// viewports scroll rather than clip. Sized to fill the left dock leaf at the
// 1280×694 compositor-clamped viewport on a typical (≤64-thread) machine.
const (
	topoContainerW float32 = 920
	topoContainerH float32 = 540
)

// initTopology performs the one-shot sysfs topology read and builds the
// treemap widget. It is called lazily on the first Topology-tab frame (not in
// newApp) so it captures the post-Mount inst.ids — the host swaps inst.ids
// from the ctor-seeded stack to its own at Mount. On read failure topoErr is
// recorded and the panel renders a message instead of a tree.
func (inst *App) initTopology() {
	topo, err := cpu.ReadTopology(cpu.TopologyOptions{})
	if err != nil {
		inst.topoErr = err
		return
	}
	root, nodeCPU := buildTopoLayout(topo)
	inst.topoNodeCPU = nodeCPU

	// Live per-core load tint. The fn resolves a PU leaf to its CPU's busy%
	// (read from the per-frame inst.topoLoad slice); every non-PU node returns
	// NaN, which treemap.ContinuousColoring treats as "no opinion" so it falls
	// through to the DepthColoring layer below. Reusing cpuHeatmapPalette()
	// (styletokens.SequentialDefault) keeps this tint identical to the CPU
	// panel's per-core heatmap.
	loadFn := func(n *layout.Node) (pct float64) {
		cpuID, ok := inst.topoNodeCPU[n]
		if !ok {
			return math.NaN()
		}
		ld := inst.topoLoad
		if cpuID < 0 || int(cpuID) >= len(ld) {
			return math.NaN()
		}
		return float64(ld[cpuID])
	}
	coloring := treemap.CompositeColoring(
		treemap.ContinuousColoring(cpuHeatmapPalette(), loadFn, 0, 100),
		treemap.DepthColoring(treemap.DefaultDepthColors),
	)
	inst.topoTreemap = treemap.New(inst.ids, "imztop-topology", root,
		treemap.WithContainerSize(topoContainerW, topoContainerH),
		treemap.WithColoring(coloring),
	)
}

// renderTopologyPanel draws the lstopo-style CPU containment tree (package →
// NUMA → L3/L2/L1 → core → SMT thread) as nested treemap boxes, with each
// thread box tinted by its current busy%. See ADR-0020 (2026-05-29 Update).
func (inst *App) renderTopologyPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("CPU Topology")

	if !inst.topoInit {
		inst.topoInit = true
		inst.initTopology()
	}
	if inst.topoErr != nil {
		c.Label("CPU topology unavailable: " + inst.topoErr.Error()).Send()
		return
	}
	if inst.topoTreemap == nil {
		c.Label("CPU topology unavailable").Send()
		return
	}

	// Refresh the live load slice the coloring closure reads. Held on *App so
	// the colors update each frame without rebuilding the (static) tree. The
	// slice is owned by the published snapshot, read within this frame only —
	// the same access the CPU panel makes.
	if snap != nil && snap.LatestCPU != nil {
		inst.topoLoad = snap.LatestCPU.PerCorePercent
	}

	for range c.Horizontal().KeepIter() {
		c.Label("boxes nest package → cache → core → thread; tint = live per-core load · drag a box to drill in").Send()
	}
	c.AddSpace(inst.spaceInner())
	inst.topoTreemap.Render()
}
