package ragged

import "iter"

// Zip2L returns an iterator that yields pairs of values from the input iterator
// and the provided slice. Iteration terminates when either the input iterator is
// exhausted, the end of the slice is reached, or the consumer stops iteration.
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

// Zip2RL returns an iterator that yields pairs of values from two input iterators.
// Iteration terminates when either of the input iterators is exhausted or the
// consumer stops iteration.
// Note that Zip2RL may be slower than Zip2L, Zip2R, Zip2 for small to medium iterators:
// you may want to use `Zip2L(seq1, slices.Collect(seq2))`
func Zip2RL[A, B any](s1 iter.Seq[A], s2 iter.Seq[B]) iter.Seq2[A, B] {
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
