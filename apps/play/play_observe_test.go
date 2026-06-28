package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// Observing an intermediate node materialises its fused SQL on the intermediate
// lane and activeSnapshot returns that result (ADR-0097 3d); observing the sink
// goes back to the main lane.
func TestActiveSnapshotObservesIntermediateNode(t *testing.T) {
	srv, hits := arrowServer(t, []int64{1, 2, 3})
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	graph := newLiveQueryGraph(client, memory.NewGoAllocator(), 10)
	app := NewPlayApp(client, graph, "")
	app.currentSplit = splitResult{
		Nodes: []splitNode{
			{ID: "recent", Kind: splitNodeCTE, SQL: "SELECT n FROM t"},
			{ID: mainNodeID, Kind: splitNodeStatement, SQL: "WITH recent AS (SELECT n FROM t) SELECT * FROM recent"},
		},
		Sink: mainNodeID,
	}

	// Observe the intermediate: the first snapshot triggers its execution.
	app.observedNode = "recent"
	rec, _, _, _, _, _, _, _ := app.activeSnapshot()
	if rec != nil {
		rec.Release()
	}
	waitNotLoading(t, app.intermediateStore)

	rec, _, numRows, loading, _, _, _, err := app.activeSnapshot()
	require.NoError(t, err)
	require.False(t, loading)
	require.NotNil(t, rec)
	defer rec.Release()
	require.Equal(t, int64(3), numRows, "panels show the intermediate node's result")
	require.Equal(t, 1, *hits, "the intermediate's fused SQL executed once")

	// Re-snapshotting the same observed node is a memo hit (no new wire call).
	r2, _, _, _, _, _, _, _ := app.activeSnapshot()
	if r2 != nil {
		r2.Release()
	}
	require.Equal(t, 1, *hits)

	// Observing the sink goes to the main lane (which never ran here), and never
	// re-hits the intermediate lane.
	app.observedNode = mainNodeID
	r3, _, _, _, _, _, _, _ := app.activeSnapshot()
	if r3 != nil {
		r3.Release()
	}
	require.Equal(t, 1, *hits, "observing the sink does not touch the intermediate lane")
}
