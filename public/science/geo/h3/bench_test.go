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

// benchSetupH acquires a Handle against the compiler-backed wazero runtime
// (or the explicitly passed config). Benchmarks that don't care about the
// interpreter path pass nil to get the compiler default.
func benchSetupH(b *testing.B, cfg wazero.RuntimeConfig) (h *Handle) {
	b.Helper()
	if cfg == nil {
		cfg = wazero.NewRuntimeConfigCompiler()
	}
	rt, err := NewRuntime(context.Background(), RuntimeConfig{PoolSize: 1, WazeroCfg: cfg})
	if err != nil {
		b.Skipf("h3 wasm bridge not built: %v", err)
		return
	}
	b.Cleanup(func() { _ = rt.Close() })
	h, err = rt.AcquireE(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(h.Release)
	return
}

// seedCells builds a random corpus of cells at the given resolution. One-time
// cost amortised by b.ResetTimer in callers.
func seedCells(b *testing.B, h *Handle, n int, res ResolutionE) (cells []uint64) {
	b.Helper()
	lats, lngs := makeLatLngCorpus(n)
	var err error
	cells, _, err = h.LatLngsToCellsE(context.Background(), res, lats, lngs, nil, nil)
	if err != nil {
		b.Fatal(err)
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
					h := benchSetupH(b, cc.cfg)
					lats, lngs := makeLatLngCorpus(n)
					cellsDst := make([]uint64, 0, n)
					statusDst := make([]StatusE, 0, n)
					b.SetBytes(int64(n * 16))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var err error
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
// matches the batched path at 10k+, the bulk path has regressed.
func BenchmarkLatLngsToCells_PerElement(b *testing.B) {
	h := benchSetupH(b, nil)
	const n = 1000
	lats, lngs := makeLatLngCorpus(n)
	cellsDst := make([]uint64, 0, 1)
	statusDst := make([]StatusE, 0, 1)
	b.SetBytes(int64(n * 16))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			var err error
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

func BenchmarkCellsToLatLngs(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(sizeLabel(n), func(b *testing.B) {
			h := benchSetupH(b, nil)
			cells := seedCells(b, h, n, ResolutionR9)
			latsDst := make([]float64, 0, n)
			lngsDst := make([]float64, 0, n)
			statusDst := make([]StatusE, 0, n)
			b.SetBytes(int64(n * 8))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				latsDst, lngsDst, statusDst, err = h.CellsToLatLngsE(
					context.Background(), cells, latsDst, lngsDst, statusDst,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCellsToChildren(b *testing.B) {
	// Seed parents at res 5; benchmark children at res 6 (7x output).
	for _, n := range benchSizes {
		b.Run(sizeLabel(n), func(b *testing.B) {
			h := benchSetupH(b, nil)
			cells := seedCells(b, h, n, ResolutionR5)
			childrenDst := make([]uint64, 0, n*7)
			offsetsDst := make([]int32, 0, n+1)
			statusDst := make([]StatusE, 0, n)
			b.SetBytes(int64(n * 8))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				childrenDst, offsetsDst, statusDst, err = h.CellsToChildrenE(
					context.Background(), ResolutionR6, cells,
					childrenDst, offsetsDst, statusDst,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkGridDisks(b *testing.B) {
	for _, k := range []uint8{1, 3} {
		b.Run("k"+string(rune('0'+k)), func(b *testing.B) {
			for _, n := range benchSizes {
				b.Run(sizeLabel(n), func(b *testing.B) {
					h := benchSetupH(b, nil)
					cells := seedCells(b, h, n, ResolutionR5)
					ringSize := 3*int(k)*(int(k)+1) + 1
					outDst := make([]uint64, 0, n*ringSize)
					offsetsDst := make([]int32, 0, n+1)
					statusDst := make([]StatusE, 0, n)
					b.SetBytes(int64(n * 8))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var err error
						outDst, offsetsDst, statusDst, err = h.GridDisksE(
							context.Background(), k, cells,
							outDst, offsetsDst, statusDst,
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

func BenchmarkCellsToStrings(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(sizeLabel(n), func(b *testing.B) {
			h := benchSetupH(b, nil)
			cells := seedCells(b, h, n, ResolutionR9)
			bufDst := make([]byte, 0, n*16)
			offsetsDst := make([]int32, 0, n+1)
			statusDst := make([]StatusE, 0, n)
			b.SetBytes(int64(n * 8))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				bufDst, offsetsDst, statusDst, err = h.CellsToStringsE(
					context.Background(), cells, bufDst, offsetsDst, statusDst,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPolygonToCells(b *testing.B) {
	// Fixed unit-square polygon, swept over resolutions so the output-cell
	// count is the throughput knob rather than batch size.
	vertsLat := []float64{0, 0, 1, 1, 0}
	vertsLng := []float64{0, 1, 1, 0, 0}
	ringOffsets := []int32{0, 5}
	for _, res := range []ResolutionE{ResolutionR5, ResolutionR7, ResolutionR9} {
		b.Run("res"+string(rune('0'+res)), func(b *testing.B) {
			h := benchSetupH(b, nil)
			cellsDst := make([]uint64, 0, 1024)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				cellsDst, err = h.PolygonToCellsE(
					context.Background(), res, ContainmentCovers,
					vertsLat, vertsLng, ringOffsets, cellsDst,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(cellsDst)), "cells/op")
		})
	}
}

func BenchmarkCompactCells(b *testing.B) {
	// Input = all res-5 children of a res-2 anchor. Cell counts are fixed
	// by the H3 grid (~2000 children per res-2), so we can't easily slide
	// over sizes — one size, benchmark for regressions.
	h := benchSetupH(b, nil)
	lats, lngs := []float64{37.7749}, []float64{-122.4194}
	anchor, _, err := h.LatLngsToCellsE(context.Background(), ResolutionR2, lats, lngs, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	children, offsets, _, err := h.CellsToChildrenE(
		context.Background(), ResolutionR5, anchor, nil, nil, nil,
	)
	if err != nil {
		b.Fatal(err)
	}
	input := children[offsets[0]:offsets[1]]
	compactedDst := make([]uint64, 0, len(input))
	b.SetBytes(int64(len(input) * 8))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compactedDst, err = h.CompactCellsE(context.Background(), input, compactedDst)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(len(compactedDst)), "compact-cells/op")
}

func BenchmarkUncompactCells(b *testing.B) {
	// Input = a compacted set; uncompact to res 5 (one level down). Size
	// ladder matches the other CSR benches.
	for _, n := range benchSizes {
		b.Run(sizeLabel(n), func(b *testing.B) {
			h := benchSetupH(b, nil)
			cells := seedCells(b, h, n, ResolutionR4)
			expandedDst := make([]uint64, 0, n*7)
			statusDst := make([]StatusE, 0, n)
			b.SetBytes(int64(n * 8))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				expandedDst, statusDst, err = h.UncompactCellsE(
					context.Background(), ResolutionR5, cells, expandedDst, statusDst,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
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
