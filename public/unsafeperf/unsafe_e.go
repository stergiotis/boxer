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
