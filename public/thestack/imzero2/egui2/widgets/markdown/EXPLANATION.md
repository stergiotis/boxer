---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imzero2 markdown — Explanation

The `markdown` package renders Obsidian-flavored Markdown documents through
the imzero2 / egui2 widget tree. Parsing happens once, in Go, via boxer's
goldmark-based `obsidian` extender; the parsed AST is lowered into a small
Go-side **segment tree** that holds pre-built `RetainedFffiHolderTyped`
blobs for static blocks (paragraphs, headings, code) and Go closures for
the few constructs that egui cannot retain holistically. Each frame's
render walks that tree and splices retained bytes into the current Ui
scope — no re-parse, no per-block allocation in the steady state.

## Background

The Obsidian markdown dialect adds wikilinks, embeds, callouts, YAML
frontmatter, tags, highlight (`==`) and comment (`%%`) on top of CommonMark
and GFM. Boxer ships a goldmark extender stack covering all of those at
`public/semistructured/markdown/obsidian/`. The interpreter side (Rust /
egui) has no markdown widget — only primitives: `Atoms` for inline rich
text, `LabelAtoms` / `Label` for paragraphs, `CodeView` for syntax-coloured
code blocks, `Frame` for bordered groups, `Vertical` / `HorizontalWrapped`
for flow, `Hyperlink` for links. A markdown view is therefore a
*composition* of those primitives, not a new widget op. This package owns
the AST→primitive lowering on the Go side.

The retention path mirrors `components.PrepareSqlView`: an expensive Go
computation produces a `RetainedFffiHolderTyped[…]` whose content is
interned by `unique.Handle[string]`, so identical results across documents
share storage and zero per-frame allocation. The same property holds here
at paragraph granularity: two notes that quote the same paragraph share
exactly one `Atoms` blob.

## How it works

### Parse → segments

`Parse(md, opts...)` constructs a goldmark instance with the configured
Obsidian features, parses the source into an AST, and walks the top-level
block children once. Each block produces a `segment` value:

- `paragraph` / `heading`  — built by walking inline children into one
  `Atoms` builder (`Strong`, `Italics`, `Code`, `Strikethrough`,
  `Highlight` styles via the RichText sub-protocol) and `.Keep()`-ing
  the result. Hyperlinks (regular, autolinks, wikilinks, embeds) break
  the flow into a sequence of *runs* (atoms blob OR hyperlink) so the
  segment can render as `HorizontalWrapped` only when needed.
- `code block`              — the source bytes wrap a `CodeViewJob` retained
  holder. Fenced blocks whose info-string token matches `go`/`golang`,
  `sql`, or `json` are highlighted via the corresponding
  `codeview.Prepare*` builder; every other language tag (and every
  indented block, which has no fence language) falls back to a plain
  `c.CodeViewJob(text).Keep()`. Adding a new language is a one-case
  extension of `lowerCodeBlock`'s dispatch switch.
- `list` / `list item`      — recursive segments. The list segment carries
  `ordered` and `start` flags; rendering emits one `Horizontal` per item
  with a glyph (`•`) or counter, then a nested `Vertical` for the item's
  children.
- `blockquote`              — a recursive `Frame.PresetGroup()` wrapper
  around child segments.
- `thematic break`          — a `Separator()` opcode.
- `callout`                 — themed `Frame` (border + tinted fill chosen
  by callout type via `theme.go`) with a strong-styled title row above
  the body. Foldable callouts swap the Frame for a `CollapsingHeader`
  that wraps the same Frame internally so the body still gets the
  themed chrome.

Inline nodes that cannot become atoms (links, wikilinks, embeds) are
extracted as separate runs at paragraph build time. Strong, Em, Code,
Strikethrough and Highlight nest as flag bits in a `styleE` bitmask,
applied to the RichText scope at text-emit time. Highlight uses a
distinct code path: it routes through `StyledTextColored(fg, bg)`
with a yellow "highlighter pen" palette since plain `RichText` has no
background-color knob.

Wikilinks and embeds resolve their URLs through a
`resolver.ResolverI` (defaults to `resolver.NoopResolver`, which
generates `/page#heading` URLs). Image references — both CommonMark
inline images and Obsidian image embeds `![[file.png]]` —
additionally call the resolver's `LoadImage(ref)` method at parse
time; the returned RGBA8 buffer is stashed on a `runKindImage`
paragraph run and the renderer splices a `c.Image` widget into the
surrounding `HorizontalWrapped` flow. `NoopResolver.LoadImage`
returns `ok=false` so the default behaviour remains the
pre-image-widget fallback: a glyph-prefixed hyperlink (`📄 Note`
for note embeds, `🖼 file` for image embeds or CommonMark images
the resolver does not recognise). Note transclusions (`![[Note]]`)
skip `LoadImage` altogether — they go straight to the `📄`
hyperlink, since `isImage` from `ResolveEmbed` is false.

