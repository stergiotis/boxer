package portolan

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapPixels answers with the colour it read when the call began, as a
// viewer's source answers from the content published when its worker
// started, and holds its calls until released. With honour set a held call
// ends when its context does.
type snapPixels struct {
	colour atomic.Uint32
	calls  atomic.Int64
	ended  atomic.Int64 // calls that ended on their context
	honour bool
	mu     sync.Mutex
	gate   chan struct{}
}

func (s *snapPixels) TilePixels(ctx context.Context, _ TileCoords) ([]uint32, int, int, error) {
	colour := s.colour.Load()
	s.calls.Add(1)
	s.mu.Lock()
	gate := s.gate
	s.mu.Unlock()
	if gate != nil {
		if s.honour {
			select {
			case <-gate:
			case <-ctx.Done():
				s.ended.Add(1)
				return nil, 0, 0, ctx.Err()
			}
		} else {
			<-gate
		}
	}
	return []uint32{colour, colour, colour, colour}, 2, 2, nil
}

func (s *snapPixels) release() {
	s.mu.Lock()
	gate := s.gate
	s.gate = nil
	s.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

func snapMap(t *testing.T, src *snapPixels) *Map {
	t.Helper()
	ts := TileSource{TileSize: Pt(256, 256), MinZoom: 0, MaxZoom: 4, NoWrap: true}
	m := New(nil, "", Options{Source: ts, Loader: LoaderOptions{Workers: 2, Pixels: src}, CRS: Simple, Center: LL(-300, 300), Zoom: 0})
	m.view.SetSize(Pt(600, 600))
	m.view.SetView(m.opts.Center, m.opts.Zoom)
	m.pyramid.Sync(m.view, m.view.TakeEvents())
	return m
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.True(t, cond(), what)
}

// A source switched while tiles of the last are being computed, to one of
// the same tiles: the calls under way answer for the old content, and none
// of their answers may stand for the new — the defect a page turned while
// the page before was rendering met. Held to the end here, so the calls
// still answer after the switch.
func TestMap_ASourceSwitchNeverShowsTheLastSourcesTiles(t *testing.T) {
	src := &snapPixels{gate: make(chan struct{})}
	src.colour.Store(0xaaaaaaff)
	m := snapMap(t, src)
	defer m.Close()
	waitFor(t, "both workers hold a tile", func() bool { return src.calls.Load() == 2 })

	src.colour.Store(0xbbbbbbff)
	m.SetSource(m.Source())
	src.release()

	shownOld := false
	settle(t, m, "every tile shows the new source", func() bool {
		for _, px := range m.pixels {
			shownOld = shownOld || px.px[0] == 0xaaaaaaff
		}
		return !m.pyramid.IsLoading() && m.loader.Pending() == 0 && allPixels(m, 0xbbbbbbff)
	})
	assert.False(t, shownOld, "no tile of the last source was stored, even for a frame")
	assert.Zero(t, m.pyramid.Stats().TileError)
	assert.Zero(t, m.Health().ConsecutiveFailures)
}

// A call no request waits on any more has its context cancelled, and its
// end is not booked as a failure.
func TestMap_ASwitchCancelsTheCallsUnderWay(t *testing.T) {
	src := &snapPixels{gate: make(chan struct{}), honour: true}
	src.colour.Store(0xaaaaaaff)
	m := snapMap(t, src)
	defer m.Close()
	waitFor(t, "both workers hold a tile", func() bool { return src.calls.Load() == 2 })

	src.colour.Store(0xbbbbbbff)
	m.SetSource(m.Source())
	waitFor(t, "the old calls ended on their context", func() bool { return src.ended.Load() == 2 })
	src.release()

	settle(t, m, "every tile shows the new source", func() bool {
		return !m.pyramid.IsLoading() && m.loader.Pending() == 0 && allPixels(m, 0xbbbbbbff)
	})
	assert.Zero(t, m.Health().ConsecutiveFailures, "a cancelled call is not a failure")
	assert.Zero(t, m.pyramid.Stats().TileError)
}

// An answer to a request made before the last switch is not shown even
// when it reaches the map — a call the switch could not withdraw — and the
// tile is asked of the source again.
func TestMap_AnArrivalFromBeforeASwitchIsAskedAgain(t *testing.T) {
	src := &snapPixels{}
	src.colour.Store(0xaaaaaaff)
	m := snapMap(t, src)
	defer m.Close()
	before := m.contentRev
	src.colour.Store(0xbbbbbbff)
	m.SetSource(m.Source())
	require.Greater(t, m.sourceRev, before)
	settle(t, m, "every tile shows the new source", func() bool { return !m.pyramid.IsLoading() && allPixels(m, 0xbbbbbbff) })

	var coords, wrapped TileCoords
	for coords, wrapped = range m.wrappedOf {
		break
	}
	calls := src.calls.Load()
	m.absorbArrivals([]TileArrival{{Coords: coords, Wrapped: wrapped, Pixels: []uint32{1, 1, 1, 1}, W: 2, H: 2, Revision: before}}, time.Now())
	assert.Equal(t, uint32(0xbbbbbbff), m.pixels[wrapped].px[0], "the old answer is not stored")
	waitFor(t, "the tile is asked for again", func() bool { return src.calls.Load() == calls+1 })
}

// The loader withdraws a call no request waits on, and a request for the
// same coordinates afterwards starts a call of its own.
func TestTileLoader_CancelOfTheLastWaiterAbandonsTheCall(t *testing.T) {
	src := &snapPixels{gate: make(chan struct{}), honour: true}
	src.colour.Store(0xaaaaaaff)
	l := NewTileLoader(LoaderOptions{Workers: 2, Pixels: src})
	defer l.Close()
	at := TileCoords{X: 1, Y: 2, Z: 3}
	l.RequestPixels(at, at, 1)
	waitFor(t, "the call began", func() bool { return src.calls.Load() == 1 })

	l.Cancel(at)
	waitFor(t, "the call ended on its context", func() bool { return src.ended.Load() == 1 })
	assert.Zero(t, l.Pending())

	src.colour.Store(0xbbbbbbff)
	src.release()
	l.RequestPixels(at, at, 2)
	var got []TileArrival
	waitFor(t, "the new call answered", func() bool { got = append(got, l.Drain()...); return len(got) > 0 })
	require.Len(t, got, 1)
	assert.Equal(t, uint64(2), got[0].Revision)
	assert.Equal(t, uint32(0xbbbbbbff), got[0].Pixels[0])
	assert.Equal(t, int64(2), src.calls.Load())
	assert.Zero(t, l.Health().ConsecutiveFailures)
}
