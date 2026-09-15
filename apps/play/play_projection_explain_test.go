package play

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stretchr/testify/require"
)

// twoClusterFeatures makes n entities whose mean value length splits them
// in two, with the rest of the features held still; every third row past
// the split is noise.
func twoClusterFeatures(n int) (features []card.EntityFeatures, cl algo.HDBSCANResult) {
	features = make([]card.EntityFeatures, n)
	cl.Label = make([]int32, n)
	cl.Probability = make([]float32, n)
	for i := range n {
		f := card.EntityFeatures{TotalAttributeCount: 10, GiniAttrsPerSection: 0.3}
		switch {
		case i < n/2:
			f.MeanValueLength = 5 + float64(i%5)
			cl.Label[i] = 0
		default:
			f.MeanValueLength = 50 + float64(i%5)
			cl.Label[i] = 1
			if i%3 == 0 {
				cl.Label[i] = -1
			}
		}
		features[i] = f
	}
	cl.NumClusters = 2
	return
}

func TestExplainProjectionReadsTheSplit(t *testing.T) {
	features, cl := twoClusterFeatures(60)
	ex := explainProjection(context.Background(), features, cl)
	require.NoError(t, ex.err)
	require.NotNil(t, ex.tree)
	require.NotNil(t, ex.contrast)
	require.Len(t, ex.perCluster, 2)
	names := card.FeatureNames()
	for _, perCluster := range []bool{false, true} {
		rows := explanationRows(ex, projectionExplainDefaultDepth, names, perCluster)
		require.Len(t, rows, 2)
		require.Equal(t, "cluster 1", rows[0].cluster)
		require.Equal(t, "30", rows[0].rows)
		require.Contains(t, rows[0].rule, "mean_value_length <= ")
		require.Contains(t, rows[0].fit, "precision 100% · recall 100%")
		require.Contains(t, rows[1].rule, "mean_value_length > ")
		require.True(t, strings.HasPrefix(rows[0].features, "mean_value_length ↓ ("), rows[0].features)
		require.True(t, strings.HasPrefix(rows[1].features, "mean_value_length ↑ ("), rows[1].features)
		// The still features do not stand out, so only the one is listed.
		require.NotContains(t, rows[0].features, " · ")
	}
	// Against the rest, cluster 2's noise rows sit on the rest side of its
	// own tree, so its precision is under one there and whole in the
	// partition, which never saw them.
	part := explanationRows(ex, projectionExplainDefaultDepth, names, false)
	require.Contains(t, part[1].fit, "precision 100%")
	own := explanationRows(ex, projectionExplainDefaultDepth, names, true)
	require.Contains(t, own[1].fit, "recall 100%")
	require.NotContains(t, own[1].fit, "precision 100%")
	summary := explanationSummary(ex, projectionExplainDefaultDepth, false)
	require.Contains(t, summary, "for 50 of 50 clustered rows (100%)")
	require.Contains(t, summary, "2 leaves")
	require.Contains(t, explanationSummary(ex, 2, true), "one tree per cluster")
}

func TestExplainProjectionNothingToExplain(t *testing.T) {
	features, cl := twoClusterFeatures(12)
	cl.NumClusters = 0
	ex := explainProjection(context.Background(), features, cl)
	require.Nil(t, ex.tree)
	require.NoError(t, ex.err)
	require.Empty(t, explanationRows(ex, 3, card.FeatureNames(), true))
	require.Equal(t, "", explanationSummary(ex, 3, true))
}

func TestExplanationRowsWithoutALeaf(t *testing.T) {
	// A partition cut at depth 0 has one leaf, so the minority cluster has
	// none; each cluster's own tree at depth 0 predicts the rest.
	features, cl := twoClusterFeatures(60)
	ex := explainProjection(context.Background(), features, cl)
	require.NoError(t, ex.err)
	rows := explanationRows(ex, 0, card.FeatureNames(), false)
	require.Len(t, rows, 2)
	require.Equal(t, "true", rows[0].rule)
	require.Equal(t, "no leaf at this depth", rows[1].rule)
	require.Equal(t, "", rows[1].fit)
	own := explanationRows(ex, 0, card.FeatureNames(), true)
	require.Equal(t, "no leaf at this depth", own[0].rule)
}

