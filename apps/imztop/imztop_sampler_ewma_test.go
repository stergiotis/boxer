//go:build llm_generated_opus47

package imztop

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEWMATestSampler builds a bare *Sampler suitable for exercising
// updateProcCPUEWMA in isolation. Skips NewSampler so the tests don't
// require functioning /proc, /sys, GPU SDKs, etc. Only fields the
// EWMA path reads are populated.
func newEWMATestSampler(interval time.Duration) (inst *Sampler) {
	inst = &Sampler{procCPUEWMA: make(map[procEWMAKey]float32)}
	inst.intervalNs.Store(int64(interval))
	return
}

// TestSampler_EWMA_FirstSightingSeedsRaw locks in the deliberate
// design choice that a process appearing for the first time skips
// the EWMA decay and lands at its raw CPU%. Without this, a freshly
// spawned heavy process would have to climb from 0 over several
// ticks before the sort surfaces it.
func TestSampler_EWMA_FirstSightingSeedsRaw(t *testing.T) {
	inst := newEWMATestSampler(time.Second)
	smoothed := inst.updateProcCPUEWMA([]proc.Info{
		{PID: 42, StartedAtUnixMs: 1, CPUPercent: 75},
	})
	require.Len(t, smoothed, 1)
	assert.Equal(t, float32(75), smoothed[0],
		"first sighting must seed smoothed = raw (no decay from 0)")
}

// TestSampler_EWMA_EvictsDeadPIDs guards the mark-by-rebuild
// eviction strategy: a process absent from the current sample falls
// out of procCPUEWMA on the next tick. Without this, long-lived
// imztop instances would accumulate state for every process ever
// observed.
func TestSampler_EWMA_EvictsDeadPIDs(t *testing.T) {
	inst := newEWMATestSampler(time.Second)
	_ = inst.updateProcCPUEWMA([]proc.Info{
		{PID: 1, StartedAtUnixMs: 100, CPUPercent: 30},
		{PID: 2, StartedAtUnixMs: 200, CPUPercent: 50},
	})
	require.Len(t, inst.procCPUEWMA, 2)

	// PID 2 disappears between ticks.
	_ = inst.updateProcCPUEWMA([]proc.Info{
		{PID: 1, StartedAtUnixMs: 100, CPUPercent: 30},
	})
	assert.Len(t, inst.procCPUEWMA, 1, "dead PID must be evicted on the next tick")
	_, hasOne := inst.procCPUEWMA[procEWMAKey{PID: 1, StartedAt: 100}]
	_, hasTwo := inst.procCPUEWMA[procEWMAKey{PID: 2, StartedAt: 200}]
	assert.True(t, hasOne)
	assert.False(t, hasTwo)
}

// TestSampler_EWMA_DistinguishesPIDReuse exercises the PID-reuse
// safety net. A new process landing on a recently-dead PID has a
// different StartedAtUnixMs and must therefore start fresh from raw,
// not inherit the dead process's smoothed value.
func TestSampler_EWMA_DistinguishesPIDReuse(t *testing.T) {
	inst := newEWMATestSampler(time.Second)

	// Tick 1: PID 1234 starting at time A, heavy load.
	_ = inst.updateProcCPUEWMA([]proc.Info{
		{PID: 1234, StartedAtUnixMs: 1_000_000, CPUPercent: 90},
	})

	// Tick 2: PID 1234 is now a brand-new process (different
	// StartedAt) running idle. Without the (PID, StartedAt) key,
	// the new process would inherit the old one's smoothed=90.
	smoothed := inst.updateProcCPUEWMA([]proc.Info{
		{PID: 1234, StartedAtUnixMs: 2_000_000, CPUPercent: 0},
	})
	require.Len(t, smoothed, 1)
	assert.Equal(t, float32(0), smoothed[0],
		"reused PID with a fresh StartedAt must seed at raw, not inherit dead-PID state")

	// Old entry was a different key; rebuild drops it.
	_, hadOld := inst.procCPUEWMA[procEWMAKey{PID: 1234, StartedAt: 1_000_000}]
	assert.False(t, hadOld)
}

// TestSampler_EWMA_CadenceInvariant locks in the time-invariance
// fix: for the same wall-clock duration of step input, the smoothed
// value must be identical (within FP noise) regardless of cadence,
// because α is now derived per-tick from inst.Interval() and the
// fixed τ. This test would have FAILED against the prior hard-coded
// α=0.5 (which gave ~1 s half-rise at 1 Hz but only ~100 ms
// half-rise at 10 Hz).
func TestSampler_EWMA_CadenceInvariant(t *testing.T) {
	// 6 s of step input ≈ 4 τ — long enough that per-tick α
	// differences would show up loudly if the math is broken.
	// Chosen so wallClock/interval is an exact integer at every
	// cadence under test; otherwise integer truncation in the tick
	// count would smuggle a real difference in actual wall-clock
	// time into the comparison and the test wouldn't be measuring
	// cadence invariance any more.
	const stepValue float32 = 100
	const wallClock = 6 * time.Second

	feed := func(interval time.Duration) (final float32) {
		require.Zero(t, wallClock%interval, "tick count must be exact")
		inst := newEWMATestSampler(interval)
		// Seed at zero so the EWMA starts from idle (first call goes
		// through the seed-from-raw branch with raw=0).
		_ = inst.updateProcCPUEWMA([]proc.Info{
			{PID: 1, StartedAtUnixMs: 1, CPUPercent: 0},
		})
		ticks := int(wallClock / interval)
		var out []float32
		for i := 0; i < ticks; i++ {
			out = inst.updateProcCPUEWMA([]proc.Info{
				{PID: 1, StartedAtUnixMs: 1, CPUPercent: stepValue},
			})
		}
		final = out[0]
		return
	}

	s1Hz := feed(1 * time.Second)
	s10Hz := feed(100 * time.Millisecond)
	s100Hz := feed(10 * time.Millisecond)

	// Closed-form: under the time-invariant formulation,
	// s_N = 100·(1 − (1−α)^N) and (1−α)^N collapses to exp(−T/τ)
	// regardless of cadence. At T=6 s, τ=1.5 s the limit is
	// 100·(1 − exp(−4)) ≈ 98.17.
	const want float32 = 98.17
	assert.InDelta(t, want, s1Hz, 0.5, "1 Hz")
	assert.InDelta(t, want, s10Hz, 0.5, "10 Hz")
	assert.InDelta(t, want, s100Hz, 0.5, "100 Hz")

	// And — the real point of the test — the spread across cadences
	// must be tiny. The previous α=const implementation would have
	// produced s1Hz ≈ 96.9 vs s10Hz ≈ 100 vs s100Hz ≈ 100, a >3pt
	// spread; the time-invariant formulation keeps them within
	// floating-point rounding.
	assert.InDelta(t, s1Hz, s10Hz, 0.1, "cadence-invariance: 1 Hz vs 10 Hz")
	assert.InDelta(t, s1Hz, s100Hz, 0.1, "cadence-invariance: 1 Hz vs 100 Hz")
}
