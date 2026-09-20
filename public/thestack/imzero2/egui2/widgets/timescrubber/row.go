package timescrubber

import (
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// button draws one control of the row. Every label carries a word beside its
// glyph: a glyph alone has no name for a screen reader or a scripted driver
// to find it by. A control that does not apply is disabled and stays where it
// is, so the row does not move under the reader (ADR-0251 §SD6); its tooltip
// says what it moves by and its key (§SD7).
func (inst *Scrubber) button(key, label, tip string, enabled, selected bool) (clicked bool) {
	for range c.HoverText(tip).KeepIter() {
		for range c.Scope().KeepIter() {
			if !enabled {
				c.UiDisable()
			}
			clicked = c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).
				Selected(selected).SendResp().HasPrimaryClicked()
		}
	}
	return clicked && enabled
}

func (inst *Scrubber) renderTransportRow(steps []Step) (moved bool) {
	t := &inst.Transport
	n := len(steps)
	at := int(math.Round(t.Pos))
	lo, hi := t.Bounds(n)
	some := n > 1
	for range c.HorizontalWrapped().KeepIter() {
		if inst.button("first", icons.PhSkipBack+" First", "To the first step of the range · Home", some && at != lo, false) {
			t.First(n)
			moved = true
		}
		if inst.button("prev", icons.PhCaretLeft+" Prev", fmt.Sprintf("One step back · Left · Shift or PageDown %d steps · Ctrl a day · Alt a mark", stride(n)), some && at > 0, false) {
			t.StepBy(-1, n)
			moved = true
		}
		play := icons.PhPlay + " Play"
		if t.Playing {
			play = icons.PhPause + " Pause"
		}
		if inst.button("play", play, "Play or pause · Space · playback waits for steps that are loading", some, false) {
			t.Toggle(n)
			inst.focusKeys()
		}
		if inst.button("next", "Next "+icons.PhCaretRight, fmt.Sprintf("One step on · Right · Shift or PageUp %d steps · Ctrl a day · Alt a mark", stride(n)), some && at < n-1, false) {
			t.StepBy(1, n)
			moved = true
		}
		if inst.button("last", "Last "+icons.PhSkipForward, "To the last step of the range · End", some && at != hi, false) {
			t.Last(n)
			moved = true
		}
		c.Separator().Vertical().Send()

		rate := t.Rate
		if !(rate > 0) {
			rate = defaultRate
		}
		for range c.ComboBox(inst.ids.PrepareStr("rate"),
			c.WidgetText().Text("").Keep(),
			c.WidgetText().Text(formatRate(rate)).Keep()).KeepIter() {
			for i, r := range rates {
				if c.Button(inst.ids.PrepareSeq(uint64(0x7200+i)), c.Atoms().Text(formatRate(r)).Keep()).
					Frame(false).Selected(r == rate).SendResp().HasPrimaryClicked() {
					t.Rate = r
				}
			}
		}
		if inst.button("mode", modeIcon(t.Mode)+" "+t.Mode.label(), "What playback does at the end: loop, bounce, or stop", true, false) {
			t.Mode = AllModes[(int(t.Mode)+1)%len(AllModes)]
		}
		c.Separator().Vertical().Send()

		if inst.button("range-in", icons.PhBracketsSquare+" In", "Start the range at the playhead · Shift+Home", some && at < n-1, false) {
			t.SetIn(at, n)
		}
		if inst.button("range-out", "Out", "End the range at the playhead · Shift+End", some && at > 0, false) {
			t.SetOut(at, n)
		}
		if inst.button("clear-range", "Clear range", "Play every step again · Delete, or a double click on the band", t.RangeOn, false) {
			t.ClearRange()
		}
		c.Separator().Vertical().Send()

		nowStep, spans := inst.nowStep(steps)
		if inst.button("now", icons.PhClock+" Now", "To the step nearest the wall clock", spans, spans && nowStep == at) {
			t.Seek(float64(nowStep), n)
			moved = true
		}
	}
	return
}

