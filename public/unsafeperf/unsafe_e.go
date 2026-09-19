//go:build extrasafe

package unsafeperf

func UnsafeStringToBytes(str string) []byte {
	return []byte(str)
}

// UnsafeStringToByte is a misnamed alias kept for backwards compatibility.
//
// Deprecated: use UnsafeStringToBytes.
func UnsafeStringToByte(str string) []byte {
	return []byte(str)
}
func UnsafeBytesToString(b []byte) string {
	return string(b)
}

// FixedSize is the element types whose slices may be viewed as raw memory:
// fixed width on every platform and free of pointers.
type FixedSize interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64 | ~complex64 | ~complex128
}

// UnsafeSliceToBytes makes no view under extrasafe: ok is false and the caller
// takes its copying path.
func UnsafeSliceToBytes[T FixedSize](s []T) (b []byte, ok bool) {
	return
}

// UnsafeSliceReinterpret makes no view under extrasafe: ok is false and the
// caller takes its element-wise path.
func UnsafeSliceReinterpret[To, From FixedSize](s []From) (r []To, ok bool) {
	return
}
