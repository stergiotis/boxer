package tiles

import (
	"context"
	"errors"
	"image"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// testImage is a w × h image of the model and kind whose samples follow a
// fixed pattern, with NaN sprinkled into a float one.
func testImage(t *testing.T, w, h int, model imaging.ColorModelE, kind imaging.SampleKindE) imaging.Image {
	t.Helper()
	planes := make([]imaging.Plane, model.PlaneCount())
	for i := range planes {
		n := w * h
		var err error
		switch kind {
		case imaging.SampleKindU8:
			s := make([]uint8, n)
			for j := range s {
				s[j] = uint8((j*31 + i*77 + j/w*13) % 256)
			}
			planes[i], err = imaging.NewPlaneU8(w, h, s)
		case imaging.SampleKindU16:
			s := make([]uint16, n)
			for j := range s {
				s[j] = uint16((j*7919 + i*101) % 65536)
			}
			planes[i], err = imaging.NewPlaneU16(w, h, s)
		default:
			s := make([]float32, n)
			for j := range s {
				s[j] = float32((j*37+i)%5000)/3 - 200
				if j%97 == 0 {
					s[j] = float32(math.NaN())
				}
			}
			planes[i], err = imaging.NewPlaneF32(w, h, s)
		}
		require.NoError(t, err)
	}
	img, err := imaging.NewImage(model, planes, imaging.Provenance{SourceName: "test"})
	require.NoError(t, err)
	return img
}

// samePlane compares two planes sample for sample, NaN equal to NaN.
func samePlane(t *testing.T, want, got imaging.Plane, what string) {
	t.Helper()
	require.Equal(t, want.Width(), got.Width(), what)
	require.Equal(t, want.Height(), got.Height(), what)
	for y := range want.Height() {
		for x := range want.Width() {
			a, okA := want.StoredAt(x, y)
			b, okB := got.StoredAt(x, y)
			require.Equal(t, okA, okB, "%s at (%d,%d)", what, x, y)
			if okA {
				require.Equal(t, a, b, "%s at (%d,%d)", what, x, y)
			}
		}
	}
}

// region is rect of p as a plane of its own.
func region(t *testing.T, p imaging.Plane, rect image.Rectangle) imaging.Plane {
	t.Helper()
	out, err := cut(p, rect.Min.X, rect.Max.X)
	require.NoError(t, err)
	rows, err := rowsOf(out, rect.Min.Y, rect.Max.Y)
	require.NoError(t, err)
	return rows
}

// Every tile of every level of a store built band by band is the region of
// that level of the whole image's build: the samples at level 0, and the
// means, minima and maxima above it — across tile and band edges, for every
// sample kind, rows added in any chunks.
func TestAStoreIsTheWholeImageCutIntoTiles(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		model imaging.ColorModelE
		kind  imaging.SampleKindE
		chunk int
	}{
		{1100, 1300, imaging.ColorModelGray, imaging.SampleKindU8, 512},
		{600, 2100, imaging.ColorModelGrayAlpha, imaging.SampleKindU16, 7},
		{1, 1100, imaging.ColorModelGray, imaging.SampleKindF32, 513},
		{1030, 600, imaging.ColorModelRGBA, imaging.SampleKindU8, 1},
		{40, 30, imaging.ColorModelRGB, imaging.SampleKindF32, 30},
	} {
		img := testImage(t, tc.w, tc.h, tc.model, tc.kind)
		b, err := NewBuilder(tc.w, tc.h, tc.model, tc.kind, nil, img.Provenance(), Options{})
		require.NoError(t, err)
		for y := 0; y < tc.h; y += tc.chunk {
			y1 := min(y+tc.chunk, tc.h)
			planes := make([]imaging.Plane, img.PlaneCount())
			for i := range planes {
				planes[i], err = rowsOf(img.Plane(i), y, y1)
				require.NoError(t, err)
			}
			band, err := imaging.NewImage(tc.model, planes, imaging.Provenance{})
			require.NoError(t, err)
			require.NoError(t, b.Add(band))
		}
		require.NoError(t, b.Finish())
		s := b.Store()
		assert.Equal(t, img.Hash(), s.Provenance().SampleHash, "%dx%d: the digest of the samples", tc.w, tc.h)
		assert.Equal(t, levels.CountFor(tc.w, tc.h, levels.Options{}), s.Reduced())

		whole, err := levels.Build(context.Background(), img, levels.Options{})
		require.NoError(t, err)
		ctx := context.Background()
		for k := 0; k <= s.Reduced(); k++ {
			lw, lh := s.LevelSize(k)
			for row := 0; row*Size < lh; row++ {
				for col := 0; col*Size < lw; col++ {
					tile, err := s.Tile(ctx, k, col, row)
					require.NoError(t, err)
					rect := image.Rect(col*Size, row*Size, min((col+1)*Size, lw), min((row+1)*Size, lh))
					for i := range img.PlaneCount() {
						what := func(part string) string {
							return part + " of a tile of level " + string(rune('0'+k))
						}
						if k == 0 {
							samePlane(t, region(t, img.Plane(i), rect), tile.Mean.Plane(i), what("samples"))
							continue
						}
						lv, _ := whole.Level(k)
						samePlane(t, region(t, lv.Mean.Plane(i), rect), tile.Mean.Plane(i), what("means"))
						samePlane(t, region(t, lv.Min[i], rect), tile.Min[i], what("minima"))
						samePlane(t, region(t, lv.Max[i], rect), tile.Max[i], what("maxima"))
					}
				}
			}
		}
	}
}

