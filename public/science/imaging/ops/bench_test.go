package ops

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// BenchmarkOperators4096 runs each operator over a 4096 × 4096 image of
// 16-bit samples — the whole run: reading, computing, the store and its
// levels — at the default band, with GOMAXPROCS workers and with one; the
// store alone is BenchmarkStore4096.
func BenchmarkOperators4096(b *testing.B) {
	const side = 4096
	a, c := benchImage(b, side, 1), benchImage(b, side, 2)
	for _, bc := range []struct {
		name string
		req  Request
	}{
		{"expr", Request{Operator: "expr", Params: Params{"expression": Text("(a - c) / (a + c)")}, Inputs: []Input{in("a", a), in("c", c)}}},
		{"axis-stat-median", Request{Operator: "axis-stat", Params: Params{"stat": Text("median")}, Inputs: []Input{in("a", a), in("c", c)}}},
		{"gaussian-sigma2", Request{Operator: "gaussian", Params: Params{"sigma": Number(2), "border": Text("nearest")}, Inputs: []Input{in("a", a)}}},
		{"median-filter-r1", Request{Operator: "median-filter", Params: Params{"radius": Integer(1), "border": Text("nearest")}, Inputs: []Input{in("a", a)}}},
		{"median-filter-r3", Request{Operator: "median-filter", Params: Params{"radius": Integer(3), "border": Text("nearest")}, Inputs: []Input{in("a", a)}}},
		{"standardize", Request{Operator: "standardize", Inputs: []Input{in("a", a)}}},
	} {
		for _, workers := range []int{0, 1} {
			name := bc.name
			if workers == 1 {
				name += "-oneworker"
			}
			b.Run(name, func(b *testing.B) {
				for b.Loop() {
					_, err := Run(context.Background(), bc.req, Options{Workers: workers})
					require.NoError(b, err)
				}
			})
		}
	}
}

// benchImage is a side × side image of 16-bit samples, all valid, so that
// no window is cut short by an invalid sample.
func benchImage(b *testing.B, side int, seed uint64) imaging.Image {
	rng := rand.New(rand.NewPCG(seed, 1))
	s := make([]uint16, side*side)
	for i := range s {
		s[i] = uint16(rng.IntN(1 << 16))
	}
	p, err := imaging.NewPlaneU16(side, side, s)
	require.NoError(b, err)
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	require.NoError(b, err)
	return img
}

// BenchmarkStore4096 is what building the store of a 4096 × 4096 image of
// 32-bit floats costs by itself: the part of every operation's run that is
// not the operator.
func BenchmarkStore4096(b *testing.B) {
	img := kindImage(b, imaging.SampleKindF32, 4096, 4096, 3)
	for b.Loop() {
		_, err := tiles.FromImage(context.Background(), img, tiles.Options{})
		require.NoError(b, err)
	}
}
