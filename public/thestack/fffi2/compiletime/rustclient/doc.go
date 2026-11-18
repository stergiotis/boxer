// Package rustclient generates Rust-side dispatch tables for FFFI2 IDL
// definitions. For imzero2 the output lands in rust/imzero2/src/imzero2/ as
// `interpreter.rs` (inside its marked region) and `enums_out.rs`, providing
// the read side of the wire format that goserver's bindings write on the Go
// end.
package rustclient
