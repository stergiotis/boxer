//go:build !amd64 && !arm64

package runtime

// goroutineToken falls back to the goroutine id parsed from the stack header
// where there is no assembly to read the runtime descriptor (wasm among
// them). It costs what currentGoroutineId costs: microseconds per call,
// growing with stack depth. 0 when the header does not parse.
func goroutineToken() uintptr {
	return uintptr(currentGoroutineId())
}
