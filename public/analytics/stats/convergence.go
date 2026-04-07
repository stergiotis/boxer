package stats

import (
	"math"
)

// ConvergenceDetector wraps StreamStats to detect when the standard deviation stabilizes.
type ConvergenceDetector struct {
	*StreamStats

	// Configuration
	windowSize int     // How many samples to look back (e.g., 100)
	tolerance  float64 // Relative error threshold (e.g., 0.001 for 0.1%)

	// State
	history []float64 // Ring buffer storing the last 'windowSize' StdDevs
	head    int       // Current position in ring buffer
	isFull  bool      // Whether the history buffer is filled
}

func NewConvergenceDetector(windowSize int, varianceTolerance float64) (inst *ConvergenceDetector) {
	if windowSize < 2 {
		windowSize = 2
	}
	inst = &ConvergenceDetector{
		StreamStats: NewStreamStats(),
		windowSize:  windowSize,
		tolerance:   varianceTolerance,
		history:     make([]float64, windowSize),
	}
	return
}

func (inst *ConvergenceDetector) CheckConvergence() (stable bool) {
	currentVariance := inst.Variance()

	if !inst.isFull {
		inst.history[inst.head] = currentVariance
		inst.head++
		if inst.head >= inst.windowSize {
			inst.head = 0
			inst.isFull = true
		}
		return false
	}

	oldVariance := inst.history[inst.head]

	inst.history[inst.head] = currentVariance
	inst.head = (inst.head + 1) % inst.windowSize

	if currentVariance == 0 {
		return oldVariance == 0
	}

	diff := math.Abs(currentVariance - oldVariance)
	relErr := diff / currentVariance

	return relErr < inst.tolerance
}

func (inst *ConvergenceDetector) Reset() {
	inst.StreamStats.Reset()
	inst.head = 0
	inst.isFull = false
	clear(inst.history)
}

func (inst *ConvergenceDetector) Push(x float64) (stable bool) {
	inst.StreamStats.Push(x)
	stable = inst.CheckConvergence()
	return
}
