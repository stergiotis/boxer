//go:build llm_generated_opus48

package imztop

import (
	"fmt"
	"math"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sensors"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// topoMinW / topoMinH floor the dynamically-sized treemap canvas. The panel
// resizes the treemap to fill its dock pane every frame (renderTopologyPanel);
// the floor keeps the tree legible on a small pane — below it the tab's
// ScrollArea scrolls rather than collapsing the boxes. topoScrollbarAllowPx is
// shaved off the width while the vertical scrollbar is showing so the
// Vscroll-only ScrollArea does not clip the treemap's right edge.
const (
	topoMinW             float32 = 360
	topoMinH             float32 = 320
	topoScrollbarAllowPx float32 = 16
)

// topoDimE selects which live per-core dimension the continuous (sequential)
// tint encodes. The discrete depth palette is independent of this choice.
type topoDimE uint8

const (
	topoDimLoad topoDimE = iota // per-core busy %
	topoDimFreq                 // per-core frequency
)

var topoDims = []topoDimE{topoDimLoad, topoDimFreq}

func topoDimLabel(d topoDimE) string {
	if d == topoDimFreq {
		return "Frequency"
	}
	return "CPU %"
}

// topoDepthPalette builds a discrete palette from the IDS qualitative cycle so
// structural depth (machine / package / cache / core …) reads as distinct
// hues — leaving the sequential gradient (cpuHeatmapPalette) to mean magnitude,
// i.e. load. 8 entries is deeper than the topology ever nests, so adjacent
// levels never collide.
func topoDepthPalette() (p []uint32) {
	const n = 8
	p = make([]uint32, n)
	for i := range n {
		q := styletokens.QualitativeCycle(i)
		p[i] = uint32(q.R)<<24 | uint32(q.G)<<16 | uint32(q.B)<<8 | uint32(q.A)
	}
	return
}

// initTopology performs the one-shot sysfs topology read and builds the treemap
// widget. It is called lazily on the first Topology-tab frame (not in newApp)
// so it captures the post-Mount inst.ids. On read failure topoErr is recorded
// and the panel renders a message instead of a tree.
func (inst *App) initTopology() {
	topo, err := cpu.ReadTopology(cpu.TopologyOptions{})
	if err != nil {
		inst.topoErr = err
		return
	}
	root, nodeObj := buildTopoLayout(topo)
	inst.topoNodeObj = nodeObj

	// Continuous (sequential) tint over the live dimension, normalised to
	// [0,1] so switching dimension (% ↔ MHz) only changes how loadFn scales —
	// the colormap range stays fixed. Non-PU nodes return NaN (ok=false) and
	// fall through to the discrete depth base. Layer order matters:
	// CompositeColoring is last-ok-wins and DepthColoring always returns ok, so
	// the depth base must be FIRST and the load override LAST.
	loadFn := func(n *layout.Node) (v float64) {
		obj := inst.topoNodeObj[n]
		if obj == nil || obj.Kind != cpu.TopoKindPU {
			return math.NaN()
		}
		id := int(obj.OSIndex)
		switch inst.topoDim {
		case topoDimFreq:
			if id < 0 || id >= len(inst.topoFreq) || inst.topoFreqMaxMHz == 0 {
				return math.NaN()
			}
			return float64(inst.topoFreq[id]) / float64(inst.topoFreqMaxMHz)
		default:
			if id < 0 || id >= len(inst.topoLoad) {
				return math.NaN()
			}
			return float64(inst.topoLoad[id]) / 100
		}
	}
	coloring := treemap.CompositeColoring(
		treemap.DepthColoring(topoDepthPalette()),
		treemap.ContinuousColoring(cpuHeatmapPalette(), loadFn, 0, 1),
	)
	// No WithContainerSize: the canvas is sized per-frame to fill the dock
	// pane. WithMaxNestingDepth(0) renders the whole hierarchy at once
	// (lstopo-style); drill-in still works on the top-level boxes.
	inst.topoTreemap = treemap.New(inst.ids, "imztop-topology", root,
		treemap.WithMaxNestingDepth(0),
		treemap.WithColoring(coloring),
	)
}

// renderTopologyPanel draws the lstopo-style CPU containment tree (package →
// NUMA → L3/L2/L1 → core → SMT thread) as nested treemap boxes: depth by a
// discrete IDS palette, the selected live dimension by a sequential gradient.
// A hover-detail line below reports per-object data. See ADR-0020 (Update).
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

	// Refresh the live slices the coloring closure reads. Held on *App so the
	// colors update each frame without rebuilding the (static) tree. Frequency
	// is normalised against a running max so the tint reads as "fraction of
	// peak observed clock".
	if snap != nil && snap.LatestCPU != nil {
		inst.topoLoad = snap.LatestCPU.PerCorePercent
		inst.topoFreq = snap.LatestCPU.PerCoreFreqMHz
		for _, f := range inst.topoFreq {
			if f > inst.topoFreqMaxMHz {
				inst.topoFreqMaxMHz = f
			}
		}
	}

	// Tint-dimension switch (% ↔ frequency).
	for range c.ComboBox(
		inst.ids.PrepareStr("topo-dim-cb"),
		c.WidgetText().Text("tint").Keep(),
		c.WidgetText().Text(topoDimLabel(inst.topoDim)).Keep(),
	).KeepIter() {
		for i, d := range topoDims {
			sel := d == inst.topoDim
			if c.Button(inst.ids.PrepareSeq(uint64(0x300+i)), c.Atoms().Text(topoDimLabel(d)).Keep()).
				Selected(sel).
				FrameWhenInactive(!sel).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				inst.topoDim = d
			}
		}
	}

	for range c.Horizontal().KeepIter() {
		c.Label("boxes nest package → cache → core → thread · drag a box to drill in · hover for details").Send()
	}
	c.AddSpace(inst.spaceInner())

	// Fill the dock pane: track ui.available_size each frame and resize the
	// treemap to it (one-frame lag, the CPU-heatmap idiom). Captured here —
	// after the header/controls — so it reflects the space left for the tree.
	c.CaptureAvailableSize()
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	if avail.W > 0 && avail.H > 0 &&
		!math.IsNaN(float64(avail.W)) && !math.IsNaN(float64(avail.H)) {
		w, h := avail.W, avail.H
		if h < topoMinH {
			h = topoMinH
			w -= topoScrollbarAllowPx // vertical scrollbar is showing
		}
		if w < topoMinW {
			w = topoMinW
		}
		inst.topoTreemap.SetContainerSize(w, h)
	}
	inst.topoTreemap.Render()

	c.AddSpace(inst.spaceTight())
	inst.renderTopoHoverDetail(snap)
}

