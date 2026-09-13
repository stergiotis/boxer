package knn

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/engine"
)

// The benchmark runs at the Projection lane's row cap and feature width
// (ADR-0230 §SD1): ten thousand rows of sixteen features, fifteen
// neighbours. The zero-worker engine uses every core; the *1 variant pins
// one worker for the COST comparison.

func benchInput(b *testing.B) ([]float32, []uint64) {
	b.Helper()
	const n, d = 10_000, 16
	return randomMatrix(n, d, 42), seqIDs(n)
}

func benchBuild(b *testing.B, e *engine.Engine) {
	x, ids := benchInput(b)
	b.ReportAllocs()
	for b.Loop() {
		_, err := Build(context.Background(), e, x, 16, ids, Options{K: 15})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild(b *testing.B)  { benchBuild(b, engine.New(0)) }
func BenchmarkBuild1(b *testing.B) { benchBuild(b, engine.New(1)) }
