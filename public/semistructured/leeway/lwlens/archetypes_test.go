package lwlens

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jobsModel is twelve jobs — a constant attempt, a numeric runtime and a
// state label — beside twelve hosts with a cpu. job-00 carries the batch's
// only extreme runtime, job-10 lacks its runtime, job-11 alone is failed.
// Row order puts the outlier first and the rare label last, which is the
// order an unranked budget would keep. The labels are the two kinds.
func jobsModel() (m *Model, labels []int32) {
	m = &Model{
		Slots: []Slot{
			{Section: "num", Member: "attempt", NumericType: true},
			{Section: "num", Member: "runtime", NumericType: true},
			{Section: "sym", Member: "state"},
			{Section: "num", Member: "cpu", NumericType: true},
		},
		Sections: []string{"num", "sym"},
	}
	for i := range 12 {
		r := Row{Label: fmt.Sprintf("job-%02d", i)}
		r.Cells = append(r.Cells, Cell{Slot: 0, Text: "2", Num: 2, HasNum: true, Arity: 1})
		if i != 10 {
			v := 100 + float64(i)
			if i == 0 {
				v = 9000
			}
			r.Cells = append(r.Cells, Cell{Slot: 1, Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1})
		}
		state := "running"
		if i == 11 {
			state = "failed"
		}
		r.Cells = append(r.Cells, Cell{Slot: 2, Text: state, Arity: 1})
		m.Rows = append(m.Rows, r)
		labels = append(labels, 0)
	}
	for i := range 12 {
		v := 10 + float64(i)
		m.Rows = append(m.Rows, Row{Label: fmt.Sprintf("host-%02d", i),
			Cells: []Cell{{Slot: 3, Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1}}})
		labels = append(labels, 1)
	}
	return
}

func jobsArchetype(t *testing.T, values float64) (a Analysis, ar Archetype) {
	t.Helper()
	m, labels := jobsModel()
	a, err := Analyze(context.Background(), m, AnalyzeOptions{Labels: labels})
	require.NoError(t, err)
	p := PlanRows(&a, Intent{Values: values, Stable: 0.5})
	for _, x := range Archetypes(&a, &p) {
		if a.Bands[x.Band].Cluster == 0 {
			return a, x
		}
	}
	require.Fail(t, "no jobs band")
	return
}

func TestArchetypeTemplateHoldsTheTypicalValues(t *testing.T) {
	a, ar := jobsArchetype(t, 0.5)
	assert.Equal(t, []int32{0}, ar.Constants, "attempt is one value throughout: a constant, said once")
	var slots []int32
	for _, tp := range ar.Template {
		slots = append(slots, tp.Slot)
		switch tp.Slot {
		case 1:
			assert.True(t, tp.Numeric)
			assert.Equal(t, 11, tp.Count)
			assert.InDelta(t, 106, tp.Median, 1e-9)
		case 2:
			assert.Equal(t, "running", tp.Mode)
			assert.InDelta(t, 11.0/12, tp.Share, 1e-9)
		}
	}
	slices.Sort(slots)
	assert.Equal(t, []int32{1, 2}, slots)
	require.Len(t, ar.Extremes, 1, "runtime varies; state is a label")
	e := ar.Extremes[0]
	assert.Equal(t, "job-01", a.Model.Rows[e.LowRow].Label)
	assert.Equal(t, "job-00", a.Model.Rows[e.HighRow].Label)
	assert.Equal(t, 9000.0, e.HighValue)
}

func TestArchetypeExceptionsRankStructureThenRareLabelsThenOutliers(t *testing.T) {
	a, ar := jobsArchetype(t, 0.5)
	// job-01's runtime is the slot's lowest: value surprise is two-sided,
	// so it is an outlier too, and the milder one.
	require.Len(t, ar.Exceptions, 4)
	first, second, third, fourth := ar.Exceptions[0], ar.Exceptions[1], ar.Exceptions[2], ar.Exceptions[3]

	assert.Equal(t, ExceptionClassStructural, first.Class)
	assert.Equal(t, "job-10", a.Model.Rows[first.Rows[0]].Label)
	assert.Equal(t, []Departure{{Kind: DepartureKindMissing, Slot: 1}}, first.Departures)

	assert.Equal(t, ExceptionClassRareLabel, second.Class)
	assert.Equal(t, "job-11", a.Model.Rows[second.Rows[0]].Label)
	assert.Equal(t, []Departure{{Kind: DepartureKindRareLabel, Slot: 2, Text: "failed", Count: 1}}, second.Departures,
		"a rare label carries its count, so its list can be seen complete")

	assert.Equal(t, ExceptionClassOutlier, third.Class)
	assert.Equal(t, "job-00", a.Model.Rows[third.Rows[0]].Label)
	require.Len(t, third.Departures, 1)
	assert.Equal(t, DepartureKindOutlier, third.Departures[0].Kind)
	assert.True(t, third.Departures[0].High)
	assert.Equal(t, "job-01", a.Model.Rows[fourth.Rows[0]].Label)
	assert.False(t, fourth.Departures[0].High)
	assert.GreaterOrEqual(t, third.Surprise, fourth.Surprise, "within a class, most extreme first")
}

func TestArchetypeShapeDetailKeepsOnlyStructure(t *testing.T) {
	_, ar := jobsArchetype(t, 0.1)
	for _, e := range ar.Exceptions {
		assert.Equal(t, ExceptionClassStructural, e.Class, "rare labels and outliers are read from the gist detail up")
	}
}
