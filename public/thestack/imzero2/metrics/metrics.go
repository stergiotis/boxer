//go:build llm_generated_opus47

package metrics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
)

// FrameBudgetNs is the wall-clock budget for a single 60 Hz frame.
const FrameBudgetNs int64 = 16_666_667

// SlowFrameThresholdNs is the real-work budget at or above which
// [FrameMetrics.RecordBytes] emits a structured warning. "Real work" is the
// Go-side widget build (render) plus the Rust-side interpret — the two slots
// the app can actually regress. Sync wait is deliberately excluded; see
// [shouldWarnSlowFrame] for why. Set to 1.5 × the 60 Hz frame budget so
// jitter that stays inside vsync slack stays quiet, but a frame whose work
// missed its deadline surfaces with its breakdown (render_us / sync_us /
// interpret_us / written_b / read_b / frame).
//
// The log line is intentionally emitted from RecordBytes — last call in
// the frame lifecycle — so the timings (set by EndFrame just before) and
// the wire byte counters (set inside RecordBytes itself) line up against
// the same frame. Use case is stutter triage: a run of log lines whose
// render_us or interpret_us is elevated names the slot — Go-render or
// Rust-interpret — that overran. Zero disables.
const SlowFrameThresholdNs int64 = 25_000_000

// slowFrameTopNScopes is the number of scope-hint entries (highest
// bytes first) included in the slow-frame log line. 4 is enough to
// name the dominant contributors without making the log line wider
// than the rest of its fields combined; the full table remains
// available via [runtime.ScopeHintsSnapshot] for callers that want it.
const slowFrameTopNScopes = 4

// emaAlpha controls the responsiveness of the smoothed values displayed in
// the overlay. 0.1 ≈ 10-frame effective window — stable enough to read at
// 60 Hz, responsive enough that a regression shows up within ~150 ms.
const emaAlpha float64 = 0.1

// fpsWindowFrames is the size of the sliding window backing the overlay's
// frame-rate distribution. 240 ≈ 4 s at 60 Hz — long enough for stable
// quantiles, short enough that during interaction the window is all-active
// so idle-heartbeat frames (reactive cadence) don't dominate the median.
const fpsWindowFrames = 240

// fpsDigestRefreshFrames is how often the windowed t-digest is rebuilt from
// the ring. The distribution barely moves frame-to-frame, so rebuilding
// every 6 frames (~10 Hz) keeps the overlay current at negligible cost; the
// overlay reads the persistent digest every frame regardless, so the
// 5-number anchor never looks stale.
const fpsDigestRefreshFrames = 6

// FrameMetrics holds per-frame counters captured on the Go side. The struct
// is single-threaded by construction: the imzero2 frame loop runs in one
// goroutine, so no synchronisation is required.
//
// The lifecycle of a single frame is:
//
//	BeginFrame()   — at StartServersideFrame, stamps tStart
//	BeforeSync()   — after user widget code, before End/Reset/Sync
//	EndFrame()     — after Sync returns, commits render/sync deltas
//	RecordBytes()  — from the application loop, after RenderLoopHandler
//	                  returned, captures wire bytes for the just-finished frame
//
// All "Last*" fields hold the most recently committed frame; the overlay
// renders these at the *next* frame's MenuBar (one-frame display lag,
// invisible at 60 Hz).
type FrameMetrics struct {
	tStart       time.Time
	tBeforeSync  time.Time
	frameCounter uint64

	LastRenderNs    int64
	LastSyncNs      int64
	LastTotalNs     int64
	LastWritten     int64
	LastRead        int64
	LastInterpretNs int64
	LastPassNr      uint64

	EmaRenderNs    float64
	EmaSyncNs      float64
	EmaTotalNs     float64
	EmaWritten     float64
	EmaRead        float64
	EmaInterpretNs float64

	emaInitialized bool

	// Sliding-window frame-rate distribution. Raw per-frame fps samples live
	// in a fixed-N ring; fpsDigest is rebuilt from the ring every
	// fpsDigestRefreshFrames and handed to the overlay's distsummary widget.
	// The ring evicts the oldest sample exactly, so Min/Max/Count over the
	// digest stay windowed — the recency a weight-decaying digest can't give
	// (merged centroids can't be un-merged, and its extrema never age out).
	fpsRing      []float64
	fpsRingIdx   int
	fpsRingLen   int
	fpsRefreshCt int
	fpsDigest    *tdigest.TDigest
}

func NewFrameMetrics() *FrameMetrics {
	return &FrameMetrics{
		fpsRing:   make([]float64, fpsWindowFrames),
		fpsDigest: tdigest.NewTDigest(),
	}
}

// Current is the singleton consumed by the overlay widget and written to
// from the frame loop. Zero-value usable via NewFrameMetrics.
var Current = NewFrameMetrics()

func (inst *FrameMetrics) BeginFrame() {
	inst.tStart = time.Now()
}

func (inst *FrameMetrics) BeforeSync() {
	inst.tBeforeSync = time.Now()
}

