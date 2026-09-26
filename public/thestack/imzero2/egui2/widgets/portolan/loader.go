package portolan

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"image"
	"image/draw"
	_ "image/jpeg" // raster tile servers serve PNG; aerial layers JPEG
	_ "image/png"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TileFetcherI fetches one tile's bytes. The loader does no HTTP of its
// own (ADR-0262 §SD6): a hosted app hands it a fetcher over the
// `net.http.fetch.<destination>` family, whose destination holds the
// transport policy — trust, user agent, timeout. A status other than 200
// is the fetcher's error.
type TileFetcherI interface {
	Get(ctx context.Context, url string) (data []byte, err error)
}

// errNoFetcher is every fetch's outcome for a loader built without one.
var errNoFetcher = errors.New("portolan: no tile fetcher")

// LoaderOptions configures a TileLoader. The zero value is usable but
// fetches nothing: without a Fetcher every tile fails with errNoFetcher.
// Otherwise six workers, a 512-tile byte cache, a 30 s negative cache and a
// 30 s timeout.
type LoaderOptions struct {
	// Fetcher fetches a tile's bytes; nil fails every tile.
	Fetcher TileFetcherI
	// Workers is the number of concurrent fetches — Leaflet's and the old binding's
	// per-host budget of six.
	Workers int
	// ByteCacheTiles is how many compressed tiles the loader keeps, by count;
	// a re-visited tile decodes from memory without a fetch.
	ByteCacheTiles int
	// NegativeTTL is how long a failed tile is not retried; the pyramid
	// re-requests it the next time it becomes current after that.
	NegativeTTL time.Duration
	// Timeout bounds one fetch.
	Timeout time.Duration
	// MaxTileBytes caps one tile's body.
	MaxTileBytes int64
}

func (o LoaderOptions) withDefaults() LoaderOptions {
	if o.Workers <= 0 {
		o.Workers = 6
	}
	if o.ByteCacheTiles <= 0 {
		o.ByteCacheTiles = 512
	}
	if o.NegativeTTL <= 0 {
		o.NegativeTTL = 30 * time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.MaxTileBytes <= 0 {
		o.MaxTileBytes = 4 << 20
	}
	return o
}

// TileArrival is one tile the loader has finished with: decoded pixels in the
// paintImage packing (0xRRGGBBAA, row-major), or Failed with the error. Coords
// are the unwrapped coordinates the tile was requested as.
type TileArrival struct {
	Coords, Wrapped TileCoords
	// URL is what was fetched — the source's tile, or its ErrorTileURL.
	URL    string
	Pixels []uint32
	W, H   int
	Failed bool
	Err    error
}

// LoaderHealth is what an operator would want to know when the map is grey:
// how many fetches in a row have failed and what the last one said (ADR-0204
// §SD4's "persistently failing source" flag).
type LoaderHealth struct {
	ConsecutiveFailures int
	LastError           string
	LastFailureAt       time.Time
	Pending, InFlight   int
}

type tileRequest struct {
	coords, wrapped TileCoords
	url             string
}

// tileWaiter is a request riding on a URL's fetch; several can share one
// URL with different coordinates (an ErrorTileURL), so each keeps its own.
type tileWaiter struct {
	coords, wrapped TileCoords
}

// TileLoader fetches, caches and decodes tiles on a worker pool, off the
// frame thread (ADR-0165's constraint): the frame requests tiles and drains
// arrivals. Requests are served in the order they were made — the pyramid
// requests from the viewport's centre outward — and a request for the same
// URL as one in flight attaches to it rather than fetching twice.
type TileLoader struct {
	opts LoaderOptions

	mu        sync.Mutex
	cond      *sync.Cond
	queue     []TileCoords
	pending   map[TileCoords]*tileRequest // queued, not started
	inflight  map[string][]tileWaiter     // url → requests waiting on it
	cancelled map[TileCoords]struct{}     // cancelled while in flight
	arrivals  []TileArrival
	cache     *byteCache
	negative  map[string]time.Time
	health    LoaderHealth
	closed    bool
}

// NewTileLoader starts the workers.
func NewTileLoader(opts LoaderOptions) *TileLoader {
	opts = opts.withDefaults()
	l := &TileLoader{
		opts:      opts,
		pending:   make(map[TileCoords]*tileRequest, 64),
		inflight:  make(map[string][]tileWaiter, 16),
		cancelled: make(map[TileCoords]struct{}, 16),
		cache:     newByteCache(opts.ByteCacheTiles),
		negative:  make(map[string]time.Time, 16),
	}
	l.cond = sync.NewCond(&l.mu)
	for i := 0; i < opts.Workers; i++ {
		go l.worker()
	}
	return l
}

// Request asks for a tile. It is idempotent per coords while the request is
// pending or in flight.
func (l *TileLoader) Request(coords, wrapped TileCoords, url string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	delete(l.cancelled, coords)
	if _, ok := l.pending[coords]; ok {
		return
	}
	for _, waiting := range l.inflight {
		for _, w := range waiting {
			if w.coords == coords {
				return
			}
		}
	}
	l.pending[coords] = &tileRequest{coords: coords, wrapped: wrapped, url: url}
	l.queue = append(l.queue, coords)
	l.cond.Signal()
}

// Cancel withdraws a request: a queued one is dropped, one in flight is
// finished but not delivered.
func (l *TileLoader) Cancel(coords TileCoords) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.pending[coords]; ok {
		delete(l.pending, coords)
		return
	}
	for _, waiting := range l.inflight {
		for _, w := range waiting {
			if w.coords == coords {
				l.cancelled[coords] = struct{}{}
				return
			}
		}
	}
}

