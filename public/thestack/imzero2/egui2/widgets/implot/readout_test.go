package implot

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A time axis's readout and tags name one instant with its date, at the
// precision of the tick ladder's unit; a linear axis keeps %.6g and the tick
// formatter.
func TestAxisTextTimeScale(t *testing.T) {
	instant := float64(time.Date(2026, 10, 18, 19, 7, 42, 0, time.UTC).Unix())
	for _, c := range []struct {
		name string
		span time.Duration
		want string
	}{
		{"hours", 6 * time.Hour, "2026-10-18 19:07"},
		{"days", 4 * 24 * time.Hour, "2026-10-18 19:00"},
		{"weeks", 21 * 24 * time.Hour, "2026-10-18"},
		{"years", 3 * 365 * 24 * time.Hour, "2026-10-18"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ax := axisState{scale: ScaleTime, rng: Range{Min: instant - c.span.Seconds()/2, Max: instant + c.span.Seconds()/2}}
			ticks := locateTicksTime(ax.rng, 800, nil)
			require.GreaterOrEqual(t, countMajor(ticks), 2)
			require.Equal(t, c.want, axisValueText(&ax, ticks, instant))
			require.Equal(t, c.want, axisTagText(&ax, ticks, instant))
		})
	}

	t.Run("seconds", func(t *testing.T) {
		ax := axisState{scale: ScaleTime, rng: Range{Min: instant - 30, Max: instant + 30}}
		ticks := locateTicksTime(ax.rng, 800, nil)
		require.Equal(t, "2026-10-18 19:07:42", axisValueText(&ax, ticks, instant+0.4))
	})

	t.Run("one tick in view", func(t *testing.T) {
		ax := axisState{scale: ScaleTime, rng: Range{Min: instant, Max: instant + 600}}
		require.Equal(t, "2026-10-18 19:07", axisValueText(&ax, nil, instant))
	})

	t.Run("linear unchanged", func(t *testing.T) {
		ax := axisState{scale: ScaleLinear, rng: Range{Min: 0, Max: 2e9}}
		require.Equal(t, "1.79235e+09", axisValueText(&ax, nil, 1.79235e9))
		require.Equal(t, "63.2", axisValueText(&ax, nil, 63.2))
		require.Equal(t, formatTick(1.79235e9, 2), axisTagText(&ax, nil, 1.79235e9))
	})

	t.Run("out of time range", func(t *testing.T) {
		require.Equal(t, "NaN", formatTimeCursor(math.NaN(), 3600))
		require.Equal(t, "1e+300", formatTimeCursor(1e300, 3600))
	})
}

func TestReadoutTextJoinsAxes(t *testing.T) {
	st := &plotState{}
	st.x = axisState{scale: ScaleTime, rng: Range{Min: 0, Max: 1}}
	st.y = axisState{scale: ScaleLinear}
	require.Equal(t, "1970-01-01 00:00:00, 63.2", st.readoutText(0, 63.2))
}