// EndFrame commits the timing deltas measured between BeginFrame,
// BeforeSync, and now. Tolerant of a missing BeforeSync stamp: if
// BeforeSync was never called for this frame (e.g. an early-return path
// in the renderer) render time is reported as the full frame and sync
// time as zero, rather than spuriously spiking the display.
func (inst *FrameMetrics) EndFrame() {
	tEnd := time.Now()
	if inst.tStart.IsZero() {
		return
	}
	totalNs := tEnd.Sub(inst.tStart).Nanoseconds()
	var renderNs, syncNs int64
	if inst.tBeforeSync.IsZero() || inst.tBeforeSync.Before(inst.tStart) {
		renderNs = totalNs
		syncNs = 0
	} else {
		renderNs = inst.tBeforeSync.Sub(inst.tStart).Nanoseconds()
		syncNs = tEnd.Sub(inst.tBeforeSync).Nanoseconds()
	}
	inst.LastRenderNs = renderNs
	inst.LastSyncNs = syncNs
	inst.LastTotalNs = totalNs
	inst.frameCounter++

	if !inst.emaInitialized {
		inst.EmaRenderNs = float64(renderNs)
		inst.EmaSyncNs = float64(syncNs)
		inst.EmaTotalNs = float64(totalNs)
		inst.EmaWritten = float64(inst.LastWritten)
		inst.EmaRead = float64(inst.LastRead)
		inst.emaInitialized = true
	} else {
		inst.EmaRenderNs = ema(inst.EmaRenderNs, float64(renderNs))
		inst.EmaSyncNs = ema(inst.EmaSyncNs, float64(syncNs))
		inst.EmaTotalNs = ema(inst.EmaTotalNs, float64(totalNs))
	}

	// Feed the sliding-window frame-rate distribution with this frame's raw
	// instantaneous fps (not the EMA): the window's quantiles then capture
	// true frame-to-frame spread, and a single slow frame lands in the
	// max/p99 tail instead of dragging a smoothed scalar — the failure mode
	// of the former 1/EMA(period) readout.
	if totalNs > 0 {
		inst.pushFps(1e9 / float64(totalNs))
	}

	inst.tStart = time.Time{}
	inst.tBeforeSync = time.Time{}
}

// pushFps records one frame's instantaneous fps into the sliding window and
// rebuilds the windowed digest every fpsDigestRefreshFrames. The ring is a
// plain circular buffer; a t-digest is order-insensitive, so the rebuild
// just repushes the live entries. Lazily initialises its backing storage so
// a zero-value FrameMetrics stays usable.
func (inst *FrameMetrics) pushFps(fps float64) {
	if inst.fpsRing == nil {
		inst.fpsRing = make([]float64, fpsWindowFrames)
	}
	inst.fpsRing[inst.fpsRingIdx] = fps
	inst.fpsRingIdx++
	if inst.fpsRingIdx >= len(inst.fpsRing) {
		inst.fpsRingIdx = 0
	}
	inst.fpsRingLen = min(inst.fpsRingLen+1, len(inst.fpsRing))
	inst.fpsRefreshCt++
	if inst.fpsRefreshCt >= fpsDigestRefreshFrames {
		inst.fpsRefreshCt = 0
		inst.rebuildFpsDigest()
	}
}

// rebuildFpsDigest resets the persistent digest and repushes the current
// window. Reset + repush of ≤fpsWindowFrames points is microseconds and,
// unlike weight-decay, yields exact windowed Min/Max/Count for the
// distsummary 5-number readout.
func (inst *FrameMetrics) rebuildFpsDigest() {
	if inst.fpsDigest == nil {
		inst.fpsDigest = tdigest.NewTDigest()
	}
	inst.fpsDigest.Reset()
	for i := range inst.fpsRingLen {
		inst.fpsDigest.Push(inst.fpsRing[i])
	}
}

// FpsDigest returns the windowed frame-rate distribution for the overlay to
// hand to a distsummary widget. The pointer is stable across frames; the
// digest is rebuilt in place by rebuildFpsDigest. Single-threaded with the
// frame loop (same goroutine), so no synchronisation is required.
func (inst *FrameMetrics) FpsDigest() *tdigest.TDigest {
	if inst.fpsDigest == nil {
		inst.fpsDigest = tdigest.NewTDigest()
	}
	return inst.fpsDigest
}

