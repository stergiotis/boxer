---
type: adr
status: proposed
date: 2026-09-23
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0255: GFM footnotes — a feature flag, and hover glosses in the markdown widget

## Context

GFM footnotes — `[^term]` references and `[^term]: text` definitions —
render as literal prose everywhere in boxer. goldmark ships a footnote
extension, but no bit of `obsidian.FeatureE` binds it, so `FeatureGFM`
buys tables, strikethrough and task lists and nothing more. The obsidian
skill, the widget's package doc and its `WithFeatures` comment all say so —
and slightly overstate how inert the syntax is: CommonMark reads a
one-token definition such as `[^1]: Gloss.` as a link reference
definition, so without the extension that reference renders as a
hyperlink to `Gloss.`

The first consumer that wants them is the positioning pages under
`doc/explanation/positioning/`, which gloss house terms (`bus`, `facts`,
`capability`) with footnotes and expect a reader to see the gloss by
pointing at the term. That is a tooltip, which the imzero2 markdown widget
can draw with the existing `HoverText` block: no IDL change is needed.

The widget's lowering drops what it cannot represent and counts the drop
by goldmark kind name under `Doc.Dropped()`
([ADR-0180](./0180-markdown-rendering-fidelity-pass.md) made that tally
the corpus gate). With the extension on, goldmark produces three node
kinds the lowering has no case for, so wiring the flag without the widget
cases would convert literal prose into *lost* prose.

Facts about the extension that shape the design:

- A reference only becomes a `FootnoteLink` when a definition with the
  same label exists. `[^x]` without one stays literal text, split across
  `Text` nodes by the link parser.
- The definitions are collected into one `FootnoteList` appended to the
  document root, numbered in order of first reference. Each `Footnote`
  holds ordinary blocks.
- The AST transformer appends one `FootnoteBacklink` per reference to the
  last paragraph of each definition, and removes definitions nothing
  references — before any consumer sees the tree.

## Decision

We will add `FeatureFootnote` to `obsidian.FeatureE`, wire it to goldmark's
footnote extension, and lower its nodes in the imzero2 markdown widget as a
superscript reference with a hover tooltip plus a numbered list of the
definitions at the end of the document.

### SD1 — The flag

- `FeatureFootnote` is the next free bit, `1 << 10`. It joins `FeatureAll`,
  whose definition moves to `((1 << 11) - 1) &^ FeatureMath`: the flag is
  wired, so the "a newly declared bit is opted in" rule applies unchanged.
- `FeatureGFM` still does not imply it. GFM in this package has always
  meant goldmark's `extension.GFM` bundle, which does not contain
  footnotes; widening it would change the meaning of an existing flag
  for every consumer that set it on purpose.
- The HTML output is goldmark's standard footnote HTML (`<sup>` reference,
  `div.footnotes` list, `↩︎` backlinks), unconfigured.
- The markdown widget's default feature set gains the flag, so mdedit's
  preview and the other default-config viewers render footnotes without a
  change of their own. The editor's text handling is untouched.

### SD2 — What the widget does with each node kind

| Node | Rendering |
| --- | --- |
| `FootnoteLink` (reference) | Its own paragraph run: `[n]` in the small-raised (superscript) text style in the link tone, wrapped in a `HoverText` block whose text is the definition's plain text. |
| `FootnoteList` / `Footnote` (definitions) | One trailing block: a separator, then an ordered list numbered from the first definition's index, each item the definition's blocks lowered like any other list item. |
| `FootnoteBacklink` | Dropped, and **not** counted under `Dropped()`. It is navigation chrome the HTML renderer invents, not authored text. A click-to-scroll back to the reference would need a scroll target per reference; the widget's scroll dispatch (`WithScrollToSection`) resolves headings only, and the tooltip already removes the reason to jump. |

`[n]` rather than a bare `n` follows Obsidian's reading view, and gives the
pointer a target two glyphs wider than a lone digit.

### SD3 — Tooltip text

The tooltip carries the definition flattened to plain text: text runs
concatenated, soft line breaks as spaces, hard breaks and block boundaries
as newlines, code spans as their text, links and wikilinks as their label.
A link inside a footnote is not clickable in the tooltip — `HoverText`
takes a string. The trailing list renders the same definition with its
links live, which is where a reader who wants to follow one goes.

The tooltip is computed once at parse time, like every other retained
payload in the widget.

### SD4 — Unresolved references

