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
		inst.renderRateHistoryPlot(times, ratePlotSpec{
			plotID:            "disk-io-plot",
			primaryByDev:      snap.HistoryDiskReadByDev,
			secondaryByDev:    snap.HistoryDiskWriteByDev,
			primaryDevLabel:   "R",
			secondaryDevLabel: "W",
			primarySum:        snap.HistoryDiskRead,
			secondarySum:      snap.HistoryDiskWrite,
			primarySumLabel:   "Σ read",
			secondarySumLabel: "Σ write",
		})
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
