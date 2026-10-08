package grib

import "iter"

// iterPull adapts a point sequence to a pull function for callers that walk
// two sequences in step, as the dump command does with values and points.
func iterPull(seq iter.Seq2[float64, float64]) (next func() (float64, float64, bool), stop func()) {
	next, stop = iter.Pull2(seq)
	return
}
