// Package metricsoverlay is an immediate-mode widget (ADR-0267) rendering
// frame-timing readouts for embedding in a menu or status bar. The time/byte values reflect the
// previous completed frame (one-frame display lag, invisible at 60 Hz) and
// are EMA-smoothed by the metrics package so they are stable enough to read
// at 60 Hz. The frame rate is instead surfaced as a windowed distribution
// (a distsummary 5-number anchor over the metrics package's sliding window)
// so its median stays bias-free and slow frames show up as a tail rather
// than dragging a smoothed average — see [Render].
//
// The package composes Atoms + LabelAtoms with monospace styling so the
// fixed-width strings stay pixel-stable across frames.
package metricsoverlay

import (
	"fmt"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/distsummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// Input places the readout. The frame-rate anchor is a distsummary whose
// inspector state the host owns.
type Input struct {
	// Ids is the host's widget id stack; Render opens one IdScope under it.
	// ScopeKey names the readout within it; empty uses "metrics".
	Ids      *c.WidgetIdStack
	ScopeKey string
	// Fps is the frame-rate distsummary's host-owned state (pinned
	// inspector, tab). Required.
	Fps *distsummary.State
}

// Result is what one Render reports.
type Result struct {
	// FpsPinned is whether the frame-rate inspector window is open.
	FpsPinned bool
	// Err is a host programming error — a nil Ids or Fps.
	Err error
}

// formatFps renders one fps quantile for the compact inline summary. Whole
// numbers read cleanly at a glance in the bar; the inspector window carries
// full precision.
func formatFps(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// Render renders a one-line frame-budget summary suitable for embedding in
// a menu or status bar. It runs inside the host chrome's own status-bar
// c.Horizontal, so the frame-rate anchor emits straight into that row
// (distsummary's Inline): left to wrap itself, its nested horizontal would
// seat the fps summary a few px below the neighbouring readout (the
// "Ragged Control Row" note in the imzero2 skill).
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
func Render(in Input) (res Result) {
	if in.Ids == nil || in.Fps == nil {
		res.Err = eh.Errorf("metricsoverlay: Input.Ids and Input.Fps are required")
		monoLabel("metricsoverlay: missing Ids or Fps", color.Color{}, false)
		return
	}
	if in.ScopeKey == "" {
		in.ScopeKey = "metrics"
	}
	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
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
		dr := distsummary.Render(distsummary.Input{
			Ids: in.Ids, ScopeKey: "fps", Digest: metrics.Current.FpsDigest(), State: in.Fps,
			Title: "frame rate", Format: formatFps, Unit: "fps", Inline: true,
		})
		res.FpsPinned = dr.Pinned
	}
	return
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
