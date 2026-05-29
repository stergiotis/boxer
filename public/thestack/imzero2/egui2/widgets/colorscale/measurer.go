//go:build llm_generated_opus47

package colorscale

import (
	"fmt"
	"hash/fnv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// cachingMeasurer is a TextMeasurerI backed by the egui MeasureText FFFI
// binding, with a per-text cache so each unique (text, fontSize, monospace)
// triple is measured at most once.
//
// # Lifecycle
//
// MeasureText returns widths via a Go-side databinding that's populated
// by the next StateManager.Sync — i.e. the value arrives ONE FRAME after
// the call. That doesn't compose with synchronous Talbot scoring, so:
//
//  1. First encounter of a label: store an approximation (0.6 × fontSize
//     × len) in the cache, queue a MeasureTextBind for the real width,
//     and flag AnyNew so the colorscale knows to invalidate its cached
//     axis next frame.
//  2. Next frame: Sync has copied the real width into the cache entry.
//     Talbot re-runs and sees real pixel widths. AnyNew is false this time
//     (cache hit), so the axis is stable from then on.
//  3. Each frame, RenewBindings re-registers the databinding for every
//     entry seen during the frame's Talbot run, so the cache entry stays
//     current across Sync's databind-reset semantics.
type cachingMeasurer struct {
	cache         map[string]*measurement
	seenThisFrame map[string]bool
	anyNew        bool
}

type measurement struct {
	text      string
	fontSize  float32
	monospace bool
	measureId uint64
	width     float64 // pointer-bound; written by Sync via MeasureTextBind
}

func newCachingMeasurer() *cachingMeasurer {
	return &cachingMeasurer{
		cache:         make(map[string]*measurement, 64),
		seenThisFrame: make(map[string]bool, 16),
	}
}

// MeasureSingleLine implements finddivisions.TextMeasurerI. Called
// synchronously by Talbot's scorer; returns the cached width (or an
// approximation on a cache miss).
func (m *cachingMeasurer) MeasureSingleLine(s string, fontSizePt, dpi float64) float64 {
	fontSize := float32(fontSizePt)
	key := measurementKey(s, fontSize, false)
	e, ok := m.cache[key]
	if !ok {
		e = &measurement{
			text:      s,
			fontSize:  fontSize,
			monospace: false,
			measureId: hashKey(key),
			width:     approxWidth(s, fontSizePt, dpi),
		}
		m.cache[key] = e
		m.anyNew = true
	}
	m.seenThisFrame[key] = true
	return e.width
}

// RenewBindings registers a MeasureTextBind call for every entry seen
// during the frame's scorer invocations. Call once per frame AFTER the
// Talbot run so the FFFI traffic is bounded by the live tick set rather
// than every string ever cached.
func (m *cachingMeasurer) RenewBindings() {
	for key := range m.seenThisFrame {
		e := m.cache[key]
		c.MeasureTextBind(e.measureId, e.text, e.fontSize, e.monospace, &e.width)
	}
	// Reset the "seen" set for the next frame; the underlying `cache`
	// stays so future lookups still hit even if a label is briefly absent.
	for k := range m.seenThisFrame {
		delete(m.seenThisFrame, k)
	}
}

// AnyNew reports (and resets) whether at least one cache miss occurred
// during the most recent scorer invocations. When true, the caller should
// invalidate any cached axis on the next frame — the widths used by Talbot
// this frame are approximate, and real values will arrive after Sync.
func (m *cachingMeasurer) AnyNew() bool {
	b := m.anyNew
	m.anyNew = false
	return b
}

// measurementKey builds a stable string key for cache lookup.
func measurementKey(text string, fontSize float32, monospace bool) string {
	return fmt.Sprintf("%s|%v|%v", text, fontSize, monospace)
}

// hashKey derives a 64-bit measureId from the cache key using FNV-1a.
// Collisions are benign — they'd cause two distinct strings to share a
// databinding and write the same value, which just means one overwrites
// the other's width. For short-lived tick labels this is statistically
// negligible and the consequences are cosmetic.
func hashKey(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

// approxWidth is the same heuristic the old approxMeasurer used: 0.6 ×
// fontSize × len, with a DPI correction applied by the scorer's
// pt-to-pixel conversion.
func approxWidth(s string, fontSizePt, dpi float64) float64 {
	pxPerPt := dpi / 72.0
	return float64(len(s)) * 0.6 * fontSizePt * pxPerPt
}
