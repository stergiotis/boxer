// Package wasmsurvey surveys which of the module's Go packages are amenable to
// TinyGo/WebAssembly compilation, and explains why the others are not
// (ADR-0078).
//
// It is the inverse question to ADR-0003: where that work runs a *foreign*
// (Rust) wasm module *inside* boxer via wazero, this asks which of boxer's own
// pure-Go packages could themselves *become* wasm guests under TinyGo. The
// answer matters because the repo is already cgo-free and consumer-toolchain-
// neutral (CODINGSTANDARDS.md), so the only blockers are unsupported stdlib
// imports, the reflect subset, unsafe, the json/v2 experiment, and external
// modules — exactly what this survey classifies.
//
// The pipeline runs once per wasm target (wasi, js, wasm-unknown), because a
// package's selected files — and therefore its imports and verdict — depend on
// GOOS:
//
//  1. Collect the transitive closure under the target's GOOS/GOARCH=wasm,
//     reusing godepcollect (the ADR-0064 collector) extended with an Env knob.
//  2. Static triage: seed each package from a curated TinyGo-support set
//     (support.go), then propagate the worst verdict up the import DAG; blame
//     is the shortest import path to the offending leaf.
//  3. Empirical confirm: for every package the triage did not rule out, wrap
//     it in a synthetic main referencing its exported functions and run
//     `tinygo build` for the target, classifying any failure. This stage needs
//     `tinygo` on PATH; absent it, the survey reports static verdicts only.
//
// The static set is an approximation of a moving target (TinyGo 0.39); the
// empirical pass is ground truth and overrides it, so a disagreement column
// flags where a curated guess was refuted by the real compiler. Verdicts are
// package-level (the exported API compiles and links), not per-function.
//
// The command is registered under `golang` (sibling to llmuse/stubber):
//
//	app code analysis golang wasmsurvey [--target …] [--mode …] [--json …]
package wasmsurvey
