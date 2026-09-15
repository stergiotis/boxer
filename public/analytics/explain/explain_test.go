package explain

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// boxes draws n rows of d features whose label is decided by two axis-aligned
// thresholds — label 0 where x0 <= 1, else 1 where x1 <= 1, else 2 — with
// the other features noise, and a share of rows relabelled noise (−1).
func boxes(rng *rand.Rand, n, d int, noise float64) (x []float64, labels []int32) {
	x = make([]float64, n*d)
	labels = make([]int32, n)
	for r := range n {
		for f := range d {
			x[r*d+f] = rng.Float64() * 2
		}
		switch {
		case x[r*d+0] <= 1:
			labels[r] = 0
		case x[r*d+1] <= 1:
			labels[r] = 1
		default:
			labels[r] = 2
		}
		if rng.Float64() < noise {
			labels[r] = -1
		}
	}
	return
}

func TestFitTreeRecoversBoxes(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	x, labels := boxes(rng, 2000, 4, 0.1)
	tr, err := FitTree(context.Background(), x, 4, labels, TreeOptions{MaxDepth: 2, MinLeaf: 5})
	require.NoError(t, err)
	require.False(t, tr.Truncation.Truncated)
	require.Equal(t, 3, tr.NumLabels)
	root := tr.Nodes[0]
	require.EqualValues(t, 0, root.Feature)
	require.InDelta(t, 1, root.Threshold, 0.02)
	agree, fitted := tr.Fidelity(2)
	require.Equal(t, fitted, tr.Fitted)
	require.Equal(t, agree, fitted, "two thresholds separate the boxes exactly")
	rules := tr.Rules(2)
	require.Len(t, rules, 3)
	for _, r := range rules {
		require.InDelta(t, 1, r.Precision, 1e-6)
		require.InDelta(t, 1, r.Recall, 1e-6)
	}
	// Leaf 0's rule is the one term; the last leaf carries both thresholds.
	require.Len(t, rules[0].Terms, 1)
	require.Len(t, rules[2].Terms, 2)
	require.Equal(t, Term{Feature: 0, Above: true, Threshold: rules[2].Terms[0].Threshold}, rules[2].Terms[0])
	require.Equal(t, Term{Feature: 1, Above: true, Threshold: rules[2].Terms[1].Threshold}, rules[2].Terms[1])
	require.InDelta(t, 1, rules[2].Terms[0].Threshold, 0.02)
	require.InDelta(t, 1, rules[2].Terms[1].Threshold, 0.02)
	// Noise rows were not fitted and reach no leaf.
	for r, lb := range labels {
		if lb < 0 {
			require.EqualValues(t, -1, tr.Leaf[r])
			require.EqualValues(t, -1, tr.LeafAt(r, 1))
		} else {
			require.GreaterOrEqual(t, tr.Leaf[r], int32(0))
		}
	}
}

func TestFitTreeCutIsPrefix(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	x, labels := boxes(rng, 500, 3, 0)
	// Splits inside a pure box are refused, so depth stops at 2 even when
	// more is allowed; a shallower separate fit is the deeper tree's cut.
	deep, err := FitTree(context.Background(), x, 3, labels, TreeOptions{MaxDepth: 5, MinLeaf: 3})
	require.NoError(t, err)
	shallow, err := FitTree(context.Background(), x, 3, labels, TreeOptions{MaxDepth: 1, MinLeaf: 3})
	require.NoError(t, err)
	require.Equal(t, shallow.Rules(1), deep.Rules(1))
	a1, _ := shallow.Fidelity(1)
	a2, _ := deep.Fidelity(1)
	require.Equal(t, a1, a2)
	for r := range labels {
		require.Equal(t, shallow.LeafAt(r, 1) >= 0, deep.LeafAt(r, 1) >= 0)
		require.Equal(t, shallow.Nodes[shallow.LeafAt(r, 1)].Label, deep.Nodes[deep.LeafAt(r, 1)].Label)
	}
}

