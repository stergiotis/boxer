// Package mdgraph turns a corpus of extracted Markdown documents into a
// document graph: a node per note, per tag and per unresolved link target,
// an edge per note-to-note link or embed and per note-to-tag use (ADR-0353).
//
// The package reads no files: the caller extracts each document with
// mdextract and hands the corpus to [Build]. [Resolver] holds the rule that
// decides which note a link means (ADR-0353 §SD3), for callers that follow
// links without drawing a graph.
package mdgraph
