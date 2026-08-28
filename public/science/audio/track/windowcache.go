package track

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

const (
	// MaxWindowFrames caps one [Track.Window] request. It is the deepest zoom
	// worth fetching: a window this long reduces to more columns than any
	// pane has pixels, so a request past it is a caller bug rather than a
	// view, and refusing it keeps one request from evicting the whole cache.
	MaxWindowFrames int64 = 1 << 22
	// DefaultWindowCacheBytes bounds the window cache when
	// [Options.WindowCacheBytes] is zero.
	DefaultWindowCacheBytes int64 = 64 << 20
	// windowFailBackoff is how long a window whose fetch failed is not
	// fetched again. The frame thread asks for the same window every frame
	// while it is on screen, so without a backoff a decoder that errors is
	// retried sixty times a second.
	windowFailBackoff = 2 * time.Second
	// sampleBytes is what one float32 sample costs in the cache's accounting.
	sampleBytes int64 = 4
)

// windowKey is the half-open frame range of one cached window. The requested
// range is the key even when the source delivered fewer frames, so a window
// at the end of the recording is one entry rather than a repeated fetch.
type windowKey struct {
	from int64
	to   int64
}

type windowEntry struct {
	key     windowKey
	samples []float32
}

// windowCache is ADR-0208 §SD3's byte-bounded cache of raw frames: the frame
// thread asks for a window, gets a miss and a scheduled fetch, draws the
// pyramid instead, and asks again next frame. It is the portolan tile
// pattern (ADR-0204 §SD4) with one worker and a single-slot mailbox — a
// zooming or panning view supersedes its own requests faster than a decoder
// can serve them, so queueing them all would spend the decoder on windows
// nobody will look at.
//
// Every method is safe from any goroutine; the cache's own reads run on its
// worker, and [Track.Window] is documented for one caller because the LRU's
// recency, not its integrity, is what several callers would blur.
type windowCache struct {
	src      pcm.SourceI
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	format   pcm.Format
	frames   int64
	maxBytes int64

	mu          sync.Mutex
	cond        *sync.Cond
	order       *list.List
	items       map[windowKey]*list.Element
	failed      map[windowKey]time.Time
	bytes       int64
	hits        uint64
	misses      uint64
	fetches     uint64
	queued      windowKey
	inflight    windowKey
	hasQueued   bool
	hasInflight bool
	started     bool
	closed      bool
}

// newWindowCache prepares the cache; its worker starts on the first miss, so
// a track whose caller never zooms past the base bin costs no goroutine. The
// context is the track's rather than the open call's, and cancelling it is
// what unblocks a decoder read at [Track.CloseE].
func newWindowCache(ctx context.Context, src pcm.SourceI, maxBytes int64) (inst *windowCache) {
	if maxBytes <= 0 {
		maxBytes = DefaultWindowCacheBytes
	}
	ctx, cancel := context.WithCancel(ctx)
	inst = &windowCache{
		src:      src,
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		format:   src.Format(),
		frames:   src.Frames(),
		maxBytes: maxBytes,
		order:    list.New(),
		items:    make(map[windowKey]*list.Element, 16),
		failed:   make(map[windowKey]time.Time, 4),
	}
	inst.cond = sync.NewCond(&inst.mu)
	return inst
}

// Window returns the raw frames of [fromFrame, toFrame) when they are
// cached, and otherwise schedules a fetch and reports ok=false — the caller
// draws the pyramid in the meantime and asks again on the next frame
// (ADR-0208 §SD3). It never blocks on I/O.
//
// The returned slice is read-only and stays valid for as long as the caller
// holds it: an evicted window is released to the garbage collector, never
// reused for another fetch. It is shorter than the request when the window
// runs past the end of the recording; a window starting at or past the end
// is empty with ok=true, since there is nothing to wait for.
//
// A request longer than [MaxWindowFrames], or one that alone would not fit
// in the cache's byte bound, is refused with ok=false every time — polling
// will not make it arrive. So is a window whose fetch has just failed, until
// a short backoff has passed.
//
// One caller — the frame thread. Concurrent calls are safe but blur the
// LRU's recency and can make two frames' worth of requests supersede each
// other.
func (inst *Track) Window(fromFrame int64, toFrame int64) (samples []float32, ok bool) {
	return inst.wc.get(fromFrame, toFrame)
}

// WindowPending reports that a [Track.Window] fetch is queued or in flight,
// which is the caller's reason to keep repainting (ADR-0208 §SD11).
func (inst *Track) WindowPending() (yes bool) {
	return inst.wc.pending()
}

// WindowCacheStats reports the window cache's occupancy and its counters
// since the track was opened. fetches counts the reads that were started, so
// misses minus fetches is roughly what the mailbox dropped as superseded.
func (inst *Track) WindowCacheStats() (entries int, bytes int64, hits uint64, misses uint64, fetches uint64) {
	return inst.wc.stats()
}

