package explain

import (
	"context"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// plantedItems draws n rows over 6 items where label 0 is "item 0 and item
// 1 held" (with a little noise), everyone else is label 1 or noise, and
// items 2..5 are coin flips.
func plantedItems(rng *rand.Rand, n int) (rows [][]int32, labels []int32) {
	rows = make([][]int32, n)
	labels = make([]int32, n)
	for r := range n {
		var row []int32
		for it := int32(0); it < 6; it++ {
			if rng.Float64() < 0.5 {
				row = append(row, it)
			}
		}
		rows[r] = row
		has0 := len(row) > 0 && row[0] == 0
		has1 := false
		for _, it := range row {
			if it == 1 {
				has1 = true
			}
		}
		switch {
		case has0 && has1 && rng.Float64() < 0.95:
			labels[r] = 0
		case rng.Float64() < 0.1:
			labels[r] = -1
		default:
			labels[r] = 1
		}
	}
	return
}

func TestItemContrastsPlanted(t *testing.T) {
	rng := rand.New(rand.NewPCG(21, 22))
	rows, labels := plantedItems(rng, 2000)
	c, err := ItemContrasts(context.Background(), rows, 6, labels)
	require.NoError(t, err)
	require.Equal(t, 2, c.NumLabels)
	require.Equal(t, 12, c.Tests)
	require.InDelta(t, 0.01/12, c.Level, 1e-12)
	c0 := c.Cell(0, 0)
	require.Greater(t, c0.In, float32(0.99), "every label-0 row holds item 0")
	require.Less(t, c0.Rest, float32(0.5))
	require.Greater(t, c0.Lift, float32(1))
	require.Greater(t, c0.WRAcc, float32(0))
	require.True(t, c0.Significant)
	c3 := c.Cell(0, 3)
	require.InDelta(t, 0, c3.WRAcc, 0.03, "a coin flip carries no association")
	require.False(t, c3.Significant)
	ranked := c.Ranked(0)
	require.Contains(t, []int32{0, 1}, ranked[0])
	require.Contains(t, []int32{0, 1}, ranked[1])
}

func TestFisherTwoSidedKnownTable(t *testing.T) {
	// The tea-tasting table: 3 of 4 in a draw of 4 from 8 with 4 successes.
	// Two-sided p = P(3) + P(1) + P(4) + P(0) = (16+16+1+1)/70.
	lf := newLogFactorials(8)
	require.InDelta(t, 34.0/70, fisherTwoSided(lf, 3, 4, 4, 8), 1e-9)
	require.InDelta(t, 1, fisherTwoSided(lf, 2, 4, 4, 8), 1e-9)
	require.InDelta(t, 2.0/70, fisherTwoSided(lf, 4, 4, 4, 8), 1e-9)
	require.Equal(t, 1.0, fisherTwoSided(lf, 0, 0, 4, 8))
	// A p-value is a probability whatever the table.
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 60).Draw(rt, "n")
		m := rapid.IntRange(0, n).Draw(rt, "m")
		tot := rapid.IntRange(0, n).Draw(rt, "tot")
		x := rapid.IntRange(max(0, m+tot-n), min(m, tot)).Draw(rt, "x")
		p := fisherTwoSided(newLogFactorials(n), x, m, tot, n)
		require.GreaterOrEqual(rt, p, 0.0)
		require.LessOrEqual(rt, p, 1.0)
		require.False(rt, math.IsNaN(p))
	})
}

func TestFindSubgroupPlanted(t *testing.T) {
	rng := rand.New(rand.NewPCG(23, 24))
	rows, labels := plantedItems(rng, 2000)
	sg, err := FindSubgroup(context.Background(), rows, 6, labels, 0, SubgroupOptions{MaxTerms: 3, Beam: 4})
	require.NoError(t, err)
	require.Len(t, sg.Literals, 2, "the planted conjunction has two items")
	require.Equal(t, []Literal{{Item: 0, Present: true}, {Item: 1, Present: true}}, sg.Literals)
	require.Greater(t, sg.Precision, float32(0.9))
	require.InDelta(t, 1, sg.Recall, 1e-6)
	require.Greater(t, sg.WRAcc, float32(0))
	spell := func(item int32) (string, bool) {
		if item == 1 {
			return "", false
		}
		return "has(items, " + itoa(item) + ")", true
	}
	sql, ok := sg.SQL(spell, nil)
	require.False(t, ok)
	require.Contains(t, sql, "has(items, 0) AND /* item 1 has no SQL spelling */ true")
	sql, ok = sg.SQL(spell, func(item int32) string { return "tag:x" })
	require.False(t, ok)
	require.Contains(t, sql, "/* tag:x has no SQL spelling */")
	sql, ok = sg.SQL(func(item int32) (string, bool) { return "i" + itoa(item), true }, nil)
	require.True(t, ok)
	require.Equal(t, "i0 AND i1", sql)
	// Negations: label 1 is best described by lacking one of the two.
	sg1, err := FindSubgroup(context.Background(), rows, 6, labels, 1, SubgroupOptions{Negations: true})
	require.NoError(t, err)
	require.NotEmpty(t, sg1.Literals)
	require.False(t, sg1.Literals[0].Present)
	sql, ok = sg1.SQL(func(item int32) (string, bool) { return "i" + itoa(item), true }, nil)
	require.True(t, ok)
	require.Contains(t, sql, "NOT (i")
	// Determinism and the empty result.
	again, err := FindSubgroup(context.Background(), rows, 6, labels, 0, SubgroupOptions{MaxTerms: 3, Beam: 4})
	require.NoError(t, err)
	require.Equal(t, sg, again)
	_, err = FindSubgroup(context.Background(), rows, 6, labels, 5, SubgroupOptions{})
	require.Error(t, err)
	none, err := FindSubgroup(context.Background(), [][]int32{{}, {}, {}, {}, {}, {}}, 0, []int32{0, 0, 0, 1, 1, 1}, 0, SubgroupOptions{})
	require.NoError(t, err)
	require.Empty(t, none.Literals)
	sql, ok = none.SQL(nil, nil)
	require.True(t, ok)
	require.Equal(t, "true", sql)
}

func TestFindSubgroupProperties(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(6, 80).Draw(rt, "n")
		d := rapid.IntRange(1, 5).Draw(rt, "d")
		rows := make([][]int32, n)
		labels := make([]int32, n)
		for r := range n {
			for it := range d {
				if rapid.Bool().Draw(rt, "has") {
					rows[r] = append(rows[r], int32(it))
				}
			}
			labels[r] = int32(rapid.IntRange(-1, 1).Draw(rt, "label"))
		}
		labels[0] = 0
		sg, err := FindSubgroup(context.Background(), rows, d, labels, 0, SubgroupOptions{MaxTerms: 2, Beam: 3, MinRows: 2, Negations: true})
		require.NoError(rt, err)
		// Every covered row satisfies the literals, and the counts agree.
		cover, hits := 0, 0
		for r, row := range rows {
			ok := true
			for _, l := range sg.Literals {
				has := false
				for _, it := range row {
					if it == l.Item {
						has = true
					}
				}
				if has != l.Present {
					ok = false
				}
			}
			if ok {
				cover++
				if labels[r] == 0 {
					hits++
				}
			}
		}
		require.EqualValues(rt, cover, sg.Rows)
		require.EqualValues(rt, hits, sg.Hits)
		require.GreaterOrEqual(rt, sg.WRAcc, float32(0), "the empty conjunction scores 0, so the best is never worse")
	})
}
