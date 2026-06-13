// Package idsshowcase renders the IDS token catalogue (palette spine +
// semantic roles + density + rounding + stroke) as a single carousel app.
//
// Stakeholder-facing demo: validates that the IDS style overlay
// (applied at Rust startup per src/rust/src/imzero2/app.rs) propagates
// through to a Go-side render path, and gives reviewers something
// concrete to look at when assessing the M1 milestones of ADRs
// 0029 / 0030 / 0031 / 0032.
//
// What's shown
//
//   - Neutral spine — the 10 dark-theme L-points (bg.extreme through
//     text.extreme) from ADR-0031 §SD4.
//   - Semantic palette — 6 roles × 3 emphasis = 18 swatches from ADR-0031
//     §SD2 (info / success / warning / error / neutral / accent at
//     subtle / default / strong).
//   - Type scale — six rows (Display / Heading / Body / Caption / Micro
//     plus Body.Mono) rendered through the egui TextStyle slots
//     apply_typography bound on the Rust side. ADR-0030 §SD3.
//   - Data encoding — qualitative (batlowS, 10 categorical chips),
//     sequential (batlow, 24 samples across t∈[0,1]), and diverging
//     (vik, 24 samples across t∈[-1,1]). ADR-0031 §SD3; all three are
//     verbatim from Crameri 2018 (MIT-licensed scientific colormaps).
//   - Data encoding in egui_plot — six phase-shifted sine waves
//     colored by `styletokens.QualitativeCycle(i)`, validating that
//     the IDS palette consumes through `c.PlotLine(...).Color(...)`
//     end-to-end. ADR-0031 §SD7.
//   - Density readout — the active DensityE and its 8-value PX_TABLE
//     column. Set IMZERO2_DENSITY=tight|standard|roomy at startup to
//     compare. The IDS overlay re-resolves spacing tokens on apply;
//     nothing in this panel re-renders on density change.
//   - Rounding ladder — the four density-independent corner-radius
//     tokens (0 / 2 / 4 / 6 px) from ADR-0032 §SD3.
//   - Stroke ladder — the three density-independent stroke-width tokens
//     (1.0 / 1.5 / 2.0 px) from ADR-0032 §SD4.
//
// What's not shown
//
//   - Motion. The duration tokens are time-based; a static screenshot
//     doesn't read them. ADR-0032 §SD5 reduced-motion plumbing also
//     lives outside this demo.
package idsshowcase
