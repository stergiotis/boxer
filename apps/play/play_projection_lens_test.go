package play

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
)

// plantedKinds is the structure matrix of a batch of four record kinds —
// sixteen, twelve, twelve and eight rows, each kind its own set of
// attributes — as the structure feature set sees it: rows of a kind are
// identical.
func plantedKinds() (x []float32, d int, ids []uint64, kindOf []int) {
	d = 8
	sizes := []int{16, 12, 12, 8}
	for k, n := range sizes {
		for range n {
			row := make([]float32, d)
			row[2*k], row[2*k+1] = 1, 1
			if k == 3 {
				row[0] = 1 // the smallest kind shares an attribute with the first
			}
			x = append(x, row...)
			kindOf = append(kindOf, k)
		}
	}
	for i := range kindOf {
		ids = append(ids, uint64(i)+1)
	}
	return
}

func clusterPlanted(t *testing.T, coreDist func(g *knn.Result) []float32) (clusters int) {
	t.Helper()
	x, d, ids, _ := plantedKinds()
	g, err := knn.Build(context.Background(), nil, x, d, ids, knn.Options{K: projectionDefaultK, Metric: knn.MetricCosine})
	require.NoError(t, err)
	dg, err := g.DistanceGraph()
	require.NoError(t, err)
	cl, err := algo.HDBSCAN(context.Background(), dg, coreDist(&g), algo.HDBSCANOptions{MinClusterSize: projectionDefaultMinClusterSize})
	require.NoError(t, err)
	return cl.NumClusters
}

// At the panel's defaults the planted kinds are found: the core distance is
// read under the smallest kind, though the graph keeps fifteen neighbours.
// Read at the fifteenth neighbour, the kinds of twelve and eight find their
// core distance in another kind, and the clustering loses them.
func TestProjectionCoreDistFindsKindsSmallerThanK(t *testing.T) {
	found := clusterPlanted(t, func(g *knn.Result) []float32 { return projectionCoreDist(g, projectionDefaultMinClusterSize) })
	assert.Equal(t, 4, found)
	atK := clusterPlanted(t, func(g *knn.Result) []float32 { return g.CoreDist })
	assert.Less(t, atK, 4, "the K-th neighbour's core distance is what the default replaced")
}

func TestProjectionCoreDistReadsTheEarlierNeighbour(t *testing.T) {
	g := &knn.Result{K: 3, CoreDist: []float32{3, 30}, Dists: []float32{1, 2, 3, 10, 20, 30}}
	assert.Equal(t, []float32{2, 20}, projectionCoreDist(g, 3))
	assert.Equal(t, []float32{3, 30}, projectionCoreDist(g, 10), "past K, the K-th")
	assert.Equal(t, []float32{1, 10}, projectionCoreDist(g, 1), "at least the nearest")
}

// lensJobsHosts is twelve jobs and twelve hosts as a lens model, the jobs
// with a missing runtime (job-10), a rare state (job-11) and an extreme
// runtime (job-00); two rows are noise.
func lensJobsHosts() (m *lwlens.Model, labels []int32) {
	m = &lwlens.Model{
		Slots: []lwlens.Slot{
			{Section: "num", Member: "runtime", NumericType: true},
			{Section: "sym", Member: "state"},
			{Section: "num", Member: "cpu", NumericType: true},
		},
		Sections: []string{"num", "sym"},
	}
	for i := range 12 {
		r := lwlens.Row{Label: fmt.Sprintf("job-%02d", i)}
		if i != 10 {
			v := 100 + float64(i)
			if i == 0 {
				v = 9000
			}
			r.Cells = append(r.Cells, lwlens.Cell{Slot: 0, Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1})
		}
		state := "running"
		if i == 11 {
			state = "failed"
		}
		r.Cells = append(r.Cells, lwlens.Cell{Slot: 1, Text: state, Arity: 1})
		m.Rows = append(m.Rows, r)
		labels = append(labels, 0)
	}
	for i := range 14 {
		v := 10 + float64(i)
		m.Rows = append(m.Rows, lwlens.Row{Label: fmt.Sprintf("host-%02d", i),
			Cells: []lwlens.Cell{{Slot: 2, Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1}}})
		lb := int32(1)
		if i >= 12 {
			lb = -1
		}
		labels = append(labels, lb)
	}
	return
}

