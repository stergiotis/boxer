package ragged

import "iter"

// Zip2L returns an iterator that yields pairs of values from the input iterator
// and the provided slice. Iteration terminates when either the input iterator is
// exhausted, the end of the slice is reached, or the consumer stops iteration.
//
// The sequence is always invoked, and when it is strictly longer than
// the slice (including an empty slice) exactly one extra element of s1
// is pulled and discarded — the slice bound is checked after pulling.
// This is deliberate: stream-backed sequences may perform mandatory
// work when invoked (the fffi2 Retr sequences read a length header and
// self-drain on early termination), so the zip must not skip or
// under-run them. It matters only for single-use or side-effecting
// sequences; when the sequence is not longer than the slice, nothing
// extra is pulled.
func Zip2L[A, B any](s1 iter.Seq[A], s2 []B) iter.Seq2[A, B] {
	return func(yield func(A, B) bool) {
		l2 := len(s2)
		i := 0
		for a := range s1 {
			if i >= l2 {
				break
			}
			if !yield(a, s2[i]) {
				break
			}
			i++
		}
	}
}

// Zip2R returns an iterator that yields pairs of values from the provided slice
// and the input iterator. Iteration terminates when either the end of the slice
// is reached, the input iterator is exhausted, or the consumer stops iteration.
//
// The sequence is always invoked, and when it is strictly longer than
// the slice (including an empty slice) exactly one extra element of s2
// is pulled and discarded — see [Zip2L] for why this is deliberate.
func Zip2R[A, B any](s1 []A, s2 iter.Seq[B]) iter.Seq2[A, B] {
	return func(yield func(A, B) bool) {
		l1 := len(s1)
		i := 0
		for b := range s2 {
			if i >= l1 {
				break
			}
			if !yield(s1[i], b) {
				break
			}
			i++
		}
	}
}

// Zip2LR returns an iterator that yields pairs of values from two input iterators.
// Iteration terminates when either of the input iterators is exhausted or the
// consumer stops iteration.
// Note that Zip2LR may be slower than Zip2L, Zip2R, Zip2 for small to medium iterators:
// you may want to use `Zip2L(seq1, slices.Collect(seq2))`.
//
// When s2 is exhausted first, one already-pulled element of s1 is
// discarded: s2's end is only discoverable after pulling from s1. As
// with [Zip2L], this matters only for single-use or side-effecting
// sequences.
func Zip2LR[A, B any](s1 iter.Seq[A], s2 iter.Seq[B]) iter.Seq2[A, B] {
	return func(yield func(A, B) bool) {
		next, stop := iter.Pull(s2)
		defer stop()

		for v1 := range s1 {
			v2, ok := next()
			if !ok {
				break
			}
			if !yield(v1, v2) {
				break
			}
		}
	}
}
