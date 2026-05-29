//go:build llm_generated_opus47

// Package sccmap is the "Repo code exploration" app — visualises
// `go tool scc` output over the current repository as a frame-based
// treemap with one or two distsummary widgets reporting the
// statistical distribution of the currently-selected size / color
// metrics. Cell area defaults to lines-of-code, cell colour to
// log-scaled cyclomatic complexity (green → yellow → red).
//
// Migrated out of the gallery demo set (formerly `treemap2-scc`) into
// a standalone AppI so it can be opened directly from the Apps menu
// and so its scc-subprocess side effects are not entangled with the
// gallery demo lifecycle. The display name was changed from
// "Repo SCC treemap" to "Repo code exploration" once the distsummary
// row landed — the surface is no longer treemap-only.
package sccmap
