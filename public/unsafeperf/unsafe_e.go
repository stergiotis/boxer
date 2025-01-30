//go:build extrasafe

package unsafeperf

func UnsafeStringToByte(str string) []byte {
	return []byte(str)
}
func UnsafeBytesToString(b []byte) string {
	return string(b)
}
