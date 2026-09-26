package portolan

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests of the loader's own seams — what Leaflet leaves to the browser: the
// decode's alpha convention and the negative cache's bookkeeping.

func TestDecodeTile_StraightAlpha(t *testing.T) {
	// A half-transparent red pixel must come out as 0xff000080, not the
	// premultiplied 0x80000080: the host's texture upload premultiplies
	// itself (Color32::from_rgba_unmultiplied), so a premultiplied decode
	// would darken translucent tiles twice.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0xff, G: 0, B: 0, A: 0x80})
	img.SetNRGBA(1, 0, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	px, w, h, err := decodeTile(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, 2, w)
	assert.Equal(t, 1, h)
	assert.Equal(t, uint32(0xff000080), px[0], "straight alpha: red untouched by the half alpha")
	assert.Equal(t, uint32(0x102030ff), px[1])
}

// failingFetcher fails every tile and counts the fetches.
type failingFetcher struct {
	calls atomic.Int32
}

func (f *failingFetcher) Get(context.Context, string) ([]byte, error) {
	f.calls.Add(1)
	return nil, errors.New("500")
}

func drainOne(t *testing.T, l *TileLoader) TileArrival {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a := l.Drain(); len(a) > 0 {
			require.Len(t, a, 1)
			return a[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no arrival")
	return TileArrival{}
}

func TestTileLoader_NegativeCacheHitIsNotAFailure(t *testing.T) {
	// A re-request inside the TTL is answered from the negative cache: it
	// is delivered as failed, but it neither fetches, nor extends the
	// entry, nor counts in Health — so the tile is retried once the TTL
	// from the real failure has passed, however often it was asked for
	// meanwhile (ADR-0204 §SD4 Q3).
	tr := &failingFetcher{}
	l := NewTileLoader(LoaderOptions{Workers: 1, NegativeTTL: 80 * time.Millisecond, Fetcher: tr})
	defer l.Close()
	c := TileCoords{1, 2, 3}

	l.Request(c, c, "http://tiles.example/1")
	a := drainOne(t, l)
	assert.True(t, a.Failed)
	assert.Equal(t, "http://tiles.example/1", a.URL)
	assert.Equal(t, int32(1), tr.calls.Load())
	assert.Equal(t, 1, l.Health().ConsecutiveFailures)

	// Inside the TTL: served from the negative cache.
	l.Request(c, c, "http://tiles.example/1")
	a = drainOne(t, l)
	assert.True(t, a.Failed)
	assert.Contains(t, a.Err.Error(), "failed recently")
	assert.Equal(t, int32(1), tr.calls.Load(), "no fetch")
	assert.Equal(t, 1, l.Health().ConsecutiveFailures, "a cache hit is not a new failure")

	// After the TTL from the real failure: fetched again.
	time.Sleep(120 * time.Millisecond)
	l.Request(c, c, "http://tiles.example/1")
	a = drainOne(t, l)
	assert.True(t, a.Failed)
	assert.Equal(t, int32(2), tr.calls.Load(), "retried after the TTL")
	assert.Equal(t, 2, l.Health().ConsecutiveFailures)
}

// Without a fetcher the loader does no HTTP of its own: every tile fails
// with the reason, and Health carries it (ADR-0262 §SD6).
func TestTileLoader_NoFetcherFailsWithTheReason(t *testing.T) {
	l := NewTileLoader(LoaderOptions{Workers: 1})
	defer l.Close()
	c := TileCoords{1, 2, 3}
	l.Request(c, c, "http://tiles.example/1")
	a := drainOne(t, l)
	require.True(t, a.Failed)
	assert.ErrorIs(t, a.Err, errNoFetcher)
	assert.Contains(t, l.Health().LastError, "not hosted")
}
