package ragged

import "iter"

func Iterate2[A, B any](s1 []A, s2 []B) iter.Seq2[A, B] {
	return func(yield func(A, B) bool) {
		l1 := len(s1)
		l2 := len(s2)
		l := min(l1, l2)
		for i := 0; i < l; i++ {
			if !yield(s1[i], s2[i]) {
				return
			}
		}
	}
}