func TestItemPredicateSpellings(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "tv:geo:value:val:sh:5::7:0::data", Type: arrow.BinaryTypes.String},
		{Name: "tv:geo:lr:lr:u64:1247:::0::data", Type: arrow.BinaryTypes.String},
	}, nil)
	cases := []struct {
		it   card.Item
		want string
		ok   bool
	}{
		{card.Item{Kind: card.ItemKindSection, Column: "tv:geo:value:val"}, "length(`tv:geo:value:val`) > 0", true},
		{card.Item{Kind: card.ItemKindSection}, "", false},
		{card.Item{Kind: card.ItemKindTaggedValue, Column: "c", Value: "it's", Quoted: true}, "has(`c`, 'it\\'s')", true},
		{card.Item{Kind: card.ItemKindTaggedValue, Column: "c", Value: "42"}, "has(`c`, 42)", true},
		{card.Item{Kind: card.ItemKindTagRef, Section: "geo", PhysicalSection: "geo", Ref: 9}, "has(`tv:geo:lr:lr:u64:1247:::0::data`, 9)", true},
		{card.Item{Kind: card.ItemKindTagRef, Section: "geo-x", PhysicalSection: "geoX", Ref: 9}, "", false},
		{card.Item{Kind: card.ItemKindCoGroup, Section: "g"}, "", false},
	}
	for _, tc := range cases {
		got, ok := itemPredicate(tc.it, schema)
		require.Equal(t, tc.ok, ok, tc.it.Kind)
		require.Equal(t, tc.want, got)
	}
}

func TestExplainItemsPlanted(t *testing.T) {
	// 80 entities: cluster 0 holds section "geo", the rest "sym"; a third
	// item everyone has and a fourth almost no one has are pruned away.
	n := 80
	items := []card.Item{
		{Name: "section:geo", Kind: card.ItemKindSection, Column: "g"},
		{Name: "section:sym", Kind: card.ItemKindSection, Column: "s"},
		{Name: "section:all", Kind: card.ItemKindSection, Column: "a"},
		{Name: "section:rare", Kind: card.ItemKindSection, Column: "r"},
	}
	sets := card.ItemSets{Items: items, Rows: make([][]int32, n), Support: make([]int32, 4)}
	cl := algo.HDBSCANResult{Label: make([]int32, n), NumClusters: 2}
	for r := range n {
		row := []int32{2}
		if r < 30 {
			row = append(row, 0)
			cl.Label[r] = 0
		} else {
			row = append(row, 1)
			cl.Label[r] = 1
			if r%7 == 0 {
				cl.Label[r] = -1
			}
		}
		if r == 3 {
			row = append(row, 3)
		}
		slices.Sort(row)
		sets.Rows[r] = row
		for _, it := range row {
			sets.Support[it]++
		}
	}
	pi := explainItems(context.Background(), sets, 4, cl, nil)
	require.NoError(t, pi.err)
	require.Len(t, pi.sets.Items, 2, "the universal and the rare item are pruned")
	rows := itemsRows(pi, 2)
	require.Len(t, rows, 2)
	require.Equal(t, "length(`g`) > 0", rows[0].rule)
	require.Contains(t, rows[0].fit, "precision 100% · recall 100%")
	require.True(t, strings.HasPrefix(rows[0].features, "section:geo ↑ (100% vs 0%"), rows[0].features)
	require.Contains(t, rows[1].rule, "length(`s`) > 0")
	require.Contains(t, rows[1].fit, "recall 100%")
	require.Contains(t, itemsSummary(pi), "2 items kept of 4 candidates")
	// No clusters: nothing to read.
	none := explainItems(context.Background(), sets, 4, algo.HDBSCANResult{}, nil)
	require.Nil(t, none.contrast)
	require.Empty(t, itemsRows(none, 2))
}
