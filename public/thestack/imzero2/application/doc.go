//go:build llm_generated_opus47

// Package application owns the imzero2 process lifecycle: configuration
// parsing, observability bootstrap, render-loop driver wiring, profiling
// hooks, and graceful shutdown. The Application[U] generic parameterizes
// the FFFI2 unmarshaller plugged into the Rust child process.
package application
