package waveform

import "math"

// reduceColumns folds interleaved raw frames into one min/max pair per
// column. Column c covers frames [c*fpp, (c+1)*fpp) relative to the first
// frame of samples; frames past the end of samples are not counted, and a
// column with no frame at all is marked silent (min = max = 0). fpp must be
// at least 1 — below that the caller draws samples, not columns.
//
// Returns the number of columns that had at least one frame.
func reduceColumns(samples []float32, channels int, ch int, fpp float64, dstMin, dstMax []float32) (filled int) {
	n := min(len(dstMin), len(dstMax))
	if channels <= 0 || ch < 0 || ch >= channels || fpp < 1 || n == 0 {
		return 0
	}
	frames := len(samples) / channels
	for c := range n {
		f0 := int(math.Floor(float64(c) * fpp))
		f1 := int(math.Floor(float64(c+1) * fpp))
		if f1 <= f0 {
			f1 = f0 + 1
		}
		if f0 >= frames {
			dstMin[c], dstMax[c] = 0, 0
			continue
		}
		if f1 > frames {
			f1 = frames
		}
		lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
		for f := f0; f < f1; f++ {
			s := samples[f*channels+ch]
			if s < lo {
				lo = s
			}
			if s > hi {
				hi = s
			}
		}
		dstMin[c], dstMax[c] = lo, hi
		filled = c + 1
	}
	return filled
}
