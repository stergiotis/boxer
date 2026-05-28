//go:build llm_generated_opus47

// Package procfs is a small set of read-only primitives over a /proc-shaped
// directory tree. It is internal to sysmetrics; per-domain collectors
// (cpu, mem, proc, ...) use the [Reader] to read raw bytes and the package
// iterators to tokenize content in zero-copy fashion.
//
// All iterators yield slices that alias the input buffer; callers must
// copy bytes before retaining them past the iteration.
//
// Provenance: btop src/linux/btop_collect.cpp uses an analogous
// Shared::procPath that is configurable to redirect to a fixture tree
// during testing. The [Reader.New] entry point follows the same idiom.
package procfs
