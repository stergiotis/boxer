package algo

import (
	"context"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// BFSOptions tunes [BFS].
type BFSOptions struct {
	// Direction selects the adjacency followed; DirectionBoth walks a
	// directed graph as undirected.
	Direction engine.DirectionE
	// MaxDepth stops the walk after that many levels; zero is unbounded.
	MaxDepth int32
	// DenseFraction overrides the engine's push/pull threshold.
	DenseFraction float64
}

// BFSResult holds hop distances from the source set.
type BFSResult struct {
	// Depth is the hop distance, -1 when unreached.
	Depth []int32
	// Parent is the lowest-slot frontier neighbour that reached the vertex;
	// a source is its own parent; -1 when unreached.
	Parent []int32
	// Reached counts vertices with a depth, sources included.
	Reached int
	// Levels is the number of edge-map sweeps that found new vertices.
	Levels int32
	// Pulls counts the sweeps that ran in pull form.
	Pulls int32
	Truncation
}

// BFS computes hop distances from sources. Parents are deterministic: the
// lowest-slot frontier neighbour wins, under push and pull alike.
func BFS(ctx context.Context, e *engine.Engine, g *csr.Graph, sources []int32, opts BFSOptions) (r BFSResult, err error) {
	e = engineOrDefault(e)
	n := g.NumVertices()
	for _, s := range sources {
		if s < 0 || int(s) >= n {
			err = eb.Build().Int32("source", s).Int("vertices", n).Errorf("source slot out of range")
			return
		}
	}
	r.Depth = make([]int32, n)
	r.Parent = make([]int32, n)
	fill(r.Depth, -1)
	fill(r.Parent, -1)
	frontier := engine.FromSlots(n, sources)
	for _, s := range frontier.Sparse() {
		r.Depth[s] = 0
		r.Parent[s] = s
	}
	r.Reached = frontier.Len()
	depth := r.Depth
	parent := r.Parent
	funcs := engine.EdgeFuncs{
		Update: func(s, d int32) bool {
			if parent[d] != -1 {
				return false
			}
			parent[d] = s
			depth[d] = depth[s] + 1
			return true
		},
		Cond: func(d int32) bool { return parent[d] == -1 },
	}
	emo := engine.EdgeMapOptions{Direction: opts.Direction, DenseFraction: opts.DenseFraction}
	for !frontier.IsEmpty() {
		if opts.MaxDepth > 0 && r.Levels >= opts.MaxDepth {
			break
		}
		if ctxDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			return
		}
		next, sweep := e.EdgeMap(g, frontier, funcs, emo)
		if sweep == engine.SweepPull {
			r.Pulls++
		}
		if next.IsEmpty() {
			break
		}
		r.Levels++
		r.Reached += next.Len()
		frontier = next
	}
	return
}
