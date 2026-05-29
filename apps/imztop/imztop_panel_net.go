//go:build llm_generated_opus47

package imztop

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Aggregate Σ-rx / Σ-tx series colors come from the IDS qualitative
// cycle (qualitativeColor in imztop_theme.go) — slots 0/1 give a
// CVD-distinct pair from Crameri batlowS, replacing the pre-IDS
// 0x44cc88 / 0xcc4488 ad-hoc duo.

// renderNetPanel is an App method because the selected-interface
// state (inst.netSelectedIfaceIdx) is per-window: clicking an entry
// in window 1's ComboBox must not flip window 2's selection.
func (inst *App) renderNetPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Network")
	if snap.LatestNet == nil {
		c.Label("Net collector unavailable").Send()
		return
	}
	netSnap := snap.LatestNet

	if len(netSnap.Interfaces) == 0 {
		c.Label("No interfaces detected").Send()
		return
	}

	if inst.netSelectedIfaceIdx >= len(netSnap.Interfaces) {
		inst.netSelectedIfaceIdx = 0
	}
	current := netSnap.Interfaces[inst.netSelectedIfaceIdx]

	for range c.ComboBox(
		inst.ids.PrepareStr("net-iface-cb"),
		c.WidgetText().Text("interface").Keep(),
		c.WidgetText().Text(current.Name).Keep(),
	).KeepIter() {
		for i, ifc := range netSnap.Interfaces {
			selected := i == inst.netSelectedIfaceIdx
			if c.Button(inst.ids.PrepareSeq(uint64(0x200+i)), c.Atoms().Text(ifc.Name).Keep()).
				Selected(selected).
				FrameWhenInactive(!selected).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				inst.netSelectedIfaceIdx = i
			}
		}
	}

	state := "down"
	switch {
	case current.Up && current.Running:
		state = "up"
	case current.Up:
		state = "up (no carrier)"
	}
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(current.Name) {
			rt.Strong()
		}
		c.AddSpace(inst.spaceInline())
		c.Label(fmt.Sprintf("[%s]", state)).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label(fmt.Sprintf("RX %s/s", humanBytes(current.RxBytesPerSec))).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label(fmt.Sprintf("TX %s/s", humanBytes(current.TxBytesPerSec))).Send()
	}
	if current.HardwareAddr != "" {
		c.Label(fmt.Sprintf("MAC %s", current.HardwareAddr)).Send()
	}
	if len(current.IPv4) > 0 {
		c.Label(fmt.Sprintf("IPv4: %s", joinShort(current.IPv4))).Send()
	}
	if len(current.IPv6) > 0 {
		c.Label(fmt.Sprintf("IPv6: %s", joinShort(current.IPv6))).Send()
	}

	// No per-interface distsummary here — utilization (the
	// commensurable cross-device metric Disk/GPU use) needs link
	// capacity to be meaningful, and netcoll.Interface doesn't yet
	// expose link speed. A throughput-based summary would mix
	// heterogeneous interfaces (lo vs ethX vs wlanX vs virtual)
	// and degenerate under typical "one busy + many idle" patterns,
	// so the row was dropped rather than ship a misleading summary.
	// Re-add once the net collector exposes /sys/class/net/<iface>/speed.

	times := snap.HistoryTimeUnixSec
	if len(times) >= 2 {
		// Visual separator before the net-IO plot; without it the
		// Y-axis labels read as part of the interface info above.
		c.AddSpace(inst.spaceInner())
		c.Separator().Horizontal().Send()
		c.AddSpace(inst.spaceOuter())
		for i, s := range snap.HistoryNetRxByIface {
			if len(s.Y) != len(times) {
				continue
			}
			c.PlotLine(fmt.Sprintf("%s rx", s.Name), times, s.Y).
				Width(1.2).Color(markerColor(i)).Send()
		}
		for i, s := range snap.HistoryNetTxByIface {
			if len(s.Y) != len(times) {
				continue
			}
			c.PlotLine(fmt.Sprintf("%s tx", s.Name), times, s.Y).
				Width(1.2).Color(markerColor(i)).Highlight(false).Send()
		}
		if len(snap.HistoryNetRx) == len(times) {
			c.PlotLine("Σ rx", times, snap.HistoryNetRx).
				Width(2.4).Color(markerColor(0)).Send()
		}
		if len(snap.HistoryNetTx) == len(times) {
			c.PlotLine("Σ tx", times, snap.HistoryNetTx).
				Width(2.4).Color(markerColor(1)).Send()
		}
		plot := c.Plot(inst.ids.PrepareStr("net-io-plot")).
			Height(168).
			YAxisLabel("MiB/s").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false)
		plot = applyYTalbotTicks(plot, 0, rateUpperBound(snap.HistoryNetRx, snap.HistoryNetTx), 5)
		plot.Send()
	}
}

func joinShort(items []string) (out string) {
	const max = 3
	if len(items) <= max {
		for i, s := range items {
			if i > 0 {
				out += ", "
			}
			out += s
		}
		return
	}
	for i := range max {
		if i > 0 {
			out += ", "
		}
		out += items[i]
	}
	out += fmt.Sprintf(", …+%d", len(items)-max)
	return
}
