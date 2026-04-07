package stats

import (
	"encoding/json"
	"iter"
	"math"
)

// StreamStats computes Mean, Variance, Skewness, Kurtosis, Min, and Max
// using Compensated (Kahan) Welford's Algorithm.
type StreamStats struct {
	n int64

	min float64
	max float64

	// Mean
	mean  float64
	cMean float64

	// Sum of squares of differences from the current mean (M2)
	m2  float64
	cM2 float64

	// Sum of cubes of differences from the current mean (M3)
	m3  float64
	cM3 float64

	// Sum of fourth powers of differences from the current mean (M4)
	m4  float64
	cM4 float64
}

func NewStreamStats() (inst *StreamStats) {
	inst = &StreamStats{}
	return
}

func (inst *StreamStats) Reset() {
	inst.n = 0
	inst.min = 0.0
	inst.max = 0.0
	inst.mean = 0.0
	inst.cMean = 0.0
	inst.m2 = 0.0
	inst.cM2 = 0.0
	inst.m3 = 0.0
	inst.cM3 = 0.0
	inst.m4 = 0.0
	inst.cM4 = 0.0
}

// Push adds a single float64 value.
// Complexity: O(1)
func (inst *StreamStats) Push(x float64) {
	if inst.n == 0 {
		inst.min = x
		inst.max = x
	} else {
		if x < inst.min {
			inst.min = x
		}
		if x > inst.max {
			inst.max = x
		}
	}

	n1 := float64(inst.n)
	inst.n++
	n := float64(inst.n)

	delta := x - inst.mean
	deltaN := delta / n
	term1 := delta * deltaN * n1

	// Update M4 (uses OLD M2 and M3)
	valM4 := term1*deltaN*deltaN*(n*n-3*n+3) + 6*deltaN*deltaN*inst.m2 - 4*deltaN*inst.m3
	inst.kahanAddM4(valM4)

	// Update M3 (uses OLD M2)
	valM3 := term1*deltaN*(n-2) - 3*deltaN*inst.m2
	inst.kahanAddM3(valM3)

	// Update M2
	inst.kahanAddM2(term1)

	// Update Mean
	inst.kahanAddMean(deltaN)
}

// --- Kahan Helpers ---

func (inst *StreamStats) kahanAddMean(inc float64) {
	y := inc - inst.cMean
	t := inst.mean + y
	inst.cMean = (t - inst.mean) - y
	inst.mean = t
}

func (inst *StreamStats) kahanAddM2(inc float64) {
	y := inc - inst.cM2
	t := inst.m2 + y
	inst.cM2 = (t - inst.m2) - y
	inst.m2 = t
}

func (inst *StreamStats) kahanAddM3(inc float64) {
	y := inc - inst.cM3
	t := inst.m3 + y
	inst.cM3 = (t - inst.m3) - y
	inst.m3 = t
}

func (inst *StreamStats) kahanAddM4(inc float64) {
	y := inc - inst.cM4
	t := inst.m4 + y
	inst.cM4 = (t - inst.m4) - y
	inst.m4 = t
}

// --- Bulk Helpers ---

func (inst *StreamStats) PushFloat32(x float32) {
	inst.Push(float64(x))
}

func (inst *StreamStats) PushSeq(seq iter.Seq[float64]) {
	for v := range seq {
		inst.Push(v)
	}
}

// --- Getters ---

func (inst *StreamStats) Min() float64 { return inst.min }
func (inst *StreamStats) Max() float64 { return inst.max }
func (inst *StreamStats) Mean() float64 {
	if inst.n == 0 {
		return 0.0
	}
	return inst.mean
}
func (inst *StreamStats) Count() int64 { return inst.n }

// Variance returns the sample variance.
func (inst *StreamStats) Variance() float64 {
	if inst.n < 2 {
		return 0.0
	}
	return inst.m2 / float64(inst.n-1)
}

func (inst *StreamStats) StdDev() float64 {
	return math.Sqrt(inst.Variance())
}

