package timeticks_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
)

func TestPickOffsetStep(t *testing.T) {
	// 20 s over 1200 px at 90 px/tick → 2 s ticks (1 s would be 60 px).
	require.Equal(t, 2*time.Second, timeticks.PickOffsetStep(20*time.Second, 1200, 90))
	// 12 h over 1200 px → one hour is 100 px, so 1 h.
	require.Equal(t, time.Hour, timeticks.PickOffsetStep(12*time.Hour, 1200, 90))
	// 5 ms over 1200 px → 500 µs ticks (120 px).
	require.Equal(t, 500*time.Microsecond, timeticks.PickOffsetStep(5*time.Millisecond, 1200, 90))
	// Degenerate spans do not panic.
	require.Equal(t, timeticks.OffsetLadder[0], timeticks.PickOffsetStep(0, 1200, 90))
}

func TestOffsetTicksAreMultiplesInsideTheSpan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		from := time.Duration(rapid.Int64Range(0, int64(13*time.Hour)).Draw(t, "from"))
		span := time.Duration(rapid.Int64Range(1, int64(time.Hour)).Draw(t, "span"))
		to := from + span
		step := timeticks.PickOffsetStep(span, 1200, 90)
		ticks := timeticks.OffsetTicks(from, to, step, nil)
		if span >= 2*step {
			// A span narrower than a step legitimately holds no tick.
			require.NotEmpty(t, ticks)
		}
		for i, tk := range ticks {
			require.Zero(t, tk%step)
			require.GreaterOrEqual(t, tk, from)
			require.LessOrEqual(t, tk, to)
			if i > 0 {
				require.Equal(t, step, tk-ticks[i-1])
			}
		}
		// Spacing at or above the target bounds the count.
		require.LessOrEqual(t, len(ticks), 1200/90+2)
	})
}

func TestFormatOffsetGolden(t *testing.T) {
	cases := []struct {
		d    time.Duration
		step time.Duration
		want string
	}{
		{0, time.Second, "0:00"},
		{83*time.Second + 250*time.Millisecond, time.Second, "1:23"},
		{83*time.Second + 250*time.Millisecond, 100 * time.Millisecond, "1:23.250"},
		{83*time.Second + 250*time.Millisecond + 7*time.Microsecond, 5 * time.Microsecond, "1:23.250007"},
		{12*time.Hour + 5*time.Minute + 9*time.Second, time.Hour, "12:05:09"},
		{2*time.Second - time.Microsecond, time.Millisecond, "0:01.999"}, // truncated, not rounded
		{-1500 * time.Millisecond, 100 * time.Millisecond, "-0:01.500"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, timeticks.FormatOffset(tc.d, tc.step), "%v @ %v", tc.d, tc.step)
	}
}

func TestFormatClock(t *testing.T) {
	epoch := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	require.Equal(t, "09:30:05", timeticks.FormatClock(epoch.Add(5*time.Second), time.Second))
	require.Equal(t, "09:30:05.250", timeticks.FormatClock(epoch.Add(5250*time.Millisecond), 10*time.Millisecond))
}
