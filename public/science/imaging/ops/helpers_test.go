package ops

import (
	"context"
	"image"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// kindImage is a w × h grey image of a kind with invalid samples: NaN and
// an infinity in a float plane, a declared invalid value in an integer one,
// and a value transform other than the identity for u16 and i32.
func kindImage(t testing.TB, kind imaging.SampleKindE, w, h int, seed uint64) imaging.Image {
	t.Helper()
	return imageOf(t, imaging.ColorModelGray, kind, w, h, seed)
}

func imageOf(t testing.TB, model imaging.ColorModelE, kind imaging.SampleKindE, w, h int, seed uint64) imaging.Image {
	t.Helper()
	planes := make([]imaging.Plane, model.PlaneCount())
	for i := range planes {
		planes[i] = kindPlane(t, kind, w, h, seed+uint64(i)*7919)
	}
	if model.HasAlpha() {
		// Alpha declares no invalid value: a plane of ones.
		u8, err := imaging.NewPlaneU8(w, h, slices.Repeat([]uint8{1}, w*h))
		require.NoError(t, err)
		require.Equal(t, imaging.SampleKindU8, kind, "a test image with alpha is of 8-bit samples")
		planes[len(planes)-1] = u8
	}
	img, err := imaging.NewImage(model, planes, imaging.Provenance{SourceName: "test " + kind.String()})
	require.NoError(t, err)
	return img
}

func kindPlane(t testing.TB, kind imaging.SampleKindE, w, h int, seed uint64) imaging.Plane {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	n := w * h
	invalid := func() bool { return rng.IntN(20) == 0 }
	var p imaging.Plane
	var err error
	switch kind {
	case imaging.SampleKindU8:
		s := make([]uint8, n)
		for i := range s {
			s[i] = uint8(rng.IntN(255))
			if invalid() {
				s[i] = 255
			}
		}
		p, err = imaging.NewPlaneU8(w, h, s)
		require.NoError(t, err)
		p, err = p.WithInvalid(255)
	case imaging.SampleKindU16:
		s := make([]uint16, n)
		for i := range s {
			s[i] = uint16(1 + rng.IntN(65535))
			if invalid() {
				s[i] = 0
			}
		}
		p, err = imaging.NewPlaneU16(w, h, s)
		require.NoError(t, err)
		if p, err = p.WithInvalid(0); err == nil {
			p, err = p.WithTransform(imaging.ValueTransform{Scale: 0.25, Offset: -100})
		}
	case imaging.SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(rng.IntN(65535) - 32767)
			if invalid() {
				s[i] = math.MinInt16
			}
		}
		p, err = imaging.NewPlaneI16(w, h, s)
		require.NoError(t, err)
		p, err = p.WithInvalid(math.MinInt16)
	case imaging.SampleKindI32:
		s := make([]int32, n)
		for i := range s {
			s[i] = int32(rng.Int64N(1<<32-1) - (1<<31 - 1))
			if invalid() {
				s[i] = math.MinInt32
			}
		}
		p, err = imaging.NewPlaneI32(w, h, s)
		require.NoError(t, err)
		if p, err = p.WithInvalid(math.MinInt32); err == nil {
			p, err = p.WithTransform(imaging.ValueTransform{Scale: 1e-3, Offset: 0})
		}
	case imaging.SampleKindF32:
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(rng.Float64()*2000 - 1000)
			if invalid() {
				s[i] = float32(math.NaN())
			}
		}
		s[rng.IntN(n)] = float32(math.Inf(1))
		p, err = imaging.NewPlaneF32(w, h, s)
	case imaging.SampleKindF64:
		s := make([]float64, n)
		for i := range s {
			s[i] = 1e6 + rng.Float64()*10
			if invalid() {
				s[i] = math.NaN()
			}
		}
		s[rng.IntN(n)] = math.Inf(-1)
		p, err = imaging.NewPlaneF64(w, h, s)
	}
	require.NoError(t, err)
	return p
}

// in is a named input of an image.
func in(name string, img imaging.Image) Input { return Input{Name: name, Source: FromImage(img)} }

func run(t testing.TB, op string, params Params, opts Options, inputs ...Input) Result {
	t.Helper()
	res, err := Run(context.Background(), Request{Operator: op, Params: params, Inputs: inputs}, opts)
	require.NoError(t, err)
	return res
}

// readAll reads a finished store whole.
func readAll(t testing.TB, s *tiles.Store) imaging.Image {
	t.Helper()
	img, err := s.Read(context.Background(), image.Rect(0, 0, s.Width(), s.Height()))
	require.NoError(t, err)
	return img
}

// directF is what a test computes for output plane p at (x, y): the value
// and whether it is valid, before rounding to the output kind.
type directF func(p, x, y int) (float64, bool)

// tolerance is how far a result may be from the direct evaluation, relative
// to 1 + |want|: 0 for exact.
type tolerance float64

// checkDirect compares every sample of a result with the direct evaluation,
// rounded to the result's kind; a value that is not finite there is
// invalid.
func checkDirect(t testing.TB, res Result, want directF, tol tolerance) {
	t.Helper()
	img := readAll(t, res.Store)
	f32 := img.Kind() == imaging.SampleKindF32
	for p := range img.PlaneCount() {
		plane := img.Plane(p)
		for y := range img.Height() {
			for x := range img.Width() {
				w, wok := want(p, x, y)
				if wok {
					if f32 {
						w = float64(float32(w))
					}
					wok = isFinite(w)
				}
				g, gok := plane.ValueAt(x, y)
				if gok != wok {
					t.Fatalf("plane %d (%d,%d): valid %v, the direct evaluation says %v (%v)", p, x, y, gok, wok, w)
				}
				if !gok {
					continue
				}
				if d := math.Abs(g - w); d > float64(tol)*(1+math.Abs(w)) {
					t.Fatalf("plane %d (%d,%d): %v, the direct evaluation says %v", p, x, y, g, w)
				}
			}
		}
	}
}

// valueAt is plane p's value at (x, y) as the operation sees it.
func valueAt(img imaging.Image, p, x, y int) (float64, bool) {
	return img.Plane(p).ValueAt(x, y)
}

// windowAt reads plane p at (x, y) under a border rule: NaN outside under
// invalid, the nearest edge sample under nearest; ok false where invalid.
func windowAt(img imaging.Image, p, x, y int, border BorderE) (float64, bool) {
	if !img.Plane(p).Contains(x, y) {
		if border == BorderInvalid {
			return 0, false
		}
		x = min(max(x, 0), img.Width()-1)
		y = min(max(y, 0), img.Height()-1)
	}
	return valueAt(img, p, x, y)
}

// nearEdge reports whether (x, y)'s neighbourhood of radius r reaches
// outside the image.
func nearEdge(img imaging.Image, x, y, r int) bool {
	return x < r || y < r || x >= img.Width()-r || y >= img.Height()-r
}
