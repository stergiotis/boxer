package imageview

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The same samples show the same, as an image in memory or as a store:
// every tile of every level, pixel for pixel, under the same recipe; and
// the store offers what the image does — its data range as the default
// limits, a histogram, percentile limits, the readout.
func TestAStoreShowsWhatTheImageShows(t *testing.T) {
	img := grayImage(t, 2300, 1100)
	store, err := tiles.FromImage(context.Background(), img, tiles.Options{})
	require.NoError(t, err)

	mem := New(c.NewWidgetIdStack(), "", Options{})
	defer mem.Close()
	mem.SetImage(img)
	waitForLevels(t, mem)
	st := New(c.NewWidgetIdStack(), "", Options{})
	defer st.Close()
	require.NoError(t, st.SetSource(StoreSource(store), false))

	assert.Equal(t, mem.Recipe(), st.Recipe(), "the store opens on its data range, as the image does")
	assert.Equal(t, mem.SourceInfo().Reduced, st.SourceInfo().Reduced)
	for k := 0; k <= 3; k++ {
		lw, lh := levelSizeOf(img, k)
		for y := 0; y*tileSize < lh; y++ {
			for x := 0; x*tileSize < lw; x++ {
				require.Equal(t, sourceTile(t, mem, -k, x, y), sourceTile(t, st, -k, x, y), "level %d tile (%d,%d)", k, x, y)
			}
		}
	}

	waitForHistogram(t, st)
	h, ok := st.Histogram()
	require.True(t, ok, "a store counts its own histogram")
	assert.EqualValues(t, 2300*1100, h.Valid())
	require.NoError(t, st.SetRecipe(percentile(st, 1, 99)))
	waitForHistogram(t, mem)
	require.NoError(t, mem.SetRecipe(percentile(mem, 1, 99)))
	assert.Equal(t, mem.Recipe().Limits, st.Recipe().Limits)

	r, ok := st.readoutAt(portolan.LL(-10.5, 20.5))
	require.True(t, ok)
	want, _ := img.Plane(0).StoredAt(20, 10)
	assert.Equal(t, want, r.Stored[0], "the readout reads the store's tile in place")
}

func levelSizeOf(img imaging.Image, k int) (int, int) {
	return LevelSize(img.Width(), img.Height(), -k)
}

// A store still being built is shown as it fills: a tile not built is
// waited for on the loader's workers, the readout does not wait, and the
// default recipe falls back to the nominal range until the data range is
// known.
func TestAStoreIsShownWhileItIsBuilt(t *testing.T) {
	b, err := tiles.NewBuilder(600, 1100, imaging.ColorModelGray, imaging.SampleKindU16, nil, imaging.Provenance{}, tiles.Options{})
	require.NoError(t, err)
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	require.NoError(t, v.SetSource(StoreSource(b.Store()), false))
	assert.Equal(t, display.LimitsSourceNominal, v.Recipe().Limits.Source)
	r, ok := v.readoutAt(portolan.LL(-5.5, 5.5))
	require.True(t, ok)
	assert.Zero(t, r.Planes, "nothing at hand yet, and the frame did not wait")

	got := make(chan []uint32, 1)
	go func() {
		px, _, _, err := v.src.TilePixels(context.Background(), portolan.TileCoords{X: 0, Y: 1, Z: 0})
		if err == nil {
			got <- px
		}
		close(got)
	}()
	img := grayImage(t, 600, 1100)
	require.NoError(t, b.Add(img))
	require.NoError(t, b.Finish())
	select {
	case px, ok := <-got:
		require.True(t, ok, "the waiting tile was answered")
		assert.Len(t, px, tileSize*tileSize)
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting tile was never answered")
	}
}
