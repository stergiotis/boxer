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
// returns ok; glyph-prefixed hyperlink fallback otherwise), and
// frontmatter exposure (via [Doc.Frontmatter]).
//
// Tables and math are still deferred.
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
