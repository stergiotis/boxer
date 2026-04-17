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