`[^x]` with no matching definition renders literally, as it did before the
flag existed. When the flag is on it is also counted under `Dropped()` as
`FootnoteLink`: the author wrote a reference and the reader gets none. The
detection scans each paragraph's text (code spans excluded) for the
`[^label]` shape. With the flag off nothing is counted — literal brackets
are then what the configuration asked for.

`Dropped()` has so far meant "text the reader will never see"; this widens
it to "a construct that did not render as authored", and its doc comment
says so.

### SD5 — Deferred, recorded

- **Unreferenced definitions.** goldmark's transformer removes them before
  the lowering sees the tree, so their text is lost on the HTML path and
  in the widget alike, uncounted. Counting them needs a pre-transform
  look at the tree (a transformer of our own ahead of goldmark's).
  Trigger: an author loses a gloss this way.
- **Section filtering.** The definitions block is the document's last
  segment and therefore belongs to its last section. Under
  `WithSectionFilter` a reference in a visible section keeps its tooltip,
  but the list shows only when the last section does.
- **References in tables and headings.** A table cell is flattened to a
  string, so a reference inside one shows as `[n]` without a tooltip. A
  heading's TOC text and slug leave the marker out.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `obsidian.FeatureE` | `FeatureFootnote` added; `FeatureAll` widened by one bit | the feature-table in the obsidian-markdown skill; the `FeatureAll` tests; `mdextract`, which parses with `FeatureAll` |
| `markdown.Doc.Dropped()` | may report `FootnoteLink` for an unresolved reference | its doc comment |
| markdown widget default feature set | gains `FeatureFootnote` | `doc.go`, the `WithFeatures` comment, play's rich-cell feature comment |
| `imzero2 drive` `hover` step | also takes an anchor, hovering the node's centre | the imzero2-drive skill's step table |
| `IMZERO2_MARKDOWN_DEMO_PATH` | new seed variable: the widget gallery's markdown demo loads this file when its window opens | `doc/env-vars.md` (generated) |

## Alternatives

- **Fold footnotes into `FeatureGFM`.** Rejected: changes what an existing
  flag parses for every consumer that set it, and the bundle it names does
  not contain footnotes.
- **Render references as their label (`bus`) instead of a number.**
  Rejected: the label is an author's handle, often `1`, and the positioning
  pages already write the glossed term in the prose next to it; a number
  matches the trailing list, which is what a reader without hover uses.
- **Tooltip only, no trailing list.** Rejected: a reader on a touch host,
  or one reading a capture, would never see the glosses.
- **A new IDL opcode for a rich tooltip (clickable links).** Rejected: the
  trailing list already carries live links, and a wire change for one
  consumer fails ADR-0180's batching rule.
- **Keep the backlink as a click that scrolls to the reference.** Rejected
  for now (SD2): needs a per-reference scroll target the render path does
  not have.

## Consequences

### Positive

- Footnotes render on the HTML path and in every default-config markdown
  viewer, with the gloss one pointer move away.
- The corpus gate catches a reference whose definition is missing.

### Negative

- Paragraphs with a reference leave the single-label fast path for the
  mixed-run `HorizontalWrapped` layout, which wraps at run boundaries —
  the cost links already pay.
- `FeatureAll` consumers (`mdextract`) now see definitions as `Footnote`
  blocks at the end of the tree, and lose unreferenced ones (SD5).

### Neutral

- A reference takes no id-sequence slot: the `HoverText` block opens its
  own scope and senses hover on it. The definitions list adds only what
  its items render, so documents without footnotes keep their ids.

## Verification plan — Tier 1

- The obsidian package asserts the HTML for a reference + definition pair
  and that `FeatureGFM` alone leaves `[^1]` literal.
- The widget's parse tests assert the reference run (label and tooltip
  text), the trailing definitions block, a zero `Dropped()` for a fully
  footnoted document, and a counted `FootnoteLink` for an unresolved one.
- A headless scene (ADR-0248), `widgets/markdown/scenes/footnote-hover`,
  seeds a positioning page into the widget gallery's markdown demo, hovers
  a reference by name, reads the tooltip text and captures the tooltip and
  the definitions list. The demo's path box is a text input the driver
  cannot focus, which is why the file arrives by seed variable rather than
  by typing.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0180](./0180-markdown-rendering-fidelity-pass.md) — the widget's fidelity pass and the `Dropped()` corpus gate.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — scenes.
- [obsidian-markdown skill](../skills/obsidian-markdown/SKILL.md).
- goldmark's footnote extension: https://github.com/yuin/goldmark#footnotes-extension