// RecordBytes is called from the outer frame loop once the just-finished
// frame's wire counters can be sampled (after RenderLoopHandler returned
// and Sync drained the inbound register fetches).
//
// When the frame's real work (render + interpret) crosses
// [SlowFrameThresholdNs] — see [shouldWarnSlowFrame] — a structured warning
// is emitted with the full per-frame breakdown, total_us included so the
// excluded sync wait stays visible during triage.
func (inst *FrameMetrics) RecordBytes(written int, read int) {
	inst.LastWritten = int64(written)
	inst.LastRead = int64(read)
	if inst.emaInitialized {
		inst.EmaWritten = ema(inst.EmaWritten, float64(written))
		inst.EmaRead = ema(inst.EmaRead, float64(read))
	}
	if shouldWarnSlowFrame(inst.LastRenderNs, inst.LastInterpretNs, SlowFrameThresholdNs) {
		log.Warn().
			Uint64("frame", inst.frameCounter).
			Int64("render_us", inst.LastRenderNs/1000).
			Int64("sync_us", inst.LastSyncNs/1000).
			Int64("total_us", inst.LastTotalNs/1000).
			Int64("interpret_us", inst.LastInterpretNs/1000).
			Int64("written_b", inst.LastWritten).
			Int64("read_b", inst.LastRead).
			Str("top_scopes", formatTopScopes(runtime.ScopeHintsSnapshot(), slowFrameTopNScopes)).
			Msg("imzero2: slow frame")
	}
}

// shouldWarnSlowFrame reports whether a frame's real work — Go-side widget
// build (renderNs) plus Rust-side interpret (interpretNs) — crossed
// thresholdNs. It deliberately ignores sync wait: when a window is occluded
// or simply idle the compositor throttles vblank delivery (≈1 Hz on Wayland),
// and with vsync the Go frame loop blocks in Sync for that whole interval.
// That inflates total wall-clock to ~1 s while render and interpret stay in
// the low milliseconds — a display-pacing artifact, not a regression the app
// can act on. Gating on render+interpret keeps the warning tied to work.
// thresholdNs <= 0 disables the warning.
func shouldWarnSlowFrame(renderNs int64, interpretNs int64, thresholdNs int64) bool {
	return thresholdNs > 0 && renderNs+interpretNs > thresholdNs
}

// formatTopScopes returns "kind1=bytes1,kind2=bytes2,…" for up to n
// entries from snap, ordered by Bytes desc. Returns "" when snap is
// empty (e.g. before any deferred-block scope has been constructed).
// Allocated only on slow-frame log lines (rare by design), so the
// steady-state RecordBytes path stays allocation-free.
func formatTopScopes(snap []runtime.ScopeHintSnapshot, n int) string {
	if len(snap) == 0 {
		return ""
	}
	sort.Slice(snap, func(i, j int) bool { return snap[i].Bytes > snap[j].Bytes })
	if n > len(snap) {
		n = len(snap)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%d", snap[i].Kind, snap[i].Bytes)
	}
	return b.String()
}

// RecordRust commits the Rust-side per-frame metrics drained from the
// fetchFrameMetrics fetcher in StateManager.Sync. Reports the previous
// completed Rust frame's interpret_commands_outer elapsed (one-frame display
// lag, invisible at 60 Hz). interpretUs is microseconds; 0 means the first
// frame has not yet completed Rust-side.
func (inst *FrameMetrics) RecordRust(interpretUs uint64, passNr uint64) {
	ns := int64(interpretUs) * 1000
	inst.LastInterpretNs = ns
	inst.LastPassNr = passNr
	if inst.emaInitialized {
		inst.EmaInterpretNs = ema(inst.EmaInterpretNs, float64(ns))
	} else {
		inst.EmaInterpretNs = float64(ns)
	}
}

// Snapshot is an immutable view of the last completed frame, suitable for
// rendering. All ns values are smoothed via EMA; LastTotalNs is also
// included raw for callers that want the unsmoothed value. SlackNs is the
// vsync residual: TotalNs (Go-side wall clock = render + sync) minus
// InterpretNs (Rust compute). It captures how much of the 16.6 ms budget
// is spent waiting on the next vsync rather than on either side's work.
type Snapshot struct {
	FrameCounter   uint64
	RenderNs       int64
	SyncNs         int64
	TotalNs        int64
	RawTotalNs     int64
	InterpretNs    int64
	SlackNs        int64
	WrittenBytes   int64
	ReadBytes      int64
	RustPassNr     uint64
	BudgetFraction float64
}

func (inst *FrameMetrics) Snapshot() (s Snapshot) {
	s.FrameCounter = inst.frameCounter
	s.RenderNs = int64(inst.EmaRenderNs)
	s.SyncNs = int64(inst.EmaSyncNs)
	s.TotalNs = int64(inst.EmaTotalNs)
	s.RawTotalNs = inst.LastTotalNs
	s.InterpretNs = int64(inst.EmaInterpretNs)
	s.SlackNs = s.TotalNs - s.InterpretNs
	if s.SlackNs < 0 {
		s.SlackNs = 0
	}
	s.WrittenBytes = int64(inst.EmaWritten)
	s.ReadBytes = int64(inst.EmaRead)
	s.RustPassNr = inst.LastPassNr
	if FrameBudgetNs > 0 {
		s.BudgetFraction = float64(s.TotalNs) / float64(FrameBudgetNs)
	}
	return
}

func ema(prev float64, sample float64) (next float64) {
	next = prev + emaAlpha*(sample-prev)
	return
}
