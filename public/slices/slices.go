package slices

import (
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
)

func CopySliceInt[U constraints.Integer, V constraints.Integer](src []U, dest []V) (retr []V) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		retr = append(retr, V(src[i]))
	}
	return
}
func CopySliceFloat[U constraints.Float, V constraints.Float](src []U, dest []V) (retr []V) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		retr = append(retr, V(src[i]))
	}
	return
}
func CopySliceComplex[U constraints.Complex, V constraints.Complex](src []U, dest []V) (retr []V) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		retr = append(retr, V(src[i]))
	}
	return
}
func CopySliceString[U ~string, V ~string](src []U, dest []V) (retr []V) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		retr = append(retr, V(src[i]))
	}
	return
}
