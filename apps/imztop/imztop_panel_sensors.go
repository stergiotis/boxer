package imztop

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderSensorsPanel(snap *PublishedSnapshot) {
	if len(snap.Sensors) == 0 {
		return
	}
	inst.sectionHeader("Sensors")
	for i, r := range snap.Sensors {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(0x400 + i))) {
			for range c.Horizontal().KeepIter() {
				c.Label(fmt.Sprintf("%-32s", trimTo(r.Name, 32))).Send()
				c.AddSpace(inst.spaceItems())
				c.Label(fmt.Sprintf("%5.1f °C", r.TempC)).Send()
				if r.CriticalC > 0 {
					c.AddSpace(inst.spaceOuter())
					c.Label(fmt.Sprintf("crit %.0f °C", r.CriticalC)).Send()
				}
			}
		}
	}
}
