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
	"strconv"
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

// TilePixelsI supplies a tile's pixels directly, for a raster that is not
// fetched by URL: an image held in memory, a field rendered on demand
// (ADR-0373 §SD7). TilePixels is called on a loader worker, off the frame
// thread and possibly from several workers at once, with the tile's wrapped
// coordinates; it returns the tile in the paintImage packing — 0xRRGGBBAA,
// straight alpha, row-major — at the source's tile size, transparent where
// the raster has nothing. ctx ends at the loader's timeout, and earlier
// when the view gives the tile up — it scrolled away, or the map's source
// was switched — so a source that renders should check it. The loader keeps
// no copy of what it returns and does not cache it: a source whose content
// changes says so through [Map.InvalidateTiles].
type TilePixelsI interface {
	TilePixels(ctx context.Context, coords TileCoords) (px []uint32, w, h int, err error)
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
	// Pixels, when set, supplies every tile's pixels and the source's URL
	// template, the fetcher and the byte cache are not used.
	Pixels TilePixelsI
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
	// Revision is the content revision a pixel-source tile was requested
	// under (Map.InvalidateTiles); 0 for a fetched tile.
	Revision uint64
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
	// pixels marks a request the pixel source answers; revision is the
	// content revision it was made under.
	pixels   bool
	revision uint64
}

// tileWaiter is a request riding on a URL's fetch; several can share one
// URL with different coordinates (an ErrorTileURL), so each keeps its own.
type tileWaiter struct {
	coords, wrapped TileCoords
}

// flight is one fetch or pixel-source call under way and the requests
// waiting on it. When every one of them is withdrawn the flight is
// abandoned: its context is cancelled, it leaves the in-flight index so a
// new request for the same coordinates starts a call of its own, and what it
// returns is delivered to no one and booked as neither success nor failure.
type flight struct {
	waiters   []tileWaiter
	cancel    context.CancelFunc
	abandoned bool
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
	inflight  map[string]*flight          // url → the call and who waits on it
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
		inflight:  make(map[string]*flight, 16),
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

// RequestPixels asks the pixel source for a tile under a content revision.
// Like Request it is idempotent per coords while one is pending or in flight,
// so a tile asked for again under a newer revision before the first answer
// arrives gets the first answer, marked with the older revision.
func (l *TileLoader) RequestPixels(coords, wrapped TileCoords, revision uint64) {
	key := "pixels:" + strconv.FormatUint(revision, 10) + ":" + wrapped.Key()
	l.request(&tileRequest{coords: coords, wrapped: wrapped, url: key, pixels: true, revision: revision})
}

// Request asks for a tile. It is idempotent per coords while the request is
// pending or in flight.
func (l *TileLoader) Request(coords, wrapped TileCoords, url string) {
	l.request(&tileRequest{coords: coords, wrapped: wrapped, url: url})
}

func (l *TileLoader) request(req *tileRequest) {
	coords := req.coords
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	delete(l.cancelled, coords)
	if _, ok := l.pending[coords]; ok {
		return
	}
	for _, f := range l.inflight {
		for _, w := range f.waiters {
			if w.coords == coords {
				return
			}
		}
	}
	l.pending[coords] = req
	l.queue = append(l.queue, coords)
	l.cond.Signal()
}

// Cancel withdraws a request: a queued one is dropped, one in flight is not
// delivered. When it was the last request waiting on its call, the call's
// context is cancelled and the call is forgotten, so a request for the same
// coordinates made afterwards — by a source that replaced this one — starts
// a call of its own instead of waiting on this one's answer.
func (l *TileLoader) Cancel(coords TileCoords) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.pending[coords]; ok {
		delete(l.pending, coords)
		return
	}
	for url, f := range l.inflight {
		for _, w := range f.waiters {
			if w.coords != coords {
				continue
			}
			l.cancelled[coords] = struct{}{}
			for _, o := range f.waiters {
				if _, gone := l.cancelled[o.coords]; !gone {
					return
				}
			}
			l.abandonLocked(url, f)
			return
		}
	}
}

