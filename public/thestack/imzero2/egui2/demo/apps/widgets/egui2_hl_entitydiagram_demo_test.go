//go:build !(js || wasip1)

package widgets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
)

// The demo's model lays out as drawn: every node and edge placed, the
// entities as row nodes, the parallel edges and the self-loop all kept.
func TestEntityDiagramModelLaysOut(t *testing.T) {
	eng, err := goccyengine.New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	m := entityDiagramModel()
	lay, err := eng.Layout(context.Background(), m, layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight, FontSize: 14})
	require.NoError(t, err)
	require.Len(t, lay.Nodes, len(m.Nodes))
	require.Len(t, lay.Edges, len(m.Edges))
	for _, n := range lay.Nodes {
		require.Equal(t, n.ID != "encounter", n.IsRowNode(), "node %s", n.ID)
	}
}
