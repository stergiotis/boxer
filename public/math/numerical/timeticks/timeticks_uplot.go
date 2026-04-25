//go:build llm_generated_opus47

package timeticks

import (
	"time"
)

/* Time-axis tick generation derived from the uPlot project (MIT).

   The curated step ladder, format-by-bucket convention, and multi-row
   context-label boundary detection follow uPlot's design in src/opts.js
   (xAxisTimeIncrs / _timeAxisStamps / timeAxisVals). The Go code below
   is an independent re-implementation written from notes, not a
   line-by-line port. Rollover rows here are range-based (each row groups
   ticks into contiguous runs sharing the same boundary value), which
   leaves the renderer free to centre, anchor-left, or anchor at the
   boundary tick — a small generalisation over uPlot's point-anchored
   rendering of the same data.

   Original licence:
   > The MIT License (MIT)
   >
   > Copyright (c) 2022 Leon Sorokin
   >
   > Permission is hereby granted, free of charge, to any person obtaining
   > a copy of this software and associated documentation files (the
   > "Software"), to deal in the Software without restriction, including
   > without limitation the rights to use, copy, modify, merge, publish,
   > distribute, sublicense, and/or sell copies of the Software, and to
   > permit persons to whom the Software is furnished to do so, subject to
   > the following conditions:
   >
   > The above copyright notice and this permission notice shall be
   > included in all copies or substantial portions of the Software.
   >
   > THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
   > EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
   > MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
   > NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
   > LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
   > OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
   > WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

const (
	defaultTargetSpacingPx int32   = 50
	defaultHysteresisFrac  float64 = 0.2

	approxMonthDuration = time.Duration(30.4375 * 24 * float64(time.Hour))
	approxYearDuration  = time.Duration(365.25 * 24 * float64(time.Hour))
)

// uplotLadder is the ordered list of human-meaningful tick steps. Ordered
// strictly increasing by approxDuration so a linear scan finds the smallest
// step whose duration meets the required spacing.
var uplotLadder = [...]TimeStep{
	{TimeStepUnitMillisecond, 1},
	{TimeStepUnitMillisecond, 2},
	{TimeStepUnitMillisecond, 5},
	{TimeStepUnitMillisecond, 10},
	{TimeStepUnitMillisecond, 20},
	{TimeStepUnitMillisecond, 50},
	{TimeStepUnitMillisecond, 100},
	{TimeStepUnitMillisecond, 200},
	{TimeStepUnitMillisecond, 500},
	{TimeStepUnitSecond, 1},
	{TimeStepUnitSecond, 5},
	{TimeStepUnitSecond, 10},
	{TimeStepUnitSecond, 15},
	{TimeStepUnitSecond, 30},
	{TimeStepUnitMinute, 1},
	{TimeStepUnitMinute, 5},
	{TimeStepUnitMinute, 10},
	{TimeStepUnitMinute, 15},
	{TimeStepUnitMinute, 30},
	{TimeStepUnitHour, 1},
	{TimeStepUnitHour, 2},
	{TimeStepUnitHour, 3},
	{TimeStepUnitHour, 4},
	{TimeStepUnitHour, 6},
	{TimeStepUnitHour, 8},
	{TimeStepUnitHour, 12},
	{TimeStepUnitDay, 1},
	{TimeStepUnitDay, 2},
	{TimeStepUnitDay, 3},
	{TimeStepUnitDay, 4},
	{TimeStepUnitDay, 5},
	{TimeStepUnitDay, 6},
	{TimeStepUnitDay, 7},
	{TimeStepUnitDay, 8},
	{TimeStepUnitDay, 9},
	{TimeStepUnitDay, 10},
	{TimeStepUnitDay, 15},
	{TimeStepUnitMonth, 1},
	{TimeStepUnitMonth, 2},
	{TimeStepUnitMonth, 3},
	{TimeStepUnitMonth, 4},
	{TimeStepUnitMonth, 6},
	{TimeStepUnitYear, 1},
	{TimeStepUnitYear, 2},
	{TimeStepUnitYear, 5},
	{TimeStepUnitYear, 10},
	{TimeStepUnitYear, 25},
	{TimeStepUnitYear, 50},
	{TimeStepUnitYear, 100},
}

type bucketE uint8

const (
	bucketMillis bucketE = iota
	bucketSecond
	bucketMinute
	bucketHour
	bucketDay
	bucketMonth
	bucketYear
)

// innerFormat is the Go time-format layout used for the primary tick label
// at each bucket. Layouts are intentionally short — date / year context is
// rendered separately via ContextLabels.
var innerFormat = [...]string{
	bucketMillis: "15:04:05.000",
	bucketSecond: "15:04:05",
	bucketMinute: "15:04",
	bucketHour:   "15:04",
	bucketDay:    "Jan 2",
	bucketMonth:  "Jan",
	bucketYear:   "2006",
}

// rolloverConfig is one row in a bucket's rollover stack. The layout must
// produce the same string for every tick that falls inside one boundary
// unit at this level (e.g. for BoundaryHour at second-step the layout is
// "Jan 2 2006 15:00" — literal "00" minute keeps the string constant
// across all ticks within the hour). Labels are grouped by string equality
// at the layout, so this property is what makes runs cohere.
//
// Each layout is "full content" — it carries everything coarser-than-or-
// equal-to its own boundary (the day row includes the year, the hour row
// includes the date). A renderer can therefore display a single row at
// coarse zoom and the full stack at fine zoom without losing context.
type rolloverConfig struct {
	boundary BoundaryE
	layout   string
}

// bucketRollovers maps a chosen step's bucket to its ordered rollover
// stack, coarsest first. Buckets that need no context row (year-step) get
// nil. uPlot's three-row design at second / sub-second step is preserved
// here as multiple distinct rows.
var bucketRollovers = [...][]rolloverConfig{
	bucketMillis: {
		{BoundaryYear, "2006"},
		{BoundaryDay, "Jan 2 2006"},
		{BoundaryHour, "Jan 2 2006 15:00"},
		{BoundaryMinute, "Jan 2 2006 15:04"},
	},
	bucketSecond: {
		{BoundaryYear, "2006"},
		{BoundaryDay, "Jan 2 2006"},
		{BoundaryHour, "Jan 2 2006 15:00"},
	},
	bucketMinute: {
		{BoundaryYear, "2006"},
		{BoundaryDay, "Jan 2 2006"},
	},
	bucketHour: {
		{BoundaryYear, "2006"},
		{BoundaryDay, "Jan 2 2006"},
	},
	bucketDay: {
		{BoundaryYear, "2006"},
	},
	bucketMonth: {
		{BoundaryYear, "2006"},
	},
	bucketYear: nil,
}

// ApproxDuration returns an upper-bound average duration for the step.
// Months and years are approximated at 30.4375 and 365.25 days; sub-day
// units are exact. Use this for span / ladder math, query-side interval
// computation (e.g. ClickHouse INTERVAL parameters), or rough density
// estimates. For calendar-correct stepping use Add, which honours DST,
// leap years, and irregular month widths.
func (inst TimeStep) ApproxDuration() (d time.Duration) {
	switch inst.Unit {
	case TimeStepUnitMillisecond:
		d = time.Duration(inst.Count) * time.Millisecond
	case TimeStepUnitSecond:
		d = time.Duration(inst.Count) * time.Second
	case TimeStepUnitMinute:
		d = time.Duration(inst.Count) * time.Minute
	case TimeStepUnitHour:
		d = time.Duration(inst.Count) * time.Hour
	case TimeStepUnitDay:
		d = time.Duration(inst.Count) * 24 * time.Hour
	case TimeStepUnitMonth:
		d = time.Duration(inst.Count) * approxMonthDuration
	case TimeStepUnitYear:
		d = time.Duration(inst.Count) * approxYearDuration
	}
	return
}

func (inst TimeStep) bucket() (b bucketE) {
	switch inst.Unit {
	case TimeStepUnitMillisecond:
		b = bucketMillis
	case TimeStepUnitSecond:
		b = bucketSecond
	case TimeStepUnitMinute:
		b = bucketMinute
	case TimeStepUnitHour:
		b = bucketHour
	case TimeStepUnitDay:
		b = bucketDay
	case TimeStepUnitMonth:
		b = bucketMonth
	case TimeStepUnitYear:
		b = bucketYear
	default:
		b = bucketSecond
	}
	return
}

// snapDown rounds t down to the nearest tick boundary in loc. Sub-day
// boundaries are computed in local-clock fields, so a 30-minute step on
// a half-hour-offset zone (e.g. India, UTC+05:30) snaps to local :00 / :30,
// not UTC :00 / :30. Day / month / year snapping honours non-uniform
// calendar widths via time.Date constructed in loc.
func (inst TimeStep) snapDown(t time.Time, loc *time.Location) (out time.Time) {
	t = t.In(loc)
	count := int(inst.Count)
	if count <= 0 {
		count = 1
	}
	switch inst.Unit {
	case TimeStepUnitMillisecond:
		out = t.Truncate(time.Duration(count) * time.Millisecond)
	case TimeStepUnitSecond:
		y, mo, d := t.Date()
		h, mi, sec := t.Clock()
		sec -= sec % count
		out = time.Date(y, mo, d, h, mi, sec, 0, loc)
	case TimeStepUnitMinute:
		y, mo, d := t.Date()
		h, mi, _ := t.Clock()
		mi -= mi % count
		out = time.Date(y, mo, d, h, mi, 0, 0, loc)
	case TimeStepUnitHour:
		y, mo, d := t.Date()
		h, _, _ := t.Clock()
		h -= h % count
		out = time.Date(y, mo, d, h, 0, 0, 0, loc)
	case TimeStepUnitDay:
		y, mo, d := t.Date()
		if count > 1 {
			doy := t.YearDay()
			doy0 := ((doy-1)/count)*count + 1
			out = time.Date(y, time.January, doy0, 0, 0, 0, 0, loc)
		} else {
			out = time.Date(y, mo, d, 0, 0, 0, 0, loc)
		}
	case TimeStepUnitMonth:
		y, mo, _ := t.Date()
		m0 := ((int(mo)-1)/count)*count + 1
		out = time.Date(y, time.Month(m0), 1, 0, 0, 0, 0, loc)
	case TimeStepUnitYear:
		y, _, _ := t.Date()
		y0 := (y / count) * count
		out = time.Date(y0, time.January, 1, 0, 0, 0, 0, loc)
	default:
		out = t
	}
	return
}

// Add advances t by one step, returning the next tick time. Day / Month /
// Year units use time.Time.AddDate, which honours DST transitions, leap
// years, and irregular month widths; sub-day units use time.Time.Add.
//
// Returns t unchanged for an invalid TimeStep (Unit == TimeStepUnitInvalid).
// Callers iterating with Add should always start from a finite TimeStep
// produced by PickTimeStep or TimeTicks to avoid infinite loops.
func (inst TimeStep) Add(t time.Time) (out time.Time) {
	switch inst.Unit {
	case TimeStepUnitMillisecond:
		out = t.Add(time.Duration(inst.Count) * time.Millisecond)
	case TimeStepUnitSecond:
		out = t.Add(time.Duration(inst.Count) * time.Second)
	case TimeStepUnitMinute:
		out = t.Add(time.Duration(inst.Count) * time.Minute)
	case TimeStepUnitHour:
		out = t.Add(time.Duration(inst.Count) * time.Hour)
	case TimeStepUnitDay:
		out = t.AddDate(0, 0, int(inst.Count))
	case TimeStepUnitMonth:
		out = t.AddDate(0, int(inst.Count), 0)
	case TimeStepUnitYear:
		out = t.AddDate(int(inst.Count), 0, 0)
	default:
		out = t
	}
	return
}

func ladderIndex(s TimeStep) (idx int32) {
	idx = -1
	for i, e := range uplotLadder {
		if e == s {
			idx = int32(i)
			return
		}
	}
	return
}

func abs32(v int32) (out int32) {
	if v < 0 {
		out = -v
		return
	}
	out = v
	return
}

// pickStep selects the smallest ladder step whose approxDuration meets the
// minimum required to keep tick count ≤ panelWidthPx / targetSpacingPx.
// Returns the largest ladder entry if no smaller step satisfies the target,
// so very long spans still produce ticks rather than failing.
func pickStep(span time.Duration, panelWidthPx int32, targetSpacingPx int32) (step TimeStep) {
	if panelWidthPx <= 0 || targetSpacingPx <= 0 || span <= 0 {
		step = uplotLadder[0]
		return
	}
	maxTicks := float64(panelWidthPx) / float64(targetSpacingPx)
	if maxTicks < 2 {
		maxTicks = 2
	}
	minStepDur := time.Duration(float64(span) / maxTicks)
	for _, s := range uplotLadder {
		if s.ApproxDuration() >= minStepDur {
			step = s
			return
		}
	}
	step = uplotLadder[len(uplotLadder)-1]
	return
}

// stickyStep applies hysteresis on continuous zoom. If prev is one ladder
// rung away from the natural pick AND prev's tick count is within
// (1 ± hysteresisFrac) of the target, prev is preferred — preventing the
// axis from flickering between two formats during a zoom gesture.
func stickyStep(natural TimeStep, prev TimeStep, span time.Duration, panelWidthPx int32, targetSpacingPx int32, hysteresisFrac float64) (chosen TimeStep) {
	chosen = natural
	if prev.IsZero() || hysteresisFrac <= 0 {
		return
	}
	naturalIdx := ladderIndex(natural)
	prevIdx := ladderIndex(prev)
	if naturalIdx < 0 || prevIdx < 0 {
		return
	}
	if abs32(naturalIdx-prevIdx) > 1 {
		return
	}
	target := float64(panelWidthPx) / float64(targetSpacingPx)
	prevTicks := float64(span) / float64(prev.ApproxDuration())
	low := target * (1 - hysteresisFrac)
	high := target * (1 + hysteresisFrac)
	if prevTicks >= low && prevTicks <= high {
		chosen = prev
	}
	return
}

func generateTicks(dataMin, dataMax time.Time, step TimeStep, loc *time.Location) (ticks []time.Time) {
	t := step.snapDown(dataMin, loc)
	for t.Before(dataMin) {
		t = step.Add(t)
	}
	span := dataMax.Sub(dataMin)
	approx := step.ApproxDuration()
	if approx > 0 {
		ticks = make([]time.Time, 0, int(span/approx)+2)
	}
	for !t.After(dataMax) {
		ticks = append(ticks, t)
		t = step.Add(t)
	}
	return
}

func formatInner(ticks []time.Time, b bucketE) (labels []string) {
	layout := innerFormat[b]
	labels = make([]string, len(ticks))
	for i, t := range ticks {
		labels[i] = t.Format(layout)
	}
	return
}

// generateRolloverRows produces one RolloverRow per entry in the bucket's
// rollover config. Each row groups ticks into contiguous runs that share
// the same formatted string at that level. A bucket with no rollover
// config (year-step) yields no rows.
func generateRolloverRows(ticks []time.Time, b bucketE) (rows []RolloverRow) {
	cfgs := bucketRollovers[b]
	if len(cfgs) == 0 || len(ticks) == 0 {
		return
	}
	rows = make([]RolloverRow, 0, len(cfgs))
	for _, cfg := range cfgs {
		labels := groupRunsByLayout(ticks, cfg.layout)
		rows = append(rows, RolloverRow{Boundary: cfg.boundary, Labels: labels})
	}
	return
}

// groupRunsByLayout walks ticks and emits one ContextLabel per contiguous
// run of ticks that format to the same string under layout.
func groupRunsByLayout(ticks []time.Time, layout string) (out []ContextLabel) {
	if layout == "" || len(ticks) == 0 {
		return
	}
	startIdx := int32(0)
	prev := ticks[0].Format(layout)
	for i := int32(1); i < int32(len(ticks)); i++ {
		cur := ticks[i].Format(layout)
		if cur != prev {
			out = append(out, ContextLabel{StartIdx: startIdx, EndIdx: i, Label: prev})
			startIdx = i
			prev = cur
		}
	}
	out = append(out, ContextLabel{StartIdx: startIdx, EndIdx: int32(len(ticks)), Label: prev})
	return
}

// PickTimeStep returns the step that TimeTicks would choose for the same
// inputs, without generating ticks or labels. Use this on the query side
// to fetch data at the same granularity the axis will render — e.g. as
// the INTERVAL parameter in a ClickHouse M4 GROUP BY.
//
// The same TimeTickOptions fields apply (PanelWidthPx, TargetSpacingPx,
// PrevStep, HysteresisFrac); Location is irrelevant to step selection
// and is ignored.
//
// A degenerate span (dataMax ≤ dataMin) returns the smallest ladder step
// rather than a zero TimeStep, so callers iterating with Add never loop
// indefinitely.
func PickTimeStep(dataMin, dataMax time.Time, opts TimeTickOptions) (step TimeStep) {
	target := opts.TargetSpacingPx
	if target <= 0 {
		target = defaultTargetSpacingPx
	}
	hysteresisFrac := opts.HysteresisFrac
	if hysteresisFrac <= 0 && !opts.PrevStep.IsZero() {
		hysteresisFrac = defaultHysteresisFrac
	}
	span := dataMax.Sub(dataMin)
	if span <= 0 {
		step = uplotLadder[0]
		return
	}
	natural := pickStep(span, opts.PanelWidthPx, target)
	step = stickyStep(natural, opts.PrevStep, span, opts.PanelWidthPx, target, hysteresisFrac)
	return
}

// TimeTicks computes a calendar-aware tick layout for a time axis spanning
// [dataMin, dataMax]. The returned layout is ready for direct rendering:
// TickValues / TickLabels for the primary axis row, RolloverRows for the
// secondary context rows (year, date, hour, minute as appropriate to the
// chosen step). RolloverRows is ordered coarsest first.
//
// Step selection (delegated to PickTimeStep) picks the smallest ladder
// entry whose approximate duration yields ≤ (PanelWidthPx / TargetSpacingPx)
// ticks across the span. The first tick is snapped to a locale-aware
// boundary in opts.Location (midnight, hour, month, year). Subsequent
// ticks advance via TimeStep.Add, so DST transitions and non-uniform
// calendar widths are honoured.
//
// If opts.PrevStep is set and is one ladder rung away from the natural
// pick, hysteresis keeps PrevStep when its spacing is within
// (1 ± HysteresisFrac) of the target — preventing axis flicker on
// continuous zoom. Pass the zero TimeStep{} to disable.
//
// The function is pure: no I/O, no errors, no global state. A degenerate
// span (dataMax ≤ dataMin) returns an empty layout.
func TimeTicks(dataMin, dataMax time.Time, opts TimeTickOptions) (layout TimeAxisLayout) {
	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}

	layout.DataMin = dataMin
	layout.DataMax = dataMax
	layout.ViewMin = dataMin
	layout.ViewMax = dataMax
	layout.Algorithm = "uplot-ladder"

	if dataMax.Sub(dataMin) <= 0 {
		return
	}

	layout.Step = PickTimeStep(dataMin, dataMax, opts)

	ticks := generateTicks(dataMin, dataMax, layout.Step, loc)
	layout.TickValues = ticks
	layout.TickLabels = formatInner(ticks, layout.Step.bucket())
	layout.RolloverRows = generateRolloverRows(ticks, layout.Step.bucket())
	return
}
