package explain

import (
	"context"
	"math/rand/v2"
	"testing"
)

// BenchmarkFitTree fits the Projection lane's budget — ten thousand rows of
// sixteen features, a dozen labels, a tenth noise — to the lane's depth.
func BenchmarkFitTree(b *testing.B) {
	x, labels := benchInput(10000, 16, 12)
	b.ResetTimer()
	for b.Loop() {
		_, err := FitTree(context.Background(), x, 16, labels, TreeOptions{MaxDepth: 6, MinLeaf: 50})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContrasts(b *testing.B) {
	x, labels := benchInput(10000, 16, 12)
	b.ResetTimer()
	for b.Loop() {
		_, err := Contrasts(context.Background(), x, 16, labels)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchInput(n, d, k int) (x []float64, labels []int32) {
	rng := rand.New(rand.NewPCG(11, 12))
	x = make([]float64, n*d)
	labels = make([]int32, n)
	for r := range n {
		lb := rng.IntN(k)
		labels[r] = int32(lb)
		if rng.Float64() < 0.1 {
			labels[r] = -1
		}
		for f := range d {
			x[r*d+f] = rng.NormFloat64() + float64(lb%(f+1))
		}
	}
	return
}
