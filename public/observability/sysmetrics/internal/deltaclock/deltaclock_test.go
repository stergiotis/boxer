package deltaclock_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/deltaclock"
)

func TestDiff_Forward(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	prev := deltaclock.Tick{At: t0, Value: 100}
	now := deltaclock.Tick{At: t0.Add(2 * time.Second), Value: 250}

	dv, dt := deltaclock.Diff(prev, now, 0)
	assert.Equal(t, uint64(150), dv)
	assert.Equal(t, 2*time.Second, dt)
}

func TestDiff_BackwardsNoRollover(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	prev := deltaclock.Tick{At: t0, Value: 100}
	now := deltaclock.Tick{At: t0.Add(time.Second), Value: 50}

	dv, _ := deltaclock.Diff(prev, now, 0)
	assert.Equal(t, uint64(0), dv, "backwards step without rollover hint must clamp to 0")
}

func TestDiff_RolloverU32(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	prev := deltaclock.Tick{At: t0, Value: math.MaxUint32 - 9}
	now := deltaclock.Tick{At: t0.Add(time.Second), Value: 100}

	dv, _ := deltaclock.Diff(prev, now, math.MaxUint32)
	// Steps prev→MaxUint32 (9) + wrap (1) + 0→now (100) = 110.
	assert.Equal(t, uint64(110), dv)
}

func TestRatePerSecond(t *testing.T) {
	assert.InDelta(t, 50.0, deltaclock.RatePerSecond(100, 2*time.Second), 0.0001)
	assert.Equal(t, 0.0, deltaclock.RatePerSecond(100, 0))
	assert.Equal(t, 0.0, deltaclock.RatePerSecond(100, -1*time.Second))
}

func TestCounter_FirstSampleReturnsZero(t *testing.T) {
	c := deltaclock.NewCounter(0)
	t0 := time.Unix(1_000_000, 0)

	dv, dt := c.Sample(t0, 100)
	assert.Equal(t, uint64(0), dv)
	assert.Equal(t, time.Duration(0), dt)
	assert.True(t, c.Primed())
}

func TestCounter_SecondSample(t *testing.T) {
	c := deltaclock.NewCounter(0)
	t0 := time.Unix(1_000_000, 0)

	c.Sample(t0, 100)
	dv, dt := c.Sample(t0.Add(2*time.Second), 250)
	assert.Equal(t, uint64(150), dv)
	assert.Equal(t, 2*time.Second, dt)
}

func TestCounter_RolloverU32(t *testing.T) {
	c := deltaclock.NewCounter(math.MaxUint32)
	t0 := time.Unix(1_000_000, 0)

	c.Sample(t0, math.MaxUint32-9)
	dv, _ := c.Sample(t0.Add(time.Second), 100)
	assert.Equal(t, uint64(110), dv)
}

func TestCounter_Reset(t *testing.T) {
	c := deltaclock.NewCounter(0)
	t0 := time.Unix(1_000_000, 0)
	c.Sample(t0, 100)
	c.Reset()
	assert.False(t, c.Primed())

	dv, _ := c.Sample(t0.Add(time.Second), 250)
	assert.Equal(t, uint64(0), dv, "Reset must drop prior sample so the next is 'first' again")
}
