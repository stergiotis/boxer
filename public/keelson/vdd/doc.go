// Package vdd is the registry of the memberships keelson's fact kinds use:
// every leeway membership a keelson DTO references is declared here once,
// with a stable tagged id, a place in a parent hierarchy and a restriction
// that fixes its cardinality, sub-type and wire encoding. DTO authors, the
// codec generator and query writers read the same declaration.
//
// The declarations are grouped by subsystem in the keelson_dimdata_*.go
// files; [KeelsonContract] and the natural-key registry hold them.
// EXPLANATION.md states how a declaration's cardinality and sub-type
// determine the wire encoding and the Go type a DTO uses.
//
// An id, once assigned, is stored in rows and must not change: add a
// membership, never renumber one.
package vdd
