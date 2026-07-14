package sccmap

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/scctree"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
)

// TestBuildTreeForMetricsDegenerateRange locks in the colormap-range contract
// rebuildTreemap depends on: buildTreeForMetrics must never return a maxValue
// that makes treemap.NewLogColormap(palette, 1, maxValue) panic — NewConfig
// requires min < max. Regression for the demo crashing with
//
//	panic: colormap: NewConfig requires min < max
//
// The two degenerate inputs — no scan has landed yet (inst.data == nil) and an
// empty dataset — must both land on the shared maxValue clamp rather than
// slipping through with maxValue == 1.
func TestBuildTreeForMetricsDegenerateRange(t *testing.T) {
	cases := []struct {
		name string
		data *sccData
	}{
		{name: "no scan data (nil)", data: nil},
		{name: "empty dataset", data: &sccData{rootName: "root"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := &App{data: tc.data}
			_, _, maxValue := inst.buildTreeForMetrics(defaultSizeMetricIdx, defaultColorMetricIdx, nil)
			if !(1 < maxValue) {
				t.Fatalf("maxValue = %v; want > 1 so NewLogColormap(palette, 1, maxValue) holds", maxValue)
			}
			// The exact call rebuildTreemap makes (sccmap.go) — must not panic.
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("NewLogColormap(ComplexityPalette, 1, %v) panicked: %v", maxValue, r)
					}
				}()
				_ = treemap.NewLogColormap(scctree.ComplexityPalette, 1, maxValue)
			}()
		})
	}
}
