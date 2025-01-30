//go:build !extrasafe

package unsafeperf

import "unsafe"

func UnsafeStringToByte(str string) []byte {
	return unsafe.Slice(unsafe.StringData(str), len(str))
}
func UnsafeBytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
