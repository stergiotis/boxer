//go:build llm_generated_opus47

package timeticks

import (
	"time"
)

/* Time-axis tick generation derived from the uPlot project (MIT).

   The curated step ladder, format-by-bucket convention, and dual-row
   context-label boundary detection follow uPlot's design in src/opts.js
   (xAxisTimeIncrs / _timeAxisStamps / timeAxisVals). The Go code below
   is an independent re-implementation written from notes — not a
   line-by-line port — and simplifies the rendering to two label rows
   instead of uPlot's three-row rollover.

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

// contextFormat is the layout for the secondary (outer-row) context label.
// Empty string disables the context row. A context label spans every run
// of consecutive ticks that produce the same context-formatted string —
// equivalent to "the next coarser unit didn't change yet."
var contextFormat = [...]string{
	bucketMillis: "Jan 2 2006",
	bucketSecond: "Jan 2 2006",
	bucketMinute: "Jan 2 2006",
	bucketHour:   "Jan 2 2006",
	bucketDay:    "2006",
	bucketMonth:  "2006",
	bucketYear:   "",
}

// approxDuration returns an upper-bound average duration. Used only for
// ladder selection; tick generation is calendar-correct via AddDate.
func (inst TimeStep) approxDuration() (d time.Duration) {
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

func (inst TimeStep) add(t time.Time) (out time.Time) {
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
		if s.approxDuration() >= minStepDur {
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
	prevTicks := float64(span) / float64(prev.approxDuration())
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
		t = step.add(t)
	}
	span := dataMax.Sub(dataMin)
	approx := step.approxDuration()
	if approx > 0 {
		ticks = make([]time.Time, 0, int(span/approx)+2)
	}
	for !t.After(dataMax) {
		ticks = append(ticks, t)
		t = step.add(t)
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

// generateContextLabels emits one ContextLabel per contiguous run of ticks
// that produce the same context-formatted string. A bucket with empty
// contextFormat yields no labels.
func generateContextLabels(ticks []time.Time, b bucketE) (out []ContextLabel) {
	layout := contextFormat[b]
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

// TimeTicks computes a calendar-aware tick layout for a time axis spanning
// [dataMin, dataMax]. The returned layout is ready for direct rendering:
// TickValues / TickLabels for the primary axis row, ContextLabels for the
// secondary date / year context row.
//
// Step selection picks the smallest entry in a curated ladder whose
// approximate duration yields ≤ (PanelWidthPx / TargetSpacingPx) ticks
// across the span. The first tick is snapped to a locale-aware boundary
// in opts.Location (midnight, hour, month, year). Subsequent ticks advance
// via AddDate, so DST transitions and non-uniform calendar widths are
// honoured.
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
	target := opts.TargetSpacingPx
	if target <= 0 {
		target = defaultTargetSpacingPx
	}
	hysteresisFrac := opts.HysteresisFrac
	if hysteresisFrac <= 0 && !opts.PrevStep.IsZero() {
		hysteresisFrac = defaultHysteresisFrac
	}

	layout.DataMin = dataMin
	layout.DataMax = dataMax
	layout.ViewMin = dataMin
	layout.ViewMax = dataMax
	layout.Algorithm = "uplot-ladder"

	span := dataMax.Sub(dataMin)
	if span <= 0 {
		return
	}

	natural := pickStep(span, opts.PanelWidthPx, target)
	step := stickyStep(natural, opts.PrevStep, span, opts.PanelWidthPx, target, hysteresisFrac)
	layout.Step = step

	ticks := generateTicks(dataMin, dataMax, step, loc)
	layout.TickValues = ticks
	layout.TickLabels = formatInner(ticks, step.bucket())
	layout.ContextLabels = generateContextLabels(ticks, step.bucket())
	return
}
