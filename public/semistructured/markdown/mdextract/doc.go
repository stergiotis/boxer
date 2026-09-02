// Package mdextract reads one Obsidian-flavoured markdown document into the
// structure a fact store or an index wants, without rendering it: the
// frontmatter exploded into typed (path, params, value) leaves, the heading
// tree, every fenced code block, every link in each of its spellings, every
// emphasised span, and every tag — each item with its document-order ordinal,
// its source line and the heading it sits under.
//
// The package is the parse half of the markdown ingestor its ADR records; the
// persistence half lives in
// [github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts].
// Keeping them apart makes the extraction testable as a pure function over
// bytes and reusable by anything that wants the same reading — an editor
// outline, a link checker — without a store.
//
// # What is deliberately not here
//
// Rendering, vault resolution and word statistics that depend on an editor's
// lexer. Link targets are carried as written (percent-decoded for non-URL
// targets, the heading fragment split off) and resolving them against a set
// of documents is the reader's job — in SQL, over the facts, is where the
// ingestor expects that to happen.
//
// The frontmatter explosion follows the canonical leeway JSON mapping (the
// mlvhp scheme, see the leeway-advanced skill and the jsonbench shredder):
// object keys stay in the path, array positions become "_" with the elided
// index in [Leaf.Params], and every scalar carries its YAML type. Two
// Obsidian-specific properties are read a second time on top of that —
// `tags` become [Tag] entries with [TagSourceFrontmatter], and `aliases` are
// surfaced on [Frontmatter.Aliases] — because both have a list-or-comma-string
// spelling a generic leaf reader should not have to know about.
package mdextract
