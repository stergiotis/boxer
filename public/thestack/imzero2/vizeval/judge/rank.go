package judge

import (
	"cmp"
	"math"
	"slices"
)

// Strengths are Bradley–Terry scores, one per candidate id: the log of the
// fitted strength, so 0 is the average candidate and a difference of d means
// odds of e^d that the first is preferred to the second.
type Strengths map[string]float64

// Fit fits Bradley–Terry strengths (Bradley and Terry 1952) to pairwise
// outcomes on one criterion, or on all when criterion is empty, by the MM
// algorithm (Hunter 2004). A tie or a split counts half a win to each side.
//
// Each candidate also plays one win and one loss against a fixed virtual
// opponent of strength 1. Without that prior a candidate that won every
// comparison has no finite strength, which with a handful of candidates is
// the common case rather than the exception.
func Fit(ids []string, vs []PairVerdict, criterion string) (s Strengths) {
	n := len(ids)
	idx := make(map[string]int, n)
	for i, id := range ids {
		idx[id] = i
	}
	wins := make([]float64, n)
	games := make([][]float64, n)
	for i := range games {
		games[i] = make([]float64, n)
	}
	for _, v := range vs {
		a, okA := idx[v.A]
		b, okB := idx[v.B]
		if !okA || !okB || v.Error != "" {
			continue
		}
		for c, p := range v.Prefer {
			if criterion != "" && c != criterion {
				continue
			}
			games[a][b]++
			games[b][a]++
			switch p {
			case PreferA:
				wins[a]++
			case PreferB:
				wins[b]++
			default:
				wins[a] += 0.5
				wins[b] += 0.5
			}
		}
	}
	p := make([]float64, n)
	for i := range p {
		p[i] = 1
	}
	next := make([]float64, n)
	for iter := 0; iter < 1000; iter++ {
		var delta float64
		for i := range p {
			// The prior: one win (the +1 in the numerator) and two games
			// against strength 1.
			denom := 2 / (p[i] + 1)
			for j := range p {
				if games[i][j] > 0 {
					denom += games[i][j] / (p[i] + p[j])
				}
			}
			next[i] = (wins[i] + 1) / denom
		}
		for i := range p {
			delta = max(delta, math.Abs(math.Log(next[i])-math.Log(p[i])))
			p[i] = next[i]
		}
		if delta < 1e-10 {
			break
		}
	}
	var mean float64
	for _, v := range p {
		mean += math.Log(v)
	}
	mean /= float64(max(n, 1))
	s = make(Strengths, n)
	for i, id := range ids {
		s[id] = math.Log(p[i]) - mean
	}
	return s
}

// Order is the ids by descending strength, ties broken by id for stability.
func (inst Strengths) Order() (ids []string) {
	ids = make([]string, 0, len(inst))
	for id := range inst {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		switch {
		case inst[a] > inst[b]:
			return -1
		case inst[a] < inst[b]:
			return 1
		}
		return cmp.Compare(a, b)
	})
	return ids
}
