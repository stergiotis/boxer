package view

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

func rowNode(fieldTops ...float64) *layeredgraph.NodeLayout {
	rect := func(i int) layeredgraph.Rect {
		return layeredgraph.Rect{Min: layeredgraph.Point{X: 0.5, Y: fieldTops[i]}, Max: layeredgraph.Point{X: 99.5, Y: fieldTops[i+1]}}
	}
	n := &layeredgraph.NodeLayout{Title: rect(0)}
	for i := 1; i+1 < len(fieldTops); i++ {
		n.Rows = append(n.Rows, layeredgraph.RowLayout{ID: string(rune('a' + i)), Rect: rect(i)})
	}
	return n
}

// The fields keep their own boundaries inside the box, and the outermost
// ones are stretched over Graphviz's half-point inset to the box's border.
func TestFieldSpansMeetTheBorder(t *testing.T) {
	scale := func(p layeredgraph.Point) (float32, float32) { return float32(2 * p.X), float32(2 * p.Y) }
	n := rowNode(0.5, 25, 50, 74.5) // title and two rows inside a 0..75 box
	spans := fieldSpans(n, 0, 150, scale)
	require.Equal(t, [][2]float32{{0, 50}, {50, 100}, {100, 150}}, spans)
}

// A row node with no rows is its title alone, from border to border.
func TestFieldSpansTitleOnly(t *testing.T) {
	identity := func(p layeredgraph.Point) (float32, float32) { return float32(p.X), float32(p.Y) }
	spans := fieldSpans(rowNode(0.5, 35.5), 0, 36, identity)
	require.Equal(t, [][2]float32{{0, 36}}, spans)
}
