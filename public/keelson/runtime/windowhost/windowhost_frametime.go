package windowhost

import (
	"slices"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
)

// Every open window's Frame runs on the render goroutine, one after another
// (ADR-0261), so a slow app delays every window and the chrome with it. The
// host times each call so the app center can say which app that is.
//
// A sample is the wall time of one app's Frame call and the FFFI messages it
// produced. The wall time is Go-side: building the frame's messages, and
// writing any that go straight to the pipe. It does not include the client's
// interpretation and paint, which happen in another process. Mount is timed
// once, apart, because it runs on the same goroutine the first time a
// window is drawn.

// frameRingLen is how many recent samples the quantiles are over: a few
// seconds at the continuous cadence, longer under the reactive one.
const frameRingLen = 256

// FrameScopeE says what a FrameTimeInfo row measures.
type FrameScopeE uint8

const (
	// FrameScopeWindow is one window's Frame call.
	FrameScopeWindow FrameScopeE = iota
	// FrameScopeLoop is the render loop's period: start of one host Frame
	// to the start of the next. It is the budget the window rows spend
	// from, and it includes what they do not — the chrome, the client's
	// paint, the wait for its reply, and any idle time the render cadence
	// leaves between frames.
	FrameScopeLoop
)

func (inst FrameScopeE) String() string {
	switch inst {
	case FrameScopeWindow:
		return "window"
	case FrameScopeLoop:
		return "loop"
	}
	return "unknown"
}

// FrameTimeInfo is one row of keelson('frame_times').
type FrameTimeInfo struct {
	Scope FrameScopeE
	// Key and AppId are zero for the loop row.
	Key   WindowKeyT
	AppId app.AppIdT
	// Frames counts every sample since the window opened; Samples those the
	// quantiles below are over.
	Frames  uint64
	Samples int
	// Total is the sum over every sample since the window opened.
	Total time.Duration
	Last  time.Duration
	Mean  time.Duration
	P50   time.Duration
	P95   time.Duration
	Max   time.Duration
	// MessagesLast and MessagesMean count FFFI messages per sample, over the
	// same recent samples as the quantiles.
	MessagesLast uint64
	MessagesMean float64
	// Mount is how long the app's Mount took on the render goroutine; zero
	// for a window that shares an already mounted instance, and for the loop
	// row.
	Mount time.Duration
}

type frameSample struct {
	dur  time.Duration
	msgs uint64
}

// frameRing holds one row's recent samples and running totals.
type frameRing struct {
	appId  app.AppIdT
	buf    [frameRingLen]frameSample
	n      int
	next   int
	frames uint64
	total  time.Duration
	last   frameSample
	mount  time.Duration
}

func (inst *frameRing) add(s frameSample) {
	inst.buf[inst.next] = s
	inst.next = (inst.next + 1) % frameRingLen
	if inst.n < frameRingLen {
		inst.n++
	}
	inst.frames++
	inst.total += s.dur
	inst.last = s
}

func (inst *frameRing) info(scope FrameScopeE, key WindowKeyT) (r FrameTimeInfo) {
	r = FrameTimeInfo{
		Scope: scope, Key: key, AppId: inst.appId,
		Frames: inst.frames, Samples: inst.n, Total: inst.total,
		Last: inst.last.dur, MessagesLast: inst.last.msgs, Mount: inst.mount,
	}
	if inst.n == 0 {
		return
	}
	durs := make([]time.Duration, inst.n)
	var sum time.Duration
	var msgs uint64
	for i := range inst.n {
		durs[i] = inst.buf[i].dur
		sum += inst.buf[i].dur
		msgs += inst.buf[i].msgs
	}
	slices.Sort(durs)
	r.Mean = sum / time.Duration(inst.n)
	r.P50 = durs[(inst.n-1)/2]
	r.P95 = durs[(inst.n-1)*95/100]
	r.Max = durs[inst.n-1]
	r.MessagesMean = float64(msgs) / float64(inst.n)
	return
}

// frameTimes is the host's recorder. The render goroutine writes it, and an
// introspection read on another goroutine snapshots it, hence the mutex; the
// render goroutine is its only writer, so the lock is uncontended but for
// the moment of a read.
type frameTimes struct {
	mu      sync.Mutex
	windows map[WindowKeyT]*frameRing
	loop    frameRing

	// The render goroutine's own fields: no lock.
	loopStart time.Time
	loopMsgs  uint64
}

// frameMessages is the FFFI message counter the samples difference, 0
// before the channel exists (a test drives Frame without one).
func frameMessages() uint64 {
	if f := typed.GetCurrentFffiVar(); f != nil {
		return f.Messages()
	}
	return 0
}

// beginLoop records the period since the previous host Frame.
func (inst *frameTimes) beginLoop(now time.Time) {
	msgs := frameMessages()
	if !inst.loopStart.IsZero() {
		s := frameSample{dur: now.Sub(inst.loopStart), msgs: msgs - inst.loopMsgs}
		inst.mu.Lock()
		inst.loop.add(s)
		inst.mu.Unlock()
	}
	inst.loopStart, inst.loopMsgs = now, msgs
}

func (inst *frameTimes) ring(w *window) (r *frameRing) {
	if inst.windows == nil {
		inst.windows = make(map[WindowKeyT]*frameRing)
	}
	r = inst.windows[w.key]
	if r == nil {
		r = &frameRing{appId: w.manifest.Id}
		inst.windows[w.key] = r
	}
	return
}

func (inst *frameTimes) recordMount(w *window, d time.Duration) {
	inst.mu.Lock()
	inst.ring(w).mount = d
	inst.mu.Unlock()
}

func (inst *frameTimes) recordFrame(w *window, d time.Duration, msgs uint64) {
	inst.mu.Lock()
	inst.ring(w).add(frameSample{dur: d, msgs: msgs})
	inst.mu.Unlock()
}

// forget drops a reaped window's row: the table is about open windows.
func (inst *frameTimes) forget(key WindowKeyT) {
	inst.mu.Lock()
	delete(inst.windows, key)
	inst.mu.Unlock()
}

// FrameTimes snapshots the recorder: the loop row first, then one row per
// open window that has drawn, by window key. Safe from any goroutine.
func (inst *Inst) FrameTimes() (out []FrameTimeInfo) {
	ft := &inst.frameTimes
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out = make([]FrameTimeInfo, 0, len(ft.windows)+1)
	out = append(out, ft.loop.info(FrameScopeLoop, 0))
	keys := make([]WindowKeyT, 0, len(ft.windows))
	for k := range ft.windows {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		out = append(out, ft.windows[k].info(FrameScopeWindow, k))
	}
	return
}
