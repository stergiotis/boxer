// Package sliceconv copies a slice into a slice of another element type,
// converting each element.
package sliceconv

import (
	"slices"
)

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type float interface {
	~float32 | ~float64
}

type complexNumber interface {
	~complex64 | ~complex128
}

func CopySliceInt[U integer, V integer](src []U, dest []V) (retr []V) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		retr = append(retr, V(src[i]))
	}
	return
}
func CopySliceFloat[U float, V float](src []U, dest []V) (retr []V) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		retr = append(retr, V(src[i]))
	}
	return
}
func CopySliceComplex[U complexNumber, V complexNumber](src []U, dest []V) (retr []V) {
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
func CopySliceInterfaceCastable[U any, I any](src []U, dest []I) (retr []I) {
	retr = slices.Grow(dest, len(src))
	for i := range src {
		var u any = src[i]
		ut, ok := u.(I)
		if ok {
			retr = append(retr, ut)
		}
	}
	return
}
