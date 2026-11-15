package stats

import (
	"math"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// FloatBins is how many bins the histogram of a float plane has.
const FloatBins = 4096

// Histogram is how often each stored sample value occurs in a plane. The
// zero value is the histogram of nothing.
type Histogram struct {
	// Counts holds one count per bin. For an integer plane bin i is the
	// stored value i; for a float plane the bins divide [Lo, Hi] evenly and
	// the last one includes Hi.
	Counts []uint64
	// Lo and Hi are the smallest and largest valid stored sample.
	Lo, Hi float64
	// Valid is the number of samples counted; Invalid the number left out.
	Valid, Invalid uint64

	kind      imaging.SampleKindE
	transform imaging.ValueTransform
}

// Of counts the samples of a plane.
func Of(plane imaging.Plane) (h Histogram) {
	h.kind, h.transform = plane.Kind(), plane.Transform()
	switch plane.Kind() {
	case imaging.SampleKindU8:
		h.Counts = make([]uint64, 1<<8)
		for _, v := range plane.U8() {
			h.Counts[v]++
		}
		h.Valid = uint64(len(plane.U8()))
	case imaging.SampleKindU16:
		h.Counts = make([]uint64, 1<<16)
		for _, v := range plane.U16() {
			h.Counts[v]++
		}
		h.Valid = uint64(len(plane.U16()))
	case imaging.SampleKindF32:
		lo, hi, ok := plane.StoredRange()
		if !ok {
			h.Invalid = uint64(len(plane.F32()))
			return h
		}
		h.Lo, h.Hi = lo, hi
		h.Counts = make([]uint64, FloatBins)
		scale := 0.0
		if hi > lo {
			scale = FloatBins / (hi - lo)
		}
		for _, v := range plane.F32() {
			if v != v {
				h.Invalid++
				continue
			}
			h.Counts[min(int((float64(v)-lo)*scale), FloatBins-1)]++
			h.Valid++
		}
		return h
	default:
		return h
	}
	// The range of an integer plane is read off its counts.
	h.Lo, h.Hi = math.NaN(), math.NaN()
	for i, n := range h.Counts {
		if n == 0 {
			continue
		}
		if h.Lo != h.Lo {
			h.Lo = float64(i)
		}
		h.Hi = float64(i)
	}
	return h
}

// IsExact reports whether every bin is one sample value, so that nothing was
// lost in counting.
func (inst Histogram) IsExact() bool { return inst.kind.IsInteger() }

// BinWidth is the width of one bin in stored units: 1 for an integer plane.
func (inst Histogram) BinWidth() float64 {
	if inst.kind.IsInteger() || len(inst.Counts) == 0 {
		return 1
	}
	return (inst.Hi - inst.Lo) / float64(len(inst.Counts))
}

// BinStored is the stored value at the lower edge of bin i.
func (inst Histogram) BinStored(i int) float64 {
	if inst.kind.IsInteger() {
		return float64(i)
	}
	return inst.Lo + float64(i)*inst.BinWidth()
}

// Value turns a stored sample into the value it stands for, through the
// value transform of the plane that was counted.
func (inst Histogram) Value(stored float64) float64 { return inst.transform.Apply(stored) }

// Range is the smallest and largest value in the plane; ok is false when it
// has no valid sample.
func (inst Histogram) Range() (lo, hi float64, ok bool) {
	if inst.Valid == 0 {
		return 0, 0, false
	}
	a, b := inst.Value(inst.Lo), inst.Value(inst.Hi)
	return min(a, b), max(a, b), true
}

// PercentileStored is the stored sample below or at which p percent of the
// valid samples lie, p between 0 and 100: for an integer plane the smallest
// sample value whose cumulative count reaches that share, which is a value
// that occurs; for a float plane a point within the bin where the count is
// reached. ok is false for a plane with no valid sample or a p outside
// [0, 100].
func (inst Histogram) PercentileStored(p float64) (stored float64, ok bool) {
	if inst.Valid == 0 || !(p >= 0 && p <= 100) {
		return 0, false
	}
	if p == 0 {
		return inst.Lo, true
	}
	if p == 100 {
		return inst.Hi, true
	}
	target := p / 100 * float64(inst.Valid)
	var seen float64
	for i, n := range inst.Counts {
		if n == 0 {
			continue
		}
		if seen+float64(n) >= target {
			if inst.kind.IsInteger() {
				return float64(i), true
			}
			within := (target - seen) / float64(n)
			return min(inst.BinStored(i)+within*inst.BinWidth(), inst.Hi), true
		}
		seen += float64(n)
	}
	return inst.Hi, true
}