// renderTopoHoverDetail prints a one-line readout of the hovered object: its
// label plus the per-level data we have (static structure + live metrics).
func (inst *App) renderTopoHoverDetail(snap *PublishedSnapshot) {
	n := inst.topoTreemap.HoveredNode()
	if n == nil {
		c.Label("Hover a box for details.").Send()
		return
	}
	obj := inst.topoNodeObj[n]
	if obj == nil {
		c.Label(n.Name).Send()
		return
	}

	var cs *cpu.Snapshot
	if snap != nil {
		cs = snap.LatestCPU
	}

	var b strings.Builder
	b.WriteString(obj.Label())
	switch obj.Kind {
	case cpu.TopoKindPU:
		id := int(obj.OSIndex)
		if cs != nil {
			if id >= 0 && id < len(cs.PerCorePercent) {
				fmt.Fprintf(&b, " · %d%% busy", cs.PerCorePercent[id])
			}
			if id >= 0 && id < len(cs.PerCoreFreqMHz) && cs.PerCoreFreqMHz[id] > 0 {
				fmt.Fprintf(&b, " · %s", mhzLabel(cs.PerCoreFreqMHz[id]))
			}
		}
	case cpu.TopoKindCore:
		fmt.Fprintf(&b, " · %d threads", countKind(obj, cpu.TopoKindPU))
		if pct, mhz, ok := inst.aggLoadFreq(obj); ok {
			fmt.Fprintf(&b, " · %.0f%% busy", pct)
			if mhz > 0 {
				fmt.Fprintf(&b, " · %s", mhzLabel(mhz))
			}
		}
	case cpu.TopoKindCache:
		fmt.Fprintf(&b, " · shared by %d threads / %d cores",
			countKind(obj, cpu.TopoKindPU), countKind(obj, cpu.TopoKindCore))
	case cpu.TopoKindPackage:
		fmt.Fprintf(&b, " · %d cores / %d threads",
			countKind(obj, cpu.TopoKindCore), countKind(obj, cpu.TopoKindPU))
		if cs != nil && cs.UsageWattsAvailable {
			fmt.Fprintf(&b, " · %.1f W", cs.UsageWatts)
		}
		if snap != nil {
			if tC, ok := packageTempC(snap.Sensors); ok {
				fmt.Fprintf(&b, " · %.0f°C", tC)
			}
		}
	case cpu.TopoKindNUMANode:
		fmt.Fprintf(&b, " · %d cores / %d threads",
			countKind(obj, cpu.TopoKindCore), countKind(obj, cpu.TopoKindPU))
	case cpu.TopoKindMachine:
		if cs != nil {
			if cs.ModelName != "" {
				fmt.Fprintf(&b, " · %s", cs.ModelName)
			}
			fmt.Fprintf(&b, " · %d threads · %d%% · load %.2f / %.2f / %.2f",
				cs.LogicalCores, cs.TotalPercent, cs.LoadAvg1, cs.LoadAvg5, cs.LoadAvg15)
			if cs.UsageWattsAvailable {
				fmt.Fprintf(&b, " · %.1f W", cs.UsageWatts)
			}
		}
	}
	c.Label(b.String()).Send()
}

// aggLoadFreq averages live busy% and frequency over the PU leaves under o.
func (inst *App) aggLoadFreq(o *cpu.TopoObject) (pct float64, mhz uint32, ok bool) {
	ids := puIndexes(o)
	var sumPct float64
	var sumMHz uint64
	var nPct, nMHz int
	for _, id := range ids {
		i := int(id)
		if i >= 0 && i < len(inst.topoLoad) {
			sumPct += float64(inst.topoLoad[i])
			nPct++
		}
		if i >= 0 && i < len(inst.topoFreq) && inst.topoFreq[i] > 0 {
			sumMHz += uint64(inst.topoFreq[i])
			nMHz++
		}
	}
	if nPct == 0 {
		return 0, 0, false
	}
	pct = sumPct / float64(nPct)
	if nMHz > 0 {
		mhz = uint32(sumMHz / uint64(nMHz))
	}
	return pct, mhz, true
}

// mhzLabel renders a core frequency as GHz above 1 GHz, else MHz.
func mhzLabel(mhz uint32) string {
	if mhz >= 1000 {
		return fmt.Sprintf("%.2f GHz", float64(mhz)/1000)
	}
	return fmt.Sprintf("%d MHz", mhz)
}

// packageTempC returns the first CPU-package temperature reading, if any. The
// label→object match is heuristic (Tctl / Tdie / "Package id N"); per-core
// matching is intentionally not attempted (AMD reports per-CCD, not per-core).
func packageTempC(readings []sensors.TempReading) (tC float32, ok bool) {
	for _, r := range readings {
		if r.KindCPUPackage {
			return r.TempC, true
		}
	}
	return 0, false
}