// renderReadoutRow is the line under the strip: the time as a field to type
// into, and the readout. It is under the strip and not in the row of controls
// so that the controls fit a pane's width and the strip does not move when
// the words grow.
func (inst *Scrubber) renderReadoutRow(steps []Step) (moved bool) {
	for range c.Horizontal().KeepIter() {
		moved = inst.timeField(steps)
		c.Label(inst.readout(steps)).Send()
	}
	return
}

// renderCompact is the one-line form: play and pause, the state lane with the
// playhead and the range on it, and the time (ADR-0251 §SD10).
func (inst *Scrubber) renderCompact(w float32, steps []Step) (ev Events) {
	t := &inst.Transport
	n := len(steps)
	for range c.Horizontal().KeepIter() {
		play := icons.PhPlay + " Play"
		if t.Playing {
			play = icons.PhPause + " Pause"
		}
		if inst.button("play", play, "Play or pause · Space", n > 1, false) {
			t.Toggle(n)
			inst.focusKeys()
		}
		ev = inst.keyed(func() Events { return inst.strip(max(w-compactReserve, 2*padX+40), compactHeight, steps, true) })
		c.Label(inst.compactReadout(steps)).Send()
	}
	return
}

// nowStep is the step nearest the wall clock, and whether the series spans
// it.
func (inst *Scrubber) nowStep(steps []Step) (step int, spans bool) {
	n := len(steps)
	if n < 2 {
		return
	}
	axis := newAxis(steps, 0, 1, false)
	at := inst.wall()
	if !axis.ordered() || at.Before(steps[0].At) || at.After(steps[n-1].At) {
		return
	}
	return int(math.Round(axis.msToPos(float64(at.UnixMilli())))), true
}

// timeField is the display time as a field a person can type into: a time, a
// date, a clock time on the day shown, or a step as "#12". It is applied
// when the field is left, which Enter does, and goes to the nearest step
// (ADR-0251 §SD7). While it is not being edited it follows the playhead.
func (inst *Scrubber) timeField(steps []Step) (moved bool) {
	n := len(steps)
	t := &inst.Transport
	axis := newAxis(steps, 0, 1, false)
	lab := newLabels(axis.ms, inst.location())
	shown := ""
	switch {
	case n == 0:
	case axis.ordered():
		at := axis.timeAt(t.Pos, inst.location())
		shown = lab.field(at)
	default:
		shown = fmt.Sprintf("#%d", int(math.Round(t.Pos))+1)
	}
	sm := c.CurrentApplicationState.StateManager
	if !inst.timeFocused {
		if inst.timeText != shown {
			inst.timeText = shown
			sm.OverrideDatabindingSPtr(&inst.timeText)
		}
		// What the field held when it was entered: leaving it untouched is
		// not a seek, even if playback has moved on meanwhile.
		inst.timeBefore = shown
	}
	var flags c.ResponseFlagsE
	for range c.HoverText("Type a time, a date, a clock time on the day shown, or a step as #12 · Enter").KeepIter() {
		for range c.Scope().KeepIter() {
			if n < 2 {
				c.UiDisable()
			}
			flags = c.TextEdit(inst.ids.PrepareStr("time"), inst.timeText, false).
				DesiredWidth(150).SendRespVal(&inst.timeText)
		}
	}
	if flags.HasLostFocus() && n > 1 && inst.timeText != inst.timeBefore {
		ref := axis.timeAt(t.Pos, inst.location())
		if at, step, isStep, ok := lab.parseField(inst.timeText, ref, n); ok {
			if !isStep && axis.ordered() {
				step = int(math.Round(axis.msToPos(float64(at.UnixMilli()))))
			}
			if isStep || axis.ordered() {
				t.Seek(float64(step), n)
				moved = true
			}
		}
	}
	inst.timeFocused = flags.HasFocus() && !flags.HasLostFocus()
	return
}

func modeIcon(m ModeE) string {
	switch m {
	case ModeBounce:
		return icons.PhArrowsLeftRight
	case ModeOnce:
		return icons.PhArrowLineRight
	}
	return icons.PhRepeat
}

func formatRate(r float64) string {
	if r >= 1 {
		return fmt.Sprintf("%g steps/s", r)
	}
	return fmt.Sprintf("1 step / %g s", 1/r)
}

