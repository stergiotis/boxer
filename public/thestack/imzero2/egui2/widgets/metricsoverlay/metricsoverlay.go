// Package metricsoverlay renders frame-timing readouts suitable for
// embedding in a menu or status bar. The time/byte values reflect the
// previous completed frame (one-frame display lag, invisible at 60 Hz) and
// are EMA-smoothed by the metrics package so they are stable enough to read
// at 60 Hz. The frame rate is instead surfaced as a windowed distribution
// (a distsummary 5-number anchor over the metrics package's sliding window)
// so its median stays bias-free and slow frames show up as a tail rather
// than dragging a smoothed average — see [RenderInline].
//
// The package composes Atoms + LabelAtoms with monospace styling so the
// fixed-width strings stay pixel-stable across frames.
package metricsoverlay

import (
	"fmt"
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/distsummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// fpsDist is the configure-once distsummary template for the windowed
// frame-rate distribution. An integer-fps formatter keeps the inline
// 5-number summary compact in the status bar; the anchor toggle opens the
// ECDF / letter-value inspector over the same window for the full picture.
// Inline: RenderInline runs inside the host chrome's own status-bar
// c.Horizontal (see host/chrome.go), so the anchor emits straight into that
// row. Left to wrap itself, distsummary's nested horizontal would seat the
// fps summary a few px below the neighbouring Go/Rust/vsync readout (the
// "Ragged Control Row" note in imzero2 SKILL.md).
var fpsDist = distsummary.New("fps").Format(formatFps).Unit("fps").Inline()

// formatFps renders one fps quantile for the compact inline summary. Whole
// numbers read cleanly at a glance in the bar; the inspector window carries
// full precision.
func formatFps(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// RenderInline renders a one-line frame-budget summary suitable for
// embedding in a menu or status bar. fpsId is a prepared id creator for the
// embedded frame-rate distsummary anchor (consumed once via Derive).
//
// Layout (monospace, fixed column widths so the bar doesn't shimmy as values
// change), followed by the frame-rate distribution anchor:
//
//	Go XX.Xms  Rust XX.Xms  vsync XX.Xms  ↑XXXXXKB ↓XXXXXKB  n=N p0 .. p50 .. p100 .. fps
//
// The three time slots are honest about what they each measure:
//   - Go render: pure Go widget code time (StartServersideFrame → Sync entry)
//   - Rust interpret: Rust's interpret_commands_outer elapsed, drained via
//     the fetchFrameMetrics fetcher with one-frame lag
//   - vsync slack: TotalNs - InterpretNs — the residual that egui spends on
//     painting + the wall-clock wait for the next vsync boundary in
//     continuous-rendering mode
//
// Frame rate is a windowed distribution — a distsummary 5-number anchor over
// the last fpsWindowFrames frames — rather than a single 1/EMA(period)
// scalar: the median is bias-free and stable, while a slow frame surfaces in
// the max/p99 tail instead of dragging the headline number down for the
// EMA's recovery window. Clicking the anchor opens the ECDF / letter-value
// inspector over the same window. The window and digest are owned by the
// metrics package (see metrics.FrameMetrics.FpsDigest).
func RenderInline(fpsId c.WidgetIdCreatorI) {
	s := metrics.Current.Snapshot()
	renderMs := float64(s.RenderNs) / 1e6
	interpretMs := float64(s.InterpretNs) / 1e6
	slackMs := float64(s.SlackNs) / 1e6
	// The trailing two spaces are the gap before the frame-rate anchor; the
	// distsummary widget renders the chart-line icon, the percentile-labelled
	// summary, the "fps" unit, and the inspector toggle itself.
	body := fmt.Sprintf("Go %5.1fms  Rust %5.1fms  vsync %5.1fms  ↑%s ↓%s  ",
		renderMs, interpretMs, slackMs,
		formatBytesFixed(s.WrittenBytes), formatBytesFixed(s.ReadBytes),
	)
	monoLabel(body, color.Color{}, false)
	fpsDist.Render(fpsId, metrics.Current.FpsDigest(), nil)
}

// monoLabel emits a single inline label with a monospace font. When
// `colored` is true the colour is applied via RichTextColored (transparent
// background); otherwise plain RichText is used. Fixed-width strings stay
// pixel-stable across frames as long as every callsite uses monospace.
func monoLabel(text string, col color.Color, colored bool) {
	a := c.Atoms()
	var rt c.RichTextScope
	if colored {
		rt = a.BeginRichTextColored(col, color.Transparent, text)
	} else {
		rt = a.BeginRichText(text)
	}
	c.LabelAtoms(rt.Monospace().End().Keep()).Send()
}

// formatBytesFixed always reports kilobytes with one decimal place so the
// rendered string is exactly seven characters wide regardless of magnitude
// (within typical per-frame ranges of a few KB to a few MB). Fixed-width
// matters more for layout stability than human-friendly unit selection.
func formatBytesFixed(n int64) (s string) {
	s = fmt.Sprintf("%5.1fKB", float64(n)/1024.0)
	return
}
