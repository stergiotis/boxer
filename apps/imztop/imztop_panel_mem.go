//go:build llm_generated_opus47

package imztop

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// colorMemFill is the translucent ProgressBar fill behind the
// RAM-used percentage. IDS AccentDefault @ alpha 0x88 marks main
// memory as the system's "focus" colour — distinct from disk's
// InfoDefault so the two bars don't read identically when both
// panels are visible side-by-side. History-plot line keeps using
// colorMetricPrimary (InfoDefault) from imztop_theme.go so the
// primary scalar series stays in the "info" family across panels.
//
// colorSwapFill marks swap-used as a Warning role: any non-trivial
// swap activity signals RAM pressure, which is a system-degradation
// hint worth surfacing in amber. Alpha 0xff (opaque) so the swap
// bar reads more emphatically than the half-alpha RAM bar above it.
var (
	colorMemFill  = withAlpha(styletokens.AccentDefault, 0x88)
	colorSwapFill = withAlpha(styletokens.WarningDefault, 0xff)
)

func (inst *App) renderMemPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Memory")
	if snap.LatestMem == nil {
		c.Label("Memory collector unavailable").Send()
		return
	}
	memSnap := snap.LatestMem

	var usedFrac float32
	if memSnap.TotalBytes > 0 {
		usedFrac = float32(memSnap.UsedBytes) / float32(memSnap.TotalBytes)
	}
	var swapFrac float32
	if memSnap.SwapTotalBytes > 0 {
		swapFrac = float32(memSnap.SwapUsedBytes) / float32(memSnap.SwapTotalBytes)
	}

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("RAM %s / %s", humanBytes(memSnap.UsedBytes), humanBytes(memSnap.TotalBytes))).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label(fmt.Sprintf("free %s", humanBytes(memSnap.FreeBytes))).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("buf %s", humanBytes(memSnap.BuffersBytes))).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label(fmt.Sprintf("cache %s", humanBytes(memSnap.CachedBytes))).Send()
	}
	c.AddSpace(inst.spaceInner())
	c.ProgressBar(usedFrac).
		Text(fmt.Sprintf("RAM %.1f%%", usedFrac*100)).
		Fill(colorMemFill).
		Send()

	if memSnap.SwapTotalBytes > 0 {
		c.AddSpace(inst.spaceInner())
		for range c.Horizontal().KeepIter() {
			c.Label(fmt.Sprintf("Swap %s / %s", humanBytes(memSnap.SwapUsedBytes), humanBytes(memSnap.SwapTotalBytes))).Send()
		}
		c.ProgressBar(swapFrac).
			Text(fmt.Sprintf("Swap %.1f%%", swapFrac*100)).
			Fill(colorSwapFill).
			Send()
	}

	if len(snap.HistoryTimeUnixSec) >= 2 && len(snap.HistoryMemUsed) == len(snap.HistoryTimeUnixSec) {
		c.AddSpace(inst.spaceTight())
		c.PlotLine("RAM %", snap.HistoryTimeUnixSec, snap.HistoryMemUsed).
			Width(2.0).Color(colorMetricPrimary).Send()
		c.PlotHLine("100%", 100).Color(colorGridLine).Width(0.5).Send()
		renderXAxisTicks(snap.HistoryTimeUnixSec, 0)
		plot := c.Plot(inst.ids.PrepareStr("mem-history-plot")).
			Height(168).
			YAxisLabel("%").
			Legend().
			IncludeY(0).IncludeY(100).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false)
		plot = applyYTalbotTicks(plot, 0, 100, 5)
		plot.Send()
	}
}

func humanBytes(n uint64) (s string) {
	const (
		kib = 1 << 10
		mib = 1 << 20
		gib = 1 << 30
		tib = 1 << 40
	)
	switch {
	case n >= tib:
		s = fmt.Sprintf("%.2f TiB", float64(n)/float64(tib))
	case n >= gib:
		s = fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		s = fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		s = fmt.Sprintf("%.0f KiB", float64(n)/float64(kib))
	default:
		s = fmt.Sprintf("%d B", n)
	}
	return
}
