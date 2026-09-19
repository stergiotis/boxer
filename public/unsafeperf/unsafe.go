//go:build !extrasafe

package unsafeperf

import "unsafe"

func UnsafeStringToBytes(str string) []byte {
	return unsafe.Slice(unsafe.StringData(str), len(str))
}

// UnsafeStringToByte is a misnamed alias kept for backwards compatibility.
//
// Deprecated: use UnsafeStringToBytes.
func UnsafeStringToByte(str string) []byte {
	return UnsafeStringToBytes(str)
}
func UnsafeBytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// FixedSize is the element types whose slices may be viewed as raw memory:
// fixed width on every platform and free of pointers.
type FixedSize interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64 | ~complex64 | ~complex128
}

// UnsafeSliceToBytes views a slice's memory as bytes, in the machine's own
// byte order, without copying. The view aliases s: it is valid while s is, and
// must not be written through. ok is false under the extrasafe build tag,
// where no view is made and the caller takes its copying path.
func UnsafeSliceToBytes[T FixedSize](s []T) (b []byte, ok bool) {
	ok = true
	if len(s) == 0 {
		return
	}
	b = unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(s))), len(s)*int(unsafe.Sizeof(s[0])))
	return
}

// UnsafeSliceReinterpret views a slice as one of another element type of the
// same size — a []float32 as the []uint32 of its bit patterns — without
// copying. A nil slice stays nil. ok is false when the sizes differ, and under
// the extrasafe build tag.
func UnsafeSliceReinterpret[To, From FixedSize](s []From) (r []To, ok bool) {
	var from From
	var to To
	if unsafe.Sizeof(from) != unsafe.Sizeof(to) {
		return
	}
	ok = true
	if s == nil {
		return
	}
	r = unsafe.Slice((*To)(unsafe.Pointer(unsafe.SliceData(s))), len(s))
	return
}
