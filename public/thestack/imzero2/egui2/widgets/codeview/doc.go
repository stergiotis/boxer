//go:build llm_generated_opus47

// Package codeview composes the egui2 CodeViewJob primitive with the
// project's SQL, JSON, and Go highlighters into a small set of retained-
// holder builders. The three highlighters share the same shape — palette
// intern at init → run highlighter → emit one CodeViewJob.Section per
// span → Keep — so they live in one package.
//
// Naming convention: Build* re-tokenises every call (use for dynamic
// strings); Prepare* is a documented alias for static / global content
// where the retained holder is constructed once and reused across frames.
//
// The Go view additionally exposes BuildGoLines / PrepareGoLines, which
// render a byte slice covering a 1-based [firstLine, lastLine] window
// with a right-aligned line-number gutter. The line-window machinery is
// internally generic; SQL/JSON equivalents can be added when a caller
// needs them.
package codeview
