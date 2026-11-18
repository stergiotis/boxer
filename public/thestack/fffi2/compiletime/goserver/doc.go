// Package goserver generates the Go side that writes the FFFI2 wire format:
// typed factories, builder methods, procedures and fetchers that marshal
// their arguments into the stream and read fetcher replies back. egui2's
// driver uses it for the imzero2 bindings. rustclient generates the Rust
// interpreter that reads what this side writes.
package goserver