func (inst *windowCache) get(fromFrame int64, toFrame int64) (samples []float32, ok bool) {
	if fromFrame < 0 || toFrame <= fromFrame {
		return nil, false
	}
	if fromFrame >= inst.frames {
		return nil, true
	}
	if toFrame > inst.frames {
		toFrame = inst.frames
	}
	key := windowKey{from: fromFrame, to: toFrame}
	if key.to-key.from > MaxWindowFrames || key.bytes(inst.format) > inst.maxBytes {
		return nil, false
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	el, hit := inst.items[key]
	if hit {
		inst.hits++
		inst.order.MoveToFront(el)
		return el.Value.(*windowEntry).samples, true
	}
	inst.misses++
	inst.requestLocked(key)
	return nil, false
}

// bytes is what the window costs in the cache, counted from the request
// rather than from what was read: the accounting has to hold before the
// fetch, since that is when the request is admitted or refused.
func (inst windowKey) bytes(format pcm.Format) (n int64) {
	return (inst.to - inst.from) * int64(format.Channels) * sampleBytes
}

// requestLocked puts key in the single-slot mailbox, replacing whatever was
// queued but not started. A window already in flight, already queued, or
// inside its failure backoff is left alone.
func (inst *windowCache) requestLocked(key windowKey) {
	if inst.closed {
		return
	}
	if inst.hasInflight && inst.inflight == key {
		return
	}
	if inst.hasQueued && inst.queued == key {
		return
	}
	until, failed := inst.failed[key]
	if failed {
		if time.Now().Before(until) {
			return
		}
		delete(inst.failed, key)
	}
	inst.queued, inst.hasQueued = key, true
	if !inst.started {
		inst.started = true
		go inst.worker()
	}
	inst.cond.Signal()
}

func (inst *windowCache) pending() (yes bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.hasQueued || inst.hasInflight
}

func (inst *windowCache) stats() (entries int, bytes int64, hits uint64, misses uint64, fetches uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.items), inst.bytes, inst.hits, inst.misses, inst.fetches
}

func (inst *windowCache) worker() {
	defer close(inst.done)
	for {
		inst.mu.Lock()
		for !inst.hasQueued && !inst.closed {
			inst.cond.Wait()
		}
		if inst.closed {
			inst.mu.Unlock()
			return
		}
		key := inst.queued
		inst.hasQueued = false
		inst.inflight, inst.hasInflight = key, true
		inst.fetches++
		inst.mu.Unlock()

		samples, err := inst.fetchE(key)

		inst.mu.Lock()
		inst.hasInflight = false
		if err == nil {
			inst.putLocked(key, samples)
		} else {
			inst.failed[key] = time.Now().Add(windowFailBackoff)
		}
		inst.mu.Unlock()

		if err != nil && inst.ctx.Err() == nil {
			log.Warn().Err(err).
				Int64("fromFrame", key.from).
				Int64("toFrame", key.to).
				Msg("unable to fetch a raw audio window; dropping the request")
		}
	}
}

func (inst *windowCache) fetchE(key windowKey) (samples []float32, err error) {
	channels := int(inst.format.Channels)
	dst := make([]float32, (key.to-key.from)*int64(channels))
	n, err := readWindowE(inst.ctx, inst.src, inst.format, inst.frames, key.from, dst)
	if err != nil {
		return nil, err
	}
	return dst[:n*channels], nil
}

// putLocked inserts the window and evicts the least recently used entries
// until the byte bound holds again. The new entry is at the front, so it
// survives its own insertion.
func (inst *windowCache) putLocked(key windowKey, samples []float32) {
	el, exists := inst.items[key]
	if exists {
		entry := el.Value.(*windowEntry)
		inst.bytes += int64(len(samples)-len(entry.samples)) * sampleBytes
		entry.samples = samples
		inst.order.MoveToFront(el)
		return
	}
	inst.items[key] = inst.order.PushFront(&windowEntry{key: key, samples: samples})
	inst.bytes += int64(len(samples)) * sampleBytes
	for inst.bytes > inst.maxBytes && inst.order.Len() > 1 {
		entry := inst.order.Remove(inst.order.Back()).(*windowEntry)
		delete(inst.items, entry.key)
		inst.bytes -= int64(len(entry.samples)) * sampleBytes
	}
}

// close stops the worker and waits for it, so that no read is in flight
// through the source by the time the caller closes that source. Cached
// windows stay readable — a caller still holding one is not left with a
// dangling slice.
func (inst *windowCache) close() {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	inst.hasQueued = false
	started := inst.started
	inst.mu.Unlock()

	inst.cancel()
	inst.cond.Broadcast()
	if started {
		<-inst.done
	}
}
