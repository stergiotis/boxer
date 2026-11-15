package portolan

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// solidPixels answers every tile with one colour, which the test changes
// between revisions; it counts calls and can hold them back.
type solidPixels struct {
	colour atomic.Uint32
	calls  atomic.Int64
	fail   atomic.Bool
	short  atomic.Bool
	mu     sync.Mutex
	gate   chan struct{} // when set, every call waits on it
}

func (s *solidPixels) TilePixels(_ context.Context, _ TileCoords) ([]uint32, int, int, error) {
	s.calls.Add(1)
	s.mu.Lock()
	gate := s.gate
	s.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if s.fail.Load() {
		return nil, 0, 0, errors.New("no pixels")
	}
	n := 4
	if s.short.Load() {
		n = 3
	}
	px := make([]uint32, n)
	for i := range px {
		px[i] = s.colour.Load()
	}
	return px, 2, 2, nil
}

func pixelMap(t *testing.T, src *solidPixels) *Map {
	t.Helper()
	ts := TileSource{TileSize: Pt(256, 256), MinZoom: 0, MaxZoom: 4, NoWrap: true}
	m := New(nil, "", Options{Source: ts, Loader: LoaderOptions{Workers: 2, Pixels: src}, CRS: Simple, Center: LL(-300, 300), Zoom: 0})
	m.view.SetSize(Pt(600, 600))
	m.view.SetView(m.opts.Center, m.opts.Zoom)
	m.pyramid.Sync(m.view, m.view.TakeEvents())
	return m
}

// settle absorbs arrivals until cond holds.
func settle(t *testing.T, m *Map, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() && time.Now().Before(deadline) {
		m.absorbArrivals(m.loader.Drain(), time.Now())
		time.Sleep(time.Millisecond)
	}
	require.True(t, cond(), what)
}

func allPixels(m *Map, colour uint32) bool {
	if len(m.wrappedOf) == 0 {
		return false
	}
	for _, wrapped := range m.wrappedOf {
		px := m.pixels[wrapped]
		if px == nil || px.px[0] != colour {
			return false
		}
	}
	return true
}

func TestMap_TilesFromAPixelSource(t *testing.T) {
	// A map with a pixel source asks it, not a URL, for every tile; the
	// template stays empty and no default source is put in its place.
	src := &solidPixels{}
	src.colour.Store(0x11223344)
	m := pixelMap(t, src)
	defer m.Close()
	assert.Empty(t, m.Source().URLTemplate)
	requested := m.pyramid.Stats().TileLoadStart
	require.Positive(t, requested)
	settle(t, m, "every tile loaded", func() bool { return !m.pyramid.IsLoading() })
	assert.Equal(t, requested, m.pyramid.Stats().TileLoad)
	assert.Zero(t, m.pyramid.Stats().TileError)
	assert.True(t, allPixels(m, 0x11223344))
	assert.EqualValues(t, requested, src.calls.Load(), "one call per tile, nothing cached or repeated")
}

func TestMap_InvalidateTilesSwapsPixelsInPlace(t *testing.T) {
	src := &solidPixels{}
	src.colour.Store(0xaaaaaaff)
	m := pixelMap(t, src)
	defer m.Close()
	settle(t, m, "every tile loaded", func() bool { return !m.pyramid.IsLoading() })
	loaded := m.pyramid.Stats().TileLoad
	versions := map[TileCoords]uint64{}
	for _, wrapped := range m.wrappedOf {
		versions[wrapped] = m.tileVersion(m.pixels[wrapped])
	}

	src.colour.Store(0xbbbbbbff)
	m.InvalidateTiles()
	// The old pixels are still what the map holds until the new ones land.
	for _, wrapped := range m.wrappedOf {
		assert.NotNil(t, m.pixels[wrapped])
	}
	settle(t, m, "every tile shows the new content", func() bool { return allPixels(m, 0xbbbbbbff) && len(m.refreshing) == 0 })
	assert.Equal(t, loaded, m.pyramid.Stats().TileLoad, "a refreshed tile is not loaded a second time: no new fade")
	for _, wrapped := range m.wrappedOf {
		assert.Greater(t, m.tileVersion(m.pixels[wrapped]), versions[wrapped], "the texture version moved, so the host re-uploads")
	}
}

func TestMap_InvalidateWhileATileIsInFlightAsksAgain(t *testing.T) {
	// A tile whose first answer was computed before the content changed is
	// accepted — something is better than nothing — and asked for again.
	src := &solidPixels{gate: make(chan struct{})}
	src.colour.Store(0xaaaaaaff)
	m := pixelMap(t, src)
	defer m.Close()
	deadline := time.Now().Add(5 * time.Second)
	for src.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.Positive(t, src.calls.Load(), "a worker is holding a tile")

	m.InvalidateTiles() // nothing is loaded, so nothing is re-requested here
	assert.Empty(t, m.refreshing)
	src.colour.Store(0xbbbbbbff)
	src.mu.Lock()
	gate := src.gate
	src.gate = nil
	src.mu.Unlock()
	close(gate)

	settle(t, m, "every tile ends on the current content", func() bool {
		return !m.pyramid.IsLoading() && len(m.refreshing) == 0 && m.loader.Pending() == 0 && allPixels(m, 0xbbbbbbff)
	})
	assert.Greater(t, src.calls.Load(), int64(m.pyramid.Stats().TileLoadStart), "the stale answers were followed by fresh requests")
}

