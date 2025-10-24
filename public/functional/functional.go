package functional

import "iter"

// TranslateEmpty if s is the empty value (type specific) TranslateEmpty returns replacement
func TranslateEmpty[T comparable](s T, replacement T) (r T) {
	if s == r {
		return replacement
	}
	return s
}

type InterfaceIsReferentialTransparentType bool

type PromiseReferentialTransparentI interface {
	PromiseToBeReferentialTransparent() (_ InterfaceIsReferentialTransparentType)
}

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
