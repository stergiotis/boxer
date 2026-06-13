// Package icons supplies the IDS iconography catalogue. All icons —
// UI affordances (gear, search, chart, file, …) and the available
// brand / language marks (Linux, GitHub, Apple, Google, …) — render
// from Phosphor Regular via the generated `phosphor.out.go` constants.
// The curated `affordances.out.go` layer aliases the high-frequency
// affordances to conceptual `Icon<Name>` names with drift notes. See
// ADR-0044 for the design.
//
// The runtime contract is a Unicode string: each constant resolves to
// a single PUA codepoint that egui's font-fallback chain routes to
// the Phosphor font registered at startup:
//
//	c.Label(icons.IconCheck)
//
// Phosphor's tech-brand coverage is selective (Python and Linux yes;
// Rust, Docker, Go, JavaScript no). Apps that need a brand mark
// Phosphor doesn't ship reach for plain text or an alternative
// affordance (e.g. `IconBracketsCurly` for code-related labels).
//
// History: an earlier two-slot design (ADR-0044 §SD2 as originally
// written) shipped a second `nf-brand` font — a Nerd Fonts subset for
// 10 brand-mark glyphs. Removed: 8 of those 10 had no caller outside
// a single demo section, and the two affordance-style entries
// (database, git-branch) collapsed back into Phosphor coverage. The
// ADR carries the removal as an Amendment.
package icons
