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
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

const (
	// topoMinW / topoMinH floor the dynamically-sized treemap canvas; below
	// the floor the tab's ScrollArea scrolls instead of collapsing the tree.
	// topoScrollbarAllowPx is shaved off the width while the vertical scrollbar
	// shows so the Vscroll-only ScrollArea does not clip the right edge.
	topoMinW             float32 = 360
	topoMinH             float32 = 320
	topoScrollbarAllowPx float32 = 16
	// topoReservedBelowPx leaves room under the tree for the colorscale legend
	// and the hover-detail line when sizing the treemap to the pane.
	topoReservedBelowPx float32 = 104
	// topoScaleW / topoScaleH size the horizontal colorscale legend. The height
	// must fit the gradient strip (the widget gives it 55%) plus the tick marks
	// and a row of labels *below* them — at 26 px the labels paint past the
	// canvas and clip, so ~44 px (cf. the colorscale demo's 42) is the floor.
	topoScaleW float32 = 420
	topoScaleH float32 = 44
	// topoFreqReleaseAlpha is the per-sample slow-release factor for the
	// frequency-max estimate (fast attack, slow release). ~0.1 at the 1 Hz
	// sampler ≈ a 10 s memory, so a one-off boost spike fades gradually rather
	// than latching the legend's top forever.
	topoFreqReleaseAlpha = 0.1
)

// topoDimE selects which live per-core dimension the continuous (sequential)
// tint encodes. The monochrome depth palette is independent of this choice.
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

// topoDepthPalette samples the IDS curated monochrome ramp — Crameri grayC,
// the same perceptually-uniform grayscale IDS uses for AccessibilityMonochrome
// ([styletokens.SequentialGrayC]) — at 8 discrete depth steps. Keeping the
// structure greyscale leaves the one *coloured* gradient (cpuHeatmapPalette)
// to mean magnitude (the load/frequency tint), so colour never competes with
// hierarchy. grayC runs white→black over t∈[0,1]; we walk a mid-dark sub-range
// so outer containers stay subdued on the dark theme and grow lighter as the
// hierarchy nests inward toward the coloured PU cells.
func topoDepthPalette() (p []uint32) {
	const (
		n             = 8
		tDark, tLight = 0.85, 0.32 // grayC t at the outer (dark) / inner (light) ends
	)
	p = make([]uint32, n)
	for i := range n {
		t := tDark + (tLight-tDark)*float64(i)/float64(n-1)
		rgba := styletokens.Sequential(styletokens.SequentialGrayC, float32(t))
		p[i] = uint32(rgba.R)<<24 | uint32(rgba.G)<<16 | uint32(rgba.B)<<8 | uint32(rgba.A)
	}
	return
}

// initTopology performs the one-shot sysfs topology read and builds the treemap
// widget. Called lazily on the first Topology-tab frame so it captures the
// post-Mount inst.ids. On read failure topoErr is recorded.
func (inst *App) initTopology() {
	topo, err := cpu.ReadTopology(cpu.TopologyOptions{})
	if err != nil {
		inst.topoErr = err
		return
	}
	root, nodeObj := buildTopoLayout(topo)
	inst.topoNodeObj = nodeObj

	// The tint is normalised to [0,1] against inst.topoScaleMax (the same value
	// the legend tops out at, so tint and colorscale agree). Non-PU nodes
	// return NaN (ok=false) and fall through to the monochrome depth base.
	// Layer order matters: CompositeColoring is last-ok-wins and DepthColoring
	// always returns ok, so depth is FIRST and the load override LAST.
	loadFn := func(n *layout.Node) (v float64) {
		obj := inst.topoNodeObj[n]
		if obj == nil || obj.Kind != cpu.TopoKindPU || inst.topoScaleMax == 0 {
			return math.NaN()
		}
		id := int(obj.OSIndex)
		var raw uint32
		if inst.topoDim == topoDimFreq {
			if id < 0 || id >= len(inst.topoFreq) {
				return math.NaN()
			}
			raw = inst.topoFreq[id]
		} else {
			if id < 0 || id >= len(inst.topoLoad) {
				return math.NaN()
			}
			raw = uint32(inst.topoLoad[id])
		}
		return float64(raw) / float64(inst.topoScaleMax)
	}
	coloring := treemap.CompositeColoring(
		treemap.DepthColoring(topoDepthPalette()),
		treemap.ContinuousColoring(cpuHeatmapPalette(), loadFn, 0, 1),
	)
	// No WithContainerSize: sized per-frame to fill the dock pane.
	// WithMaxNestingDepth(0) renders the whole hierarchy at once (lstopo-style);
	// drill-in still works on the top-level boxes.
	inst.topoTreemap = treemap.New(inst.ids, "imztop-topology", root,
		treemap.WithMaxNestingDepth(0),
		treemap.WithColoring(coloring),
	)
}

