package play

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
)

// projectionSnapFixture is a finished run over two clusters, its layout
// copied, as get_projection and explain_clusters read it.
func projectionSnapFixture(t *testing.T) (ps projectionOpsSnap) {
	t.Helper()
	features, cl := twoClusterFeatures(60)
	ex := explainProjection(context.Background(), shapeFeatureDesc(features), cl)
	require.NoError(t, ex.err)
	rows := make([]int64, len(cl.Label))
	x, y := make([]float32, len(rows)), make([]float32, len(rows))
	for i := range rows {
		rows[i] = int64(100 + i)
		x[i], y[i] = float32(i), float32(-i)
	}
	cl.Probability = make([]float32, len(rows))
	for i := range cl.Probability {
		cl.Probability[i] = 0.5
	}
	res := &projectionResult{rows: rows, clusters: cl, explanation: ex,
		params: projectionParams{K: 15, MinClusterSize: 10, FeatureSet: projectionFeatureShape}}
	return projectionOpsSnap{snap: projectorSnapshot{status: projectorStatusDone, result: res, version: 1},
		depth: projectionExplainDefaultDepth, perCluster: true, layout: "settled", x: x, y: y, rows: 60,
		neighbours: 15, minCluster: 10, summary: "60 nodes"}
}

// get_projection reads the clusters with their sizes and noise, and a page
// of points with their row, cluster, probability and position.
func TestGetProjectionReadsClustersAndPoints(t *testing.T) {
	ps := projectionSnapFixture(t)
	st, err := projectionState(ps, ProjectionArgs{})
	require.NoError(t, err)
	require.Equal(t, "done", st.Status)
	require.Equal(t, []ClusterSize{{Cluster: 1, Rows: 30}, {Cluster: 2, Rows: 20}}, st.Clusters)
	require.EqualValues(t, 10, st.Noise)
	require.EqualValues(t, 60, st.Projected)
	require.Equal(t, "settled", st.Layout)
	require.Len(t, st.Points, 60, "the default page holds them all")
	require.Equal(t, ProjectionPoint{Row: 100, Cluster: 1, Probability: 0.5, X: 0, Y: 0}, st.Points[0])

	st, err = projectionState(ps, ProjectionArgs{Points: 2, Offset: 30})
	require.NoError(t, err)
	require.Equal(t, int64(130), st.Points[0].Row)
	require.EqualValues(t, -1, st.Points[0].Cluster, "row 30 is noise")
	require.Equal(t, float32(30), st.Points[0].X)
	require.EqualValues(t, 60, st.PointsTotal)

	st, err = projectionState(ps, ProjectionArgs{Points: -1})
	require.NoError(t, err)
	require.Empty(t, st.Points)
	_, err = projectionState(ps, ProjectionArgs{Points: opsProjectionMaxPoints + 1})
	require.Error(t, err)

	idle, err := projectionState(projectionOpsSnap{cannotDraw: "not leeway-shaped", neighbours: 15}, ProjectionArgs{})
	require.NoError(t, err)
	require.Equal(t, "idle", idle.Status)
	require.Equal(t, "not leeway-shaped", idle.CannotDraw)
	require.Equal(t, "not drawn", idle.Layout)
}

// explain_clusters reads the pane's explanation: per cluster a SQL rule,
// its fit and what sets the cluster apart, by features or by attributes.
func TestExplainClustersReadsThePanesExplanation(t *testing.T) {
	ps := projectionSnapFixture(t)
	ex, err := explainClusters(ps, ExplainClustersArgs{})
	require.NoError(t, err)
	require.Equal(t, "features", ex.Reading)
	require.Contains(t, ex.Summary, "one tree per cluster")
	require.Len(t, ex.Clusters, 2)
	require.EqualValues(t, 1, ex.Clusters[0].Cluster)
	require.EqualValues(t, 30, ex.Clusters[0].Rows)
	require.Contains(t, ex.Clusters[0].Rule, "mean_value_length <= ")
	require.Contains(t, ex.Clusters[0].Fit, "precision 100%")
	require.Contains(t, ex.Clusters[0].Distinguishing, "mean_value_length")

	part, err := explainClusters(ps, ExplainClustersArgs{Partition: true, Depth: 2, Cluster: 2})
	require.NoError(t, err)
	require.Contains(t, part.Summary, "one partition")
	require.Len(t, part.Clusters, 1)
	require.EqualValues(t, 2, part.Clusters[0].Cluster)

	attrs, err := explainClusters(ps, ExplainClustersArgs{Reading: "attributes"})
	require.NoError(t, err)
	require.Equal(t, "attributes: nothing to read", attrs.Summary)

	for _, bad := range []ExplainClustersArgs{{Depth: 9}, {Cluster: 3}, {Reading: "vibes"}} {
		_, err = explainClusters(ps, bad)
		require.Error(t, err, "%+v", bad)
	}
	_, err = explainClusters(projectionOpsSnap{}, ExplainClustersArgs{})
	require.ErrorContains(t, err, "compute_projection")

	none := projectionSnapFixture(t)
	features, cl := twoClusterFeatures(12)
	cl.NumClusters = 0
	none.snap.result.explanation = explainProjection(context.Background(), shapeFeatureDesc(features), cl)
	out, err := explainClusters(none, ExplainClustersArgs{})
	require.NoError(t, err)
	require.Contains(t, out.Summary, "lower min_cluster")
}

// The pane says up front when a result cannot be projected, so list_panes
// reports it before a run would fail.
func TestProjectionPaneRejectsAResultThatIsNotLeewayShaped(t *testing.T) {
	app := &PlayApp{projector: &Projector{}}
	p := projectionPanel{app: app}
	_, reason := p.AcceptForChannel(chMain, schemaWith(strField("c")), sigWith(0))
	require.Contains(t, reason, "leeway-shaped")
	schema := arrow.NewSchema([]arrow.Field{{Name: "count()", Type: arrow.PrimitiveTypes.Uint64}}, nil)
	_, reason = p.AcceptForChannel(chMain, schema, sigWith(0))
	require.Contains(t, reason, "leeway-shaped")
	require.Same(t, schema, app.projector.shapeSchema, "the verdict is cached per schema")
}

func TestParseFeatureSet(t *testing.T) {
	fs, ok := parseFeatureSet(" Structure ")
	require.True(t, ok)
	require.Equal(t, projectionFeatureStructure, fs)
	_, ok = parseFeatureSet("umap")
	require.False(t, ok)
}

// The arguments take JSON numbers: a Go int is 64-bit, which the catalog's
// JSON spells as a decimal string, and a model sends numbers.
func TestProjectionArgumentsTakeNumbers(t *testing.T) {
	cat := playOps.Catalog()
	for op, args := range map[string]string{
		opGetProjection:     `{"points":5,"offset":10}`,
		opComputeProjection: `{"neighbours":20,"min_cluster":5,"features":"structure"}`,
		opExplainClusters:   `{"reading":"features","depth":2,"cluster":1,"partition":true}`,
	} {
		spec, ok := cat.Lookup(op)
		require.True(t, ok, op)
		_, err := opjson.Decode([]byte(args), spec.Args)
		require.NoError(t, err, op)
	}
}
