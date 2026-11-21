package stats

import (
	"image"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// FloatBins is how many bins the histogram of a binned plane has: one of
// 32-bit or 64-bit float or signed 32-bit samples.
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
	// counts holds one count per bin. For a plane of a kind with codes
	// ([imaging.SampleKindE.Codes]) bin i is the stored value codeLo + i;
	// for any other the bins divide [binLo, binHi] evenly and the last one
	// includes binHi.
	counts []uint64
	// codeLo is the stored value of bin 0 of a histogram with one bin per
	// code.
	codeLo int64
	// lo and hi are the smallest and largest valid stored sample.
	lo, hi float64
	// binLo and binHi are what a binned histogram's bins divide: the range
	// of the samples counted, or a range given so that histograms of parts
	// of a plane line up and can be merged.
	binLo, binHi float64
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
// plane, and the value a plane declares invalid ([imaging.Plane.WithInvalid],
// ADR-0398 §SD2). They are counted here and in nothing else.
func (inst Histogram) Invalid() uint64 { return inst.invalid }

// Bins is the number of bins: one per code of a kind that has codes
// ([imaging.SampleKindE.Codes]), or [FloatBins]; 0 when nothing was counted.
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

// Of counts the samples of a plane. A plane of no sample kind gives the
// histogram of nothing.
func Of(plane imaging.Plane) (h Histogram) {
	h, _ = OfRegion(plane, image.Rect(0, 0, plane.Width(), plane.Height()))
	return h
}

// OfRegion counts the samples of a plane inside rect — columns
// [Min.X, Max.X), rows [Min.Y, Max.Y) — at full resolution (ADR-0398 §SD1,
// §SD2). It costs time proportional to the region's pixels, and for a binned
// plane reads them twice: once for the range the bins divide, once to
// count. A rect that is empty or reaches outside the plane is an error, and
// so is a sample kind this package does not count.
func OfRegion(plane imaging.Plane, rect image.Rectangle) (h Histogram, err error) {
	if err = checkRegion(plane, rect); err != nil {
		return
	}
	return count(plane, runs{rect: rect}, 0, 0, false)
}

// count fills the histogram of the samples r reads: one bin per code for a
// kind with codes, [FloatBins] bins over [binLo, binHi] when given or the
// samples' own range otherwise for a binned kind.
func count(plane imaging.Plane, r runs, binLo, binHi float64, given bool) (h Histogram, err error) {
	h.kind, h.transform = plane.Kind(), plane.Transform()
	inv, hasInv := plane.Invalid()
	w := plane.Width()
	switch plane.Kind() {
	case imaging.SampleKindU8:
		h.countCodes(r, inv, hasInv, func(y, x0, x1 int, counts []uint64) {
			for _, v := range plane.U8()[y*w+x0 : y*w+x1] {
				counts[v]++
			}
		})
	case imaging.SampleKindU16:
		h.countCodes(r, inv, hasInv, func(y, x0, x1 int, counts []uint64) {
			for _, v := range plane.U16()[y*w+x0 : y*w+x1] {
				counts[v]++
			}
		})
	case imaging.SampleKindI16:
		h.countCodes(r, inv, hasInv, func(y, x0, x1 int, counts []uint64) {
			// The bin of a stored value is its offset from −32768, which is
			// its bit pattern with the sign bit flipped.
			for _, v := range plane.I16()[y*w+x0 : y*w+x1] {
				counts[uint16(v)^0x8000]++
			}
		})
	case imaging.SampleKindF32:
		countBinned(&h, plane.F32(), w, r, inv, hasInv, binLo, binHi, given)
	case imaging.SampleKindF64:
		countBinned(&h, plane.F64(), w, r, inv, hasInv, binLo, binHi, given)
	case imaging.SampleKindI32:
		countBinned(&h, plane.I32(), w, r, inv, hasInv, binLo, binHi, given)
	default:
		return Histogram{}, eb.Build().Stringer("kind", plane.Kind()).Errorf("no histogram for planes of this sample kind")
	}
	return h, nil
}

// runs is what a count reads, one run of a row's samples at a time: the rows
// of a rectangle, or the row spans of a shape when spans is not nil.
type runs struct {
	rect  image.Rectangle
	spans []RowSpan
}

func (inst runs) len() int {
	if inst.spans != nil {
		return len(inst.spans)
	}
	return inst.rect.Dy()
}

// at is run i: its row and its columns [x0, x1).
func (inst runs) at(i int) (y, x0, x1 int) {
	if inst.spans != nil {
		s := inst.spans[i]
		return s.Row, s.Min, s.Max
	}
	return inst.rect.Min.Y + i, inst.rect.Min.X, inst.rect.Max.X
}

// total is the number of samples the runs read.
func (inst runs) total() (n uint64) {
	if inst.spans == nil {
		return uint64(inst.rect.Dx()) * uint64(inst.rect.Dy())
	}
	for _, s := range inst.spans {
		n += uint64(s.Max - s.Min)
	}
	return n
}

func checkRegion(plane imaging.Plane, rect image.Rectangle) error {
	if rect.Empty() || !rect.In(image.Rect(0, 0, plane.Width(), plane.Height())) {
		return eb.Build().Stringer("region", rect).Int("width", plane.Width()).Int("height", plane.Height()).
			Errorf("region to count is empty or not inside the plane")
	}
	return nil
}

// countCodes fills the histogram of a region of a plane whose kind has
// codes: count fills one run's counts, indexed by stored − lo. The declared
// invalid value is counted with the rest and then moved to the invalid
// count, so that the inner loop carries no test for it. The range and the
// moments are read off the counts: the mean first, then the squared
// deviations from it, which loses nothing a float64 can hold.
func (inst *Histogram) countCodes(r runs, inv float64, hasInv bool, count func(y, x0, x1 int, counts []uint64)) {
	lo, n, _ := inst.kind.Codes()
	inst.codeLo = lo
	inst.counts = make([]uint64, n)
	for i := range r.len() {
		y, x0, x1 := r.at(i)
		count(y, x0, x1, inst.counts)
	}
	total := r.total()
	if hasInv {
		// The declared value is a code of the kind (Plane.WithInvalid).
		i := int(int64(inv) - lo)
		inst.invalid, inst.counts[i] = inst.counts[i], 0
	}
	inst.valid = total - inst.invalid
	if inst.valid == 0 {
		return
	}
	inst.lo, inst.hi = math.NaN(), math.NaN()
	var sum float64
	for i, c := range inst.counts {
		if c == 0 {
			continue
		}
		v := float64(lo + int64(i))
		if inst.lo != inst.lo {
			inst.lo = v
		}
		inst.hi = v
		sum += v * float64(c)
	}
	inst.mean = sum / float64(inst.valid)
	for i, c := range inst.counts {
		if c != 0 {
			d := float64(lo+int64(i)) - inst.mean
			inst.m2 += d * d * float64(c)
		}
	}
}

// OfRegionIn is [OfRegion] with a binned plane's bins dividing
// [binLo, binHi] instead of the region's own range, so that histograms of
// several regions of one plane — the tiles of a store — line up and [Merge]
// can add them. A sample outside the range is counted in the nearest end
// bin. For a plane whose kind has codes, whose bins are its codes, the range
// is not read.
func OfRegionIn(plane imaging.Plane, rect image.Rectangle, binLo, binHi float64) (h Histogram, err error) {
	if _, _, codes := plane.Kind().Codes(); codes || !slices.Contains(imaging.AllSampleKinds, plane.Kind()) {
		return OfRegion(plane, rect)
	}
	if err = checkRegion(plane, rect); err != nil {
		return
	}
	if !(binHi >= binLo) || math.IsInf(binLo, 0) || math.IsInf(binHi, 0) {
		return h, eb.Build().Float64("lo", binLo).Float64("hi", binHi).Errorf("bin range is not a finite ascending pair")
	}
	return count(plane, runs{rect: rect}, binLo, binHi, true)
}

// binnedSampleI is a sample kind whose histogram divides a range into
// [FloatBins] bins.
type binnedSampleI interface {
	float32 | float64 | int32
}

// countBinned fills the histogram of a binned region: the range first — the
// region's own unless one is given — then the counts and the running mean
// and squared deviations (Welford's recurrence, which does not subtract two
// large sums). NaN and the declared invalid value are counted as invalid and
// in nothing else.
func countBinned[T binnedSampleI](inst *Histogram, samples []T, w int, r runs, inv float64, hasInv bool, binLo, binHi float64, given bool) {
	// The declared value is one the kind holds exactly (Plane.WithInvalid),
	// so the conversion is exact.
	skip := T(inv)
	first := true
	var lo, hi T
	for i := range r.len() {
		y, x0, x1 := r.at(i)
		for _, v := range samples[y*w+x0 : y*w+x1] {
			if v != v || (hasInv && v == skip) {
				continue
			}
			if first {
				lo, hi, first = v, v, false
				continue
			}
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	inst.counts = make([]uint64, FloatBins)
	if given {
		inst.binLo, inst.binHi = binLo, binHi
	}
	if first {
		inst.invalid = r.total()
		return
	}
	inst.lo, inst.hi = float64(lo), float64(hi)
	if !given {
		inst.binLo, inst.binHi = inst.lo, inst.hi
	}
	scale := 0.0
	if inst.binHi > inst.binLo {
		scale = FloatBins / (inst.binHi - inst.binLo)
	}
	for i := range r.len() {
		y, x0, x1 := r.at(i)
		for _, v := range samples[y*w+x0 : y*w+x1] {
			if v != v || (hasInv && v == skip) {
				inst.invalid++
				continue
			}
			x := float64(v)
			inst.counts[min(max(int((x-inst.binLo)*scale), 0), FloatBins-1)]++
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
// lost in counting: the plane's kind has codes ([imaging.SampleKindE.Codes]).
// A signed 32-bit plane is integer and still binned.
func (inst Histogram) IsExact() bool {
	_, _, ok := inst.kind.Codes()
	return ok
}

// BinWidth is the width of one bin in stored units: 1 for an exact
// histogram.
func (inst Histogram) BinWidth() float64 {
	if inst.IsExact() || len(inst.counts) == 0 {
		return 1
	}
	return (inst.binHi - inst.binLo) / float64(len(inst.counts))
}

// BinStored is the stored value at the lower edge of bin i: for an exact
// histogram the code lo + i, lo the kind's smallest code.
func (inst Histogram) BinStored(i int) float64 {
	if inst.IsExact() {
		return float64(inst.codeLo + int64(i))
	}
	return inst.binLo + float64(i)*inst.BinWidth()
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
// valid samples lie, p between 0 and 100: for an exact histogram the smallest
// sample value whose cumulative count reaches that share, which is a value
// that occurs; for a binned one a point within the bin where the count is
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
			if inst.IsExact() {
				return inst.BinStored(i), true
			}
			within := (target - seen) / float64(n)
			return min(max(inst.BinStored(i)+within*inst.BinWidth(), inst.lo), inst.hi), true
		}
		seen += float64(n)
	}
	return inst.hi, true
}

// Merge is the histogram of the samples two histograms counted, as if they
// had been counted together: of two parts of one plane, such as two tiles.
// They must be of one sample kind and value transform and, for a binned
// plane, of one bin range ([OfRegionIn]). The moments combine exactly (Chan
// et al.'s pairwise update), so the mean and standard deviation are those of
// all the samples; a float histogram's percentiles stay right to a bin.
func Merge(a, b Histogram) (h Histogram, err error) {
	switch {
	case a.counts == nil && a.invalid == 0:
		return b, nil
	case b.counts == nil && b.invalid == 0:
		return a, nil
	case a.kind != b.kind || a.transform != b.transform:
		return h, eb.Build().Stringer("kind", a.kind).Stringer("other kind", b.kind).Errorf("histograms of different kinds or transforms do not merge")
	case len(a.counts) != len(b.counts) || a.binLo != b.binLo || a.binHi != b.binHi:
		return h, eb.Build().Float64("lo", a.binLo).Float64("hi", a.binHi).Float64("other lo", b.binLo).Float64("other hi", b.binHi).
			Errorf("float histograms over different bin ranges do not merge")
	}
	h = Histogram{kind: a.kind, transform: a.transform, codeLo: a.codeLo, binLo: a.binLo, binHi: a.binHi,
		valid: a.valid + b.valid, invalid: a.invalid + b.invalid, counts: make([]uint64, len(a.counts))}
	for i := range h.counts {
		h.counts[i] = a.counts[i] + b.counts[i]
	}
	switch {
	case a.valid == 0:
		h.lo, h.hi, h.mean, h.m2 = b.lo, b.hi, b.mean, b.m2
	case b.valid == 0:
		h.lo, h.hi, h.mean, h.m2 = a.lo, a.hi, a.mean, a.m2
	default:
		na, nb, n := float64(a.valid), float64(b.valid), float64(h.valid)
		delta := b.mean - a.mean
		h.mean = a.mean + delta*nb/n
		h.m2 = a.m2 + b.m2 + delta*delta*na*nb/n
		h.lo, h.hi = min(a.lo, b.lo), max(a.hi, b.hi)
	}
	return h, nil
}
