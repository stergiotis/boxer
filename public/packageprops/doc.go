// Package packageprops is the shared, zero-dependency vocabulary for
// per-package property declarations (ADR-0080). Each participating package
// declares a top-level value referencing these types:
//
//	package option
//
//	import "github.com/stergiotis/boxer/public/packageprops"
//
//	// PackageProps records this package's curated properties (ADR-0080).
//	var PackageProps = packageprops.Props{
//		WASMWASI:         packageprops.WASMCompiles,
//		WASMJS:           packageprops.WASMCompiles,
//		WASMFreestanding: packageprops.WASMCompiles,
//	}
//
// The declaration is co-located with the package (single source of truth),
// typed (goto-definition and find-references work — find-references on
// packageprops.WASMCompiles lists every package in that state), readable at
// runtime as pkg.PackageProps, and statically harvestable into an overview
// table by `wasmsurvey props harvest`.
//
// Props grows over time (ADR-0080 SD4). Besides the WASM* verdicts it carries a
// Kind classifying the package's primary role — KindDemo, KindExample, or
// KindIntegrationTest — for the packages that are not ordinary library code;
// the zero KindUnspecified asserts nothing. Kind is purely human-curated (no
// survey computes it), so `wasmsurvey props verify` does not reconcile it,
// though `props generate` seeds the obvious demo/example cases from directory
// name.
//
// The lifecycle is hybrid (ADR-0080 SD3): `wasmsurvey props generate` seeds the
// declarations from the computed verdict (idempotent-create, never clobbering a
// curated file), humans then curate them as intent, and `wasmsurvey props
// verify` reconciles declaration against the freshly computed reality and gates
// regressions in CI.
//
// Two discovery surfaces (registry.go): generated files Register their Props
// from init, so packageprops.All() enumerates the packages compiled into the
// running binary; `wasmsurvey props harvest --emit go` emits a static Table of
// the whole repo for embedding into a binary that does not link everything.
//
// This package depends only on the standard library (sync, sort) and on no
// boxer or external package (ADR-0080 SD2): every package imports it, so a
// boxer/external dependency would become universal and could create cycles or
// taint the very wasm verdicts it records. It mirrors public/compiletimeflags.
package packageprops
