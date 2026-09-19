// Package jobprogress is a small, stateless widget that renders the
// progress of a background job — an optional title, a progress bar, and
// a humanized "47% · 1.2 k rows/s · 2m05s left" status line with an
// optional Cancel button — for embedding inline beneath (or beside) the
// thing the job is computing (e.g. below a plot whose band is still being
// solved). It is deliberately generic: callers map their own job state onto
// Input, so the widget has no dependency on the keelson task framework or
// any particular producer.
//
// The widget estimates nothing. A caller holding only a job's counters gets
// the rate and ETA from hmi/progressest's Tracker (ADR-0247), the estimator
// the keelson task framework and bgjob also use, so the figures match
// wherever the same job is shown; the spellings are progressest's too.
package jobprogress

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/hmi/progressest"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Input is the per-frame render state for one job's progress row.
type Input struct {
	// Title is the short label shown above the bar (e.g. "computing
	// confidence band"). Empty hides the title line. Not drawn Inline.
	Title string
	// Fraction is progress in [0,1]. A negative value renders an
	// indeterminate (animated) bar with no percentage or ETA.
	Fraction float32
	// EtaMs is the estimated milliseconds remaining; ≤0 means unknown
	// and is omitted from the status line. Ignored for indeterminate.
	EtaMs int64
	// Rate is the job's speed in RateUnit per second; ≤0 means unknown and
	// is omitted. Shown for indeterminate jobs too, where it is often the
	// only sign of life.
	Rate float64
	// RateUnit labels Rate: "rows" reads "1.2 k rows/s", "bytes" spells IEC
	// bytes ("18 MiB/s"), empty reads "1.2 k/s".
	RateUnit string
	// Amount, when non-empty, replaces the percentage at the head of the
	// status line — for a job whose counts say more than its percentage
	// (e.g. "1.2 M / 3.4 M rows").
	Amount string
	// Note is optional trailing status text (e.g. an error reason).
	Note string
	// CancelId, when non-nil, renders a compact "Cancel" button; Render then
	// returns true on the frame it is clicked. The id must be stable across
	// frames and unique among sibling widgets. Prefer a relative id off the
	// owning surface's per-instance id stack (e.g. ids.PrepareStr("cancel")):
	// it inherits the host's per-instance salt, so two concurrently-open
	// instances of the same surface cannot collide on this button's id. An
	// AbsoluteWidgetId also satisfies the interface — for widget-in-a-box
	// callers addressed purely by an absolute scope string — but such an id
	// must be made instance-unique by the caller (the host salt does not
	// reach absolute ids). The widget stays a pure display: it only reports
	// the click, leaving the caller to decide what cancelling means (abort
	// the producer, suppress its re-schedule, …). Nil hides the button.
	CancelId c.WidgetIdCreatorI
	// Inline draws Cancel, bar and status as consecutive widgets into the
	// caller's current layout — meant for a Horizontal row the caller
	// already has, such as a toolbar, where a row that came and went with
	// the job would move everything below it. The status is drawn small
	// and weak, and the Title is not drawn.
	Inline bool
	// BarWidth fixes the bar's width in points. Zero lets a stacked bar
	// take the available width and gives an inline bar DefaultInlineBarWidth
	// (egui's bar otherwise claims the whole row, pushing the status off).
	BarWidth float32
}

// DefaultInlineBarWidth is an inline bar's width when Input.BarWidth is 0.
const DefaultInlineBarWidth float32 = 190

// cancelGap is the inline space (points) between the status text and the
// Cancel button. A small literal rather than a styletokens lookup keeps
// this widget free of the design-system dependency, in line with its
// "deliberately generic" charter.
const cancelGap float32 = 8

// Render emits the progress row in the current layout. Stacked (the
// default), top to bottom: title, bar, then a status line optionally paired
// with a Cancel button. Inline: Cancel, bar, status, left to right, into
// whatever layout the caller has open. Stateless — call once per frame with
// the latest Input, inside an active layout scope (the caller controls
// placement, e.g. after a c.Plot block plus a c.AddSpace).
//
// cancelClicked is true only on the frame the Cancel button (rendered when
// Input.CancelId is non-nil) is clicked; it is always false when no
// CancelId is supplied.
func Render(in Input) (cancelClicked bool) {
	if in.Inline {
		return renderInline(in)
	}
	if in.Title != "" {
		c.Label(in.Title).Send()
	}
	renderBar(in, in.BarWidth)
	status := StatusLine(in)
	if in.CancelId == nil {
		if status != "" {
			c.Label(status).Send()
		}
		return
	}
	// Cancel affordance present: lay the status text and a compact Cancel
	// button on one row so the control reads as part of the progress
	// readout. The button draws exactly one widget id from the caller's id
	// creator: a relative id resolves against the surrounding scope, an
	// absolute id ignores it — neither pushes a scope, so the surrounding
	// id stack is left balanced either way.
	for range c.Horizontal().KeepIter() {
		if status != "" {
			c.Label(status).Send()
			c.AddSpace(cancelGap)
		}
		cancelClicked = cancelButton(in.CancelId)
	}
	return
}

func renderInline(in Input) (cancelClicked bool) {
	if in.CancelId != nil {
		cancelClicked = cancelButton(in.CancelId)
	}
	width := in.BarWidth
	if width <= 0 {
		width = DefaultInlineBarWidth
	}
	renderBar(in, width)
	if status := StatusLine(in); status != "" {
		for rt := range c.RichTextLabel(status) {
			rt.Small().Weak()
		}
	}
	return
}

func cancelButton(id c.WidgetIdCreatorI) (clicked bool) {
	return c.Button(id, c.Atoms().Text("Cancel").Keep()).
		Small().
		SendResp().
		HasPrimaryClicked()
}

// renderBar draws the bar alone. Deliberately no ShowPercentage: egui paints
// the bar's text at its left edge, where the fill sits early in a run, so
// "1%" reads as a blob followed by "%"; the percentage rides the status line.
func renderBar(in Input, width float32) {
	var bar c.ProgressBarFluid
	if in.Fraction < 0 {
		bar = c.ProgressBar(0).Animate(true)
	} else {
		bar = c.ProgressBar(clampUnit(in.Fraction))
	}
	if width > 0 {
		bar = bar.DesiredWidth(width)
	}
	bar.Send()
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

// StatusLine composes "47% · 1.2 k rows/s · 2m05s left · <note>", dropping
// the parts that aren't applicable (no percentage or ETA when indeterminate,
// Amount in place of the percentage when set, no rate or ETA when unknown,
// no note when empty). Exported so a caller that lays the readout out
// itself spells it the same way.
func StatusLine(in Input) (s string) {
	parts := make([]string, 0, 4)
	switch {
	case in.Amount != "":
		parts = append(parts, in.Amount)
	case in.Fraction >= 0:
		parts = append(parts, fmt.Sprintf("%d%%", int(clampUnit(in.Fraction)*100)))
	}
	if r := progressest.FormatRate(in.Rate, in.RateUnit); r != "" {
		parts = append(parts, r)
	}
	if in.Fraction >= 0 && in.EtaMs > 0 {
		parts = append(parts, progressest.FormatRemaining(time.Duration(in.EtaMs)*time.Millisecond))
	}
	if in.Note != "" {
		parts = append(parts, in.Note)
	}
	for i, p := range parts {
		if i > 0 {
			s += " · "
		}
		s += p
	}
	return
}
