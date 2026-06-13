package imztop

import (
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// renderXAxisTicks places PlotText labels at calendar-aware tick
// positions across the time range covered by timeUnixSec. Call inside
// the same Plot scope as PlotLine, before Plot.Send. The labels land at
// y=labelY in plot data coordinates — choose a value within the plot's
// Y range (0 works for non-negative plots).
func renderXAxisTicks(timeUnixSec []float64, labelY float64) {
	if len(timeUnixSec) < 2 {
		return
	}
	minT := time.Unix(int64(timeUnixSec[0]), 0).Local()
	maxT := time.Unix(int64(timeUnixSec[len(timeUnixSec)-1]), 0).Local()
	if !maxT.After(minT) {
		return
	}
	layout := timeticks.TimeTicks(minT, maxT, timeticks.TimeTickOptions{
		PanelWidthPx:    600,
		TargetSpacingPx: 90,
		Location:        time.Local,
	})
	for i, tv := range layout.TickValues {
		x := float64(tv.Unix())
		if x < timeUnixSec[0] || x > timeUnixSec[len(timeUnixSec)-1] {
			continue
		}
		c.PlotText("xt"+strconv.Itoa(i), x, labelY, layout.TickLabels[i]).Send()
	}
}
