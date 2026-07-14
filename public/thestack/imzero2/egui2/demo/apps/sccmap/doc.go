// Package sccmap is the "Repo code exploration" app — visualises
// `go tool scc` output over a chosen repository as a frame-based
// treemap with one or two distsummary widgets reporting the
// statistical distribution of the currently-selected size / color
// metrics. Cell area defaults to lines-of-code, cell colour to
// log-scaled cyclomatic complexity (green → yellow → red).
//
// The scan target is entered in the header path box — the git worktree the
// host runs in by default, or the SCCMAP_REPO env var — and resolves to that
// path's git toplevel (falling back to the path itself for a plain, non-repo
// directory). Each scan runs on a background job (keelson/runtime/bgjob), so
// the view shows a scanning placeholder rather than freezing while scc walks a
// large tree, and a Cancel affordance aborts an in-flight scan.
//
// Two checkboxes filter what the statistics survey: "Include generated"
// (scctree.IsGenerated) and "Include tests" (scctree.IsTest); both default
// off so the view starts focused on hand-written, non-test code. The
// filter feeds the treemap tree and both distsummary digests from one
// shared predicate, so toggling either reshapes every surface together. A
// "Show values" checkbox draws the humanized size- and color-metric value
// of each tile directly under its name (e.g. "1.2k · 34"); it reads live so
// toggling it preserves the current drill position.
//
// Migrated out of the gallery demo set (formerly `treemap2-scc`) into
// a standalone AppI so it can be opened directly from the Apps menu
// and so its scc-subprocess side effects are not entangled with the
// gallery demo lifecycle. The display name was changed from
// "Repo SCC treemap" to "Repo code exploration" once the distsummary
// row landed — the surface is no longer treemap-only.
package sccmap
