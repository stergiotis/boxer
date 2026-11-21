package tiles

import (
	"context"
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// kindImage is a w × h image of any of the six kinds whose samples follow a
// fixed pattern over the kind's own range — negative for the signed kinds,
// NaN sprinkled into the float ones — and, when declare is set, every
// thirteenth sample of each colour plane the declared invalid value, a
// whole block of them in the top left corner.
func kindImage(t *testing.T, w, h int, model imaging.ColorModelE, kind imaging.SampleKindE, declare bool) (img imaging.Image, inv float64) {
	t.Helper()
	switch kind {
	case imaging.SampleKindU8:
		inv = 7
	case imaging.SampleKindU16:
		inv = 0
	case imaging.SampleKindI16:
		inv = -32768
	case imaging.SampleKindI32:
		inv = -9999
	case imaging.SampleKindF32, imaging.SampleKindF64:
		inv = math.Ldexp(-1, 100)
	}
	planes := make([]imaging.Plane, model.PlaneCount())
	for i := range planes {
		colour := !(model.HasAlpha() && i == len(planes)-1)
		n := w * h
		v := make([]float64, n)
		for j := range v {
			x := float64((j*7919 + i*101 + j/w*13) % 65536)
			switch kind {
			case imaging.SampleKindU8:
				x = float64(int(x) % 256)
			case imaging.SampleKindI16:
				x -= 32768
			case imaging.SampleKindI32:
				x = x*32771 - 1<<31
			case imaging.SampleKindF32, imaging.SampleKindF64:
				x = x/3 - 9000
				if j%97 == 0 {
					x = math.NaN()
				}
			}
			if declare && colour && (j%13 == 0 || (j%w < 40 && j/w < 40)) {
				x = inv
			}
			v[j] = x
		}
		var err error
		switch kind {
		case imaging.SampleKindU8:
			s := make([]uint8, n)
			for j := range s {
				s[j] = uint8(v[j])
			}
			planes[i], err = imaging.NewPlaneU8(w, h, s)
		case imaging.SampleKindU16:
			s := make([]uint16, n)
			for j := range s {
				s[j] = uint16(v[j])
			}
			planes[i], err = imaging.NewPlaneU16(w, h, s)
		case imaging.SampleKindI16:
			s := make([]int16, n)
			for j := range s {
				s[j] = int16(v[j])
			}
			planes[i], err = imaging.NewPlaneI16(w, h, s)
		case imaging.SampleKindI32:
			s := make([]int32, n)
			for j := range s {
				s[j] = int32(v[j])
			}
			planes[i], err = imaging.NewPlaneI32(w, h, s)
		case imaging.SampleKindF32:
			s := make([]float32, n)
			for j := range s {
				s[j] = float32(v[j])
			}
			planes[i], err = imaging.NewPlaneF32(w, h, s)
		case imaging.SampleKindF64:
			planes[i], err = imaging.NewPlaneF64(w, h, v)
		}
		require.NoError(t, err)
		if declare && colour {
			planes[i], err = planes[i].WithInvalid(inv)
			require.NoError(t, err)
		}
	}
	img, err := imaging.NewImage(model, planes, imaging.Provenance{SourceName: "test", Notes: []string{"a note"}})
	require.NoError(t, err)
	return img, inv
}

// samePlaneDeclared is samePlane, and the two planes declare the same
// invalid value.
func samePlaneDeclared(t *testing.T, want, got imaging.Plane, what string) {
	t.Helper()
	wi, wok := want.Invalid()
	gi, gok := got.Invalid()
	require.Equal(t, wok, gok, "%s: declares", what)
	require.Equal(t, wi, gi, "%s: the declared value", what)
	require.Equal(t, want.Kind(), got.Kind(), what)
	require.Equal(t, want.Width(), got.Width(), what)
	require.Equal(t, want.Height(), got.Height(), what)
	for y := range want.Height() {
		for x := range want.Width() {
			a, okA := want.StoredAt(x, y)
			b, okB := got.StoredAt(x, y)
			if okA != okB || (okA && a != b) {
				t.Fatalf("%s at (%d,%d): %v %v, want %v %v", what, x, y, b, okB, a, okA)
			}
		}
	}
}

// For every kind, with and without a declared invalid value: every tile of
// every level of a store built band by band is that region of the whole
// image's build, declaring what the image declares, and a histogram and a
// read of the store are the image's.
func TestEveryKindIsStoredAsTheWholeImage(t *testing.T) {
	ctx := context.Background()
	for _, kind := range imaging.AllSampleKinds {
		for _, declare := range []bool{false, true} {
			model := imaging.ColorModelGrayAlpha
			if declare {
				model = imaging.ColorModelRGB
			}
			img, inv := kindImage(t, 1030, 530, model, kind, declare)
			s, err := FromImage(ctx, img, Options{})
			require.NoError(t, err)
			gotInv, gotDecl := s.Invalid()
			require.Equal(t, declare, gotDecl, kind.String())
			if declare {
				require.Equal(t, inv, gotInv)
			}
			assert.Equal(t, img.Hash(), s.Provenance().SampleHash, "%s: the digest of the samples", kind)
			assert.Equal(t, []string{"a note"}, s.Provenance().Notes)
			assert.Equal(t, int64(0), s.Bytes()%int64(kind.Bytes()), "bytes are counted in the kind's size")

			whole, err := levels.Build(ctx, img, levels.Options{})
			require.NoError(t, err)
			for k := 0; k <= s.Reduced(); k++ {
				lw, lh := s.LevelSize(k)
				for row := 0; row*Size < lh; row++ {
					for col := 0; col*Size < lw; col++ {
						tile, err := s.Tile(ctx, k, col, row)
						require.NoError(t, err)
						rect := image.Rect(col*Size, row*Size, min((col+1)*Size, lw), min((row+1)*Size, lh))
						for i := range img.PlaneCount() {
							what := kind.String() + " plane " + string(rune('0'+i)) + " level " + string(rune('0'+k))
							if k == 0 {
								samePlaneDeclared(t, region(t, img.Plane(i), rect), tile.Mean.Plane(i), what)
								continue
							}
							lv, _ := whole.Level(k)
							samePlaneDeclared(t, region(t, lv.Mean.Plane(i), rect), tile.Mean.Plane(i), "means of "+what)
							samePlaneDeclared(t, region(t, lv.Min[i], rect), tile.Min[i], "minima of "+what)
							samePlaneDeclared(t, region(t, lv.Max[i], rect), tile.Max[i], "maxima of "+what)
						}
					}
				}
			}

			for _, rect := range []image.Rectangle{image.Rect(0, 0, 1030, 530), image.Rect(10, 10, 60, 30), image.Rect(500, 400, 530, 530)} {
				for i := range img.PlaneCount() {
					want, err := stats.OfRegion(img.Plane(i), rect)
					require.NoError(t, err)
					got, err := s.Histogram(ctx, i, rect)
					require.NoError(t, err)
					assert.Equal(t, want.Valid(), got.Valid(), "%s %v", kind, rect)
					assert.Equal(t, want.Invalid(), got.Invalid(), "%s %v", kind, rect)
					assert.Equal(t, want.IsExact(), got.IsExact())
					for _, p := range []float64{0, 1, 50, 99, 100} {
						a, _ := want.PercentileStored(p)
						b, _ := got.PercentileStored(p)
						assert.Equal(t, a, b, "%s %v p=%v", kind, rect, p)
					}
				}
				r, err := s.Read(ctx, rect)
				require.NoError(t, err)
				for i := range img.PlaneCount() {
					samePlaneDeclared(t, region(t, img.Plane(i), rect), r.Plane(i), "read")
				}
			}
			for i := range img.PlaneCount() {
				lo, hi, ok := s.StoredRange(i)
				wl, wh, wok := img.Plane(i).StoredRange()
				assert.Equal(t, wok, ok)
				assert.Equal(t, [2]float64{wl, wh}, [2]float64{lo, hi}, "%s plane %d: the range leaves invalid samples out", kind, i)
			}
		}
	}
}

// The store says what the image it was built from says beside its samples:
// the labels of its planes and where they are; a region read moves the
// placement to its own origin.
func TestAStoreCarriesLabelsAndAPlacement(t *testing.T) {
	ctx := context.Background()
	img, _ := kindImage(t, 30, 20, imaging.ColorModelRGB, imaging.SampleKindU16, false)
	img, err := img.WithPlaneLabels([]string{"B8", "B4", "B3"})
	require.NoError(t, err)
	pl := imaging.Placement{Affine: [6]float64{2_600_000, 10, 0, 1_200_000, 0, -10},
		Frame: imaging.Frame{Kind: imaging.FrameKindReferenceSystem, Code: "EPSG:2056"}, Unapplied: []string{"a vertical datum"}}
	img = img.WithPlacement(pl)
	s, err := FromImage(ctx, img, Options{})
	require.NoError(t, err)
	assert.Equal(t, []string{"B8", "B4", "B3"}, s.PlaneLabels())
	assert.Equal(t, pl, s.Placement())
	got := s.Placement()
	got.Unapplied[0] = "changed"
	assert.Equal(t, "a vertical datum", s.Placement().Unapplied[0], "the placement handed out is a copy")

	r, err := s.Read(ctx, image.Rect(5, 3, 9, 7))
	require.NoError(t, err)
	assert.Equal(t, []string{"B8", "B4", "B3"}, r.PlaneLabels())
	x, y, ok := r.Placement().World(0, 0)
	require.True(t, ok)
	wx, wy, _ := pl.World(5, 3)
	assert.Equal(t, [2]float64{wx, wy}, [2]float64{x, y}, "pixel (0, 0) of the read is pixel (5, 3) of the image")

	plain, err := FromImage(ctx, imaging.Image{}, Options{})
	assert.Error(t, err)
	assert.Nil(t, plain)
	gray, _ := kindImage(t, 4, 4, imaging.ColorModelGray, imaging.SampleKindI16, false)
	g, err := FromImage(ctx, gray, Options{})
	require.NoError(t, err)
	assert.Equal(t, []string{"gray"}, g.PlaneLabels())
	assert.True(t, g.Placement().IsZero())
}

func TestABuilderRefusesADescriptionThatDoesNotFit(t *testing.T) {
	_, err := NewBuilder(4, 4, imaging.ColorModelGray, imaging.SampleKindE(99), nil, imaging.Provenance{}, Options{})
	assert.Error(t, err, "a kind the store does not know is refused by name")
	_, err = NewBuilder(4, 4, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{Invalid: 300, HasInvalid: true})
	assert.Error(t, err, "a value the kind cannot hold")
	_, err = NewBuilder(4, 4, imaging.ColorModelRGB, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{PlaneLabels: []string{"a"}})
	assert.Error(t, err, "one label per plane")
	_, err = NewBuilder(4, 4, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{},
		Options{Placement: imaging.Placement{Frame: imaging.Frame{Kind: imaging.FrameKindPhysical}}})
	assert.Error(t, err, "an affine that is not invertible")

	b, err := NewBuilder(4, 2, imaging.ColorModelGrayAlpha, imaging.SampleKindI16, nil, imaging.Provenance{}, Options{Invalid: -1, HasInvalid: true})
	require.NoError(t, err)
	g, _ := imaging.NewPlaneI16(4, 1, []int16{-1, 2, 3, 4})
	a, _ := imaging.NewPlaneI16(4, 1, []int16{-1, -1, 9, 9})
	other, _ := g.WithInvalid(2)
	assert.Error(t, b.AddPlanes([]imaging.Plane{other, a}), "rows that declare another value")
	declaredAlpha, _ := a.WithInvalid(-1)
	assert.Error(t, b.AddPlanes([]imaging.Plane{g, declaredAlpha}), "alpha declares none")
	require.NoError(t, b.AddPlanes([]imaging.Plane{g, a}), "rows that declare nothing take the store's value")
	require.NoError(t, b.AddPlanes([]imaging.Plane{g, a}))
	require.NoError(t, b.Finish())
	r, err := b.Store().Read(context.Background(), image.Rect(0, 0, 4, 2))
	require.NoError(t, err)
	_, ok := r.Plane(0).StoredAt(0, 0)
	assert.False(t, ok, "the gray plane declares −1")
	v, ok := r.Plane(1).StoredAt(0, 0)
	assert.True(t, ok, "alpha does not")
	assert.Equal(t, -1.0, v)
	lo, _, _ := b.Store().StoredRange(0)
	assert.Equal(t, 2.0, lo)
}
