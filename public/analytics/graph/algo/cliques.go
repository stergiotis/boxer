package algo

import (
	"context"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"slices"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
)

// CliqueOptions tunes [MaximalCliques].
type CliqueOptions struct {
	// MaxCliques caps the listing; zero means [DefaultMaxCliques]. The cap is
	// the budget: the number of maximal cliques can be exponential in the
	// degeneracy.
	MaxCliques int
	// MinSize drops cliques smaller than this; zero or one keeps all.
	MinSize int
}

// DefaultMaxCliques is the listing cap when none is given.
const DefaultMaxCliques = 100_000

// CliqueResult lists maximal cliques of the graph viewed as undirected, as a
// CSR: clique i is Members[Start[i]:Start[i+1]], ascending slots.
type CliqueResult struct {
	Start   []int32
	Members []int32
	// Count is the number of cliques listed.
	Count int
	// Largest is the size of the largest clique listed.
	Largest int
	Truncation
}

// MaximalCliques lists maximal cliques by Bron–Kerbosch with Tomita pivoting,
// the outer level in degeneracy order (Eppstein, Löffler & Strash 2010), so
// the recursion depth is bounded by the degeneracy and the listing order is
// a function of the topology alone. It runs serially: the output cap makes
// the listing a prefix, and a prefix is only meaningful in a fixed order.
func MaximalCliques(ctx context.Context, g *csr.Graph, opts CliqueOptions) (r CliqueResult) {
	capCliques := opts.MaxCliques
	if capCliques <= 0 {
		capCliques = DefaultMaxCliques
	}
	u := newUndirectedView(g)
	n := g.NumVertices()
	r.Start = append(r.Start, 0)
	if n == 0 {
		return
	}
	kc := KCore(ctx, g)
	if kc.Truncated {
		r.Truncation = kc.Truncation
		return
	}
	rank := make([]int32, n)
	for i, v := range kc.Order {
		rank[v] = int32(i)
	}
	bk := &bkState{u: u, res: &r, capCliques: capCliques, minSize: max(opts.MinSize, 1)}
	for i, v := range kc.Order {
		if i&0xFF == 0 && engine.ContextDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			return
		}
		var p, x []int32
		for _, w := range u.nbr(v) {
			if rank[w] > rank[v] {
				p = append(p, w)
			} else {
				x = append(x, w)
			}
		}
		bk.clique = append(bk.clique[:0], v)
		if !bk.expand(p, x) {
			r.Truncation = truncatedBy(LimitOutput)
			return
		}
	}
	return
}

type bkState struct {
	u          undirectedView
	res        *CliqueResult
	capCliques int
	minSize    int
	clique     []int32
}

// expand is BKPivot(R=clique, P, X): P and X are ascending. It returns false
// once the output cap is hit.
func (s *bkState) expand(p, x []int32) bool {
	if len(p) == 0 {
		if len(x) == 0 {
			return s.emit()
		}
		return true
	}
	// Tomita pivot: the vertex of P ∪ X with the most neighbours in P.
	pivot := int32(-1)
	best := -1
	for _, cand := range [][]int32{p, x} {
		for _, v := range cand {
			c := intersectCount(p, s.u.nbr(v))
			if c > best {
				best, pivot = c, v
			}
		}
	}
	// Candidates: P minus N(pivot), ascending.
	cands := make([]int32, 0, len(p)-best)
	pn := s.u.nbr(pivot)
	j := 0
	for _, v := range p {
		for j < len(pn) && pn[j] < v {
			j++
		}
		if j < len(pn) && pn[j] == v {
			continue
		}
		cands = append(cands, v)
	}
	for _, v := range cands {
		nv := s.u.nbr(v)
		np := intersectSorted(p, nv, make([]int32, 0, min(len(p), len(nv))))
		nx := intersectSorted(x, nv, make([]int32, 0, min(len(x), len(nv))))
		s.clique = append(s.clique, v)
		ok := s.expand(np, nx)
		s.clique = s.clique[:len(s.clique)-1]
		if !ok {
			return false
		}
		// Move v from P to X, keeping both ascending.
		p = removeSorted(p, v)
		x = insertSorted(x, v)
	}
	return true
}

func (s *bkState) emit() bool {
	if len(s.clique) < s.minSize {
		return true
	}
	if s.res.Count >= s.capCliques {
		return false
	}
	// Members ascending, so the listing is canonical.
	start := len(s.res.Members)
	s.res.Members = append(s.res.Members, s.clique...)
	slices.Sort(s.res.Members[start:])
	s.res.Start = append(s.res.Start, int32(len(s.res.Members)))
	s.res.Count++
	s.res.Largest = max(s.res.Largest, len(s.clique))
	return true
}

func removeSorted(a []int32, v int32) []int32 {
	out := make([]int32, 0, len(a))
	for _, w := range a {
		if w != v {
			out = append(out, w)
		}
	}
	return out
}

func insertSorted(a []int32, v int32) []int32 {
	out := make([]int32, 0, len(a)+1)
	placed := false
	for _, w := range a {
		if !placed && v < w {
			out = append(out, v)
			placed = true
		}
		out = append(out, w)
	}
	if !placed {
		out = append(out, v)
	}
	return out
}
