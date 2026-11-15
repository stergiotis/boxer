package stats

import (
	"image"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// FloatBins is how many bins the histogram of a float plane has.
const FloatBins = 4096

// Histogram is how often each stored sample value occurs in a plane or a
// region of one, with the mean and the standard deviation of what it
// counted. It is made by [Of] or [OfRegion] and read through its methods; the
// zero value is the histogram of nothing.
//
// Two scales meet here. A bin, its edges and [Histogram.StoredRange] are in
// stored samples, which is what was counted; [Histogram.Range],
// [Histogram.Mean], [Histogram.StdDev] and [Histogram.Percentile] are in
// values, the samples through the plane's value transform
// ([imaging.ValueTransform]); [Histogram.Value] turns one into the other.
type Histogram struct {
	// counts holds one count per bin. For an integer plane bin i is the
	// stored value i; for a float plane the bins divide [lo, hi] evenly and
	// the last one includes hi.
	counts []uint64
	// lo and hi are the smallest and largest valid stored sample.
	lo, hi float64
	// valid is the number of samples counted; invalid the number left out.
	valid, invalid uint64

	kind      imaging.SampleKindE
	transform imaging.ValueTransform
	// mean and m2 are the mean of the stored samples and the sum of their
	// squared deviations from it.
	mean, m2 float64
}

// Valid is the number of samples counted.
func (inst Histogram) Valid() uint64 { return inst.valid }

// Invalid is the number of samples left out as invalid: NaN in a float
// plane.
func (inst Histogram) Invalid() uint64 { return inst.invalid }

// Bins is the number of bins: one per value an integer kind can hold, or
// [FloatBins]; 0 when nothing was counted.
func (inst Histogram) Bins() int { return len(inst.counts) }

// Count is the number of samples in bin i, and 0 for a bin that does not
// exist.
func (inst Histogram) Count(i int) uint64 {
	if i < 0 || i >= len(inst.counts) {
		return 0
	}
	return inst.counts[i]
}

// StoredRange is the smallest and largest stored sample counted; ok is false
// when nothing was counted.
func (inst Histogram) StoredRange() (lo, hi float64, ok bool) {
	if inst.valid == 0 {
		return 0, 0, false
	}
	return inst.lo, inst.hi, true
}

// Of counts the samples of a plane.
func Of(plane imaging.Plane) (h Histogram) {
	h, _ = OfRegion(plane, image.Rect(0, 0, plane.Width(), plane.Height()))
	return h
}

// OfRegion counts the samples of a plane inside rect — columns
// [Min.X, Max.X), rows [Min.Y, Max.Y) — at full resolution. It costs time
// proportional to the region's pixels, and for a float plane reads them
// twice: once for the range the bins divide, once to count. A rect that is
// empty or reaches outside the plane is an error.
func OfRegion(plane imaging.Plane, rect image.Rectangle) (h Histogram, err error) {
	if rect.Empty() || !rect.In(image.Rect(0, 0, plane.Width(), plane.Height())) {
		return h, eb.Build().Stringer("region", rect).Int("width", plane.Width()).Int("height", plane.Height()).
			Errorf("region to count is empty or not inside the plane")
	}
	h.kind, h.transform = plane.Kind(), plane.Transform()
	w := plane.Width()
	switch plane.Kind() {
	case imaging.SampleKindU8:
		h.counts = make([]uint64, 1<<8)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for _, v := range plane.U8()[y*w+rect.Min.X : y*w+rect.Max.X] {
				h.counts[v]++
			}
		}
	case imaging.SampleKindU16:
		h.counts = make([]uint64, 1<<16)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for _, v := range plane.U16()[y*w+rect.Min.X : y*w+rect.Max.X] {
				h.counts[v]++
			}
		}
	case imaging.SampleKindF32:
		h.countFloats(plane.F32(), w, rect)
		return h, nil
	default:
		return h, nil
	}
	// An integer plane's range and moments are read off its counts: the
	// mean first, then the squared deviations from it, which loses nothing
	// a float64 can hold.
	h.valid = uint64(rect.Dx()) * uint64(rect.Dy())
	h.lo, h.hi = math.NaN(), math.NaN()
	var sum float64
	for i, n := range h.counts {
		if n == 0 {
			continue
		}
		if h.lo != h.lo {
			h.lo = float64(i)
		}
		h.hi = float64(i)
		sum += float64(i) * float64(n)
	}
	h.mean = sum / float64(h.valid)
	for i, n := range h.counts {
		if n != 0 {
			d := float64(i) - h.mean
			h.m2 += d * d * float64(n)
		}
	}
	return h, nil
}

