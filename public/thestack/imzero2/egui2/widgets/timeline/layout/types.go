//go:build llm_generated_opus47

// Package layout provides the pure-Go layout primitives for the ImZero2
// timeline widget: greedy lane packing for interval events, multi-resolution
// bin indexing for point events, and a wrapper over boxer's timeticks for
// renderer-ready tick coordinates.
//
// All algorithms are independent of any UI — callers feed events and view
// bounds, receive arrangement data + screen-x positions. The widget layer
// (sibling package) composes these into a paint-canvas timeline in the
// spirit of LifeLines (Plaisant et al., CHI '96) and EventFlow (Monroe
// et al. 2013): a calendar axis bearing point events on one band and
// lane-packed interval events on another, with optional annotation
// markers and shaded background bands.
package layout

import (
	"errors"
	"time"
)

// ErrIntervalInverted is returned by IntervalEvent.Validate when ToMS is
// strictly less than FromMS. The packer silently skips such events (same
// treatment as nil pointers); callers wanting an explicit signal should
// validate upstream.
var ErrIntervalInverted = errors.New("layout: interval ToMS < FromMS")

// PointEvent is an instantaneous event on a timeline (commit, alert, log entry).
//
// The wire format mirrors timerangepicker.EvaluatedRange: int64 epoch
// milliseconds (always UTC). Caller-defined KindID slots into a categorical
// palette; Intensity in [0,1] maps to color/alpha for density rendering.
type PointEvent struct {
	TMS       int64
	KindID    int32
	Intensity float32
}

// AsTime converts TMS to a time.Time in UTC. Provided for ergonomic
// interop with timeticks and tzdata-aware formatting.
func (inst PointEvent) AsTime() (t time.Time) {
	t = time.UnixMilli(inst.TMS).UTC()
	return
}

// IntervalEvent is a [FromMS, ToMS] event with optional intensity (LLM session,
// deploy, scheduled task, build pipeline span).
//
// FromMS <= ToMS is required. Degenerate FromMS == ToMS is permitted and
// represents a zero-width "instant in interval form"; renderers typically
// clamp to one pixel. LaneHint pins the event to a named row (LifeLines-style
// per-actor lane); empty hint hands placement to the greedy packer.
type IntervalEvent struct {
	FromMS    int64
	ToMS      int64
	KindID    int32
	Intensity float32
	LaneHint  string
}

// AsFromTime converts FromMS to a time.Time in UTC.
func (inst IntervalEvent) AsFromTime() (t time.Time) {
	t = time.UnixMilli(inst.FromMS).UTC()
	return
}

// AsToTime converts ToMS to a time.Time in UTC.
func (inst IntervalEvent) AsToTime() (t time.Time) {
	t = time.UnixMilli(inst.ToMS).UTC()
	return
}

// DurationMS returns ToMS - FromMS. Negative for inverted input (caller bug);
// zero for instant.
func (inst IntervalEvent) DurationMS() (d int64) {
	d = inst.ToMS - inst.FromMS
	return
}

// Validate returns ErrIntervalInverted when ToMS < FromMS, else nil.
// PackLanes calls this on every input and drops failures with the same
// silent treatment as nil pointers; callers wanting a hard error should
// run Validate themselves before SetIntervals.
func (inst IntervalEvent) Validate() (err error) {
	if inst.ToMS < inst.FromMS {
		err = ErrIntervalInverted
	}
	return
}

// Annotation is a marker pinned to a single calendar moment, rendered as a
// dashed vertical line with a numbered flag at the top — the Grafana
// annotation idiom. Number is caller-supplied so sibling widgets can
// reference the same annotation across data updates (slice indices would
// shift on add/remove). PaletteIdx selects a categorical hue from
// styletokens.QualitativeCycle (BatlowS, 10 entries, CVD-safe). Label
// is shown in the hover tooltip.
type Annotation struct {
	TMS        int64
	Number     int32
	PaletteIdx int32
	Label      string
}

// AsTime converts TMS to a time.Time in UTC.
func (inst Annotation) AsTime() (t time.Time) {
	t = time.UnixMilli(inst.TMS).UTC()
	return
}

// BackgroundBand is a [FromMS, ToMS] shaded region painted under all event
// glyphs. Used for weekend/office-hours overlays, maintenance windows,
// alert windows — any "this stretch of time has special meaning" cue
// that wants to recede visually. Color is packed RGBA8; choose low alpha
// so foreground events stay legible. Label is shown in the hover tooltip
// when the cursor lands inside the band.
//
// Bands are produced lazily per frame via BackgroundBandProducer so the
// generation can pivot on the current view range — only the bands that
// would actually be visible need to be materialised.
type BackgroundBand struct {
	FromMS int64
	ToMS   int64
	Color  uint32
	Label  string
}