func TestFitTreeProperties(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 120).Draw(rt, "n")
		d := rapid.IntRange(1, 4).Draw(rt, "d")
		k := rapid.IntRange(1, 4).Draw(rt, "k")
		x := make([]float64, n*d)
		labels := make([]int32, n)
		for i := range x {
			x[i] = float64(rapid.IntRange(-3, 3).Draw(rt, "x"))
		}
		anyLabel := false
		for r := range labels {
			labels[r] = int32(rapid.IntRange(-1, k-1).Draw(rt, "label"))
			anyLabel = anyLabel || labels[r] >= 0
		}
		depth := rapid.IntRange(1, 4).Draw(rt, "depth")
		minLeaf := rapid.IntRange(1, 6).Draw(rt, "minLeaf")
		tr, err := FitTree(context.Background(), x, d, labels, TreeOptions{MaxDepth: depth, MinLeaf: minLeaf})
		if !anyLabel {
			require.Error(rt, err)
			return
		}
		require.NoError(rt, err)
		// Fidelity never falls as the cut deepens, and the deepest cut is
		// the sum over real leaves.
		prev := -1
		for c := 0; c <= depth; c++ {
			agree, fitted := tr.Fidelity(c)
			require.Equal(rt, tr.Fitted, fitted)
			require.GreaterOrEqual(rt, agree, prev)
			require.LessOrEqual(rt, agree, fitted)
			prev = agree
		}
		// Every fitted row satisfies its leaf's rule at every cut and no
		// leaf is smaller than MinLeaf unless it is the root.
		for c := 0; c <= depth; c++ {
			byNode := map[int32]Rule{}
			for _, r := range tr.Rules(c) {
				byNode[r.Node] = r
				require.Equal(rt, r.Hits, tr.Nodes[r.Node].Counts[r.Label])
				if r.Node != 0 {
					require.GreaterOrEqual(rt, int(r.Rows), minLeaf)
				}
			}
			for r, lb := range labels {
				leaf := tr.LeafAt(r, c)
				if lb < 0 {
					require.EqualValues(rt, -1, leaf)
					continue
				}
				rule, ok := byNode[leaf]
				require.True(rt, ok)
				for _, term := range rule.Terms {
					v := x[r*d+int(term.Feature)]
					if term.Above {
						require.Greater(rt, v, term.Threshold)
					} else {
						require.LessOrEqual(rt, v, term.Threshold)
					}
				}
			}
		}
		// Determinism: the same input gives the same tree.
		again, err := FitTree(context.Background(), x, d, labels, TreeOptions{MaxDepth: depth, MinLeaf: minLeaf})
		require.NoError(rt, err)
		require.Equal(rt, tr.Nodes, again.Nodes)
	})
}

func TestFitTreeCancelled(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	x, labels := boxes(rng, 300, 3, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tr, err := FitTree(ctx, x, 3, labels, TreeOptions{})
	require.NoError(t, err)
	require.True(t, tr.Truncation.Truncated)
	require.Len(t, tr.Nodes, 1)
	require.Len(t, tr.Rules(6), 1)
	require.Equal(t, "always", tr.Rules(6)[0].String())
}

func TestFitTreeRejectsShape(t *testing.T) {
	_, err := FitTree(context.Background(), []float64{1, 2, 3}, 2, []int32{0, 1}, TreeOptions{})
	require.Error(t, err)
	_, err = FitTree(context.Background(), []float64{1, 2}, 2, []int32{-1}, TreeOptions{})
	require.Error(t, err)
}

func TestMergeTerms(t *testing.T) {
	got := mergeTerms([]Term{
		{Feature: 2, Above: false, Threshold: 5},
		{Feature: 0, Above: true, Threshold: 1},
		{Feature: 2, Above: false, Threshold: 3},
		{Feature: 0, Above: true, Threshold: 2},
		{Feature: 2, Above: true, Threshold: 1},
	})
	require.Equal(t, []Term{
		{Feature: 0, Above: true, Threshold: 2},
		{Feature: 2, Above: true, Threshold: 1},
		{Feature: 2, Above: false, Threshold: 3},
	}, got)
}

func TestContrastsSeparation(t *testing.T) {
	// Feature 0 separates label 0 perfectly (members low); feature 1 is
	// identical for everyone; feature 2 is a coin flip.
	n := 200
	d := 3
	x := make([]float64, n*d)
	labels := make([]int32, n)
	rng := rand.New(rand.NewPCG(7, 8))
	for r := range n {
		if r < 50 {
			labels[r] = 0
			x[r*d+0] = float64(r)
		} else if r < 100 {
			labels[r] = 1
			x[r*d+0] = float64(r)
		} else {
			labels[r] = -1
			x[r*d+0] = float64(r)
		}
		x[r*d+1] = 4
		x[r*d+2] = rng.Float64()
	}
	c, err := Contrasts(context.Background(), x, d, labels)
	require.NoError(t, err)
	require.Equal(t, 2, c.NumLabels)
	require.EqualValues(t, []int32{50, 50}, c.Rows)
	require.InDelta(t, 0, c.Cell(0, 0).AUC, 1e-6, "every member under every non-member")
	require.InDelta(t, 0.5, c.Cell(0, 1).AUC, 1e-6, "all ties")
	require.InDelta(t, 0.5, c.Cell(0, 2).AUC, 0.1)
	require.InDelta(t, 24.5, c.Cell(0, 0).Median, 1e-9)
	require.InDelta(t, 12.25, c.Cell(0, 0).Q1, 1e-9)
	require.InDelta(t, 36.75, c.Cell(0, 0).Q3, 1e-9)
	require.InDelta(t, 124.5, c.Cell(0, 0).MedianRest, 1e-9)
	// Label 1 sits in the middle of the range: above the 50 rows of label 0
	// and below the 100 noise rows, which are part of its rest.
	require.InDelta(t, 1.0/3, c.Cell(1, 0).AUC, 1e-6)
	ranked := c.Ranked(0)
	require.EqualValues(t, 0, ranked[0])
	require.InDelta(t, 0.5, c.Cell(0, 0).Effect(), 1e-6)
}

func TestContrastsAUCMatchesPairs(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(2, 40).Draw(rt, "n")
		x := make([]float64, n)
		labels := make([]int32, n)
		for r := range n {
			x[r] = float64(rapid.IntRange(0, 5).Draw(rt, "x"))
			labels[r] = int32(rapid.IntRange(-1, 1).Draw(rt, "label"))
		}
		c, err := Contrasts(context.Background(), x, 1, labels)
		if err != nil {
			for _, lb := range labels {
				require.Less(rt, lb, int32(0))
			}
			return
		}
		for lb := range int32(c.NumLabels) {
			// Count pairs by hand.
			wins, pairs := 0.0, 0.0
			for a := range n {
				if labels[a] != lb {
					continue
				}
				for b := range n {
					if labels[b] == lb {
						continue
					}
					pairs++
					switch {
					case x[a] > x[b]:
						wins++
					case x[a] == x[b]:
						wins += 0.5
					}
				}
			}
			cell := c.Cell(lb, 0)
			if pairs == 0 {
				require.True(rt, math.IsNaN(float64(cell.AUC)))
				continue
			}
			require.InDelta(rt, wins/pairs, cell.AUC, 1e-5)
		}
	})
}

