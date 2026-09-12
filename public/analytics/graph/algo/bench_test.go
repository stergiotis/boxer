package algo

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
)

// Benchmarks run on an R-MAT graph of 2^17 vertices and 2^20 edges, the
// upper end of the sizes ADR-0229 targets. The engine at zero workers uses
// every core; the *1 variants pin one worker for the COST comparison.

const benchScale, benchEPV = 17, 8

func benchGraph(b *testing.B) *csr.Graph {
	b.Helper()
	return rmatGraph(b, benchScale, benchEPV, false)
}

func BenchmarkBuild(b *testing.B) {
	src, dst := rmat(benchScale, benchEPV, 42)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = csr.BuildE(src, dst, nil, csr.Options{})
	}
}

func benchWorkers(b *testing.B, name string, f func(e *engine.Engine)) {
	for _, w := range []int{1, 0} {
		label := name + "/workers=all"
		if w == 1 {
			label = name + "/workers=1"
		}
		b.Run(label, func(b *testing.B) {
			e := engine.New(w)
			b.ReportAllocs()
			for b.Loop() {
				f(e)
			}
		})
	}
}

func BenchmarkAlgorithms(b *testing.B) {
	g := benchGraph(b)
	ctx := context.Background()
	benchWorkers(b, "BFS", func(e *engine.Engine) { _, _ = BFS(ctx, e, g, []int32{0}, BFSOptions{}) })
	benchWorkers(b, "PageRank20", func(e *engine.Engine) { _ = PageRank(ctx, e, g, PageRankOptions{Iterations: 20}) })
	benchWorkers(b, "Triangles", func(e *engine.Engine) { _ = Triangles(ctx, e, g, TriangleOptions{}) })
	benchWorkers(b, "Betweenness64pivots", func(e *engine.Engine) {
		_, _ = Betweenness(ctx, e, g, BetweennessOptions{ExactMaxVertices: 1, Pivots: 64, Seed: 1})
	})
	b.Run("ConnectedComponents", func(b *testing.B) {
		for b.Loop() {
			_ = ConnectedComponents(ctx, g)
		}
	})
	b.Run("SCC", func(b *testing.B) {
		for b.Loop() {
			_ = StronglyConnectedComponents(ctx, g)
		}
	})
	b.Run("KCore", func(b *testing.B) {
		for b.Loop() {
			_ = KCore(ctx, g)
		}
	})
	b.Run("MaximalCliques", func(b *testing.B) {
		for b.Loop() {
			_ = MaximalCliques(ctx, g, CliqueOptions{MaxCliques: 200_000})
		}
	})
}