// countFloats fills the histogram of a float region: the range first, then
// the counts and the running mean and squared deviations (Welford's
// recurrence, which does not subtract two large sums).
func (inst *Histogram) countFloats(samples []float32, w int, rect image.Rectangle) {
	lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for _, v := range samples[y*w+rect.Min.X : y*w+rect.Max.X] {
			if v == v {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	if lo > hi {
		inst.invalid = uint64(rect.Dx()) * uint64(rect.Dy())
		return
	}
	inst.lo, inst.hi = float64(lo), float64(hi)
	inst.counts = make([]uint64, FloatBins)
	scale := 0.0
	if hi > lo {
		scale = FloatBins / (inst.hi - inst.lo)
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for _, v := range samples[y*w+rect.Min.X : y*w+rect.Max.X] {
			if v != v {
				inst.invalid++
				continue
			}
			x := float64(v)
			inst.counts[min(int((x-inst.lo)*scale), FloatBins-1)]++
			inst.valid++
			d := x - inst.mean
			inst.mean += d / float64(inst.valid)
			inst.m2 += d * (x - inst.mean)
		}
	}
}

// Mean is the arithmetic mean of the values counted; ok is false when
// nothing was counted.
func (inst Histogram) Mean() (mean float64, ok bool) {
	if inst.valid == 0 {
		return 0, false
	}
	return inst.Value(inst.mean), true
}

// StdDev is the standard deviation of the values counted, taken over the
// samples as the whole population (the divisor is their number, not one
// less): it describes these samples and estimates nothing beyond them. ok is
// false when nothing was counted.
func (inst Histogram) StdDev() (sd float64, ok bool) {
	if inst.valid == 0 {
		return 0, false
	}
	return math.Abs(inst.transform.Scale) * math.Sqrt(inst.m2/float64(inst.valid)), true
}

// Percentile is [Histogram.PercentileStored] as a value.
func (inst Histogram) Percentile(p float64) (value float64, ok bool) {
	stored, ok := inst.PercentileStored(p)
	return inst.Value(stored), ok
}

// IsExact reports whether every bin is one sample value, so that nothing was
// lost in counting.
func (inst Histogram) IsExact() bool { return inst.kind.IsInteger() }

// BinWidth is the width of one bin in stored units: 1 for an integer plane.
func (inst Histogram) BinWidth() float64 {
	if inst.kind.IsInteger() || len(inst.counts) == 0 {
		return 1
	}
	return (inst.hi - inst.lo) / float64(len(inst.counts))
}

// BinStored is the stored value at the lower edge of bin i.
func (inst Histogram) BinStored(i int) float64 {
	if inst.kind.IsInteger() {
		return float64(i)
	}
	return inst.lo + float64(i)*inst.BinWidth()
}

// Value turns a stored sample into the value it stands for, through the
// value transform of the plane that was counted.
func (inst Histogram) Value(stored float64) float64 { return inst.transform.Apply(stored) }

// Range is the smallest and largest value in the plane; ok is false when it
// has no valid sample.
func (inst Histogram) Range() (lo, hi float64, ok bool) {
	if inst.valid == 0 {
		return 0, 0, false
	}
	a, b := inst.Value(inst.lo), inst.Value(inst.hi)
	return min(a, b), max(a, b), true
}

// PercentileStored is the stored sample below or at which p percent of the
// valid samples lie, p between 0 and 100: for an integer plane the smallest
// sample value whose cumulative count reaches that share, which is a value
// that occurs; for a float plane a point within the bin where the count is
// reached. ok is false for a plane with no valid sample or a p outside
// [0, 100].
func (inst Histogram) PercentileStored(p float64) (stored float64, ok bool) {
	if inst.valid == 0 || !(p >= 0 && p <= 100) {
		return 0, false
	}
	if p == 0 {
		return inst.lo, true
	}
	if p == 100 {
		return inst.hi, true
	}
	target := p / 100 * float64(inst.valid)
	var seen float64
	for i, n := range inst.counts {
		if n == 0 {
			continue
		}
		if seen+float64(n) >= target {
			if inst.kind.IsInteger() {
				return float64(i), true
			}
			within := (target - seen) / float64(n)
			return min(inst.BinStored(i)+within*inst.BinWidth(), inst.hi), true
		}
		seen += float64(n)
	}
	return inst.hi, true
}
