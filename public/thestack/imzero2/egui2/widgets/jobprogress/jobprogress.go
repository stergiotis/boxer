//go:build llm_generated_opus47

// Package jobprogress is a small, stateless widget that renders the
// progress of a background job — an optional title, a progress bar, and
// a humanized "…% · Ns left" status line — for embedding inline beneath
// the thing the job is computing (e.g. below a plot whose band is still
// being solved). It is deliberately generic: callers map their own job
// state onto Input, so the widget has no dependency on the keelson task
// framework or any particular producer.
package jobprogress

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Input is the per-frame render state for one job's progress row.
type Input struct {
	// Title is the short label shown above the bar (e.g. "computing
	// confidence band"). Empty hides the title line.
	Title string
	// Fraction is progress in [0,1]. A negative value renders an
	// indeterminate (animated) bar with no percentage or ETA.
	Fraction float32
	// EtaMs is the estimated milliseconds remaining; ≤0 means unknown
	// and is omitted from the status line. Ignored for indeterminate.
	EtaMs int64
	// Note is optional trailing status text (e.g. an error reason).
	Note string
}

// Render emits the progress row in the current layout, top to bottom:
// title, bar, status. Stateless — call once per frame with the latest
// Input, inside an active layout scope (the caller controls placement,
// e.g. after a c.Plot block plus a c.AddSpace).
func Render(in Input) {
	if in.Title != "" {
		c.Label(in.Title).Send()
	}
	if in.Fraction < 0 {
		c.ProgressBar(0).Animate(true).Send()
	} else {
		c.ProgressBar(clampUnit(in.Fraction)).Send()
	}
	if status := statusLine(in); status != "" {
		c.Label(status).Send()
	}
}

func clampUnit(f float32) float32 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}

// statusLine composes "47% · 2m05s left · <note>", dropping the parts
// that aren't applicable (no percentage when indeterminate, no ETA when
// unknown, no note when empty).
func statusLine(in Input) (s string) {
	if in.Fraction >= 0 {
		s = fmt.Sprintf("%d%%", int(in.Fraction*100))
		if in.EtaMs > 0 {
			s += " · " + formatDurationMs(in.EtaMs) + " left"
		}
	}
	if in.Note != "" {
		if s != "" {
			s += " · " + in.Note
		} else {
			s = in.Note
		}
	}
	return
}

// formatDurationMs renders a compact duration in the taskmonitor house
// format: "850ms", "12s", "3m05s".
func formatDurationMs(ms int64) (s string) {
	switch {
	case ms < 1000:
		s = fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		s = fmt.Sprintf("%ds", ms/1000)
	default:
		s = fmt.Sprintf("%dm%02ds", ms/60_000, (ms%60_000)/1000)
	}
	return
}
