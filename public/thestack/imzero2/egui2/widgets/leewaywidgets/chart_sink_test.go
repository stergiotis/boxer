package leewaywidgets

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixture's first numeric tagged section is geoPoint (lat is f32), one
// attribute tagged /geo/origin; the entity is labelled by its natural key.
func TestChartSinkProjectsTheFixture(t *testing.T) {
	s := NewChartSink()
	RunFixture(s)
	m := s.Model()
	require.False(t, m.Empty())
	assert.Equal(t, "geoPoint", m.Section)
	assert.Equal(t, "lat", m.ValueName)
	assert.Equal(t, []string{"acme/widgets/blue"}, m.Categories)
	require.Len(t, m.Series, 1)
	assert.Equal(t, "/geo/origin", m.Series[0])
	assert.InDelta(t, 37.7749, m.Values[0][0], 1e-4)
	assert.Zero(t, m.Collisions)

	// A second drive starts from scratch.
	RunFixture(s)
	assert.Len(t, s.Model().Categories, 1)
	assert.False(t, math.IsNaN(s.Model().Values[0][0]))
}

func TestOrientTransposesAndSorts(t *testing.T) {
	nan := math.NaN()
	m := &ChartModel{
		Categories: []string{"h0", "h1", "h2"},
		Series:     []string{"cpu", "mem"},
		Values:     [][]float64{{10, nan, 30}, {1, 2, 3}},
	}
	r := orient(m, ChartOptions{Sort: ChartSortDescending})
	assert.Equal(t, []string{"h2", "h0", "h1"}, r.cats, "by the first series, missing last")
	assert.Equal(t, []float64{3, 1, 2}, r.vals[1], "every series follows the category order")

	tr := orient(m, ChartOptions{Transpose: true})
	assert.Equal(t, []string{"cpu", "mem"}, tr.cats)
	assert.Equal(t, []string{"h0", "h1", "h2"}, tr.series)
	assert.Equal(t, []float64{10, 1}, tr.vals[0])
	assert.Equal(t, []string{"h0", "h1", "h2"}, m.Categories, "the model is not modified")
}
