package progressbar

import (
	"math"
	"testing"
	"time"
)

func TestEstimator_FirstSampleDoesNotYieldETA(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	est.Update(t0.Add(time.Second), 10)
	if _, ok := est.EstimateETA(90); ok {
		t.Fatalf("expected ETA invalid after 1 sample")
	}
}

func TestEstimator_SteadyRateConverges(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	count := int64(0)
	for i := 1; i <= 15; i++ {
		count += 100
		est.Update(t0.Add(time.Duration(i)*time.Second), count)
	}
	rate := est.SmoothedRate()
	if math.Abs(rate-100) > 5 {
		t.Fatalf("expected rate ≈ 100, got %f", rate)
	}
	if math.Abs(est.SmoothedTrend()) > 5 {
		t.Fatalf("expected trend ≈ 0 under steady rate, got %f", est.SmoothedTrend())
	}
}

func TestEstimator_AcceleratingTrendPositive(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	count := int64(0)
	for i := 1; i <= 12; i++ {
		rate := 50 + 10*i
		count += int64(rate)
		est.Update(t0.Add(time.Duration(i)*time.Second), count)
	}
	if est.SmoothedTrend() <= 0 {
		t.Fatalf("expected positive trend during accel, got %f", est.SmoothedTrend())
	}
}

func TestEstimator_DeceleratingTrendNegative(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	count := int64(0)
	for i := 1; i <= 12; i++ {
		rate := 200 - 15*i
		if rate < 10 {
			rate = 10
		}
		count += int64(rate)
		est.Update(t0.Add(time.Duration(i)*time.Second), count)
	}
	if est.SmoothedTrend() >= 0 {
		t.Fatalf("expected negative trend during decel, got %f", est.SmoothedTrend())
	}
}

func TestEstimator_EstimateETAZeroRemaining(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	est.Update(t0.Add(time.Second), 100)
	est.Update(t0.Add(2*time.Second), 200)
	eta, ok := est.EstimateETA(0)
	if !ok || eta != 0 {
		t.Fatalf("expected (0, true) for remaining=0, got (%v, %v)", eta, ok)
	}
}

func TestEstimator_UpdateBelowFloorIsIgnored(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	// dt = 10 ms — below the 50 ms floor; should be dropped silently.
	est.Update(t0.Add(10*time.Millisecond), 100)
	if est.Samples() != 0 {
		t.Fatalf("expected sub-50ms update to be ignored, got samples=%d", est.Samples())
	}
}

func TestEstimator_DampingDecreasePassesThrough(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	// Slow: ~10 items/s ⇒ remaining=1000 projects to ~100s.
	est.Update(t0.Add(time.Second), 10)
	est.Update(t0.Add(2*time.Second), 20)
	eta1, ok1 := est.EstimateETA(1000)
	if !ok1 || eta1 <= 0 {
		t.Fatalf("expected valid first ETA, got (%v, %v)", eta1, ok1)
	}
	// Speed up hard: DES level picks up the higher rate, raw ETA drops.
	est.Update(t0.Add(3*time.Second), 220)
	eta2, _ := est.EstimateETA(1000)
	if eta2 >= eta1 {
		t.Fatalf("decrease should pass through: eta1=%v eta2=%v", eta1, eta2)
	}
}

func TestEstimator_DampingSmallIncreaseSuppressed(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	// Steady 100 items/s for 3 seconds, fully seed the level.
	est.Update(t0.Add(time.Second), 100)
	est.Update(t0.Add(2*time.Second), 200)
	est.Update(t0.Add(3*time.Second), 300)
	eta1, _ := est.EstimateETA(1000)
	// Tiny slowdown: 95 items/s — raw ETA creeps up by < 10%.
	est.Update(t0.Add(4*time.Second), 395)
	eta2, _ := est.EstimateETA(1000)
	if eta2 > eta1 {
		t.Fatalf("small increase should be suppressed by damping: eta1=%v eta2=%v", eta1, eta2)
	}
}

func TestEstimator_DampingLargeIncreaseBreaksThrough(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	// Seed fast: 200 items/s.
	est.Update(t0.Add(time.Second), 200)
	est.Update(t0.Add(2*time.Second), 400)
	eta1, _ := est.EstimateETA(1000)
	// Collapse to 10 items/s — huge rise in raw ETA; must break through.
	for i := 3; i <= 8; i++ {
		count := int64(400 + 10*(i-2))
		est.Update(t0.Add(time.Duration(i)*time.Second), count)
	}
	eta2, _ := est.EstimateETA(1000)
	if eta2 <= eta1 {
		t.Fatalf("large increase should break through damping: eta1=%v eta2=%v", eta1, eta2)
	}
}

func TestEstimator_ResetClearsState(t *testing.T) {
	est := NewEstimator()
	t0 := time.Unix(0, 0)
	est.Start(t0, 0)
	est.Update(t0.Add(time.Second), 100)
	est.Update(t0.Add(2*time.Second), 200)
	_, _ = est.EstimateETA(500)
	est.Reset(t0.Add(10*time.Second), 0)
	if est.SmoothedRate() != 0 || est.SmoothedTrend() != 0 || est.Samples() != 0 || est.DisplayedETA() != 0 {
		t.Fatalf("Reset should clear all state: rate=%v trend=%v samples=%d displayedETA=%v",
			est.SmoothedRate(), est.SmoothedTrend(), est.Samples(), est.DisplayedETA())
	}
}
