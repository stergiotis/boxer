package estimator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInst_ThroughputZeroWithFewerThanTwoSamples(t *testing.T) {
	e := New()
	assert.Equal(t, 0.0, e.ThroughputPerSec())
	e.Add(100, 1_000)
	assert.Equal(t, 0.0, e.ThroughputPerSec())
}

func TestInst_ThroughputComputedFromWindow(t *testing.T) {
	e := NewWith(2_000, 64)
	e.Add(0, 0)
	e.Add(50, 1_000)
	e.Add(100, 2_000)
	// 100 units in 2_000 ms ⇒ 50/s
	assert.InDelta(t, 50.0, e.ThroughputPerSec(), 0.5)
}

func TestInst_ThroughputClampsNegativeDeltaToZero(t *testing.T) {
	e := NewWith(2_000, 64)
	e.Add(100, 0)
	e.Add(0, 1_000) // counter reset
	assert.Equal(t, 0.0, e.ThroughputPerSec())
}

func TestInst_EtaMsFromThroughput(t *testing.T) {
	e := NewWith(2_000, 64)
	e.Add(0, 0)
	e.Add(50, 1_000)
	e.Add(100, 2_000)
	// 50/s, 900 remaining ⇒ 18_000 ms
	eta := e.EtaMs(100, 1000)
	assert.InDelta(t, 18_000, float64(eta), 1_000)
}

func TestInst_EtaMsNegativeWhenIndeterminate(t *testing.T) {
	e := NewWith(2_000, 64)
	e.Add(0, 0)
	e.Add(100, 1_000)
	assert.EqualValues(t, -1, e.EtaMs(100, 0))
}

func TestInst_EtaMsNegativeWhenCurrentPastTotal(t *testing.T) {
	e := NewWith(2_000, 64)
	e.Add(0, 0)
	e.Add(100, 1_000)
	assert.EqualValues(t, -1, e.EtaMs(150, 100))
}

func TestInst_EtaMsNegativeWhenStalled(t *testing.T) {
	e := NewWith(2_000, 64)
	e.Add(100, 0)
	e.Add(100, 1_000)
	assert.EqualValues(t, -1, e.EtaMs(100, 1000))
}

func TestHumanize_BytesProgress(t *testing.T) {
	s := Humanize(1_300_000_000, 3_400_000_000, UnitBytes, 18_900_000, 110_000)
	// humanize.IBytes uses IEC units (GiB) — more accurate for byte
	// counts than SI (GB). Don't pin the exact rate-rounding; just
	// assert the parts that matter for the gate.
	assert.Contains(t, s, "1.2 GiB")
	assert.Contains(t, s, "/ 3.2 GiB")
	assert.Contains(t, s, "%")
	assert.Contains(t, s, "left")
}

func TestHumanize_ItemsIndeterminate(t *testing.T) {
	s := Humanize(47, 0, UnitItems, 240.0, -1)
	assert.Contains(t, s, "47 items")
	assert.Contains(t, s, "items/s")
	assert.NotContains(t, s, "left")
}

func TestHumanize_Steps(t *testing.T) {
	s := Humanize(3, 5, UnitSteps, 0, -1)
	assert.Equal(t, "step 3 of 5", s)
}

func TestHumanize_StepsIndeterminate(t *testing.T) {
	s := Humanize(3, 0, UnitSteps, 0, -1)
	assert.Equal(t, "step 3", s)
}

func TestHumanize_ZeroAndZeroIsStarting(t *testing.T) {
	s := Humanize(0, 0, UnitItems, 0, -1)
	assert.Equal(t, "starting", s)
}

func TestHumanize_PercentGate_IntegerSteps(t *testing.T) {
	// 47.0% and 47.4% should produce the same humanized string; 47.0%
	// and 48.0% should not. This is the property the emission gate
	// relies on.
	a := Humanize(470, 1000, UnitItems, 100, 5_300)
	b := Humanize(474, 1000, UnitItems, 100, 5_260)
	c := Humanize(480, 1000, UnitItems, 100, 5_200)
	assert.Equal(t, a, b, "47.0%% and 47.4%% should humanize identically")
	assert.NotEqual(t, a, c, "47%% and 48%% should differ")
}
