package sccmap

import (
	"errors"
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
// when the "repo analyzer" had no source tree: the scc-failed branch returned
// early with maxValue=1, slipping past the clamp, so NewLogColormap(_, 1, 1)
// blew up. Both degenerate inputs (scc failed, empty dataset) must now land on
// the shared clamp.
func TestBuildTreeForMetricsDegenerateRange(t *testing.T) {
	// buildTreeForMetrics reads package globals; snapshot and restore so a
	// case does not leak into the rest of the package's tests.
	savedErr, savedGroups, savedRoot := sccDataErr, sccGroups, sccRootName
	t.Cleanup(func() { sccDataErr, sccGroups, sccRootName = savedErr, savedGroups, savedRoot })

	cases := []struct {
		name  string
		setup func()
	}{
		{
			name: "scc failed (no source tree)",
			setup: func() {
				sccDataErr = errors.New("scctree.RepoRoot: no repo root")
				sccGroups, sccRootName = nil, ""
			},
		},
		{
			name: "empty dataset",
			setup: func() {
				sccDataErr = nil
				sccGroups, sccRootName = nil, "root"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			_, _, maxValue := buildTreeForMetrics(defaultSizeMetricIdx, defaultColorMetricIdx, nil)
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
