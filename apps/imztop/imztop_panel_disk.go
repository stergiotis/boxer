//go:build llm_generated_opus47

package imztop

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// colorDiskFill is the ProgressBar fill behind each mount's used/total
// percentage. IDS InfoDefault @ alpha 0x88: blue-family hue marks
// disk-capacity bars as informational (matching colorMetricPrimary
// for primary metric series), and the half-alpha keeps the bar a
// tinted background rather than a solid block so the percentage
// label reads cleanly on top. Withholds the IDS-alpha-aware token
// path scheduled in ADR-0029 §SD12 via the local withAlpha bridge.
var colorDiskFill = withAlpha(styletokens.InfoDefault, 0x88)

func (inst *App) renderDiskPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Disk")
	if snap.LatestDisk == nil {
		c.Label("Disk collector unavailable").Send()
		return
	}
	diskSnap := snap.LatestDisk

	for i, m := range diskSnap.Mounts {
		if !m.Real {
			continue
		}
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				c.Label(fmt.Sprintf("%-20s", trimTo(m.MountPoint, 20))).Send()
				c.AddSpace(inst.spaceItems())
				c.Label(fmt.Sprintf("%-8s", trimTo(m.FSType, 8))).Send()
				c.AddSpace(inst.spaceItems())
				c.Label(fmt.Sprintf("%s / %s", humanBytes(m.Capacity.UsedBytes), humanBytes(m.Capacity.TotalBytes))).Send()
			}
			var frac float32
			if m.Capacity.TotalBytes > 0 {
				frac = float32(m.Capacity.UsedBytes) / float32(m.Capacity.TotalBytes)
			}
			c.ProgressBar(frac).
				Text(fmt.Sprintf("%.1f%%", frac*100)).
				Fill(colorDiskFill).
				Send()
			c.AddSpace(inst.spaceInner())
		}
	}

	if len(diskSnap.BlockDevices) > 0 {
		c.AddSpace(inst.spaceTight())
		for rt := range c.RichTextLabel("Block devices") {
			rt.Strong()
		}
		c.AddSpace(inst.spaceInner())
		for i, d := range diskSnap.BlockDevices {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(0x100 + i))) {
				for range c.Horizontal().KeepIter() {
					for rt := range c.RichTextLabelColored(markerColor(i), colorBgClear, fmt.Sprintf("● %s", d.Name)) {
						rt.Strong()
					}
					c.AddSpace(inst.spaceItems())
					c.Label(fmt.Sprintf("R %s/s   W %s/s   busy %d%%", humanBytes(d.ReadBytesPerSec), humanBytes(d.WriteBytesPerSec), d.BusyPercent)).Send()
				}
			}
		}
		// Cross-device busy% distsummary — same idiom as the CPU
		// panel's "cores" line, surfacing "is one device hot while
		// others idle?" at a glance. Busy% is the most analogous
		// per-device load metric (matches CPU per-core %, GPU
		// busy%); throughput is already covered by the per-device
		// plot below.
		busies := make([]float64, 0, len(diskSnap.BlockDevices))
		for _, d := range diskSnap.BlockDevices {
			busies = append(busies, float64(d.BusyPercent))
		}
		c.AddSpace(inst.spaceInner())
		inst.renderPerDeviceDistsummary(
			inst.diskDistsumDigest, busies,
			"devices", "disk-distsum", "app.imztop.event.disk.busy.per_device.pct",
			time.UnixMilli(snap.SampledAtUnixMs), formatPercent,
		)
	}

	times := snap.HistoryTimeUnixSec
	if len(times) >= 2 {
		// A separator + outer-padding gap keeps the disk-IO plot's
		// Y-axis labels (which start at the very top edge of the plot
		// rect) visually distinct from the Block-devices list above.
		// Without it, "400" / "300" labels read as if they're attached
		// to the device rows.
		c.AddSpace(inst.spaceInner())
		c.Separator().Horizontal().Send()
		c.AddSpace(inst.spaceOuter())
		for i, s := range snap.HistoryDiskReadByDev {
			if len(s.Y) != len(times) {
				continue
			}
			c.PlotLine(fmt.Sprintf("%s R", s.Name), times, s.Y).
				Width(1.2).Color(markerColor(i)).Send()
		}
		for i, s := range snap.HistoryDiskWriteByDev {
			if len(s.Y) != len(times) {
				continue
			}
			c.PlotLine(fmt.Sprintf("%s W", s.Name), times, s.Y).
				Width(1.2).Color(markerColor(i)).Highlight(false).Send()
		}
		if len(snap.HistoryDiskRead) == len(times) {
			c.PlotLine("Σ read", times, snap.HistoryDiskRead).
				Width(2.4).Color(markerColor(0)).Send()
		}
		if len(snap.HistoryDiskWrite) == len(times) {
			c.PlotLine("Σ write", times, snap.HistoryDiskWrite).
				Width(2.4).Color(markerColor(1)).Send()
		}
		plot := c.Plot(inst.ids.PrepareStr("disk-io-plot")).
			Height(168).
			YAxisLabel("MiB/s").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false)
		plot = applyYTalbotTicks(plot, 0, rateUpperBound(snap.HistoryDiskRead, snap.HistoryDiskWrite), 5)
		plot.Send()
	}
}

func trimTo(s string, n int) (out string) {
	if len(s) <= n {
		out = s
		return
	}
	out = s[:n-1] + "…"
	return
}
