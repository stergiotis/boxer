//go:build llm_generated_opus47

// Package h3 provides bulk, Struct-of-Arrays access to Uber H3 hierarchical
// geospatial indexing.
//
// The implementation drives the h3o Rust crate compiled to
// wasm32-unknown-unknown through [github.com/tetratelabs/wazero]. Consumers
// allocate a shared [Runtime], check out a per-goroutine [Handle] via
// [Runtime.AcquireE], and call bulk methods that take Struct-of-Arrays inputs
// and write Struct-of-Arrays outputs. Variable-arity results use a CSR layout
// (flat values + []int32 offsets); per-element failures are reported through
// a [StatusE] slice that parallels the output values.
//
// See doc/adr/0003-h3-wasm-bridge.md for the design rationale and
// EXPLANATION.md for the H3-specific theory and invariants consumers rely on.
package h3
