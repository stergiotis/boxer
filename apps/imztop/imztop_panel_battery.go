package imztop

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/battery"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// colorBatteryFill / colorBatteryLow are the ProgressBar fills behind
// each battery's charge percentage. IDS SuccessDefault and ErrorDefault
// give the green/red threshold pair a stable semantic source; alpha
// 0x88 keeps each fill a tinted background so the "%d%%" overlay
// reads on top. The choice between them is driven by b.Percent in
// renderOneBattery (< 20 % flips to ErrorDefault).
var (
	colorBatteryFill = withAlpha(styletokens.SuccessDefault, 0x88)
	colorBatteryLow  = withAlpha(styletokens.ErrorDefault, 0x88)
)

func (inst *App) renderBatteryPanel(snap *PublishedSnapshot) {
	if snap.LatestBattery == nil || len(snap.LatestBattery.Batteries) == 0 {
		return
	}
	inst.sectionHeader("Battery")
	bs := snap.LatestBattery

	for i, b := range bs.Batteries {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(0x300 + i))) {
			inst.renderOneBattery(b)
		}
	}

	if len(bs.ACAdapters) > 0 {
		c.AddSpace(inst.spaceInner())
		for range c.Horizontal().KeepIter() {
			c.Label("AC").Send()
			for i, ac := range bs.ACAdapters {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(0x380 + i))) {
					state := "off"
					if ac.Online {
						state = "on"
					}
					c.AddSpace(inst.spaceItems())
					c.Label(fmt.Sprintf("%s [%s]", ac.Name, state)).Send()
				}
			}
		}
	}
}

func (inst *App) renderOneBattery(b battery.BatteryStatus) {
	frac := float32(b.Percent) / 100
	fill := colorBatteryFill
	if b.Percent < 20 {
		fill = colorBatteryLow
	}
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(b.Name) {
			rt.Strong()
		}
		c.AddSpace(inst.spaceItems())
		c.Label(b.State.String()).Send()
		if b.PowerWatts > 0 {
			c.AddSpace(inst.spaceOuter())
			c.Label(fmt.Sprintf("%.1f W", b.PowerWatts)).Send()
		}
		switch {
		case b.SecondsToEmpty > 0:
			c.AddSpace(inst.spaceOuter())
			c.Label(fmt.Sprintf("%s left", humanDuration(b.SecondsToEmpty))).Send()
		case b.SecondsToFull > 0:
			c.AddSpace(inst.spaceOuter())
			c.Label(fmt.Sprintf("%s to full", humanDuration(b.SecondsToFull))).Send()
		}
	}
	c.ProgressBar(frac).
		Text(fmt.Sprintf("%d%%", b.Percent)).
		Fill(fill).
		Send()
}

func humanDuration(seconds int64) (out string) {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h > 0 {
		out = fmt.Sprintf("%dh%02dm", h, m)
		return
	}
	out = fmt.Sprintf("%dm", m)
	return
}