// Drain returns the arrivals since the last call. Frame thread only.
func (l *TileLoader) Drain() (out []TileArrival) {
	l.mu.Lock()
	out, l.arrivals = l.arrivals, nil
	l.mu.Unlock()
	return
}

// Pending is the number of requests not yet delivered.
func (l *TileLoader) Pending() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pendingLocked()
}

func (l *TileLoader) pendingLocked() (n int) {
	n = len(l.pending)
	for _, w := range l.inflight {
		n += len(w)
	}
	return
}

// Health is the loader's failure state.
func (l *TileLoader) Health() LoaderHealth {
	l.mu.Lock()
	defer l.mu.Unlock()
	h := l.health
	h.Pending = len(l.pending)
	for _, w := range l.inflight {
		h.InFlight += len(w)
	}
	return h
}

// Close stops the workers; pending requests are dropped.
func (l *TileLoader) Close() {
	l.mu.Lock()
	l.closed = true
	l.pending = map[TileCoords]*tileRequest{}
	l.queue = nil
	l.mu.Unlock()
	l.cond.Broadcast()
}

func (l *TileLoader) worker() {
	for {
		l.mu.Lock()
		for len(l.queue) == 0 && !l.closed {
			l.cond.Wait()
		}
		if l.closed {
			l.mu.Unlock()
			return
		}
		coords := l.queue[0]
		l.queue = l.queue[1:]
		req, ok := l.pending[coords]
		if !ok {
			// cancelled while queued
			l.mu.Unlock()
			continue
		}
		delete(l.pending, coords)
		if waiting, busy := l.inflight[req.url]; busy {
			l.inflight[req.url] = append(waiting, tileWaiter{coords, req.wrapped})
			l.mu.Unlock()
			continue
		}
		l.inflight[req.url] = []tileWaiter{{coords, req.wrapped}}
		if exp, bad := l.negative[req.url]; bad && time.Now().Before(exp) {
			// A hit on the negative cache is not a new failure: it neither
			// extends the entry (the tile is retried once the TTL from the
			// real failure has passed) nor counts in Health.
			l.deliver(req, nil, 0, 0, eb.Build().Str("retryAfter", exp.Format(time.RFC3339)).Errorf("portolan: tile failed recently and is not retried yet"))
			continue // deliver unlocks
		}
		data, cached := l.cache.get(req.url)
		l.mu.Unlock()

		var err error
		if !cached {
			data, err = l.fetch(req.url)
			if err == nil {
				l.mu.Lock()
				l.cache.put(req.url, data)
				l.mu.Unlock()
			}
		}
		var px []uint32
		var w, h int
		if err == nil {
			px, w, h, err = decodeTile(data)
		}
		l.mu.Lock()
		l.record(req.url, err)
		l.deliver(req, px, w, h, err) // unlocks
	}
}

