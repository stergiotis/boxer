// Package codeview composes the egui2 CodeViewJob primitive with the
// project's SQL, JSON, and Go highlighters into a small set of retained-
// holder builders. The three highlighters share the same shape — palette
// intern at init → run highlighter → emit one CodeViewJob.Section per
// span → Keep — so they live in one package.
//
// Naming convention: Build* re-tokenises every call; Prepare* memoises,
// returning the same retained holder for a source it has already seen
// (ADR-0125). The two were once the same function with the distinction living
// in this comment, which read as though Prepare* were cached and cost four call
// sites a full re-highlight per frame.
//
// Reach for Prepare* by default — a probe is 30 ns for a query against 129 µs to
// re-parse it, and SQL is the expensive case (highlight.Highlight runs a full
// nanopass.Parse, not a lex). Reach for Build* when the work is one-shot, or
// when the caller already holds a cheaper key than the source text and its own
// cache: play's detail pane keys on (result, row) because it must cache a
// parsed markdown tree and decoded image pixels anyway, and hashing a
// megabyte-sized cell per frame would be the more expensive half.
//
// The memo is bounded by a byte budget and is safe for concurrent use; builds
// run outside its lock. See memo.go.
//
// The Go view additionally exposes BuildGoLines / PrepareGoLines, which
// render a byte slice covering a 1-based [firstLine, lastLine] window
// with a right-aligned line-number gutter. The line-window machinery is
// internally generic; SQL/JSON equivalents can be added when a caller
// needs them.
package codeview
