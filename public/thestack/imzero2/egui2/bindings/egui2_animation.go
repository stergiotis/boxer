//go:build llm_generated_opus47

package bindings

// Animation helpers wrap the egui::Context::animate_* primitives. Each tween
// is keyed by `animId` (a stable uint64) so egui's AnimationManager remembers
// the from-value across frames. The current animated value (0..1 for bool
// variants, the interpolated target for value variants) is pushed into the
// r9_f64 register and copied into `*out` by the next StateManager.Sync call
// — i.e. one-frame lag. Call every frame with the desired target; egui drives
// the tween and schedules repaints automatically while it's in flight.

// AnimateBoolWithTimeBind animates a 0..1 value toward `target` over `durSecs`.
// Common use: gate two visual states by `*out` (0 = off, 1 = on, intermediate during the tween).
func AnimateBoolWithTimeBind(animId uint64, target bool, durSecs float32, out *float64) {
	AnimateBoolWithTime(animId, target, durSecs)
	CurrentApplicationState.StateManager.AddR9F64Databinding(animId, out)
}

// AnimateBoolResponsiveBind is like AnimateBoolWithTimeBind but uses egui's
// fast/slow responsive curve (snappier on transitions).
func AnimateBoolResponsiveBind(animId uint64, target bool, out *float64) {
	AnimateBoolResponsive(animId, target)
	CurrentApplicationState.StateManager.AddR9F64Databinding(animId, out)
}

// AnimateValueWithTimeBind tweens an arbitrary f32 value toward `target` over `durSecs`.
// Use when you need to interpolate a numeric quantity (e.g. a layout coordinate)
// rather than a 0..1 state.
func AnimateValueWithTimeBind(animId uint64, target float32, durSecs float32, out *float64) {
	AnimateValueWithTime(animId, target, durSecs)
	CurrentApplicationState.StateManager.AddR9F64Databinding(animId, out)
}

// MeasureTextBind asks egui to lay out `text` in the given font and writes
// the resulting pixel width into `*out` on the next Sync (one-frame lag).
// Call every frame with a stable `measureId` (derived from the text you
// care about) so that the databinding refreshes if text or font changes.
//
// Typical use: axis labels in legend widgets that need to place tick
// labels without overlap, or overlap-aware tick selection.
func MeasureTextBind(measureId uint64, text string, fontSize float32, monospace bool, out *float64) {
	MeasureText(measureId, text, fontSize, monospace)
	CurrentApplicationState.StateManager.AddR9F64Databinding(measureId, out)
}

// MeasureTextSizeBind is MeasureTextBind's two-extent sibling: one layout
// pass, width into *outW and height into *outH on the next Sync (one-frame
// lag). Either out pointer may be nil to skip that binding — the Rust side
// still pushes both values; an id nobody bound is simply never read.
//
// The height of a single non-wrapped line is the font's row height,
// independent of the text content, so callers sizing text-bearing cells can
// measure a short probe string once per (fontSize, monospace) and reuse the
// height for any single-line label in that style (the treemap label gates).
func MeasureTextSizeBind(widthMeasureId, heightMeasureId uint64, text string, fontSize float32, monospace bool, outW, outH *float64) {
	MeasureTextSize(widthMeasureId, heightMeasureId, text, fontSize, monospace)
	sm := CurrentApplicationState.StateManager
	if outW != nil {
		sm.AddR9F64Databinding(widthMeasureId, outW)
	}
	if outH != nil {
		sm.AddR9F64Databinding(heightMeasureId, outH)
	}
}
