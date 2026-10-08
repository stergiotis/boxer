package gfsdemo

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield/keelsonfield/keelsonfieldtest"
)

// The committed forecast reads as ADR-0292 SD5 describes it: 17 steps three
// hours apart on a 360 × 181 global grid, plausible 10 m winds, none missing.
func TestField(t *testing.T) {
	f, err := Field()
	require.NoError(t, err)
	require.Len(t, f.Steps, 17)
	for i := 1; i < len(f.Steps); i++ {
		assert.Equal(t, 3*time.Hour, f.Steps[i].Sub(f.Steps[i-1]))
	}
	assert.Equal(t, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), f.Steps[0])
	g := f.Grids[0]
	assert.Equal(t, []any{0.0, 90.0, 1.0, 1.0, 360, 181}, []any{g.West, g.North, g.DLon, g.DLat, g.Cols, g.Rows})
	for s, grid := range f.Grids {
		peak := 0.0
		for i := range grid.U {
			u, v := float64(grid.U[i]), float64(grid.V[i])
			require.False(t, math.IsNaN(u) || math.IsNaN(v), "step %d node %d", s, i)
			peak = max(peak, math.Hypot(u, v))
		}
		assert.Greater(t, peak, 10.0, "step %d: somewhere it is windy", s)
		assert.Less(t, peak, 60.0, "step %d: 10 m wind stays below hurricane extremes", s)
	}
}

// The forecast as a field family answers as the reduction statements do over
// it (ADR-0291 §SD5), on three of its steps.
func TestFamilyParity(t *testing.T) {
	if testing.Short() {
		t.Skip("ships the 1.1 M-row relation to clickhouse-local per statement")
	}
	f, err := Field()
	require.NoError(t, err)
	keelsonfieldtest.Parity(t, "gfs_wind", f, keelsonfieldtest.Options{Steps: []int{0, 8, 16}})
}