### Render → splice

`(*Doc).Render(ids)` iterates segments. For paragraph and heading
segments it splices the retained `Atoms` blob into the current scope via
`LabelAtoms(holder).Wrap().Send()` — one opcode, content-addressed
de-duplicated. Code blocks emit `CodeView(ids.PrepareSeq(seq), holder)`.
List and blockquote segments open block iterators (`Horizontal`,
`Vertical`, `Frame`) and recurse.

Because list and blockquote IDs are derived from a monotonic
per-`Render`-invocation sequence (`PrepareSeq(0)`, `PrepareSeq(1)`, …),
the caller MUST wrap `Render` in a `c.IdScope` whenever multiple
instances of any markdown doc may coexist under the same parent scope.
The package does not push its own outer scope: that decision is the
caller's, exactly like `components.CodeView`.

### Frontmatter

When `FeatureFrontmatter` is enabled (default), the parser is invoked
with a fresh `parser.Context` and `meta.Get(pc)` extracts the YAML
data. goldmark-meta returns a `map[string]interface{}` whose iteration
order is randomised by Go on every range, which would re-shuffle any
frontmatter UI every frame. We therefore lower the map into a
`containers.BinarySearchGrowingKV[string, interface{}]` once at parse
time. Callers iterate via `IteratePairs()` and get stable, sorted-by-key
order across frames; lookup via `Get(key)` is O(log n).

The KV is stashed on the `Doc` and exposed via `Frontmatter()` for
callers that drive sub-views from note metadata (e.g. `kind: sql` →
`components.PrepareSqlView`). `(*Doc).Render(ids)` renders body
content only; `(*Doc).RenderFrontmatter()` is a separate, opt-in
helper that emits a Separator + "Frontmatter (parsed):" header + one
strong-key + value-text label per top-level entry. Decoupling the two
lets prose-only callers skip the metadata chrome and metadata-only
inspectors skip the body. Real UIs needing custom metadata layout
should iterate `Frontmatter().IteratePairs()` directly.

## Invariants

- **Retained-holder lifetime.** Every `Atoms`/`CodeViewJob` holder built
  during `Parse` is stored on the segment that references it. The
  `*Doc` therefore transitively keeps every interned content blob
  reachable; do not detach segments from the document for as long as
  any frame may still render it.
- **ID derivation order.** Segments that need an id consume the seq
  counter in render order. Two `Render` invocations of the same doc
  emit the same seq sequence, so retained ids are stable across frames.
  Adding a new id-needing segment kind shifts existing ids — bump the
  scopeKey when changing the lowering rules in a way that affects
  layout state stored against ids.
- **Inline run boundaries.** A paragraph either contains zero
  hyperlinks (one atoms run, rendered as a single `LabelAtoms`) or one
  or more (rendered as a `HorizontalWrapped` over a run sequence).
  Rendering must keep these two paths distinct: wrapping an unbroken
  paragraph in `HorizontalWrapped` defeats text wrapping at glyph
  granularity.
- **Caller-provided IdScope.** `Render` does not open an `IdScope`.
  Two coexisting instances of the same doc under one scope WILL collide
  on code-block / blockquote ids unless the caller wraps each in its
  own `c.IdScope(ids.PrepareStr("…"))`.
- **Frontmatter iteration order.** `(*Doc).Frontmatter()` returns a
  `BinarySearchGrowingKV[string, any]`, never a Go map. UI that
  iterates it with `IteratePairs()` stays put across frames; a future
  contributor must NOT swap this back to a map without also providing
  a stable iteration order, or the per-frame UI will jitter.

## Trade-offs

- **No single retained-blob doc.** Block iterators (`Vertical`,
  `Frame`, `HorizontalWrapped`) emit deferred-block opcodes that bracket
  child opcodes; the bracketing IDs are assigned at emit time and cannot
  be baked into a content-addressed retained blob. So `Doc` is a Go-side
  tree of segments, not one big `RetainedFffiHolderTyped`. The sub-blobs
  (atoms, code-view jobs) are still retained.
