package algo

import (
	"context"
	"math/rand/v2"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// BetweennessOptions tunes [Betweenness].
type BetweennessOptions struct {
	// Direction selects the adjacency shortest paths follow; DirectionBoth
	// walks a directed graph as undirected.
	Direction engine.DirectionE
	// ExactMaxVertices is the vertex count up to which every vertex is a
	// source. Above it, Pivots sources are sampled. Zero means always exact.
	ExactMaxVertices int
	// Pivots is the sample size when sampling; zero means 256.
	Pivots int
	// Seed drives the pivot sample.
	Seed uint64
	// Sources, when set, are the sources used, exactly and in this order;
	// ExactMaxVertices and Pivots are ignored. Scores are not rescaled.
	Sources []int32
}

// BetweennessResult holds vertex betweenness.
type BetweennessResult struct {
	// Score is Brandes' sum over ordered pairs s ≠ v ≠ t of the fraction of
	// shortest s–t paths through v. On an undirected graph each unordered
	// pair therefore counts twice, as in the definition as written. Under
	// sampling it is the Brandes–Pich estimate: the sum over the sampled
	// sources scaled by n over the sample size.
	Score []float64
	// Sources counts the sources whose dependencies were accumulated.
	Sources int
	// Sampled reports that Score is an estimate.
	Sampled bool
	Truncation
}

// bcChunk is the fixed number of sources per job. Partials are folded in
// chunk order, so the sum's bits do not depend on the worker count.
const bcChunk = 16

// Betweenness runs Brandes (2001): per source a breadth-first search that
// counts shortest paths, then a reverse pass that accumulates dependencies.
// Sources are processed in fixed chunks by waves of workers, each worker
// summing into a private column that is folded into the result in chunk
// order after every wave.
func Betweenness(ctx context.Context, e *engine.Engine, g *csr.Graph, opts BetweennessOptions) (r BetweennessResult, err error) {
	e = engineOrDefault(e)
	n := g.NumVertices()
	r.Score = make([]float64, n)
	if n == 0 {
		return
	}
	var sources []int32
	scale := 1.0
	switch {
	case opts.Sources != nil:
		for _, s := range opts.Sources {
			if s < 0 || int(s) >= n {
				err = eb.Build().Int32("source", s).Int("vertices", n).Errorf("source slot out of range")
				return
			}
		}
		sources = opts.Sources
	case opts.ExactMaxVertices > 0 && n > opts.ExactMaxVertices:
		k := opts.Pivots
		if k == 0 {
			k = 256
		}
		if k >= n {
			sources = allSlots(n)
		} else {
			sources = samplePivots(n, k, opts.Seed)
			scale = float64(n) / float64(k)
			r.Sampled = true
		}
	default:
		sources = allSlots(n)
	}

	workers := e.Workers()
	chunks := (len(sources) + bcChunk - 1) / bcChunk
	if chunks < workers {
		workers = max(chunks, 1)
	}
	partial := make([][]float64, workers)
	scratch := make([]*bcScratch, workers)
	for w := range workers {
		partial[w] = make([]float64, n)
		scratch[w] = newBCScratch(n)
	}
	fold := func(used int) {
		for w := range used {
			p := partial[w]
			for v := range n {
				r.Score[v] += p[v]
			}
			clear(p)
		}
	}
	for wave := 0; wave < chunks; wave += workers {
		if ctxDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			break
		}
		used := min(workers, chunks-wave)
		e.Fanout(used, func(w int) {
			c := wave + w
			clo := c * bcChunk
			chi := min(clo+bcChunk, len(sources))
			for _, s := range sources[clo:chi] {
				brandesSource(g, opts.Direction, s, scratch[w], partial[w])
			}
		})
		fold(used)
		r.Sources += min(used*bcChunk, len(sources)-wave*bcChunk)
	}
	if r.Sampled && !r.Truncated {
		r.Truncation = truncatedBy(LimitPivots)
	}
	if scale != 1 {
		for v := range n {
			r.Score[v] *= scale
		}
	}
	return
}

func allSlots(n int) []int32 {
	s := make([]int32, n)
	for i := range n {
		s[i] = int32(i)
	}
	return s
}

// samplePivots draws k distinct slots with a seeded generator, in the order
// drawn.
func samplePivots(n, k int, seed uint64) []int32 {
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	perm := rng.Perm(n)
	s := make([]int32, k)
	for i := range k {
		s[i] = int32(perm[i])
	}
	return s
}

type bcScratch struct {
	dist  []int32
	sigma []float64
	delta []float64
	order []int32 // BFS visitation order
	queue []int32
	buf   []int32
}

func newBCScratch(n int) *bcScratch {
	return &bcScratch{
		dist:  make([]int32, n),
		sigma: make([]float64, n),
		delta: make([]float64, n),
		order: make([]int32, 0, n),
		queue: make([]int32, 0, n),
		buf:   make([]int32, 0, 64),
	}
}

// brandesSource accumulates the dependencies of source s into acc.
func brandesSource(g *csr.Graph, dir engine.DirectionE, s int32, sc *bcScratch, acc []float64) {
	fill(sc.dist, -1)
	clear(sc.sigma)
	clear(sc.delta)
	sc.order = sc.order[:0]
	sc.queue = sc.queue[:0]
	sc.dist[s] = 0
	sc.sigma[s] = 1
	sc.queue = append(sc.queue, s)
	for head := 0; head < len(sc.queue); head++ {
		v := sc.queue[head]
		sc.order = append(sc.order, v)
		for _, w := range forwardRow(g, v, dir, sc.buf[:0]) {
			if sc.dist[w] == -1 {
				sc.dist[w] = sc.dist[v] + 1
				sc.queue = append(sc.queue, w)
			}
			if sc.dist[w] == sc.dist[v]+1 {
				sc.sigma[w] += sc.sigma[v]
			}
		}
	}
	for i := len(sc.order) - 1; i >= 0; i-- {
		w := sc.order[i]
		coeff := (1 + sc.delta[w]) / sc.sigma[w]
		for _, v := range backwardRow(g, w, dir, sc.buf[:0]) {
			if sc.dist[v] == sc.dist[w]-1 {
				sc.delta[v] += sc.sigma[v] * coeff
			}
		}
		if w != s {
			acc[w] += sc.delta[w]
		}
	}
}

func forwardRow(g *csr.Graph, v int32, dir engine.DirectionE, buf []int32) []int32 {
	switch dir {
	case engine.DirectionIn:
		return g.In(v)
	case engine.DirectionBoth:
		if !g.IsDirected() {
			return g.Out(v)
		}
		return mergeSorted(g.Out(v), g.In(v), buf)
	default:
		return g.Out(v)
	}
}

func backwardRow(g *csr.Graph, v int32, dir engine.DirectionE, buf []int32) []int32 {
	switch dir {
	case engine.DirectionIn:
		return g.Out(v)
	case engine.DirectionBoth:
		if !g.IsDirected() {
			return g.Out(v)
		}
		return mergeSorted(g.Out(v), g.In(v), buf)
	default:
		return g.In(v)
	}
}