// abandonLocked gives up a flight no one waits on any more. Mutex held.
func (l *TileLoader) abandonLocked(url string, f *flight) {
	f.abandoned = true
	delete(l.inflight, url)
	for _, w := range f.waiters {
		delete(l.cancelled, w.coords)
	}
	f.waiters = nil
	if f.cancel != nil {
		f.cancel()
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
	for _, f := range l.inflight {
		n += len(f.waiters)
	}
	return
}

// Health is the loader's failure state.
func (l *TileLoader) Health() LoaderHealth {
	l.mu.Lock()
	defer l.mu.Unlock()
	h := l.health
	h.Pending = len(l.pending)
	for _, f := range l.inflight {
		h.InFlight += len(f.waiters)
	}
	return h
}

// Close stops the workers; pending requests are dropped and the calls under
// way are cancelled.
func (l *TileLoader) Close() {
	l.mu.Lock()
	l.closed = true
	l.pending = map[TileCoords]*tileRequest{}
	l.queue = nil
	for url, f := range l.inflight {
		l.abandonLocked(url, f)
	}
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
		if f, busy := l.inflight[req.url]; busy {
			f.waiters = append(f.waiters, tileWaiter{coords, req.wrapped})
			l.mu.Unlock()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), l.opts.Timeout)
		f := &flight{waiters: []tileWaiter{{coords, req.wrapped}}, cancel: cancel}
		l.inflight[req.url] = f
		if req.pixels {
			l.mu.Unlock()
			px, w, h, err := l.pixels(ctx, req)
			l.mu.Lock()
			switch {
			case f.abandoned:
				// Cancelled by the loader: neither a failure nor a success.
			case err != nil:
				l.health.ConsecutiveFailures++
				l.health.LastError = err.Error()
				l.health.LastFailureAt = time.Now()
			default:
				l.health.ConsecutiveFailures = 0
			}
			l.deliver(f, req, px, w, h, err) // unlocks
			continue
		}
		if exp, bad := l.negative[req.url]; bad && time.Now().Before(exp) {
			// A hit on the negative cache is not a new failure: it neither
			// extends the entry (the tile is retried once the TTL from the
			// real failure has passed) nor counts in Health.
			l.deliver(f, req, nil, 0, 0, eb.Build().Str("retryAfter", exp.Format(time.RFC3339)).Errorf("portolan: tile failed recently and is not retried yet"))
			continue // deliver unlocks
		}
		data, cached := l.cache.get(req.url)
		l.mu.Unlock()

		var err error
		if !cached {
			data, err = l.fetch(ctx, req.url)
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
		if !f.abandoned {
			l.record(req.url, err)
		}
		l.deliver(f, req, px, w, h, err) // unlocks
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

// deliver hands a flight's outcome to every request waiting on it and ends
// the flight; an abandoned flight has no one left to hand it to. Called with
// the mutex held; releases it.
func (l *TileLoader) deliver(f *flight, req *tileRequest, px []uint32, w, h int, err error) {
	if l.inflight[req.url] == f {
		delete(l.inflight, req.url)
	}
	if f.cancel != nil {
		f.cancel()
	}
	for _, wt := range f.waiters {
		if _, gone := l.cancelled[wt.coords]; gone {
			delete(l.cancelled, wt.coords)
			continue
		}
		l.arrivals = append(l.arrivals, TileArrival{
			Coords: wt.coords, Wrapped: wt.wrapped, URL: req.url, Pixels: px, W: w, H: h, Failed: err != nil, Err: err,
			Revision: req.revision,
		})
	}
	l.mu.Unlock()
}

// pixels asks the pixel source for one tile and checks what came back.
func (l *TileLoader) pixels(ctx context.Context, req *tileRequest) (px []uint32, w, h int, err error) {
	if l.opts.Pixels == nil {
		return nil, 0, 0, eb.Build().Errorf("portolan: no pixel source")
	}
	px, w, h, err = l.opts.Pixels.TilePixels(ctx, req.wrapped)
	if err != nil {
		return nil, 0, 0, eh.Errorf("portolan: pixel source failed: %w", err)
	}
	if w <= 0 || h <= 0 || len(px) != w*h {
		return nil, 0, 0, eb.Build().Int("w", w).Int("h", h).Int("pixels", len(px)).
			Errorf("portolan: pixel source returned a tile whose size and pixel count disagree")
	}
	return px, w, h, nil
}

func (l *TileLoader) fetch(ctx context.Context, url string) ([]byte, error) {
	if l.opts.Fetcher == nil {
		return nil, errNoFetcher
	}
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
