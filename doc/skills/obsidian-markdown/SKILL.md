# SKILL: Obsidian-Flavored Markdown to HTML

## Overview

The `obsidian` package renders Obsidian.md-flavored Markdown to HTML using [goldmark](https://github.com/yuin/goldmark). Each Obsidian-specific feature is implemented as a proper goldmark extension (AST node + parser + renderer) — no regexp hacks. Features are individually toggleable via a bitmask.

## Package Location

`github.com/stergiotis/boxer/public/semistructured/markdown/obsidian`

Sub-packages:
- `resolver` — `ResolverI` interface for wikilink/embed URL resolution
- `ext/wikilink` — `[[page]]` parser and renderer
- `ext/embed` — `![[file]]` parser and renderer
- `ext/callout` — `> [!type]` AST transformer and renderer
- `ext/highlight` — `==text==` parser and renderer
- `ext/comment` — `%%text%%` parser and renderer (strips from output)
- `ext/tag` — `#tag` parser and renderer

## Quick Start

```go
md := obsidian.New(obsidian.Options{
    Features: obsidian.FeatureAll,
    Resolver: obsidian.resolver.NoopResolver{},
})

var buf bytes.Buffer
err := md.Convert([]byte(input), &buf)
```

### With Frontmatter

```go
md := obsidian.New(obsidian.Options{
    Features: obsidian.FeatureAll,
})
pc := obsidian.NewParserContext()

var buf bytes.Buffer
err := md.Convert([]byte(input), &buf, parser.WithContext(pc))

meta := obsidian.GetFrontmatter(pc) // map[string]interface{}

// Render metadata as collapsible HTML
err = obsidian.RenderFrontmatterHTML(&buf, meta, true)
```

### Composing With Other Extensions

```go
ext := obsidian.Extension(obsidian.Options{
    Features: obsidian.FeatureWikilink | obsidian.FeatureCallout,
    Resolver: myResolver,
})
md := goldmark.New(goldmark.WithExtensions(ext, myOtherExt))
```

## Features

### Feature Bitmask

| Flag | Value | Description |
|------|-------|-------------|
| `FeatureWikilink` | `1 << 0` | `[[page]]`, `[[page\|alias]]`, `[[page#heading]]` |
| `FeatureEmbed` | `1 << 1` | `![[image.png]]`, `![[note#section]]` |
| `FeatureCallout` | `1 << 2` | `> [!type] Title` with foldable support |
| `FeatureHighlight` | `1 << 3` | `==highlighted text==` → `<mark>` |
| `FeatureComment` | `1 << 4` | `%%hidden%%` → stripped from output |
| `FeatureTag` | `1 << 5` | `#tag`, `#nested/tag` |
| `FeatureMath` | `1 << 6` | Reserved (not yet implemented) |
| `FeatureGFM` | `1 << 7` | Tables, strikethrough, task lists (goldmark built-in) |
| `FeatureFrontmatter` | `1 << 8` | YAML `---` frontmatter parsing |
| `FeatureAll` | `(1<<9)-1` | All features enabled |

### Wikilinks

**Syntax:** `[[page]]`, `[[page|display text]]`, `[[page#heading]]`, `[[page#heading|alias]]`

**HTML output:**
```html
<a href="/page" class="wikilink">page</a>
<a href="/page" class="wikilink">display text</a>
<a href="/page#heading" class="wikilink">page &gt; heading</a>
<a href="/missing" class="wikilink wikilink-broken">missing</a>
```

The `wikilink-broken` class is added when `ResolverI.ResolveWikilink` returns `exists=false`.

### Embeds

**Syntax:** `![[image.png]]`, `![[note]]`, `![[note#section]]`

**HTML output (image):**
```html
<img src="/image.png" alt="image.png" class="embed-image" />
```

**HTML output (note):**
```html
<div class="embed-note" data-src="/note">note</div>
```

Image detection is based on file extension (`.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.webp`, `.bmp`, `.avif`). The `ResolverI.ResolveEmbed` method can override this.

### Callouts

**Syntax:**
```markdown
> [!note] Optional title
> Content here

> [!warning]- Foldable (collapsed)
> Hidden content

> [!tip]+ Foldable (open)
> Visible content
```

**HTML output:**
```html
<div class="callout callout-note">
<div class="callout-title">Note</div>
<div class="callout-content">
<p>Content here</p>
</div>
</div>
```

**Foldable variant:**
```html
<div class="callout callout-warning">
<details>
<summary class="callout-title">Foldable</summary>
<div class="callout-content">
<p>Hidden content</p>
</div>
</details>
</div>
```

The callout type is case-insensitive. If no title is given, the type name is capitalized as the default title.

**Implementation:** Uses a goldmark `ASTTransformer` that post-processes blockquote nodes, detecting `[!type]` in the first line and replacing the blockquote with a `CalloutNode`.

### Highlights

**Syntax:** `==highlighted text==`

**HTML output:** `<mark>highlighted text</mark>`

### Comments

**Syntax:** `%%hidden comment%%`

**HTML output:** *(nothing — stripped from output)*

### Tags

**Syntax:** `#tag`, `#nested/tag`, `#my-tag`

**HTML output (span mode, default):**
```html
<span class="tag">#tag</span>
```

**HTML output (link mode):**
```html
<a href="#tag" class="tag">#tag</a>
```

Configure via `Options.TagRender`:
- `TagRenderSpan` (default) — renders as `<span>`
- `TagRenderLink` — renders as `<a>` with fragment href

Tag characters: letters, digits, underscores, hyphens, slashes (for nesting). Tags must start with a letter, digit, or underscore after `#`.

### Frontmatter

**Syntax:**
```yaml
---
title: My Note
tags:
  - foo
  - bar
aliases:
  - my-note
cssclasses:
  - wide
---
```

The YAML block is stripped from rendered output. Metadata is available via:

```go
meta := obsidian.GetFrontmatter(pc)         // map[string]interface{}
meta, err := obsidian.TryGetFrontmatter(pc)  // with error for malformed YAML
```

#### Frontmatter HTML Rendering

```go
obsidian.RenderFrontmatterHTML(w, meta, true)  // open=true → expanded by default
```

Produces:
```html
<details class="frontmatter" open>
<summary>Properties</summary>
<dl>
<dt>tags</dt><dd><ul>
<li>foo</li>
<li>bar</li>
</ul>
</dd>
<dt>title</dt><dd>My Note</dd>
</dl>
</details>
```

Handles nested maps (recursive `<dl>`), arrays (`<ul>`), arrays of maps, nil values, booleans, and numerics. Keys are sorted alphabetically. All values are HTML-escaped.

## ResolverI Interface

The library does not know the vault structure. Consumers implement `ResolverI` to map wikilink/embed references to URLs:

```go
type ResolverI interface {
    ResolveWikilink(page string, heading string) (url string, exists bool)
    ResolveEmbed(target string, heading string) (url string, isImage bool, exists bool)
}
```

`NoopResolver` is provided for testing — generates `/page`-style paths with `exists=true`.

## Parser Priorities

Extensions are registered with specific goldmark parser priorities to ensure correct ordering:

| Extension | Priority | Rationale |
|-----------|----------|-----------|
| Embed | 100 | Must match `![[` before image parser sees `![` |
| Wikilink | 101 | Must match `[[` before standard link parser sees `[` |
| Comment | 198 | Strips content early |
| Highlight | 199 | Standard inline priority |
| Callout | 199 | AST transformer (runs after block parsing) |
| Tag | 200 | Low priority — only triggers after other `#` uses |

## Design Decisions

1. **Goldmark extensions, not regexp** — Each feature is a proper `parser.InlineParser`, `parser.BlockParser`, or `parser.ASTTransformer` with corresponding `renderer.NodeRenderer`. This ensures correct interaction with goldmark's parsing pipeline.

2. **ResolverI in separate package** — Prevents import cycles between `obsidian` and `ext/*` packages. The resolver package has no dependencies on goldmark.

3. **Callout as AST transformer** — Goldmark already parses blockquotes. The callout extension post-processes blockquote AST nodes rather than implementing a competing block parser, which would conflict with goldmark's blockquote detection.

4. **Frontmatter via goldmark-meta** — Wraps `yuin/goldmark-meta` with `GetFrontmatter`/`TryGetFrontmatter` that return `map[string]interface{}`, keeping `gopkg.in/yaml.v2` out of boxer's public API.

5. **Feature bitmask** — Allows consumers to enable exactly the features they need without paying for unused parser overhead.

## Known Limitations

1. **Math (`$...$` / `$$...$$`)** — `FeatureMath` is reserved but not yet implemented. Requires a math rendering strategy decision (KaTeX, MathJax, or raw LaTeX passthrough).
2. **Multi-line comments** — `%%...%%` only works within a single line. Block-level comments spanning multiple lines are not supported.
3. **Highlight nesting** — Inline parsers within `==...==` highlight spans do not recurse (goldmark limitation for custom inline delimiters).
4. **Embed transclusion** — Note embeds render as placeholder `<div>` elements. Actual content transclusion requires vault access, which is the consumer's responsibility.
5. **Callout nesting** — Nested callouts (callout inside a callout) are not currently supported.