func TestQuantile(t *testing.T) {
	require.True(t, math.IsNaN(quantile(nil, 0.5)))
	require.Equal(t, 3.0, quantile([]float64{3}, 0.5))
	require.Equal(t, 2.5, quantile([]float64{1, 2, 3, 4}, 0.5))
	require.Equal(t, 1.75, quantile([]float64{1, 2, 3, 4}, 0.25))
}

func TestShortestBetween(t *testing.T) {
	require.Equal(t, 37.0, shortestBetween(36.9, 37.4))
	require.Equal(t, 110.0, shortestBetween(109.7, 110.2))
	require.Equal(t, 0.5, shortestBetween(0, 1))
	require.Equal(t, 0.1, shortestBetween(0, 0.3), "one digit, nearest the midpoint")
	rapid.Check(t, func(rt *rapid.T) {
		a := rapid.Float64Range(-1e6, 1e6).Draw(rt, "a")
		w := rapid.Float64Range(1e-9, 1e3).Draw(rt, "w")
		b := a + w
		if !(b > a) {
			return
		}
		tv := shortestBetween(a, b)
		require.GreaterOrEqual(rt, tv, a)
		require.Less(rt, tv, b)
		// Exact through text: the printed threshold parses back to itself.
		back, err := strconv.ParseFloat(strconv.FormatFloat(tv, 'g', -1, 64), 64)
		require.NoError(rt, err)
		require.Equal(rt, tv, back)
	})
}

func TestFitOneVsRestAndSQL(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	x, labels := boxes(rng, 1500, 3, 0.1)
	// Label 2 is the box x0 > 1 ∧ x1 > 1; against everyone else, noise
	// included, a depth-2 tree finds it.
	tr, err := FitOneVsRest(context.Background(), x, 3, labels, 2, TreeOptions{MaxDepth: 2, MinLeaf: 5})
	require.NoError(t, err)
	require.Equal(t, 2, tr.NumLabels)
	require.Equal(t, len(labels), tr.Fitted, "noise rows are fitted on the rest side")
	rules := tr.RulesFor(2, 1)
	require.NotEmpty(t, rules)
	name := func(f int32) string { return fmt.Sprintf("x%d", f) }
	sql := SQL(rules, name)
	require.Contains(t, sql, "x0 > 1")
	require.Contains(t, sql, "x1 > 1")
	require.NotContains(t, sql, "OR", "one leaf is the whole box")
	// Noise rows that fall inside the box are the rule's misses — they were
	// counted on the rest side — so precision sits under one while recall
	// is whole.
	rows, hits, precision, recall := Coverage(rules)
	require.Greater(t, rows, hits)
	require.InDelta(t, 0.9, precision, 0.05)
	require.InDelta(t, 1, recall, 1e-6)
	require.Equal(t, "false", SQL(nil, name))
	require.Equal(t, "true", Rule{}.SQL(name))
	two := SQL([]Rule{{Terms: []Term{{Feature: 0, Above: true, Threshold: 1}}}, {Terms: []Term{{Feature: 1, Above: false, Threshold: 2.5}}}}, name)
	require.Equal(t, "(x0 > 1) OR (x1 <= 2.5)", two)
	_, err = FitOneVsRest(context.Background(), x, 3, labels, 7, TreeOptions{})
	require.Error(t, err)
}