func archetypesSnapFixture(t *testing.T) projectionOpsSnap {
	t.Helper()
	m, labels := lensJobsHosts()
	rows := make([]int64, len(m.Rows))
	for i := range rows {
		rows[i] = int64(500 + i)
	}
	a, err := projectionLens(context.Background(), m, func() (e []int) {
		for i := range m.Rows {
			e = append(e, i)
		}
		return
	}(), labels)
	require.NoError(t, err)
	res := &projectionResult{rows: rows, lens: a,
		clusters: algo.HDBSCANResult{NumClusters: 2, Label: labels},
		params:   projectionParams{K: 15, MinClusterSize: 5, FeatureSet: projectionFeatureStructure}}
	return projectionOpsSnap{snap: projectorSnapshot{status: projectorStatusDone, result: res, version: 1}}
}

func TestGetArchetypesReadsTheClusters(t *testing.T) {
	ps := archetypesSnapFixture(t)
	out, err := archetypesReading(ps, ArchetypesArgs{})
	require.NoError(t, err)
	require.Len(t, out.Clusters, 3, "two clusters, then the unclustered rows")

	jobs := out.Clusters[0]
	require.EqualValues(t, 1, jobs.Cluster, "numbered from 1, as get_projection numbers them")
	require.EqualValues(t, 12, jobs.Rows)
	var typical []string
	for _, v := range jobs.Typical {
		typical = append(typical, v.Attribute+" "+v.Typical)
	}
	assert.Contains(t, typical, "sym·state running (92%)")
	require.NotEmpty(t, jobs.Exceptions)
	assert.Equal(t, []string{"job-10"}, jobs.Exceptions[0].Rows)
	assert.Equal(t, []int64{510}, jobs.Exceptions[0].RowNumbers, "the result's row, not the lens's")
	assert.Equal(t, []string{"−num·runtime"}, jobs.Exceptions[0].Departures)
	var departures []string
	for _, e := range jobs.Exceptions {
		departures = append(departures, strings.Join(e.Departures, " "))
	}
	assert.Contains(t, departures, "sym·state failed (1 of 12 rows)")
	assert.Contains(t, departures, "num·runtime 9000↑")
	require.Len(t, jobs.Extremes, 1)
	assert.Equal(t, "job-00 9000", jobs.Extremes[0].Highest)

	rest := out.Clusters[2]
	assert.EqualValues(t, -1, rest.Cluster)
	assert.Equal(t, []string{"host-12", "host-13"}, rest.Members)

	one, err := archetypesReading(ps, ArchetypesArgs{Cluster: 2})
	require.NoError(t, err)
	require.Len(t, one.Clusters, 1)
	assert.EqualValues(t, 2, one.Clusters[0].Cluster)

	_, err = archetypesReading(ps, ArchetypesArgs{Cluster: 3})
	require.Error(t, err)
	_, err = archetypesReading(projectionOpsSnap{}, ArchetypesArgs{})
	require.Error(t, err, "no run, no reading")
}

func TestGetArchetypesCatalogEntry(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opGetArchetypes)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassQuery, spec.Class)
	assert.True(t, spec.Untrusted, "it quotes the data")
	assert.True(t, spec.Agents)
	assert.Equal(t, []string{opsResProjection}, spec.Reads)
}

func TestProjectionLensRefusesAShortModel(t *testing.T) {
	m, labels := lensJobsHosts()
	_, err := projectionLens(context.Background(), m, []int{0, len(m.Rows)}, labels[:2])
	require.Error(t, err)
}
