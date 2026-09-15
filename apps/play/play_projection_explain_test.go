package play

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
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
	cases := []struct {
		it   card.Item
		want string
		ok   bool
	}{
		{card.Item{Kind: card.ItemKindSection, Handle: "geo:lat"}, "length(`geo:lat`) > 0", true},
		{card.Item{Kind: card.ItemKindSection}, "", false},
		{card.Item{Kind: card.ItemKindTaggedValue, Handle: "sym:value", Value: "it's", Quoted: true}, "has(`sym:value`, 'it\\'s')", true},
		{card.Item{Kind: card.ItemKindTaggedValue, Handle: "u8:value", Value: "42"}, "has(`u8:value`, 42)", true},
		{card.Item{Kind: card.ItemKindTagRef, Handle: "geo:lr", Ref: 9}, "has(`geo:lr`, 9)", true},
		{card.Item{Kind: card.ItemKindTagVerbatim, Handle: "f64:lv", Value: "elevation", Quoted: true}, "has(`f64:lv`, 'elevation')", true},
		{card.Item{Kind: card.ItemKindComponent, Value: "SysCpu"}, "LW_COMPONENT_FILTER('SysCpu')", true},
		{card.Item{Kind: card.ItemKindCoGroup, Section: "g"}, "", false},
	}
	for _, tc := range cases {
		got, ok := itemPredicate(tc.it)
		require.Equal(t, tc.ok, ok, tc.it.Kind)
		require.Equal(t, tc.want, got)
	}
}

func TestWithComponentItems(t *testing.T) {
	sets := card.ItemSets{
		Items:   []card.Item{{Name: "section:a", Kind: card.ItemKindSection}},
		Rows:    [][]int32{{0}, {}, {0}},
		Support: []int32{2},
	}
	out := withComponentItems(sets, []string{"K1", "K2"}, [][]int32{{1}, {0, 1}, {}})
	require.Len(t, out.Items, 3)
	require.Equal(t, "component:K1", out.Items[1].Name)
	require.Equal(t, card.ItemKindComponent, out.Items[2].Kind)
	require.Equal(t, [][]int32{{0, 2}, {1, 2}, {0}}, out.Rows)
	require.Equal(t, []int32{2, 1, 2}, out.Support)
	require.Equal(t, sets, withComponentItems(sets, nil, nil), "no kinds: unchanged")
	require.Equal(t, sets, withComponentItems(sets, []string{"K"}, [][]int32{{}}), "row count mismatch: unchanged")
}

func TestExplainItemsPlanted(t *testing.T) {
	// 80 entities: cluster 0 holds section "geo", the rest "sym"; a third
	// item everyone has and a fourth almost no one has are pruned away.
	n := 80
	items := []card.Item{
		{Name: "section:geo", Kind: card.ItemKindSection, Handle: "geo:v"},
		{Name: "section:sym", Kind: card.ItemKindSection, Handle: "sym:v"},
		{Name: "section:all", Kind: card.ItemKindSection, Handle: "all:v"},
		{Name: "section:rare", Kind: card.ItemKindSection, Handle: "rare:v"},
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
	pi := explainItems(context.Background(), sets, 4, cl, 10)
	require.NoError(t, pi.err)
	require.Len(t, pi.sets.Items, 2, "the universal and the rare item are pruned")
	rows := itemsRows(pi, 2)
	require.Len(t, rows, 2)
	require.Equal(t, "length(`geo:v`) > 0", rows[0].rule)
	require.Contains(t, rows[0].fit, "precision 100% · recall 100%")
	require.True(t, strings.HasPrefix(rows[0].features, "section:geo ↑ (100% vs 0%"), rows[0].features)
	require.Contains(t, rows[1].rule, "length(`sym:v`) > 0")
	require.Contains(t, rows[1].fit, "recall 100%")
	require.Contains(t, itemsSummary(pi), "2 items kept of 4 candidates")
	// No clusters: nothing to read.
	none := explainItems(context.Background(), sets, 4, algo.HDBSCANResult{}, 10)
	require.Nil(t, none.contrast)
	require.Empty(t, itemsRows(none, 2))
}

func TestBuildProjectionDatasets(t *testing.T) {
	alloc := memory.NewGoAllocator()
	features, cl := twoClusterFeatures(60)
	ex := explainProjection(context.Background(), features, cl)
	require.NoError(t, ex.err)
	// A result whose slots are rows 5..0 of a six-row record, with an
	// identity column and a tagged-section column the publish must skip.
	idb := array.NewInt64Builder(alloc)
	idb.AppendValues([]int64{100, 101, 102, 103, 104, 105}, nil)
	tvb := array.NewStringBuilder(alloc)
	for range 6 {
		tvb.Append("v")
	}
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id:id:u64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "tv:s:value:val", Type: arrow.BinaryTypes.String},
	}, nil), []arrow.Array{idb.NewArray(), tvb.NewArray()}, 6)
	defer rec.Release()
	res := &projectionResult{
		rows:         []int64{5, 4, 3, 2, 1, 0},
		slotFeatures: features[:6],
		clusters:     algo.HDBSCANResult{Label: []int32{0, 0, 1, 1, -1, 1}, Probability: []float32{1, 1, 1, 1, 0, 0.5}, NumClusters: 2},
		params:       projectionParams{FeatureSet: projectionFeatureStructure},
		explanation:  ex,
	}
	in := projectionPublishInput{rec: rec, res: res, depth: 3, perCluster: true, x: []float32{1, 2, 3, 4, 5, 6}, y: []float32{6, 5, 4, 3, 2, 1}}
	rows, err := buildProjectionRows(in, alloc)
	require.NoError(t, err)
	defer rows.Release()
	require.EqualValues(t, 6, rows.NumRows())
	names := []string{}
	for _, f := range rows.Schema().Fields() {
		names = append(names, f.Name)
	}
	require.Contains(t, names, "id:id:u64")
	require.NotContains(t, names, "tv:s:value:val")
	require.Contains(t, names, "mean_value_length")
	require.Contains(t, names, "cluster")
	require.Contains(t, names, "items")
	ids := rows.Column(1).(*array.Int64)
	require.Equal(t, int64(105), ids.Value(0), "identity gathered in slot order")
	require.Equal(t, int64(100), ids.Value(5))
	clusters := rows.Column(rows.Schema().FieldIndices("cluster")[0]).(*array.Int32)
	require.Equal(t, []int32{1, 1, 2, 2, -1, 2}, clusters.Int32Values(), "numbered from one, noise stays −1")
	fs := rows.Column(rows.Schema().FieldIndices("feature_set")[0]).(*array.String)
	require.Equal(t, "structure", fs.Value(0))

	rules := buildProjectionRules(in, alloc)
	defer rules.Release()
	require.GreaterOrEqual(t, rules.NumRows(), int64(2))
	kinds := rules.Column(1).(*array.String)
	ruleText := rules.Column(2).(*array.String)
	sawFeatures := false
	for i := range int(rules.NumRows()) {
		if kinds.Value(i) == "features" {
			sawFeatures = true
			require.Contains(t, ruleText.Value(i), "mean_value_length")
		}
	}
	require.True(t, sawFeatures)
	require.Contains(t, projectionScaffold(), "keelson('projection')")
}
