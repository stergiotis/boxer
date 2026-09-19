package progressest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTracker_SteadyCounterGivesRateAndEta(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(0, 0)
	var v View
	for i := 0; i <= 10; i++ {
		v = tr.Observe(t0.Add(time.Duration(i)*time.Second), int64(i*100), 2_000)
	}
	require.InDelta(t, 100, v.Rate, 5)
	require.True(t, v.EtaValid)
	require.InDelta(t, 10*time.Second, v.Eta, float64(time.Second))
	require.InDelta(t, 0.5, v.Fraction, 1e-6)
}

func TestTracker_UnchangedCounterWithinStallWindowDoesNotFold(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(0, 0)
	for i := 0; i <= 5; i++ {
		tr.Observe(t0.Add(time.Duration(i)*time.Second), int64(i*100), 0)
	}
	before := tr.Observe(t0.Add(5*time.Second), 500, 0).Rate
	// Frames re-reading the same counter inside the stall window.
	var v View
	for ms := 5_016; ms < 5_900; ms += 16 {
		v = tr.Observe(t0.Add(time.Duration(ms)*time.Millisecond), 500, 0)
	}
	require.Equal(t, before, v.Rate)
	require.Equal(t, float32(-1), v.Fraction)
	require.False(t, v.EtaValid)
}

func TestTracker_StallPullsRateDown(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(0, 0)
	for i := 0; i <= 5; i++ {
		tr.Observe(t0.Add(time.Duration(i)*time.Second), int64(i*100), 0)
	}
	v := tr.Observe(t0.Add(8*time.Second), 500, 0)
	require.Less(t, v.Rate, 90.0)
}

func TestTracker_CounterGoingBackwardsReanchors(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(0, 0)
	for i := 0; i <= 5; i++ {
		tr.Observe(t0.Add(time.Duration(i)*time.Second), int64(i*1_000), 10_000)
	}
	v := tr.Observe(t0.Add(6*time.Second), 10, 10_000)
	require.Equal(t, 0.0, v.Rate)
	require.False(t, v.EtaValid)
}

func TestTracker_ObserveFraction(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(0, 0)
	var v View
	for i := 0; i <= 5; i++ {
		v = tr.ObserveFraction(t0.Add(time.Duration(i)*time.Second), float64(i)*0.1)
	}
	require.InDelta(t, 0.1, v.Rate, 0.01)
	require.True(t, v.EtaValid)
	require.InDelta(t, 5*time.Second, v.Eta, float64(time.Second))
	v = tr.ObserveFraction(t0.Add(6*time.Second), -1)
	require.Equal(t, float32(-1), v.Fraction)
	require.False(t, v.EtaValid)
}

func TestView_EtaMs(t *testing.T) {
	require.Equal(t, int64(-1), View{}.EtaMs())
	require.Equal(t, int64(1500), View{Eta: 1500 * time.Millisecond, EtaValid: true}.EtaMs())
}
