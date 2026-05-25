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

	inst.history[inst.head] = currentVariance
	inst.head = (inst.head + 1) % inst.windowSize

	if !inst.isFull {
		if inst.head == 0 {
			inst.isFull = true
		} else {
			return false
		}
	}

	// Stability is declared on the full window's max-min spread,
	// not the endpoint-to-endpoint diff: a stream with periodic
	// spikes whose period matches windowSize, or an outlier that
	// happens to land on a value matching its predecessor 100
	// steps later, would otherwise fake convergence.
	minV, maxV := inst.history[0], inst.history[0]
	for _, v := range inst.history[1:] {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	if currentVariance == 0 {
		return maxV == 0
	}

	spread := maxV - minV
	return (spread / math.Abs(currentVariance)) < inst.tolerance
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
