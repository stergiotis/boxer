package implot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func loadZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	return loc
}

// Located ticks in a zone fall on its calendar: across the end of summer
// time in Zurich (2026-10-25, a 25-hour day) daily ticks stay on local
// midnights, and hourly ticks step evenly through the repeated hour.
func TestTimeTicksInAZoneAcrossDST(t *testing.T) {
	zrh := loadZone(t, "Europe/Zurich")
	unix := func(tm time.Time) float64 { return float64(tm.Unix()) }

	days := locateTicksTime(Range{unix(time.Date(2026, 10, 22, 12, 0, 0, 0, zrh)), unix(time.Date(2026, 10, 28, 12, 0, 0, 0, zrh))}, 800, zrh, nil)
	require.GreaterOrEqual(t, len(days), 5)
	sawLong := false
	for i, tk := range days {
		at := time.Unix(int64(tk.value), 0).In(zrh)
		require.Equal(t, [2]int{0, 0}, [2]int{at.Hour(), at.Minute()}, "tick %d %q at %v", i, tk.label, at)
		if i > 0 && tk.value-days[i-1].value == 25*3600 {
			sawLong = true
		}
	}
	require.True(t, sawLong, "no 25-hour day in %v", days)

	hours := locateTicksTime(Range{unix(time.Date(2026, 10, 25, 0, 30, 0, 0, zrh)), unix(time.Date(2026, 10, 25, 5, 30, 0, 0, zrh))}, 800, zrh, nil)
	var labels []string
	for i, tk := range hours {
		labels = append(labels, tk.label)
		if i > 0 {
			require.Equal(t, 3600.0, tk.value-hours[i-1].value, "uneven hourly step in %v", hours)
		}
	}
	require.Equal(t, []string{"01:00", "02:00", "02:00", "03:00", "04:00", "05:00"}, labels)

	// Three-hour ticks keep to the same wall-clock hours on either side.
	threes := locateTicksTime(Range{unix(time.Date(2026, 10, 24, 1, 0, 0, 0, zrh)), unix(time.Date(2026, 10, 26, 1, 0, 0, 0, zrh))}, 800, zrh, nil)
	for _, tk := range threes {
		at := time.Unix(int64(tk.value), 0).In(zrh)
		require.Zero(t, at.Hour()%3, "tick %q at %v", tk.label, at)
	}
}

// A zone whose offset is not whole hours: hourly ticks are its local hours,
// not UTC's.
func TestTimeTicksInAHalfHourZone(t *testing.T) {
	ktm := loadZone(t, "Asia/Kathmandu") // +05:45
	lo := float64(time.Date(2026, 3, 1, 9, 10, 0, 0, ktm).Unix())
	ticks := locateTicksTime(Range{lo, lo + 5*3600}, 800, ktm, nil)
	require.NotEmpty(t, ticks)
	for _, tk := range ticks {
		require.Zero(t, time.Unix(int64(tk.value), 0).In(ktm).Minute(), "tick %q", tk.label)
	}
}

// The readout on either side of the repeated hour names the zone, which is
// what tells the two 02:30s apart; UTC keeps its bare form.
func TestReadoutInAZone(t *testing.T) {
	zrh := loadZone(t, "Europe/Zurich")
	summer := float64(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC).Unix()) // 02:30 CEST
	winter := summer + 3600                                                  // 02:30 CET
	ax := axisState{scale: ScaleTime, loc: zrh, rng: Range{summer - 3*3600, summer + 3*3600}}
	ticks := locateTicksTime(ax.rng, 800, zrh, nil)
	require.Equal(t, "2026-10-25 02:30 CEST", axisValueText(&ax, ticks, summer))
	require.Equal(t, "2026-10-25 02:30 CET", axisValueText(&ax, ticks, winter))
	require.Equal(t, "2026-10-25 02:30 CET", axisTagText(&ax, ticks, winter))

	// A day-precision readout has no time of day to qualify.
	wide := axisState{scale: ScaleTime, loc: zrh, rng: Range{summer - 30*86400, summer + 30*86400}}
	require.Equal(t, "2026-10-25", axisValueText(&wide, locateTicksTime(wide.rng, 800, zrh, nil), winter))

	// The hour precision takes the wall clock's minutes off, not UTC's.
	ktm := loadZone(t, "Asia/Kathmandu")
	at := float64(time.Date(2026, 3, 1, 9, 50, 0, 0, ktm).Unix())
	require.Equal(t, "2026-03-01 09:00 +0545", formatTimeCursor(at, 86400, ktm))

	utc := axisState{scale: ScaleTime, rng: ax.rng}
	require.Equal(t, "2026-10-25 00:30", axisValueText(&utc, locateTicksTime(utc.rng, 800, nil, nil), summer))
}

// The zone is Setup state for one frame, like the scale.
func TestSetupAxisTimeZoneIsPerFrame(t *testing.T) {
	zrh := loadZone(t, "Europe/Zurich")
	p := newTestPlot()
	p.SetupAxisScale(AxisX1, ScaleTime).SetupAxisTimeZone(AxisX1, zrh)
	require.Same(t, zrh, p.st.x.loc)
	require.Nil(t, p.st.y.loc)
	p.st.beginFrame()
	require.Nil(t, p.st.x.loc)
}