func TestMap_AFailedRefreshKeepsTheOldPixels(t *testing.T) {
	src := &solidPixels{}
	src.colour.Store(0xaaaaaaff)
	m := pixelMap(t, src)
	defer m.Close()
	settle(t, m, "every tile loaded", func() bool { return !m.pyramid.IsLoading() })

	src.fail.Store(true)
	m.InvalidateTiles()
	settle(t, m, "the refresh finished", func() bool { return len(m.refreshing) == 0 && m.loader.Pending() == 0 })
	assert.True(t, allPixels(m, 0xaaaaaaff), "what was on screen stays")
	assert.Positive(t, m.Health().ConsecutiveFailures)
	for coords := range m.wrappedOf {
		assert.False(t, m.pyramid.Failed(coords), "a tile that loaded once is not a failed tile")
	}
}

func TestMap_APixelSourceThatLiesAboutItsSizeIsAFailure(t *testing.T) {
	src := &solidPixels{}
	src.short.Store(true)
	m := pixelMap(t, src)
	defer m.Close()
	settle(t, m, "every tile settled", func() bool { return !m.pyramid.IsLoading() })
	assert.Zero(t, m.pyramid.Stats().TileLoad)
	assert.Equal(t, m.pyramid.Stats().TileLoadStart, m.pyramid.Stats().TileError)
}

func TestMap_InvalidateTilesIsNothingOnAFetchedMap(t *testing.T) {
	m := New(nil, "", Options{Loader: LoaderOptions{Workers: 1}, Center: LL(0, 0), Zoom: 1})
	defer m.Close()
	m.InvalidateTiles()
	assert.Zero(t, m.contentRev)
	assert.NotEmpty(t, m.Source().URLTemplate, "a map without a pixel source still defaults to a URL source")
}

func TestMap_PreferFinerNeverDrawsATileLargerThanItIs(t *testing.T) {
	// Between two levels Leaflet takes the nearer one; a source that prefers
	// the finer takes the one above, so its tiles are drawn reduced.
	for _, tc := range []struct {
		zoom   float64
		finer  bool
		levels []int
	}{
		{-0.6, false, []int{-1}},
		{-0.6, true, []int{0}},
		{-0.4, false, []int{0}},
		{-0.4, true, []int{0}},
		{-1, true, []int{-1}},
		{-1.0000001, true, []int{-1}},
		{-1.2, true, []int{-1}},
		{-2.01, true, []int{-2}},
	} {
		ts := TileSource{TileSize: Pt(256, 256), MinZoom: -3, MaxZoom: 2, NoWrap: true, PreferFiner: tc.finer}
		m := New(nil, "", Options{Source: ts, Loader: LoaderOptions{Workers: 1, Pixels: &solidPixels{}}, CRS: Simple})
		m.view.SetSize(Pt(600, 600))
		m.view.SetView(LL(-300, 300), tc.zoom)
		m.pyramid.Sync(m.view, m.view.TakeEvents())
		assert.Equal(t, tc.levels, m.pyramid.Levels(), "zoom %v, finer %v", tc.zoom, tc.finer)
		m.Close()
	}
}

func TestMap_UploadBudgetSpreadsABurstOverFrames(t *testing.T) {
	// Each tile of the test source is 4 pixels, 16 bytes. With a budget of
	// 40 bytes a frame ships three tiles — the third passes the bound — and
	// the rest wait their turn; after a content change the same holds, and
	// a tile still waiting keeps the version the host has.
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	src := &solidPixels{}
	src.colour.Store(0xaaaaaaff)
	ts := TileSource{TileSize: Pt(256, 256), MinZoom: 0, MaxZoom: 4, NoWrap: true}
	m := New(c.NewWidgetIdStack(), "budget", Options{Source: ts, Loader: LoaderOptions{Workers: 2, Pixels: src}, CRS: Simple,
		Center: LL(-300, 300), Zoom: 0, UploadBudgetBytes: 40})
	defer m.Close()
	m.pyramid.SetFade(false)
	frame := func() uint64 {
		before := m.BytesShipped()
		m.Render(600, 600, nil)
		sm.ScriptReset()
		return m.BytesShipped() - before
	}
	frame()
	deadline := time.Now().Add(5 * time.Second)
	for (m.pyramid.IsLoading() || m.loader.Pending() > 0) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		// Arrivals are absorbed without drawing, so every tile is loaded
		// before the first one ships.
		m.absorbArrivals(m.loader.Drain(), time.Now())
	}
	tiles := len(m.wrappedOf)
	require.Greater(t, tiles, 6)

	// The first frame may already have shipped the tiles that arrived
	// within it, so the count is of everything shipped since the start.
	frames := 0
	for range 100 {
		n := frame()
		if n == 0 {
			break
		}
		assert.LessOrEqual(t, n, uint64(48), "a frame ships at most three tiles")
		frames++
	}
	assert.EqualValues(t, tiles*16, m.BytesShipped(), "every tile shipped exactly once")
	assert.GreaterOrEqual(t, frames, 2, "and not in one frame")

	versions := map[TileCoords]uint64{}
	for _, wrapped := range m.wrappedOf {
		v, ok := m.tracker.LastSent(wrapped)
		require.True(t, ok)
		versions[wrapped] = v
	}
	src.colour.Store(0xbbbbbbff)
	m.InvalidateTiles()
	for (len(m.refreshing) > 0 || m.loader.Pending() > 0) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		m.absorbArrivals(m.loader.Drain(), time.Now())
	}
	require.True(t, allPixels(m, 0xbbbbbbff))
	before := m.BytesShipped()
	require.EqualValues(t, 48, frame(), "the first frame after the change ships three")
	waiting := 0
	for _, wrapped := range m.wrappedOf {
		if v, _ := m.tracker.LastSent(wrapped); v == versions[wrapped] {
			waiting++
		}
	}
	assert.Equal(t, tiles-3, waiting, "the others still stand at the version the host holds")
	for range 100 {
		if frame() == 0 {
			break
		}
	}
	assert.EqualValues(t, tiles*16, m.BytesShipped()-before, "and all of them follow, once each")
}
