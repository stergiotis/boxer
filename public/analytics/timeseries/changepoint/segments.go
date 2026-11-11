package changepoint

import (
	"iter"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Segments describes the stretches between changes.
//
// Struct-of-arrays: a caller drawing a step line reads Start, End and Mean
// and never touches the rest.
type Segments struct {
	// Start is each segment's first sample; End is one past its last.
	Start []int32
	End   []int32
	// Mean and StdDev are the segment's sample mean and sample standard
	// deviation (0 for a single sample). They describe the segment whatever
	// cost found it; under [CostRank] in particular they are a summary of the
	// result, not the quantity that was fitted.
	Mean   []float64
	StdDev []float64
}

// Len returns the number of segments.
func (inst Segments) Len() (count int) {
	return len(inst.Start)
}

// Segment is one row of [Segments].
type Segment struct {
	Start, End   int32
	Mean, StdDev float64
}

// All iterates the segments in order.
func (inst Segments) All() iter.Seq2[int, Segment] {
	return func(yield func(int, Segment) bool) {
		for i := range inst.Start {
			if !yield(i, Segment{Start: inst.Start[i], End: inst.End[i], Mean: inst.Mean[i], StdDev: inst.StdDev[i]}) {
				return
			}
		}
	}
}

// Summarize cuts vals at starts — change positions as in
// [Segmentation.Starts], strictly ascending inside (0, len(vals)) — and
// describes each piece.
func Summarize(vals []float64, starts []int32) (segs Segments, err error) {
	n := len(vals)
	if n == 0 {
		err = eb.Build().Errorf("the series is empty")
		return
	}
	prev := int32(0)
	for i, s := range starts {
		if s <= prev || int(s) >= n {
			err = eb.Build().Int("at", i).Int32("start", s).Int("len", n).
				Errorf("the change positions are not strictly ascending inside the series")
			return
		}
		prev = s
	}
	count := len(starts) + 1
	segs.Start = make([]int32, 0, count)
	segs.End = make([]int32, 0, count)
	segs.Mean = make([]float64, 0, count)
	segs.StdDev = make([]float64, 0, count)
	from := 0
	for i := range count {
		to := n
		if i < len(starts) {
			to = int(starts[i])
		}
		var mean float64
		for _, v := range vals[from:to] {
			mean += v
		}
		mean /= float64(to - from)
		var m2 float64
		for _, v := range vals[from:to] {
			m2 += (v - mean) * (v - mean)
		}
		sd := 0.0
		if to-from > 1 {
			sd = math.Sqrt(m2 / float64(to-from-1))
		}
		segs.Start = append(segs.Start, int32(from))
		segs.End = append(segs.End, int32(to))
		segs.Mean = append(segs.Mean, mean)
		segs.StdDev = append(segs.StdDev, sd)
		from = to
	}
	return
}
