package timescrubber

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// labels words instants for one series. The precision of a clock time comes
// from the series' smallest spacing and never from the value, so two adjacent
// steps do not print the same label and a label's width does not change as
// the playhead moves; every form that can be read alone carries the zone
// (ADR-0251 §SD6).
type labels struct {
	loc   *time.Location
	clock string // "15:04", finer where the steps are
	day   string // what tells two days of the series apart
}

func newLabels(ms []int64, loc *time.Location) (l labels) {
	l.loc = loc
	l.clock, l.day = "15:04", "Mon"
	if len(ms) < 2 {
		return
	}
	gap := int64(math.MaxInt64)
	for i := 1; i < len(ms); i++ {
		if d := ms[i] - ms[i-1]; d > 0 && d < gap {
			gap = d
		}
	}
	switch {
	case gap < 1000:
		l.clock = "15:04:05.000"
	case gap < 60_000:
		l.clock = "15:04:05"
	}
	switch span := time.Duration(ms[len(ms)-1]-ms[0]) * time.Millisecond; {
	case span > 300*24*time.Hour:
		l.day = "2006-01-02"
	case span >= 7*24*time.Hour:
		l.day = "Jan 2"
	}
	return
}

// short is a label for the strip: the day, the clock and the zone.
func (inst labels) short(t time.Time) string {
	return t.In(inst.loc).Format(inst.day + " " + inst.clock + " MST")
}

// full is the readout's form.
func (inst labels) full(t time.Time) string {
	return t.In(inst.loc).Format("Mon 2006-01-02 " + inst.clock + " MST")
}

// field is the form the edit field holds. It has no zone — a zone's
// abbreviation does not parse back — and the zone stands beside it.
func (inst labels) field(t time.Time) string {
	return t.In(inst.loc).Format("2006-01-02 " + inst.clock)
}

func (inst labels) zone(t time.Time) string { return t.In(inst.loc).Format("MST") }

var fieldLayouts = []string{
	"2006-01-02 15:04:05.000", "2006-01-02 15:04:05", "2006-01-02 15:04",
	"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02",
}

var clockLayouts = []string{"15:04:05.000", "15:04:05", "15:04"}

// parseField reads what a person typed into the time field: a time in any
// form field prints, a date, a bare clock time (on the day of ref), an
// RFC 3339 time, or a step as "#12" or "12". ok is false for anything else.
func (inst labels) parseField(text string, ref time.Time, steps int) (at time.Time, step int, isStep, ok bool) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	if num, found := strings.CutPrefix(text, "#"); found || !strings.ContainsAny(text, ":-T ") {
		k, err := strconv.Atoi(strings.TrimSpace(num))
		if err != nil || k < 1 || k > steps {
			return
		}
		return at, k - 1, true, true
	}
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t, 0, false, true
	}
	for _, layout := range fieldLayouts {
		if t, err := time.ParseInLocation(layout, text, inst.loc); err == nil {
			return t, 0, false, true
		}
	}
	for _, layout := range clockLayouts {
		if t, err := time.ParseInLocation(layout, text, inst.loc); err == nil {
			y, m, d := ref.In(inst.loc).Date()
			return time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), inst.loc), 0, false, true
		}
	}
	return
}

// formatOffset words how far an instant is from now: "+18 h", "−45 min".
func formatOffset(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign, d = "−", -d
	}
	switch {
	case d < 30*time.Second:
		return "now"
	case d < 90*time.Minute:
		return fmt.Sprintf("%s%d min", sign, int(math.Round(d.Minutes())))
	case d < 48*time.Hour:
		return fmt.Sprintf("%s%s h", sign, trimFloat(d.Hours()))
	}
	return fmt.Sprintf("%s%s d", sign, trimFloat(d.Hours()/24))
}

// formatSpan words the length of a bracket: "3 h", "15 min".
func formatSpan(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < 2*time.Minute:
		return trimFloat(d.Seconds()) + " s"
	case d < time.Hour:
		return trimFloat(d.Minutes()) + " min"
	case d < 48*time.Hour:
		return trimFloat(d.Hours()) + " h"
	}
	return trimFloat(d.Hours()/24) + " d"
}

func trimFloat(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
