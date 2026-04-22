//go:build llm_generated_opus47

package h3

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/tetratelabs/wazero"
)

var benchSizes = []int{1000, 10_000, 100_000}

func makeLatLngCorpus(n int) (lats, lngs []float64) {
	lats = make([]float64, n)
	lngs = make([]float64, n)
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < n; i++ {
		lats[i] = r.Float64()*180.0 - 90.0
		lngs[i] = r.Float64()*360.0 - 180.0
	}
	return
}

func BenchmarkLatLngsToCells(b *testing.B) {
	configs := []struct {
		name string
		cfg  wazero.RuntimeConfig
	}{
		{"compiler", wazero.NewRuntimeConfigCompiler()},
		{"interpreter", wazero.NewRuntimeConfigInterpreter()},
	}
	for _, cc := range configs {
		b.Run(cc.name, func(b *testing.B) {
			for _, n := range benchSizes {
				b.Run(sizeLabel(n), func(b *testing.B) {
					rt, err := NewRuntime(context.Background(), RuntimeConfig{
						PoolSize:  1,
						WazeroCfg: cc.cfg,
					})
					if err != nil {
						b.Skipf("h3 wasm bridge not built: %v", err)
						return
					}
					defer func() { _ = rt.Close() }()
					h, err := rt.AcquireE(context.Background())
					if err != nil {
						b.Fatal(err)
					}
					defer h.Release()

					lats, lngs := makeLatLngCorpus(n)
					cellsDst := make([]uint64, 0, n)
					statusDst := make([]StatusE, 0, n)
					b.SetBytes(int64(n * 16))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						cellsDst, statusDst, err = h.LatLngsToCellsE(
							context.Background(), ResolutionR9,
							lats, lngs, cellsDst, statusDst,
						)
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// BenchmarkLatLngsToCells_PerElement is a deliberate anti-pattern: one
// bulk call per element. Serves as a throughput floor — if this ever
// matches the batched path at 10k+, regress in the bulk path is
// suspect.
func BenchmarkLatLngsToCells_PerElement(b *testing.B) {
	rt, err := NewRuntime(context.Background(), RuntimeConfig{PoolSize: 1})
	if err != nil {
		b.Skipf("h3 wasm bridge not built: %v", err)
		return
	}
	defer func() { _ = rt.Close() }()
	h, err := rt.AcquireE(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer h.Release()

	const n = 1000
	lats, lngs := makeLatLngCorpus(n)
	cellsDst := make([]uint64, 0, 1)
	statusDst := make([]StatusE, 0, 1)
	b.SetBytes(int64(n * 16))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			cellsDst, statusDst, err = h.LatLngsToCellsE(
				context.Background(), ResolutionR9,
				lats[j:j+1], lngs[j:j+1], cellsDst, statusDst,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func sizeLabel(n int) string {
	switch {
	case n >= 1_000_000:
		return "1M"
	case n >= 100_000:
		return "100k"
	case n >= 10_000:
		return "10k"
	case n >= 1_000:
		return "1k"
	default:
		return "small"
	}
}