// renderTopologyPanel draws the lstopo-style CPU containment tree: monochrome
// depth, the selected live dimension as a sequential gradient, a colorscale
// legend, and a hover-detail readout. See ADR-0020 (Update).
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

	// Refresh the live slices the coloring reads, and re-estimate the frequency
	// max once per new sample (fast attack, slow release). topoScaleMax is the
	// shared denominator for the tint and the legend's top value.
	if snap != nil && snap.LatestCPU != nil {
		inst.topoLoad = snap.LatestCPU.PerCorePercent
		inst.topoFreq = snap.LatestCPU.PerCoreFreqMHz
		if snap.SampledAtUnixMs != inst.topoLastSampleMs {
			inst.topoLastSampleMs = snap.SampledAtUnixMs
			inst.updateFreqMax()
		}
	}
	inst.topoScaleMax = inst.scaleMaxFor(inst.topoDim)

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

	// Size the treemap to the pane, reserving room below for the legend + hover
	// line. One-frame lag on available_size, the CPU-heatmap idiom.
	c.CaptureAvailableSize()
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	if avail.W > 0 && avail.H > 0 &&
		!math.IsNaN(float64(avail.W)) && !math.IsNaN(float64(avail.H)) {
		w, h := avail.W, avail.H-topoReservedBelowPx
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

	// Colorscale legend (gradient + value axis), rebuilt only when the
	// dimension or rounded max changes.
	c.AddSpace(inst.spaceTight())
	inst.ensureTopoScale()
	if inst.topoScale != nil {
		inst.topoScale.Render()
	}

	c.AddSpace(inst.spaceTight())
	inst.renderTopoHoverDetail(snap)
}

// updateFreqMax re-estimates the peak core frequency with a fast-attack /
// slow-release follower: jump up immediately so a live value never exceeds the
// scale, decay slowly so a transient boost spike does not pin the top forever.
func (inst *App) updateFreqMax() {
	var frameMax uint32
	for _, f := range inst.topoFreq {
		if f > frameMax {
			frameMax = f
		}
	}
	switch {
	case frameMax > inst.topoFreqMaxMHz:
		inst.topoFreqMaxMHz = frameMax
	case inst.topoFreqMaxMHz > 0:
		inst.topoFreqMaxMHz = uint32(float64(inst.topoFreqMaxMHz)*(1-topoFreqReleaseAlpha) +
			float64(frameMax)*topoFreqReleaseAlpha)
	}
}

// scaleMaxFor returns the value the gradient tops out at, in the dimension's
// real units. Utilisation has a known max (100); frequency does not, so we use
// the smoothed peak rounded up to a clean 100 MHz (stable legend, rare rebuild).
func (inst *App) scaleMaxFor(d topoDimE) uint32 {
	if d == topoDimFreq {
		return roundUp100(inst.topoFreqMaxMHz)
	}
	return 100
}

func roundUp100(v uint32) uint32 {
	if v == 0 {
		return 0
	}
	return ((v + 99) / 100) * 100
}

// ensureTopoScale (re)builds the colorscale legend when the dimension or the
// rounded max changes. nil when there is nothing to show yet (frequency before
// the first sample).
func (inst *App) ensureTopoScale() {
	if inst.topoScaleMax == 0 {
		inst.topoScale = nil
		inst.topoScaleKey = ""
		return
	}
	key := fmt.Sprintf("%d:%d", inst.topoDim, inst.topoScaleMax)
	if key == inst.topoScaleKey && inst.topoScale != nil {
		return
	}
	inst.topoScaleKey = key

	cm := treemap.NewColormap(cpuHeatmapPalette(), 0, float64(inst.topoScaleMax))
	var labelFmt func(float64) string
	if inst.topoDim == topoDimFreq {
		labelFmt = func(v float64) string { return mhzLabel(uint32(v)) }
	} else {
		labelFmt = func(v float64) string { return fmt.Sprintf("%.0f%%", v) }
	}
	inst.topoScale = colorscale.New(inst.ids, "imztop-topo-scale", cm,
		colorscale.WithSize(topoScaleW, topoScaleH),
		colorscale.WithDesiredTicks(5),
		colorscale.WithLabelFormat(labelFmt),
	)
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
