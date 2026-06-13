// Package styletokens is the Go-side mirror of the IDS token layer
// (Rust source-of-truth at src/rust/imzero2_egui/src/style/tokens/).
//
// IDS = ImZero2 Design System; see doc/adr/0029 for the framework and
// doc/adr/0030..0034 for the foundations sub-ADRs.
//
// Tokens are mirrored by hand for the spacing/density/motion subsystems
// (ADR-0032 §SD8 — direct constants, no TOML pipeline). A drift test in
// styletokens_drift_test.go reads the Rust source and asserts table
// identity. Color tokens (palette_generated.go) and font binaries are
// emitted by generators (ADR-0031 §SD8, ADR-0030 §SD7) — that side lands
// later in the IDS phasing.
//
// Surface size archetypes (surface.go, ADR-0065) are Go-only: they size
// host-created windows, never egui::Style, so they have no Rust mirror and
// no drift test.
//
// Naming on the Go side follows boxer's enum-suffix convention (DensityE).
package styletokens
