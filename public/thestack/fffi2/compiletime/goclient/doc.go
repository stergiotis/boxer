// Package goclient generates a Go-side interpreter for FFFI2 IDL
// definitions: the dispatch that reads the binary FFFI2 wire format and calls
// the typed receiver for each procedure, factory, method and fetcher. It is
// the Go counterpart of rustclient, which generates the same read side in
// Rust.
//
// Nothing in the tree runs this generator. egui2's driver writes its Go
// bindings with goserver and its interpreter with rustclient, because the
// stream is read by the Rust host. The package is kept on purpose, with its
// wire round-trip tests, for a reader of the stream written in Go.
package goclient
