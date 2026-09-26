package leewaywidgets

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	"github.com/stretchr/testify/require"
)

// TestLensReadsTheFixture drives the fixture through the lens sink and plans
// it at every corner of the intent space: the fixture has plain sections, a
// co-section group and a repeated tagged section, which is every shape the
// sink has to turn into slots.
func TestLensReadsTheFixture(t *testing.T) {
	sink := lwlens.NewSink(nil)
	RunFixture(sink)
	m := sink.Model()
	require.NotEmpty(t, m.Rows)
	require.NotEmpty(t, m.Slots)
	for _, r := range m.Rows {
		require.NotEmpty(t, r.Label)
		for i := 1; i < len(r.Cells); i++ {
			require.Less(t, r.Cells[i-1].Slot, r.Cells[i].Slot, "cells are sorted by slot and one per slot")
		}
	}
	a, err := lwlens.Analyze(context.Background(), m, lwlens.AnalyzeOptions{})
	require.NoError(t, err)
	require.Len(t, a.Order, len(m.Slots))
	for _, v := range []float64{0, 0.3, 0.6, 1} {
		for _, s := range []float64{0, 0.2, 0.5, 1} {
			p := lwlens.PlanRows(&a, lwlens.Intent{Values: v, Stable: s})
			rows := 0
			for _, pb := range p.Bands {
				for _, pr := range pb.Rows {
					rows += 1 + len(pr.Also)
				}
			}
			require.Equal(t, len(m.Rows), rows, "every row is drawn once, alone or collapsed (values %v, stable %v)", v, s)
		}
	}
}
