package timescrubber

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Two adjacent steps never print the same label, and every label that can be
// read alone carries the zone (ADR-0251 §SD6; survey rows 11, 12).
func TestAdjacentLabelsDiffer(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	require.NoError(t, err)
	rapid.Check(t, func(t *rapid.T) {
		gaps := []time.Duration{time.Millisecond, 40 * time.Millisecond, time.Second, 7 * time.Second,
			time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 24 * time.Hour, 31 * 24 * time.Hour}
		n := rapid.IntRange(2, 40).Draw(t, "steps")
		at := time.Date(2026, 3, 27, 22, 0, 0, 0, time.UTC) // runs across a DST change in Zurich
		ms := []int64{at.UnixMilli()}
		for i := 1; i < n; i++ {
			at = at.Add(gaps[rapid.IntRange(0, len(gaps)-1).Draw(t, "gap")])
			ms = append(ms, at.UnixMilli())
		}
		for _, loc := range []*time.Location{time.UTC, zurich} {
			lab := newLabels(ms, loc)
			for i := 1; i < n; i++ {
				a, b := time.UnixMilli(ms[i-1]), time.UnixMilli(ms[i])
				for name, f := range map[string]func(time.Time) string{"short": lab.short, "full": lab.full, "field": lab.field} {
					if name == "field" && loc != time.UTC {
						continue // the field has no zone, and an hour repeats when the clocks go back
					}
					if f(a) == f(b) {
						t.Fatalf("%s: steps %v and %v both read %q", name, a, b, f(a))
					}
				}
			}
		}
	})
	lab := newLabels([]int64{0, 3_600_000}, zurich)
	assert.Equal(t, "Thu 01:00 CET", lab.short(time.UnixMilli(0)))
	assert.Equal(t, "CET", lab.zone(time.UnixMilli(0)))
}

// The field parses every form it prints, and what a person is likely to type
// (ADR-0251 §SD7; survey row 58).
func TestTheTimeFieldParsesWhatItPrints(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	require.NoError(t, err)
	for _, gap := range []int64{1, 1000, 60_000, 3_600_000} {
		base := time.Date(2026, 7, 4, 13, 5, 7, 250_000_000, time.UTC).UnixMilli()
		lab := newLabels([]int64{base, base + gap}, zurich)
		at := time.UnixMilli(base + gap)
		got, _, isStep, ok := lab.parseField(lab.field(at), at, 2)
		require.True(t, ok, lab.field(at))
		assert.False(t, isStep)
		assert.Equal(t, lab.field(at), lab.field(got), "gap %d ms", gap)
	}

	lab := newLabels([]int64{0, 3_600_000}, time.UTC)
	ref := time.Date(2026, 7, 4, 13, 0, 0, 0, time.UTC)
	for text, want := range map[string]time.Time{
		"2026-07-05 06:00":          time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC),
		"  2026-07-05   06:00 ":     time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC),
		"2026-07-05":                time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		"18:30":                     time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC),
		"2026-07-05T06:00:00Z":      time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC),
		"2026-07-05T08:00:00+02:00": time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC),
	} {
		got, _, isStep, ok := lab.parseField(text, ref, 10)
		require.True(t, ok, text)
		assert.False(t, isStep, text)
		assert.True(t, want.Equal(got), "%q gave %v", text, got)
	}
	for text, want := range map[string]int{"#3": 2, "7": 6, "# 10": 9} {
		_, step, isStep, ok := lab.parseField(text, ref, 10)
		require.True(t, ok, text)
		assert.True(t, isStep)
		assert.Equal(t, want, step, text)
	}
	for _, text := range []string{"", "#0", "11", "tomorrow", "25:00", "#x"} {
		_, _, _, ok := lab.parseField(text, ref, 10)
		assert.False(t, ok, text)
	}
}

func TestOffsetsAndSpansInWords(t *testing.T) {
	assert.Equal(t, "now", formatOffset(10*time.Second))
	assert.Equal(t, "+45 min", formatOffset(45*time.Minute))
	assert.Equal(t, "−18 h", formatOffset(-18*time.Hour))
	assert.Equal(t, "+1.5 h", formatOffset(90*time.Minute))
	assert.Equal(t, "+3 d", formatOffset(72*time.Hour))
	assert.Equal(t, "3 h", formatSpan(3*time.Hour))
	assert.Equal(t, "1 h", formatSpan(time.Hour), "an hourly step reads as an hour, not as sixty minutes")
	assert.Equal(t, "1.5 h", formatSpan(90*time.Minute))
	assert.Equal(t, "15 min", formatSpan(15*time.Minute))
	assert.Equal(t, "90 s", formatSpan(90*time.Second))
	assert.Equal(t, "2 d", formatSpan(48*time.Hour))
	assert.Equal(t, "", formatSpan(0))
}

// Days are the zone's, across a change of clocks in either hemisphere.
func TestMidnightsAndDayJumps(t *testing.T) {
	for _, name := range []string{"Europe/Zurich", "Australia/Sydney", "America/Sao_Paulo", "UTC"} {
		loc, err := time.LoadLocation(name)
		require.NoError(t, err)
		from := time.Date(2026, 3, 27, 5, 0, 0, 0, loc) // Zurich goes forward on the 29th
		to := from.AddDate(0, 0, 10)
		days := midnights(from.UnixMilli(), to.UnixMilli(), loc, 400)
		require.Len(t, days, 10, name)
		for _, ms := range days {
			at := time.UnixMilli(ms).In(loc)
			assert.Equal(t, 0, at.Hour()*60+at.Minute(), "%s: %v", name, at)
		}
		assert.Len(t, midnights(from.UnixMilli(), to.UnixMilli(), loc, 3), 3, "the count is capped")
	}

	zurich, _ := time.LoadLocation("Europe/Zurich")
	var steps []Step
	for h := 0; h < 72; h += 3 {
		steps = append(steps, Step{At: time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)})
	}
	sc := &Scrubber{Opts: Options{Location: zurich}, now: time.Now}
	axis := newAxis(steps, 0, 1, false)
	sc.Transport.Pos = 2 // 06:00 UTC on the 28th
	next := sc.dayJump(axis, 1)
	assert.Equal(t, "2026-03-29 01:00", steps[next].At.In(zurich).Format("2006-01-02 15:04"), "the first step of the next local day")
	sc.Transport.Pos = float64(next)
	assert.Equal(t, 0, sc.dayJump(axis, -1), "from a day's first step, the day before")
	sc.Transport.Pos = float64(next + 2)
	assert.Equal(t, next, sc.dayJump(axis, -1), "from inside a day, its first step")

	sc.Marks = []Mark{{At: steps[5].At}, {At: steps[11].At.Add(time.Hour)}, {At: steps[20].At}}
	sc.Transport.Pos = 5
	assert.Equal(t, 11, sc.markJump(axis, 1), "the next mark that is not the step in hand")
	assert.Equal(t, 5, sc.markJump(axis, -1), "and none before it")
	sc.Transport.Pos = 12
	assert.Equal(t, 11, sc.markJump(axis, -1))
}
