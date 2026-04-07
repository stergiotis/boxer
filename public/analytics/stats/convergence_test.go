package stats

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func TestConvergence(t *testing.T) {
	// Scenario:
	// We use a window of 100 samples to ensure we don't stop on a short lucky streak.
	detector := NewConvergenceDetector(100, 0.000001)

	// Source: Normal distribution (Mean=50, StdDev=10)
	// Theoretically, the sample StdDev should converge to 10.0.
	rnd := rand.New(rand.NewSource(42))

	targetMean := 50.0
	targetStdDev := 10.0

	var stabilizedAt int
	maxIterations := 10000

	fmt.Println("Step | Current Mean | Current StdDev | Status")
	fmt.Println("-----|--------------|----------------|-------")

	for i := 1; i <= maxIterations; i++ {
		val := rnd.NormFloat64()*targetStdDev + targetMean

		isStable := detector.Push(val)

		if i%500 == 0 || isStable {
			fmt.Printf("%4d | %12.4f | %14.4f | Stable: %v\n",
				i, detector.Mean(), detector.StdDev(), isStable)
		}

		if isStable {
			stabilizedAt = i
			break
		}
	}

	if stabilizedAt == 0 {
		t.Errorf("Failed to stabilize within %d iterations", maxIterations)
	} else {
		fmt.Printf("\nConverged at N=%d\n", stabilizedAt)

		// Verify result accuracy
		actualStdDev := detector.StdDev()
		err := math.Abs(actualStdDev - targetStdDev)
		if err > 1.0 { // Allow some randomness error
			t.Errorf("Converged too early? Result %.4f is far from target %.4f", actualStdDev, targetStdDev)
		}
	}
}