// record books a fetch's outcome: a failure enters the negative cache and
// the health counters, a success resets the failure streak. Mutex held.
func (l *TileLoader) record(url string, err error) {
	if err != nil {
		l.negative[url] = time.Now().Add(l.opts.NegativeTTL)
		l.health.ConsecutiveFailures++
		l.health.LastError = err.Error()
		l.health.LastFailureAt = time.Now()
	} else {
		l.health.ConsecutiveFailures = 0
	}
}

// deliver hands an outcome to every request waiting on its URL. Called with
// the mutex held; releases it.
func (l *TileLoader) deliver(req *tileRequest, px []uint32, w, h int, err error) {
	waiting := l.inflight[req.url]
	delete(l.inflight, req.url)
	for _, wt := range waiting {
		if _, gone := l.cancelled[wt.coords]; gone {
			delete(l.cancelled, wt.coords)
			continue
		}
		l.arrivals = append(l.arrivals, TileArrival{
			Coords: wt.coords, Wrapped: wt.wrapped, URL: req.url, Pixels: px, W: w, H: h, Failed: err != nil, Err: err,
		})
	}
	l.mu.Unlock()
}

func (l *TileLoader) fetch(url string) ([]byte, error) {
	if l.opts.Fetcher == nil {
		return nil, errNoFetcher
	}
	ctx, cancel := context.WithTimeout(context.Background(), l.opts.Timeout)
	defer cancel()
	data, err := l.opts.Fetcher.Get(ctx, url)
	if err != nil {
		return nil, eh.Errorf("portolan: tile fetch failed: %w", err)
	}
	if int64(len(data)) > l.opts.MaxTileBytes {
		return nil, eb.Build().Int64("maxTileBytes", l.opts.MaxTileBytes).Errorf("portolan: tile body exceeds the size limit")
	}
	return data, nil
}

// decodeTile decodes PNG/JPEG bytes into the paintImage packing: 0xRRGGBBAA
// with STRAIGHT (non-premultiplied) alpha, which is what the host's texture
// upload expects (it premultiplies itself). Hence image.NRGBA, not RGBA,
// whose channels Go's image package premultiplies — a translucent tile
// would otherwise be premultiplied twice and darken at its edges.
func decodeTile(data []byte) (px []uint32, w, h int, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, eh.Errorf("portolan: tile undecodable: %w", err)
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), img, b.Min, draw.Src)
	px = make([]uint32, w*h)
	for i := range px {
		o := i * 4
		px[i] = uint32(nrgba.Pix[o])<<24 | uint32(nrgba.Pix[o+1])<<16 | uint32(nrgba.Pix[o+2])<<8 | uint32(nrgba.Pix[o+3])
	}
	return px, w, h, nil
}

// byteCache is a count-bounded LRU of compressed tile bodies by URL.
type byteCache struct {
	cap   int
	order *list.List
	items map[string]*list.Element
}

type byteCacheItem struct {
	url  string
	data []byte
}

func newByteCache(capacity int) *byteCache {
	return &byteCache{cap: capacity, order: list.New(), items: make(map[string]*list.Element, capacity)}
}

func (c *byteCache) get(url string) ([]byte, bool) {
	el, ok := c.items[url]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*byteCacheItem).data, true
}

func (c *byteCache) put(url string, data []byte) {
	if el, ok := c.items[url]; ok {
		el.Value.(*byteCacheItem).data = data
		c.order.MoveToFront(el)
		return
	}
	c.items[url] = c.order.PushFront(&byteCacheItem{url: url, data: data})
	for c.order.Len() > c.cap {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*byteCacheItem).url)
	}
}
