package implot

import (
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
)

// TimeTicksLocal computes calendar-aware tick positions and labels in the
// machine's local time for a Unix-seconds axis spanning [minUnix, maxUnix]
// at the given pixel width, shaped for SetupAxisTicks. It exists for the
// monitoring panels (imztop, imzrt), which read wall-clock history — a
// house extension, not upstream API: upstream's time locator (ScaleTime
// here) labels in UTC and knows no locale calendar.
func TimeTicksLocal(minUnix float64, maxUnix float64, widthPx float32) (values []float64, labels []string) {
	minT := time.Unix(int64(minUnix), 0).Local()
	maxT := time.Unix(int64(maxUnix), 0).Local()
	if !maxT.After(minT) {
		return nil, nil
	}
	layout := timeticks.TimeTicks(minT, maxT, timeticks.TimeTickOptions{
		PanelWidthPx:    int32(widthPx),
		TargetSpacingPx: 90,
		Location:        time.Local,
	})
	values = make([]float64, 0, len(layout.TickValues))
	labels = make([]string, 0, len(layout.TickValues))
	for i, tv := range layout.TickValues {
		values = append(values, float64(tv.Unix()))
		labels = append(labels, layout.TickLabels[i])
	}
	return values, labels
}
