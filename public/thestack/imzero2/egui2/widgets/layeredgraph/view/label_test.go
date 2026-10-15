package view

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Lines are stacked at Graphviz's 1.2 em around the anchor, so a label's
// lines span exactly the height the layout sized its box for.
func TestLineCentres(t *testing.T) {
	require.Equal(t, []float32{50}, lineCentres(50, 10, 1), "one line sits on the anchor")
	require.Equal(t, []float32{44, 56}, lineCentres(50, 10, 2))
	require.Equal(t, []float32{38, 50, 62}, lineCentres(50, 10, 3))
	ys := lineCentres(0, 20, 5)
	require.InDelta(t, 4*1.2*20, ys[4]-ys[0], 1e-4, "first to last centre is (n-1) pitches")
	require.InDelta(t, 0, ys[0]+ys[4], 1e-4, "centred on the anchor")
	require.Empty(t, lineCentres(0, 10, 0))
}
