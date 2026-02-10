package functional

import (
	"iter"
)

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

func MakeSingleValueIterator1[T any](val T) iter.Seq[T] {
	return func(yield func(T) bool) {
		yield(val)
	}
}
func MakeSingleValueIterator2[K, V any](key K, val V) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		yield(key, val)
	}
}
func MakeIter2FromAlternatedValue[T any](alternatedValues ...T) iter.Seq2[T, T] {
	return func(yield func(T, T) bool) {
		l := len(alternatedValues)
		for i := 0; i < l/2; i++ {
			if !yield(alternatedValues[2*i], alternatedValues[2*i+1]) {
				return
			}
		}
	}
}
func AppendSeqIter2[K any, V any](ks []K, vs []V, it iter.Seq2[K, V]) (ksOut []K, vsOut []V) {
	ksOut = ks
	vsOut = vs
	for k, v := range it {
		ksOut = append(ksOut, k)
		vsOut = append(vsOut, v)
	}
	return
}
func ConsumeIterator[T any](it iter.Seq[T]) {
	for _ = range it {
	}
}
func ConsumeIterator2[K, V any](it iter.Seq2[K, V]) {
	for _, _ = range it {
	}
}
