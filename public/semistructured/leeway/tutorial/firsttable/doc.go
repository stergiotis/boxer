// Package firsttable is the smallest complete leeway table: a schema, its
// generated write and read sides, and an example that writes rows and reads
// them back without a database.
//
// It backs the leeway first-table tutorial and the leeway EXPLANATION.md, whose
// code blocks are copied from the examples here and in the packages they
// name. It is illustrative code, not part of the shipped surface.
//
// The generated files (events_dml.out.go, events_ra.out.go) are rewritten by
// TestGenerate; edit [NewSchema] and run the package's tests to regenerate.
package firsttable