// The numbers a store gives are those of the image: a histogram over any
// region, and the samples of a region read back.
func TestAStoresHistogramAndReadAreTheImages(t *testing.T) {
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindF32} {
		img := testImage(t, 1300, 700, imaging.ColorModelGrayAlpha, kind)
		s, err := FromImage(context.Background(), img, Options{})
		require.NoError(t, err)
		for _, rect := range []image.Rectangle{image.Rect(0, 0, 1300, 700), image.Rect(500, 400, 530, 700), image.Rect(3, 3, 4, 4)} {
			want, err := stats.OfRegion(img.Plane(1), rect)
			require.NoError(t, err)
			got, err := s.Histogram(context.Background(), 1, rect)
			require.NoError(t, err)
			assert.Equal(t, want.Valid(), got.Valid(), "%s %v", kind, rect)
			assert.Equal(t, want.Invalid(), got.Invalid())
			wm, _ := want.Mean()
			gm, _ := got.Mean()
			assert.InDelta(t, wm, gm, 1e-6*(1+math.Abs(wm)))
			for _, p := range []float64{0, 1, 50, 99, 100} {
				a, _ := want.PercentileStored(p)
				b, _ := got.PercentileStored(p)
				assert.Equal(t, a, b, "%s %v p=%v", kind, rect, p)
			}
			r, err := s.Read(context.Background(), rect)
			require.NoError(t, err)
			for i := range img.PlaneCount() {
				samePlane(t, region(t, img.Plane(i), rect), r.Plane(i), "read")
			}
		}
		lo, hi, ok := s.StoredRange(0)
		wl, wh, _ := img.Plane(0).StoredRange()
		assert.True(t, ok)
		assert.Equal(t, [2]float64{wl, wh}, [2]float64{lo, hi})
		assert.Positive(t, s.Bytes())
		_, err = s.Read(context.Background(), image.Rect(0, 0, 1301, 1))
		assert.Error(t, err, "outside the image")
	}
}

// A reader asking for a tile not yet built waits for it, a cancelled wait
// returns, and a failed build reaches every reader.
func TestReadersWaitForTilesAndHearOfAFailure(t *testing.T) {
	img := testImage(t, 600, 1100, imaging.ColorModelGray, imaging.SampleKindU8)
	b, err := NewBuilder(600, 1100, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{})
	require.NoError(t, err)
	s := b.Store()
	_, ok := s.TryTile(0, 0, 0)
	assert.False(t, ok)

	got := make(chan error, 1)
	go func() {
		_, err := s.Tile(context.Background(), 0, 1, 1)
		got <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = s.Tile(ctx, 0, 0, 0)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "nothing built yet")

	first, err := rowsOf(img.Plane(0), 0, 512)
	require.NoError(t, err)
	band, _ := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{first}, imaging.Provenance{})
	require.NoError(t, b.Add(band))
	_, ok = s.TryTile(0, 1, 0)
	assert.True(t, ok, "the first band's tiles are out")
	_, ok = s.TryTile(0, 1, 1)
	assert.False(t, ok)

	boom := errors.New("the file ended")
	b.Fail(boom)
	select {
	case err := <-got:
		assert.ErrorIs(t, err, boom)
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting reader was not woken")
	}
	assert.ErrorIs(t, s.Wait(context.Background()), boom)
	assert.False(t, s.Finished())
}

func TestABuilderRefusesWhatDoesNotFit(t *testing.T) {
	_, err := NewBuilder(0, 5, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{})
	assert.Error(t, err)
	b, err := NewBuilder(4, 2, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{})
	require.NoError(t, err)
	wrong := testImage(t, 5, 1, imaging.ColorModelGray, imaging.SampleKindU8)
	assert.Error(t, b.Add(wrong), "another width")
	tall := testImage(t, 4, 3, imaging.ColorModelGray, imaging.SampleKindU8)
	assert.Error(t, b.Add(tall), "more rows than the image has")
	one := testImage(t, 4, 1, imaging.ColorModelGray, imaging.SampleKindU8)
	require.NoError(t, b.Add(one))
	assert.Error(t, b.Finish(), "a row short")
	assert.Error(t, b.Store().Err())
}

func TestABuilderRefusesASizeItCannotHold(t *testing.T) {
	_, err := NewBuilder(1<<31, 4, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{})
	assert.Error(t, err)
	_, err = NewBuilder(1<<20, 1<<20, imaging.ColorModelGray, imaging.SampleKindU8, nil, imaging.Provenance{}, Options{})
	assert.Error(t, err)
}
