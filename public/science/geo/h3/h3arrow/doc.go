//go:build llm_generated_opus47

// Package h3arrow provides zero-copy adapters from the h3 package's
// Struct-of-Arrays and CSR outputs to arrow-go arrays.
//
// The adapters wrap caller slices without copying: the returned arrow
// arrays hold memory.Buffer references to the underlying Go slice backing
// arrays, so the caller must keep those slices reachable until the arrow
// arrays are Released. This lives in a sub-package so consumers of
// [github.com/stergiotis/boxer/public/science/geo/h3] that do not use
// arrow-go do not inherit its import graph.
//
// Precedent for SoA+CSR → arrow mapping is set by
// public/semistructured/leeway/readaccess (which stores H3 cell columns
// as Uint64 / List(Uint64)). See ADR-0003 for the design rationale that
// motivates this adapter as a "SoA + CSR align with Arrow" consequence.
package h3arrow