// Skewness returns the Fisher-Pearson coefficient of skewness.
// Returns 0 if variance is 0 or N < 3.
func (inst *StreamStats) Skewness() float64 {
	if inst.n < 3 || inst.m2 == 0 {
		return 0.0
	}
	return math.Sqrt(float64(inst.n)) * inst.m3 / math.Pow(inst.m2, 1.5)
}

// Kurtosis returns the Excess Kurtosis (fisher).
// Normal distribution = 0.0.
// Returns 0 if variance is 0 or N < 4.
func (inst *StreamStats) Kurtosis() float64 {
	if inst.n < 4 || inst.m2 == 0 {
		return 0.0
	}
	return (float64(inst.n)*inst.m4)/(inst.m2*inst.m2) - 3.0
}

// --- Serialization ---

type stateDTO struct {
	N     int64   `json:"n"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
	CMean float64 `json:"c_mean"`
	M2    float64 `json:"m2"`
	CM2   float64 `json:"c_m2"`
	M3    float64 `json:"m3"`
	CM3   float64 `json:"c_m3"`
	M4    float64 `json:"m4"`
	CM4   float64 `json:"c_m4"`
}

func (inst *StreamStats) MarshalJSON() (data []byte, err error) {
	data, err = json.Marshal(stateDTO{
		N: inst.n, Min: inst.min, Max: inst.max,
		Mean: inst.mean, CMean: inst.cMean,
		M2: inst.m2, CM2: inst.cM2,
		M3: inst.m3, CM3: inst.cM3,
		M4: inst.m4, CM4: inst.cM4,
	})
	return
}

func (inst *StreamStats) UnmarshalJSON(data []byte) (err error) {
	var dto stateDTO
	err = json.Unmarshal(data, &dto)
	if err != nil {
		return
	}
	inst.n = dto.N
	inst.min = dto.Min
	inst.max = dto.Max
	inst.mean = dto.Mean
	inst.cMean = dto.CMean
	inst.m2 = dto.M2
	inst.cM2 = dto.CM2
	inst.m3 = dto.M3
	inst.cM3 = dto.CM3
	inst.m4 = dto.M4
	inst.cM4 = dto.CM4
	return
}

// Merge combines another StreamStats into this one using Pébay's formulas.
func (inst *StreamStats) Merge(other *StreamStats) {
	if other.n == 0 {
		return
	}

	if inst.n == 0 {
		*inst = *other
		return
	}

	if other.min < inst.min {
		inst.min = other.min
	}
	if other.max > inst.max {
		inst.max = other.max
	}

	n1 := float64(inst.n)
	n2 := float64(other.n)
	n := n1 + n2

	delta := other.mean - inst.mean
	delta2 := delta * delta
	delta3 := delta2 * delta
	delta4 := delta2 * delta2

	inst.m4 += other.m4 +
		delta4*(n1*n2*(n1*n1-n1*n2+n2*n2)/(n*n*n)) +
		6.0*delta2*(n1*n1*other.m2+n2*n2*inst.m2)/(n*n) +
		4.0*delta*(n1*other.m3-n2*inst.m3)/n

	inst.m3 += other.m3 +
		delta3*(n1*n2*(n1-n2)/(n*n)) +
		3.0*delta*(n1*other.m2-n2*inst.m2)/n

	inst.m2 += other.m2 + delta2*(n1*n2/n)

	inst.mean += delta * (n2 / n)

	inst.n += other.n

	// Reset Kahan compensations (not valid across merged streams)
	inst.cMean = 0
	inst.cM2 = 0
	inst.cM3 = 0
	inst.cM4 = 0
}

// IsMeanPrecise returns true if the 95% confidence interval of the mean
// is smaller than the given relative precision.
func (inst *StreamStats) IsMeanPrecise(relativePrecision float64) (precise bool) {
	if inst.n < 30 {
		return false
	}

	mean := inst.Mean()
	if mean == 0 {
		return false
	}

	sem := inst.StdDev() / math.Sqrt(float64(inst.n))
	marginOfError := 1.96 * sem

	precise = (marginOfError / math.Abs(mean)) < relativePrecision
	return
}
