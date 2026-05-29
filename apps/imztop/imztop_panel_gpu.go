//go:build llm_generated_opus47

package imztop

import (
	"fmt"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderGPUPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("GPU")
	if snap.LatestGPU == nil {
		c.Label("GPU collector unavailable").Send()
		return
	}
	if len(snap.LatestGPU.Devices) == 0 {
		c.Label("No GPU devices detected").Send()
		return
	}
	for i, d := range snap.LatestGPU.Devices {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(0x500 + i))) {
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabelColored(markerColor(i), colorBgClear, fmt.Sprintf("● [%d]", d.Index)) {
					rt.Strong()
				}
				c.AddSpace(inst.spaceInline())
				c.Label(fmt.Sprintf("%s %s", d.Vendor, trimTo(d.Name, 28))).Send()
			}
			for range c.Horizontal().KeepIter() {
				c.Label(fmt.Sprintf("busy %d%%", d.BusyPercent)).Send()
				if d.MemoryTotalBytes > 0 {
					c.AddSpace(inst.spaceOuter())
					c.Label(fmt.Sprintf("vram %s / %s", humanBytes(d.MemoryUsedBytes), humanBytes(d.MemoryTotalBytes))).Send()
				}
				if d.PowerWatts > 0 {
					c.AddSpace(inst.spaceOuter())
					c.Label(fmt.Sprintf("%.1f W", d.PowerWatts)).Send()
				}
				if d.TempC > 0 {
					c.AddSpace(inst.spaceOuter())
					c.Label(fmt.Sprintf("%.0f °C", d.TempC)).Send()
				}
				if d.FreqMHz > 0 {
					c.AddSpace(inst.spaceOuter())
					c.Label(fmt.Sprintf("%d MHz", d.FreqMHz)).Send()
				}
			}
			c.AddSpace(inst.spaceInner())
		}
	}

	// Cross-device busy% distsummary — mirrors the CPU panel's
	// "cores" line for GPU devices. Cheap and useful even with a
	// single GPU (n=1 collapses min/Q1/median/Q3/max to the same
	// value, which still answers the "is this device busy?" read
	// at a glance).
	busies := make([]float64, 0, len(snap.LatestGPU.Devices))
	for _, d := range snap.LatestGPU.Devices {
		busies = append(busies, float64(d.BusyPercent))
	}
	c.AddSpace(inst.spaceInner())
	inst.renderPerDeviceDistsummary(
		inst.gpuDistsumDigest, busies,
		"devices", "gpu-distsum", "app.imztop.event.gpu.busy.per_device.pct",
		time.UnixMilli(snap.SampledAtUnixMs), formatPercent,
	)

	times := snap.HistoryTimeUnixSec
	if len(times) < 2 {
		return
	}
	drew := false
	for i, dev := range snap.HistoryGPUBusyPerDev {
		if len(dev) != len(times) {
			continue
		}
		c.PlotLine(fmt.Sprintf("gpu%d %%", i), times, dev).
			Width(1.8).Color(markerColor(i)).Send()
		drew = true
	}
	if !drew {
		return
	}
	c.AddSpace(inst.spaceTight())
	c.PlotHLine("100%", 100).Color(colorGridLine).Width(0.5).Send()
	plot := c.Plot(inst.ids.PrepareStr("gpu-busy-plot")).
		Height(144).
		YAxisLabel("%").
		Legend().
		IncludeY(0).IncludeY(100).
		AllowZoom2(true, false).
		AllowDrag2(true, false).
		AllowScroll2(true, false)
	plot = applyYTalbotTicks(plot, 0, 100, 5)
	plot.Send()
}
