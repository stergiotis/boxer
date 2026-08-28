package pulsesink

import "math"

// resampleLinear writes up to len(out)/channels output frames by reading the
// interleaved source frames in src at fractional positions frac, frac+rate,
// frac+2·rate, …, interpolating linearly between neighbours. It stops when
// the next interpolation would need a source frame past the end of src.
//
// It returns the output frames written, the whole source frames consumed
// (to advance the caller's cursor) and the fractional remainder to carry
// into the next call, so consecutive calls read a continuous signal.
func resampleLinear(src []float32, channels int, frac float64, rate float64, out []float32) (outFrames int, consumed int64, nextFrac float64) {
	if channels <= 0 || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, 0, frac
	}
	srcFrames := len(src) / channels
	maxOut := len(out) / channels
	// Positions are the closed form frac + i·rate, never an accumulation:
	// framesNeeded uses the same expression, and a running sum drifts from it
	// in the last bit — enough to read one frame past what the caller fetched.
	for outFrames < maxOut {
		pos := frac + float64(outFrames)*rate
		k := int(math.Floor(pos))
		if k+1 >= srcFrames {
			break
		}
		t := float32(pos - float64(k))
		a := src[k*channels : (k+1)*channels]
		b := src[(k+1)*channels : (k+2)*channels]
		o := out[outFrames*channels : (outFrames+1)*channels]
		for c := range channels {
			o[c] = a[c] + (b[c]-a[c])*t
		}
		outFrames++
	}
	end := frac + float64(outFrames)*rate
	whole := math.Floor(end)
	return outFrames, int64(whole), end - whole
}

// framesNeeded is how many source frames a call to resampleLinear needs to
// produce outFrames output frames from fractional start frac: the last
// interpolation reads frame floor(frac+(outFrames-1)·rate)+1.
func framesNeeded(outFrames int, frac float64, rate float64) (n int64) {
	if outFrames <= 0 {
		return 0
	}
	last := frac + float64(outFrames-1)*rate
	return int64(math.Floor(last)) + 2
}
