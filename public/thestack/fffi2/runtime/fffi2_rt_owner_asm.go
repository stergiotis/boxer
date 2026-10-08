//go:build amd64 || arm64

package runtime

// goroutineToken returns the address of the calling goroutine's runtime
// descriptor (the g the scheduler keeps in TLS on amd64 and in R28 on
// arm64). The runtime never frees a descriptor, it only recycles one after
// its goroutine exits, so the value is stable for a live goroutine and
// distinct among live ones; that is all checkOwner needs from it. Returned
// as uintptr so the collector never treats it as a reference. About a
// nanosecond regardless of stack depth.
func goroutineToken() uintptr
