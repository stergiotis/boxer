package main

// RunDynamic keeps its interface parameter opaque: without inlining into the
// caller the compiler cannot prove the concrete type, so the call below must
// stay dynamic — the adjudication contrast to @iface-devirt.
//
//go:noinline
func RunDynamic(i I) {
	i.MDyn() // @true-dynamic
}
