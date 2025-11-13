package functional

import "iter"

func IterLeftOnly[L, R any](seq iter.Seq2[L, R]) iter.Seq[L] {
	return func(yield func(L) bool) {
		for l, _ := range seq {
			if !yield(l) {
				return
			}
		}
	}
}
func IterRightOnly[L, R any](seq iter.Seq2[L, R]) iter.Seq[R] {
	return func(yield func(R) bool) {
		for _, r := range seq {
			if !yield(r) {
				return
			}
		}
	}
}
func IterInterchanged[L, R any](seq iter.Seq2[L, R]) iter.Seq2[R, L] {
	return func(yield func(R, L) bool) {
		for l, r := range seq {
			if !yield(r, l) {
				return
			}
		}
	}
}
func MakeIter2FromIter1[K, V any](iter1 iter.Seq[V], k K) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for v := range iter1 {
			if !yield(k, v) {
				return
			}
		}
	}
}
func MakeIter2FromIter1Func[K, V any](iter1 iter.Seq[V], f func(v V) (k K)) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for v := range iter1 {
			if !yield(f(v), v) {
				return
			}
		}
	}
}
func MakeIter2FromIter1Indexed[V any](iter1 iter.Seq[V]) iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		i := 0
		for v := range iter1 {
			if !yield(i, v) {
				return
			}
			i++
		}
	}
}

type NilIteratorValueType struct{}

var NilIteratorValue = struct{}{}
