---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified by a second reader; do not
> cite as authoritative.

# Markdown rendering review — correctness, completeness, orthogonality, layout

> **Provenance.** Compiled 2026-08-09 against the working tree at a0757f82.
> Markdown is becoming a primary human-machine interface for this repository
> (help books, play docs, sqlapplet books, mdedit, capability docs), so the
> rendering stack was reviewed end to end: the `obsidian` goldmark extender,
> the `imzero2/egui2/widgets/markdown` native renderer, the
> `markdownhighlight` source tiers, the Rust interpreter ops they land on, and
> every in-repo consumer. Evidence classes are marked throughout:
> **[live]** = observed in a running instance (widgets gallery under a
> headless compositor, driving a purpose-built probe document through the
> demo's Load section), **[probe]** = a throwaway in-package parse test run
> during the review (not committed), **[code]** = read from source with
> file references, **[pinned]** = an existing test asserts it. Nothing here is
> a decision; the recommendations are candidates for a design dialogue.

## Summary

The stack is in better shape than a survey of this length suggests. The
architecture — parse once into a segment tree of retained FFFI holders, splice
per frame — honours both premises this review was asked to check: per-frame
cost is a tree walk with zero steady-state allocation (immediate-mode-clean,
no retained *layout*, only retained *content*), and the whole feature rests on
goldmark plus existing egui primitives (no HTML engine, no new dependency).
Consumers are uniformly disciplined: every render-path parse is gated, every
multi-doc scope is id-wrapped, both scroll-to-section users guard the slug.
The corpus of committed books is clean against every gap listed below — zero
task lists, HTML blocks, footnotes, or duplicate slugs in 57 book files
(the ADR corpus has two duplicate-slug pairs, consumed only by `docsections`,
which never renders).

The defects cluster in four places: (1) a small set of **silent content
drops**, one of which (task-list checkboxes) contradicts the package's own
documentation; (2) **inline images are upscaled** past native size by the fit
mode the widget hard-codes; (3) **typographic rhythm is flat** — one global
8 px gap everywhere, headings not set off, ordered-list bodies misaligned at
digit boundaries; (4) the **render path has no automated coverage at all**, so
every one of these was findable only by reading Rust or driving a live window.

## 1. Completeness

| # | Finding | Evidence | Where |
|---|---|---|---|
| C1 | **GFM task-list checkboxes are dropped.** `FeatureGFM` enables goldmark's `TaskList`; the AST carries `TaskCheckBox` nodes **[probe]**, but `emitInline` has no case for them, so `- [x] done` renders as a plain bullet "done" — the checked/unchecked state disappears **[live]**. This violates the package's own contract comment ("Every flag in the default set has a case", `markdown.go` `WithFeatures`). | live + probe + code | `visitor.go` `emitInline` switch; `obsidian.go:115` |
| C2 | **Footnotes are not parsed, but the docs claim they are.** `extension.Footnote` is wired to no flag; `[^1]` and `[^1]: text` stay literal prose **[probe][live]** — no content loss, but `WithFeatures`' doc comment advertises "GFM (tables / strikethrough / footnotes / task lists)", and `lowerBlock`'s comment speaks of dropping "GFM footnote definitions", which cannot occur. | probe + code | `markdown.go` `WithFeatures`; `visitor.go` `lowerBlock` |
| C3 | **`FeatureMath` is a dead flag inside `FeatureAll`.** Declared, documented as "reserved", and never read by `collectExtensions`/`collectParserOptions` — enabling it does nothing. Deferral is recorded (doc.go "Math is still deferred"), but a flag that is part of `FeatureAll` and wired to nothing is a trap for the next consumer. | code | `obsidian/config.go:17,36`, `obsidian.go:103-141` |
| C4 | **HTML blocks delete their prose.** `<div>…text…</div>` vanishes wholesale — segments drop from 3 to 2 and the inner text is gone **[probe][live]**. Inline raw HTML degrades better: tags are dropped, inner text survives unstyled **[live]**. The silent drop is a recorded decision (`lowerBlock` comment: an honest marker "is left for its own change"). | live + probe | `visitor.go` `lowerBlock`, `emitInline` default |
| C5 | **`![[image.png|300]]` (Obsidian size syntax) breaks image detection.** The embed parser splits only on `#`, so the pipe stays in `Target`, the `.png` suffix match fails, and the embed renders as a 📄 note link instead of an image. Common in real vaults. | code | `ext/embed/parser.go:47-58`, `resolver/resolver.go:89-97` |
| C6 | **`[[#heading]]` (same-page link) renders a stray leading "> ".** `DisplayText` has no empty-page branch (`s = Page + " > " + Heading`). URL becomes `/#fragment`. | code | `ext/wikilink/ast.go:40-49` |
| C7 | **An image inside a link renders as a text link, never as an image** (`[![alt](#img)](#url)` → link labelled "alt") **[probe]**. Consistent with cells/labels flattening, but undocumented. | probe | `visitor.go` `emitInline` `*ast.Link` case |
| C8 | **Multi-line `%%` comments are not comments.** Only single-line pairs strip; a block `%%`…`%%` emits its content verbatim — surprising for anyone using Obsidian block comments to hide notes. Known limitation in the skill doc; restated here because the failure mode is *leaking* text rather than losing it. | code + pinned (single-line only) | `ext/comment/parser.go` |
| C9 | Malformed frontmatter YAML leaks the raw YAML block plus a literal `<!-- <yaml error> -->` marker into the rendered flow (goldmark-meta behaviour on its error path). Untested anywhere. | code | goldmark-meta `meta.go:155-157,243-248` |

Not gaps: setext headings, entity references, linkify (bare URLs get a scheme
and render clickable **[probe]**), email autolinks (`mailto:`), indented code,
tight/loose lists, tables-in-lists, callouts-in-lists all lower correctly
**[probe][pinned]**.

## 2. Correctness deviations

| # | Finding | Evidence |
|---|---|---|
| R1 | **Ordered-list start is coerced:** `0.` renders as `1.` (`listStart == 0` guard conflates "unset" with a genuine zero start) **[probe]**. `1)` paren markers render as `1.` (marker style not preserved). Cosmetic-to-minor. |
| R2 | **Style nesting is one-directional for highlight:** `**==x==**` composes (bold pen), but `==**x**==` shows literal asterisks inside the pen, because the highlight parser never re-parses its interior **[live]**. The demo's own highlight section uses the broken order, so today the gallery *demonstrates* the defect. |
| R3 | **Tag body is ASCII while the tag boundary is rune-aware:** `#café` silently truncates to tag `caf` + literal `é`; `#日本語` is not a tag at all (Obsidian allows both). A lossy split, not a clean rejection. | 
| R4 | **Callout titles lose their inline markup.** The title's parsed inline nodes are stripped and the raw bytes re-emitted escaped — `> [!tip] **Bold** title` shows literal asterisks (both HTML and native paths; the native path stores `string(n.Title)`). |
| R5 | **A document that opens with `---` (thematic break) loses it to the frontmatter parser** when `FeatureFrontmatter` is on — acknowledged in a test comment, not pinned; `play_detail_rich` disables frontmatter for exactly this reason. |
| R6 | **Duplicate heading texts collide on one slug** (no `-1` dedup à la GitHub/Obsidian): `WithScrollToSection` and any TOC built from `Doc.Headings()` can only ever reach the first occurrence **[probe]**. Corpus is currently clean; the ADR corpus (parse-only) already has two colliding pairs. Related latent hazard: helphost's nav derives widget ids from slugs (`"nav-sec-…#"+slug`) while its own search list deliberately avoids exactly that because duplicate ids silently swallow clicks. |
| R7 | **Doc/comment drift (four instances):** (a) `WithFeatures` claims footnotes/task lists (C1/C2); (b) `render.go` and EXPLANATION.md justify per-frame pixel re-send with "a non-empty buffer always triggers re-upload" — the Rust `ImageCache` gates on `(version, w, h)` first, so there is **no** per-frame GPU re-upload; the real cost is wire bandwidth (the definition-file comment in `egui2_definition_d_image.go` is what's wrong); (c) EXPLANATION.md's "Atoms wrap at atom boundaries" trade-off text predates the Rust-side LayoutJob flatten and now overstates the problem — mixed-run paragraphs wrap at glyph level (§4); (d) `play_detail_rich.go` still says "a GFM table in a declared cell renders as nothing" — tables render since helphost "generation 2". |
| R8 | **`markdown.Parse`'s thread contract is ambiguous.** ADR-0178 and mdedit state it is render-goroutine-only ("built out of FFI opcodes"); in fact `.Keep()` builders write only pooled Go-side buffers (`sync.Pool`) and the codeview memo takes a mutex precisely for cross-goroutine init — and two shipping consumers already parse off the render goroutine (`docsections.go` from `Snapshot()`, `sqlapplet_store.go` from a bus handler). Today that is safe, but by accident of implementation, not by stated contract. |

## 3. Orthogonality

**What composes.** The `styleE` bitmask is genuinely orthogonal: strong /
emphasis / code / strike / highlight / tag stack across nesting (`***x***`,
tag-inside-emphasis, code-inside-bold-inside-highlight) **[probe][live]**, and
block recursion (lists ⊂ quotes ⊂ callouts ⊂ list items, tables in list
items) falls out of one `lowerBlock` switch applied uniformly. Feature bits
map 1:1 to extenders (except C3). The `fieldCarriedText` consolidation —
one enumeration of field-carrying inline nodes shared by `headingPlainText`
and `flattenInlineText` — is the right structural answer to the divergence
class that produced the tag-deletion incident.

**Where it breaks, structurally:**

1. **The default branch drops unknown nodes silently.** This is the root of
   C1/C4 and of the historical tag deletion (a feature flag whose node the
   lowering didn't know deleted the text it covered). ADR-0178 says it
   plainly: "Nothing structural stops the next divergence — only that test."
   The system's failure mode for *any* future parser feature is invisible
   content loss, which is the worst available default for a reading surface.
2. **Hyperlink runs discard inherited style.** `emitLink` takes no
   `parentStyle`; `**a [link](#u) b**` renders the link unstyled between bold
   halves **[live]**. A primitive constraint (Hyperlink takes a plain label),
   but nothing records it.
3. **Three independent readings of markdown syntax exist** — goldmark (the
   authority), `markdownhighlight.HighlightLex` (editor source tier, by
   design), and `markdownhighlight.Highlight` (canonical viewer tier). The
   only cross-tier agreement test is `#tag`, asserted as a document-level
   boolean, so the tiers can already disagree about *where* a construct sits
   without failing anything.
4. **`applyStyledText` triplicates the style application** (tag / highlight /
   plain branches) with one asymmetry (`.Code()` missing from the tag branch
   — unreachable today, but the kind of skew that becomes reachable).

## 4. Layout — quality against the premises

**Verified sound.** The core text path is as good as egui allows:
single-run paragraphs are one `LabelAtoms` → one `LayoutJob` → glyph-level
wrap; mixed paragraphs (`HorizontalWrapped` over runs) still wrap mid-galley
with correct first-row indentation, because the Rust `labelAtoms` op flattens
atoms into one `LayoutJob` unconditionally and egui's `Label` handles the
wrapped-horizontal case (first row fills the remaining width, following rows
start at the margin) — confirmed in the vendored egui source and live
**[code][live]**. Hyperlinks wrap mid-label too, so long URLs cannot overflow
a pane. Callout theming, tag pills and the highlight pen all draw from IDS
semantic tokens. Table rows scale with density. Nothing in the package
retains layout state beyond what egui itself owns per widget id (collapse,
column drags, scroll) — the immediate-mode premise holds.

**Measured gaps** (widgets gallery, standard density, probe document):

| # | Finding | Measurement |
|---|---|---|
| L1 | **Flat vertical rhythm.** Every block pair — paragraph→paragraph, list→heading, heading→paragraph — is separated by exactly the global `item_spacing` (8 px at standard density). Headings get no extra space above, so hierarchy rides on font size alone; dense documents read as an undifferentiated column. | gap above H2 = gap between paragraphs = 8 px **[live]** |
| L2 | **Ordered-list bodies misalign at digit boundaries.** Markers are left-aligned labels, so item text after `10.` starts 7.4 px right of text after `9.`. | text x: 84.3 (1-digit) vs 91.75 (2-digit) **[live]** |
| L3 | **Inline-flow gaps around links/images are style gaps, not spaces.** Each run boundary inserts `item_spacing.x` *in addition to* any trailing space in the text run, so links float with visibly wide gaps, and trailing punctuation after a link detaches ("…even a link ." ) **[live ×2]**. |
| L4 | **Small images are upscaled.** `FitAspectMax` computes `s = min(fw/nw, fh/nh)` with no `s ≤ 1` clamp, so every image smaller than the cap box (default 800×600) is blown up to it — the demo's 128×80 assets render stretched at the 200×140 cap **[live][code]**. A zero axis in `WithImageMaxSize` means "fill available", which inside a vertical ScrollArea — where every markdown doc lives — reads ~0 and collapses the image to invisible (the known zero-box fill-available trap in vertical scroll hosts; the worldmap demo hit it). |
| L5 | **Tables cannot opt out of their own vertical scroll.** The `table` op's `vscroll` apply is one-sided (`if w.vscroll { … }` with no else) and egui_extras defaults to on, so `.Vscroll(false)` is a no-op **[code]**. A table taller than the remaining viewport becomes a nested scroll region inside the document pane (min 200 px), capturing wheel input — against the "document flows, outer pane scrolls" reading model. Short tables auto-shrink and are unaffected **[live]**. |
| L6 | **Heading sizes are fixed pixels** (26/22/18/16/14/12.5) "tuned against the default 12 pt body", while sibling code (table row heights) reads the IDS type scale through `ScaledPt(…, DensityFromEnv())`. Under Tight/Roomy density, body text and tables move; headings do not. | code |
| L7 | **Mixed-run paragraphs space wrapped rows by `item_spacing.y`**, while single-run paragraphs use the galley's own line height — a subtle line-spacing inconsistency visible only when a wrap lands at a run boundary. | code |
| L8 | **Per-frame image wire cost.** Pixels ride the op stream every frame (`contentVersion` pinned, tracker deliberately skipped for multi-scope safety). GPU upload is cached (R7b), but an embedded 1920×1080 screenshot is ~8 MB of memcpy per frame each way. Fine for icons; wrong cost model for screenshot-heavy books. | code |

**Deliberate and premise-conform** (no change recommended): rune-count column
seeding (measurement is genuinely unavailable on the render path; seeds +
user drag is the honest immediate-mode answer, and the budget/clamp reasoning
is written down); fixed one-line table cells (a different op is the real fix,
recorded in EXPLANATION.md); blockquote as a full `PresetGroup` frame rather
than Obsidian's left bar (cosmetic identity, fine); heading-granular scroll
sync (degenerates correctly).

## 5. Test and demo coverage

- **The render path executes in no test.** Every `render.go` behaviour —
  scroll dispatch ordinals, action buttons, link router branches, callout
  fold/frame paths, the table push/drain adjacency, the id-sequence
  derivation-order invariant — is either re-implemented by hand against
  `doc.segments`, asserted at option level, or untested. The lowering, by
  contrast, is well covered (~60 tests), and deviations are pinned on
  purpose (alignment unapplied, cells flattened, ragged rows truncated).
- **The lexer's invariants are the strongest net in the stack** (spans tile
  the source byte-exactly, asserted over a 60-entry corpus *and every prefix
  of a representative document*) — necessary, since `CodeViewJob` renders
  only claimed bytes: a span hole is invisible text **[code]**, and none of
  the codeview builders gap-fill.
- **Cross-tier parity is #tag-only and boolean.** 23 of 43 lex categories
  have no category-level assertion.
- **The demo showcases neither `#tag` pills, heading anchors, task lists (as
  output), `RenderActions`, nor the link router** — the gallery cannot show
  what the code cannot do (C1), but it also hides several things it can.
- mdedit's preview logic is tested down to the caret/slug arithmetic, but the
  reparse gate itself (`syncDoc`) has no direct assertion.

## 6. Recommendations, ranked, with premise cost

> **Outcome.** The forks below were settled in a design dialogue on
> 2026-08-09; the decisions and milestones are recorded in
> [ADR-0180](../adr/0180-markdown-rendering-fidelity-pass.md). Items 13–16
> remain deferred with the triggers as written.

Small, premise-clean (no new dependency, no retained layout, no new op):

1. **Add the `TaskCheckBox` case** to `emitInline` — a Phosphor glyph atom
   (`icons.PhSquare` / `PhCheckSquare`, per the affordance-glyph rule) plus
   the item text; add a rendered task-list section to the demo and a parse
   test. Closes C1 and the contract violation.
2. **Fix the four doc drifts** (R7a–d) — cheapest correctness gain in the
   set; two of them actively teach the next contributor a wrong cost model.
3. **Heading rhythm:** emit a level-scaled top gap before heading segments
   (an `AddSpace` derived from the IDS type scale ×  a small factor), and
   derive `headingFontSize` from `ScaledPt` so density moves headings with
   everything else (L1, L6).
4. **Right-align ordered markers**: compute the widest marker per list at
   lowering time (rune count, same trick the tables use) and pad markers to
   it, or emit the marker column at a fixed width (L2).
5. **Clamp `FitAspectMax` to `s ≤ 1` for the markdown path** — either a
   `min(s,1)` in the op (wire change: rebuild both sides) or a widget-side
   guard passing `min(cap, native)` as the box. Document the zero-axis
   ScrollArea collapse on `WithImageMaxSize` (L4).
6. **Make the table binding's `.Vscroll(false)` real** (two-line else in the
   apply) and turn it off from `renderTable`, or set a generous
   `MaxScrollHeight`, so long tables flow with the document (L5).
7. **Strip the pipe-suffix from embed targets** before resolution (C5), and
   special-case the empty page in `DisplayText` (C6).

Medium, still premise-clean:

8. **Make the drop path visible to tooling:** count skipped nodes per kind on
   `Doc` (e.g. `Doc.Dropped() []KindCount`) and assert zero in the book
   corpus tests; optionally a `WithUnsupportedMarker()` render opt that draws
   a small ⍰ chip instead of nothing. This converts the worst failure mode
   (silent loss, finding #1 structurally) into a lintable property without
   changing default rendering.
9. **Inline-flow gap:** trim the trailing space of a text run that abuts a
   link run (and leading space after it) so the visible gap is item_spacing
   alone — or investigate a scoped `item_spacing.x` override for the wrapped
   row if the bindings grow one (L3).
10. **Broaden the two-tier parity test** from "#tag exists" to a
    per-category, per-byte-range comparison over a small corpus (§3.3).
11. **Decide `FeatureMath`:** implement (there is no math renderer today —
    real scope) or remove it from `FeatureAll` and mark reserved-not-flagged
    (C3).
12. **State the `Parse` thread contract** (R8): either bless off-render-
    goroutine parsing (documenting which guarantees make it safe: pooled
    builders, interning, memo mutex) or fix `docsections`/`sqlapplet_store`.

Larger, needs its own dialogue (descope-recorded, not blocking):

13. Render-path smoke coverage — an op-capture harness or the ADR-0154
    carrier lane; until then every §4 behaviour regresses invisibly.
14. Styled table cells via `tableCellRichText` (EXPLANATION already scopes
    it); links in cells stay impossible without a new op.
15. Image cost model for screenshot-heavy books (tracker + starved-report
    per the Lost-Sends pattern) — only if such books actually arrive.
16. Slug dedup for duplicate headings (R6) — moves existing anchors, so it
    wants corpus lint first (cheap: extend the book tests to reject
    duplicate slugs, which also protects helphost's slug-keyed nav ids).

## Verification appendix

- **Probe document** exercising C1/C2/C4, R1/R2/R6, L1–L3 and the table
  edge cases: parse-level via a temporary in-package test (segment/run/
  heading dumps), render-level by loading the same file through the widgets
  gallery markdown demo's "load file" section under a headless compositor
  (`weston --backend=headless --idle-time=0`) driven over egui-mcp, with
  geometry read from the accessibility tree rather than pixels.
- **Rust ground truth** read from `rust/imzero2/src/imzero2/interpreter.rs`
  (labelAtoms ~:7932, table ~:11428, image ~:7792), `code_view.rs`,
  `image.rs`, and vendored egui 0.35 (`widgets/label.rs:192-228` for the
  wrapped-horizontal case).
- The consumer sweep covered helphost/book, play (docs, snippets, detail,
  definition), mdedit, capdemo, capinspector, sqlapplet, docsections, the
  demo, and the codeview builders; corpus greps covered all 57 committed
  book/doc markdown files plus the ADR corpus.
