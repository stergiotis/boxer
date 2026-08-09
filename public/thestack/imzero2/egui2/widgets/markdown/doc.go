// Package markdown renders Obsidian-flavored Markdown documents through
// the imzero2 / egui2 widget tree.
//
// Parsing uses boxer's goldmark-based [obsidian] extender; the resulting
// AST is lowered once into a Go-side segment tree that pre-builds
// [components.RetainedFffiHolderTyped] blobs for paragraphs, headings and
// code blocks. Per-frame render is a tree walk that splices retained
// bytes into the current Ui scope — no re-parse, no per-block allocation
// in the steady state.
//
// # Usage
//
// Parse once, render many:
//
//	var helpDoc = markdown.Parse([]byte(`
//	# Help
//	A *short* paragraph with [a link](https://example.com).
//	`))
//
//	// in your render path:
//	for range c.IdScope(ids.PrepareStr("help-doc")) {
//	    helpDoc.Render(ids)
//	}
//
// The wrapping [components.IdScope] is the caller's responsibility —
// without it, code-block and blockquote IDs will collide if multiple doc
// instances coexist under the same parent scope.
//
// # Scope
//
// Headings, paragraphs (with strong / italic / code / strikethrough /
// highlight / hyperlinks), plain and language-highlighted code blocks
// (Go, SQL, JSON, Markdown), bullet and numbered lists, blockquotes,
// horizontal rules, callouts, Obsidian wikilinks and embeds, inline
// images (CommonMark `![alt](url)` and Obsidian `![[file.png]]` —
// rendered via [bindings.Image] when [resolver.ResolverI.LoadImage]
// returns ok; glyph-prefixed hyperlink fallback otherwise), GFM tables,
// explicit `{#anchor}` heading anchors (which name a section
// independently of its title — see [HeadingInfo]), and frontmatter
// exposure (via [Doc.Frontmatter]).
//
// Tables render through the native table op, which fixes every row to
// one height: cell text does not wrap, taller content is clipped, a
// hyperlink in a cell shows as its label rather than as a link, and
// GFM's per-column alignment (`:---:`) is parsed but not applied. See
// EXPLANATION.md for why.
//
// Math is still deferred: [obsidian.FeatureMath] is declared and
// reserved but wired to nothing, and is deliberately not part of
// [obsidian.FeatureAll]. GFM footnotes are absent for a different
// reason — goldmark's footnote extension is bound to no feature flag at
// all, so `[^1]` stays literal prose.
//
// # Concurrency
//
// [Parse] is safe to call from ANY goroutine, including concurrently
// with a frame in flight on the render goroutine. [Doc.Render] and its
// variants are not: they emit into the current Ui scope and belong to
// the render goroutine like every other widget call.
//
// The asymmetry is not an accident of the current implementation, and
// it is worth stating because "it is built out of FFI opcodes" reads
// like it should be false (ADR-0178 said as much, and is corrected).
// Parse never touches the wire. What it builds are `.Keep()` retained
// holders, and the whole path is Go-side:
//
//   - The builders come from a [sync.Pool] and write into their own
//     buffer; nothing is sent. Only Render's `Send()` reaches the FFFI
//     sink.
//   - `BuildRetained` interns the finished bytes through [unique.Make],
//     which is itself safe for concurrent use, and hands back an
//     immutable view of the interned string.
//   - The one piece of shared mutable state on the path — codeview's
//     package-level prepared-job memo — takes a mutex precisely because
//     the documented retain-once idiom is a package-level
//     `var doc = markdown.Parse(...)`, which runs at init on whatever
//     goroutine gets there.
//
// Two shipping consumers already depend on this (`docsections` parses
// from a snapshot call, `sqlapplet_store` from a bus handler); the
// contract is now stated rather than relied on by accident. The
// per-package `-race` test pins it.
//
// A [Doc] is immutable after Parse, so sharing one across goroutines is
// fine as long as only the render goroutine renders it.
//
// # See also
//
//   - EXPLANATION.md in this directory for the segment-tree design and
//     invariants.
//   - [github.com/stergiotis/boxer/public/semistructured/markdown/obsidian]
//     for the underlying parser.
//   - [github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview.PrepareSql]
//     for the parallel retain-once / render-many pattern this package follows.
package markdown
