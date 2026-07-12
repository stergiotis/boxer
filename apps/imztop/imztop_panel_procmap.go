package imztop

import (
	"fmt"
	"math"
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

const (
	// procMapMinW / procMapMinH floor the dynamically-sized treemap canvas;
	// below the floor the tab's ScrollArea scrolls instead of collapsing the
	// tree. procMapScrollbarAllowPx is shaved off the width while the vertical
	// scrollbar shows so the Vscroll-only ScrollArea does not clip the right
	// edge. Mirrors the topology panel's floors.
	procMapMinW             float32 = 360
	procMapMinH             float32 = 320
	procMapScrollbarAllowPx float32 = 16
	// procMapReservedBelowPx leaves room under the tree for the treemap's own
	// breadcrumb/status chrome and the hover-detail line when sizing to the pane.
	procMapReservedBelowPx float32 = 104
)

// initProcMap builds the Proc Map treemap once, on the first frame the panel is
// shown. The tree itself is (re)built from every sample by reconcileProcTree;
// here we only wire the stable root, the node pool, and the coloring: a
// monochrome depth base (shared with the topology panel, so nesting reads as
// brightness) overridden by a continuous CPU-load tint (the same sequential
// palette the CPU heatmap uses), so heat reads as colour and never competes
// with hierarchy.
func (inst *App) initProcMap() {
	inst.procRoot = &layout.Node{Name: "System"}
	inst.procNodes = make(map[procEWMAKey]*layout.Node)
	inst.procNodeObj = make(map[*layout.Node]*procCell)

	// Non-process nodes (the synthetic root) return NaN (ok=false) and fall
	// through to the monochrome depth base. Layer order matters: DepthColoring
	// always returns ok, so depth is FIRST and the load override LAST
	// (CompositeColoring is last-ok-wins) — the topology panel's idiom.
	loadFn := func(n *layout.Node) float64 {
		if pc := inst.procNodeObj[n]; pc != nil {
			return float64(pc.cpu)
		}
		return math.NaN()
	}
	coloring := treemap.CompositeColoring(
		treemap.DepthColoring(topoDepthPalette()),
		treemap.ContinuousColoring(cpuHeatmapPalette(), loadFn, 0, 100),
	)
	// No WithContainerSize: sized per-frame to fill the dock pane.
	// WithMaxNestingDepth(0) renders the whole forest at once (lstopo-style);
	// drilling into a top-level box still works.
	inst.procTreemap = treemap.New(inst.ids, "imztop-procmap", inst.procRoot,
		treemap.WithMaxNestingDepth(0),
		treemap.WithColoring(coloring),
		treemap.WithCellLabel(inst.procMapCellLabel),
	)
}

// procMapCellLabel is the treemap's optional secondary line: the humanized area
// metric (RSS or CPU%) on each leaf / off-path cell. It reads the metric the
// tree was last built with (procBuiltMetric) so the label and the rectangle area
// always agree.
func (inst *App) procMapCellLabel(n *layout.Node) string {
	pc := inst.procNodeObj[n]
	if pc == nil {
		return ""
	}
	if inst.procBuiltMetric == procMetricCPU {
		return fmt.Sprintf("%.0f%%", pc.cpu)
	}
	return humanBytes(pc.info.RSSBytes)
}

// renderProcMapPanel draws the process tree as a treemap: processes nested by
// PPID, each rectangle sized by RSS (or CPU%) and tinted by CPU load. See
// ADR-0020 (Update).
//
// The tree is rebuilt only when a new sample lands or the area metric changes,
// never per frame: a hidden dock tab still runs its whole Go body every frame
// (the DockArea culls late, on the Rust paint side), so the O(n) rebuild is
// gated on the sample clock to keep the hidden-tab cost to the treemap emission
// alone — matching the Topology tab's profile.
func (inst *App) renderProcMapPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Process Map")

	if inst.procTreemap == nil {
		inst.initProcMap()
	}

	if snap != nil && len(snap.Procs) > 0 &&
		(snap.SampledAtUnixMs != inst.procLastSampleMs || inst.procMetric != inst.procBuiltMetric) {
		inst.procLastSampleMs = snap.SampledAtUnixMs
		inst.procBuiltMetric = inst.procMetric
		inst.reconcileProcTree(snap.Procs, snap.ProcCPUSmoothed, inst.procMetric)
	}

	if len(inst.procRoot.Children) == 0 {
		c.Label("Process map: waiting for the first sample…").Send()
		return
	}

	// Area-metric switch (RSS ↔ CPU%) as a ComboBox — the topology panel's
	// dim-switch idiom, so the two treemap panels read the same.
	for range c.ComboBox(
		inst.ids.PrepareStr("procmap-metric-cb"),
		c.WidgetText().Text("area").Keep(),
		c.WidgetText().Text(procMetricLabel(inst.procMetric)).Keep(),
	).KeepIter() {
		for i, m := range procMetrics {
			sel := m == inst.procMetric
			if c.Button(inst.ids.PrepareSeq(uint64(0x320+i)), c.Atoms().Text(procMetricLabel(m)).Keep()).
				Selected(sel).
				FrameWhenInactive(!sel).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				inst.procMetric = m
			}
		}
	}

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%d procs · nest parent → child · area = %s · colour = CPU load · drag a box to drill in · hover for details",
			len(snap.Procs), procMetricLabel(inst.procBuiltMetric))).Send()
	}
	c.AddSpace(inst.spaceInner())

	// Size the treemap to the pane, reserving room below for the hover line.
	// One-frame lag on available_size — the CPU-heatmap / topology idiom.
	c.CaptureAvailableSize()
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	if avail.W > 0 && avail.H > 0 &&
		!math.IsNaN(float64(avail.W)) && !math.IsNaN(float64(avail.H)) {
		w, h := avail.W, avail.H-procMapReservedBelowPx
		if h < procMapMinH {
			h = procMapMinH
			w -= procMapScrollbarAllowPx // vertical scrollbar is showing
		}
		if w < procMapMinW {
			w = procMapMinW
		}
		inst.procTreemap.SetContainerSize(w, h)
	}
	inst.procTreemap.Render()

	c.AddSpace(inst.spaceTight())
	inst.renderProcMapHoverDetail()
}

// renderProcMapHoverDetail prints a one-line readout for the hovered process:
// PID, name, user, smoothed CPU%, RSS, and the command line.
func (inst *App) renderProcMapHoverDetail() {
	n := inst.procTreemap.HoveredNode()
	if n == nil {
		c.Label("Hover a box for details.").Send()
		return
	}
	pc := inst.procNodeObj[n]
	if pc == nil {
		c.Label(n.Name).Send()
		return
	}
	p := pc.info
	var b strings.Builder
	fmt.Fprintf(&b, "PID %d · %s", p.PID, p.Name)
	if p.User != "" {
		fmt.Fprintf(&b, " · %s", p.User)
	}
	fmt.Fprintf(&b, " · %.0f%% CPU · %s RSS", pc.cpu, humanBytes(p.RSSBytes))
	if p.Cmd != "" {
		fmt.Fprintf(&b, " · %s", p.Cmd)
	}
	c.Label(b.String()).Send()
}