- **`egui::Atoms` wraps at atom boundaries, not inside them.** The
  Atoms primitive that backs `LabelAtoms` is the egui compose layer:
  each pushed text fragment is one atom, atoms wrap *between*
  themselves, and only the atom's own text shaper word-wraps *inside*
  it. CommonMark soft line breaks within a paragraph generate
  successive `ast.Text` nodes — one Text() opcode per node would emit
  one atom per node, each wrapping independently with a forced
  atom-boundary break in between. The result is a visible jitter:
  one fragment wraps to many narrow lines while a short trailing
  fragment sits on a fresh line by itself. The visitor mitigates this
  by coalescing consecutive same-style text fragments inside the
  paragraph builder so each contiguous run becomes a single atom; egui
  then word-wraps the merged string. The mitigation is full for
  unstyled paragraphs and partial for paragraphs with inline styling
  (each style boundary still produces a fresh atom). For paragraphs
  with style boundaries, `labelAtoms`' Rust construction now flattens
  the atoms into a single `egui::text::LayoutJob` (one section per
  styled span) and renders via `egui::Label::new`, so the shaper
  word-wraps across style transitions as one continuous run.
- **Lists are hand-rolled.** egui has no list primitive. Bullet/numbered
  lists are `Horizontal{glyph, Vertical{children}}` per item. This
  reproduces Obsidian's visual layout closely enough for a viewer; an
  editor with cursor placement would need a richer model.
- **Code blocks are dispatched per language.** Fenced blocks tagged
  `go`/`golang`, `sql`, or `json` route through
  `codeview.Prepare{Go,Sql,Json}`; every other tag and every indented
  block (which has no fence language) falls back to a plain
  `CodeViewJob`. The dispatch is a small `switch` inside
  `lowerCodeBlock` — adding a new language is a one-case extension, and
  the retained-holder shape downstream of the segment is identical
  across paths.
- **Image bytes flow through the resolver, not a separate loader.**
  Extending `ResolverI` with `LoadImage(ref) -> (pixels, w, h, ok)`
  keeps a single seam between markdown rendering and the consumer's
  asset store: vault-aware UIs that already implement `ResolverI`
  for wikilinks supply the image decode in the same struct, while
  HTML-style renderers (which only need URLs) keep returning
  `ok=false` and stay on `ResolveEmbed` alone. The trade-off is that
  decode happens at parse time on the calling goroutine; large image
  sets in long docs make `Parse` slower but render-path allocation
  stays zero. `WithImageMaxSize(w, h)` caps the FitAspectMaxE
  bounding box at render time (default `(800, 600)`); the cap is
  per-Doc, not per-image, since Doc has no per-image style hook
  today.
- **Pixels are re-sent every frame, not tracked.** The bindings
  doc-comment at `c.ImageVersionTracker` is explicit: a tracker key
  must be 1:1 with the egui widget id, and for static assets shown
  a small fixed number of times "skipping the tracker entirely is
  usually clearer — the per-widget-id one-shot upload cost is
  negligible." Markdown image pixels are decoded once at parse time
  and shipped on every frame; the wire contract in
  `egui2_definition_d_image.go` says a non-empty buffer always
  triggers re-upload, so `contentVersion=1` is pinned and never
  needs busting. The alternative (a per-`Doc` tracker keyed by
  segment seq) silently breaks the package-level retain-once /
  render-many idiom under multi-scope rendering, since the second
  scope's widget ids are distinct from the first scope's and the
  tracker can't tell them apart.
- **Image dimensions are capped at `imageMaxPixelCount`
  (64 Mpx ≈ 256 MiB RGBA) in `emitImage`.** Defense in depth even
  though the resolver owns the allocation; rejects pathological
  buffers before they reach the segment tree. Cap covers up to 8K
  source textures while staying inside what egui can plausibly
  upload.
- **Callouts collapse a 20+ type vocabulary into 5 families.** Obsidian
  recognises `note`, `info`, `tip`, `hint`, `important`, `success`,
  `check`, `done`, `question`, `help`, `faq`, `warning`, `caution`,
  `attention`, `failure`, `fail`, `missing`, `danger`, `error`, `bug`,
  `quote`, `cite`, `example`, `summary`, `abstract`, `tldr`, `todo`.
  Mapping each to a unique colour would dilute the visual signal; we
  cluster into note (blue), tip (green), warning (amber), danger
  (red), quote (gray) plus a default fallback. Per-type emoji glyphs
  preserve enough type identity for the title row.

## Further reading

- Obsidian goldmark extender: [boxer/public/semistructured/markdown/obsidian](https://pkg.go.dev/github.com/stergiotis/boxer/public/semistructured/markdown/obsidian)
- imzero2 retention pattern: [`bindings.CodeViewJob`](../../bindings/factories.out.go)
- imzero2 widget id rules: [`keelson/runtime/widgethandle/EXPLANATION.md`](../../../../../keelson/runtime/widgethandle/EXPLANATION.md)
- imzero2 architecture: [`../../EXPLANATION.md`](../../EXPLANATION.md)
