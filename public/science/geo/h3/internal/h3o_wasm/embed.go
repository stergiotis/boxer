//go:build llm_generated_opus47

// Package h3o_wasm embeds the compiled h3 bridge WebAssembly artifact so the
// parent package can instantiate it without host filesystem access.
//
// The artifact is produced by scripts/dev/build_h3_wasm.sh from the Rust
// sources under rust/h3bridge and byte-compared in CI by
// scripts/ci/h3_wasm_parity.sh. Consumers must not edit h3.wasm by hand.
package h3o_wasm

import _ "embed"

// H3Wasm is the embedded wasm32-unknown-unknown module produced from
// rust/h3bridge. Nil-check is unnecessary: the byte slice is embedded at
// compile time and is always present if this package compiles.
//
//go:embed h3.wasm
var H3Wasm []byte