// readout is the display time in words, and beside it which step, how far from now,
// how long the bracket is — the rate is in steps, so on a true axis the
// playhead speeds up where the cadence coarsens, and the words say so
// (ADR-0251 §SD8) — and what playback is waiting for.
func (inst *Scrubber) readout(steps []Step) string {
	n := len(steps)
	if n == 0 {
		return "no steps"
	}
	t := &inst.Transport
	axis := newAxis(steps, 0, 1, false)
	lab := newLabels(axis.ms, inst.location())
	k := int(math.Round(t.Pos))
	s := ""
	if axis.ordered() {
		// The time in words, zone and all: the field beside it is for typing
		// into, and a scripted driver or a screen reader finds this by role.
		s = lab.full(axis.timeAt(t.Pos, inst.location())) + " · "
	}
	switch {
	case inst.drag == dragPlayhead && !inst.Opts.NoSnap:
		// The step a release would select, and what it holds.
		s += fmt.Sprintf("lets go on step %d of %d%s", k+1, n, inst.valueWords(steps, k))
	case math.Abs(t.Pos-float64(k)) < entrance:
		s += fmt.Sprintf("step %d of %d", k+1, n)
	default:
		s += fmt.Sprintf("between steps %d and %d", int(t.Pos)+1, int(t.Pos)+2)
	}
	if axis.ordered() {
		if now := inst.wall(); !now.Before(steps[0].At.Add(-72*time.Hour)) && !now.After(steps[n-1].At.Add(72*time.Hour)) {
			s += " · " + formatOffset(time.UnixMilli(int64(axis.posToMS(t.Pos))).Sub(now))
		}
		a := min(int(t.Pos), n-2)
		if a >= 0 {
			if span := formatSpan(time.Duration(axis.ms[a+1]-axis.ms[a]) * time.Millisecond); span != "" {
				s += " · " + span + " step"
			}
		}
	}
	if t.RangeOn {
		lo, hi := t.Bounds(n)
		s += fmt.Sprintf(" · range %d–%d", lo+1, hi+1)
	}
	if t.Buffering && t.BufferingFor >= waitingAfter && t.BufferingStep >= 0 && t.BufferingStep < n {
		what := fmt.Sprintf("step %d", t.BufferingStep+1)
		if axis.ordered() {
			what = lab.short(steps[t.BufferingStep].At)
		}
		s += fmt.Sprintf(" · waiting for %s (%.0f s)", what, t.BufferingFor)
	}
	if t.Playing && t.WaitShare > 0.15 {
		rate := t.Rate
		if !(rate > 0) {
			rate = defaultRate
		}
		s += fmt.Sprintf(" · playing at %.2g of %g steps/s", rate*(1-t.WaitShare), rate)
	}
	return s
}

func (inst *Scrubber) compactReadout(steps []Step) string {
	n := len(steps)
	if n == 0 {
		return "no steps"
	}
	t := &inst.Transport
	axis := newAxis(steps, 0, 1, false)
	if !axis.ordered() {
		return fmt.Sprintf("step %d of %d", int(math.Round(t.Pos))+1, n)
	}
	s := newLabels(axis.ms, inst.location()).short(axis.timeAt(t.Pos, inst.location()))
	if t.Buffering && t.BufferingFor >= waitingAfter {
		s += " · waiting"
	}
	return s
}

// valueWords is a step's value and peak in words, led by a separator.
func (inst *Scrubber) valueWords(steps []Step, i int) (s string) {
	v := steps[i].Value
	if v != v {
		return
	}
	name := inst.Opts.ValueName
	if name != "" {
		name += " "
	}
	s = fmt.Sprintf(" · %s%.3g", name, v)
	if p := steps[i].Peak; p == p {
		s += fmt.Sprintf(" (%s %.3g)", inst.peakName(), p)
	}
	if inst.Opts.ValueUnit != "" {
		s += " " + inst.Opts.ValueUnit
	}
	return
}

func (inst *Scrubber) peakName() string {
	if inst.Opts.PeakName != "" {
		return inst.Opts.PeakName
	}
	return "max"
}
