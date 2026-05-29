//go:build llm_generated_opus47

// Package metricsoverlay renders frame-budget readouts suitable for
// embedding in a menu bar. Values reflect the previous completed frame
// (one-frame display lag, invisible at 60 Hz). Numbers are EMA-smoothed
// by the metrics package so the readout is stable enough to read at
// 60 Hz.
//
// The package composes Atoms + LabelAtoms with monospace styling so the
// fixed-width strings stay pixel-stable across frames.
package metricsoverlay

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// RenderInline renders a one-line frame-budget summary suitable for
// embedding in a menu bar.
//
// Layout (monospace, fixed column widths so the menu bar doesn't shimmy
// as values change):
//
//	Go XX.Xms  Rust XX.Xms  vsync XX.Xms  ↑XXXXXKB ↓XXXXXKB  XXX%/16.6ms  XX.Xfps
//
// The three time slots are honest about what they each measure:
//   - Go render: pure Go widget code time (StartServersideFrame → Sync entry)
//   - Rust interpret: Rust's interpret_commands_outer elapsed, drained via
//     the fetchFrameMetrics fetcher with one-frame lag
//   - vsync slack: TotalNs - InterpretNs — the residual that egui spends on
//     painting + the wall-clock wait for the next vsync boundary in
//     continuous-rendering mode
//
// Effective FPS is derived from TotalNs (1 / Go-side wall clock per frame).
// At a steady 60 Hz with vsync engaged this hovers around 60.0; if it
// deviates, either Go or Rust is missing the budget.
func RenderInline() {
	s := metrics.Current.Snapshot()
	renderMs := float64(s.RenderNs) / 1e6
	interpretMs := float64(s.InterpretNs) / 1e6
	slackMs := float64(s.SlackNs) / 1e6
	pct := s.BudgetFraction * 100.0
	fps := 0.0
	if s.TotalNs > 0 {
		fps = 1e9 / float64(s.TotalNs)
	}
	body := fmt.Sprintf("Go %5.1fms  Rust %5.1fms  vsync %5.1fms  ↑%s ↓%s  ",
		renderMs, interpretMs, slackMs,
		formatBytesFixed(s.WrittenBytes), formatBytesFixed(s.ReadBytes),
	)
	monoLabel(body, color.Color{}, false)
	monoLabel(fmt.Sprintf("%03.0f%%/16.6ms", pct), budgetColor(s.BudgetFraction), true)
	monoLabel(fmt.Sprintf("  %5.1ffps", fps), color.Color{}, false)
}

// monoLabel emits a single inline label with a monospace font. When
// `colored` is true the colour is applied via RichTextColored (transparent
// background); otherwise plain RichText is used. Fixed-width strings stay
// pixel-stable across frames as long as every callsite uses monospace.
func monoLabel(text string, col color.Color, colored bool) {
	a := c.Atoms()
	if colored {
		a = a.RichTextColored(text, col, color.Transparent)
	} else {
		a = a.RichText(text)
	}
	c.LabelAtoms(a.Monospace().EndRichText().Keep()).Send()
}

// formatBytesFixed always reports kilobytes with one decimal place so the
// rendered string is exactly seven characters wide regardless of magnitude
// (within typical per-frame ranges of a few KB to a few MB). Fixed-width
// matters more for layout stability than human-friendly unit selection.
func formatBytesFixed(n int64) (s string) {
	s = fmt.Sprintf("%5.1fKB", float64(n)/1024.0)
	return
}

// budgetColor maps a 0..1 frame-budget fraction to a traffic-light colour
// sourced from the IDS semantic palette (ADR-0031 §SD2):
//
//   - frac < 0.5  → Success (healthy headroom)
//   - frac < 0.8  → Warning (degraded; budget tightening)
//   - frac ≥ 0.8  → Error   (at or over budget)
//
// Saturation caps at 1.0 — any value past full budget reads the same
// Error red, matching the operator's mental model that "over budget is
// over budget."
func budgetColor(frac float64) (col color.Color) {
	switch {
	case frac < 0.5:
		col = color.Hex(styletokens.SuccessDefault.AsHex())
	case frac < 0.8:
		col = color.Hex(styletokens.WarningDefault.AsHex())
	default:
		col = color.Hex(styletokens.ErrorDefault.AsHex())
	}
	return
}
