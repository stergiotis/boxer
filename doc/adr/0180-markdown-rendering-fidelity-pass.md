---
type: adr
status: proposed
date: 2026-08-09
---

# ADR-0180: markdown rendering fidelity pass

## Context

Markdown is becoming a primary human-machine interface for this repository —
help books, play docs, sqlapplet books, capability docs, mdedit. A full review
of the rendering stack
([background work](../adr-background-work/markdown-rendering-review.md),
2026-08-09) found the architecture sound against the immediate-mode and
minimalism premises, and a cluster of fidelity defects: silent content drops
(GFM task checkboxes foremost), image upscaling, flat typographic rhythm,
tables that cannot flow with the document, and four places where comments
teach a wrong cost model. The fix shapes below were settled in a design
dialogue on 2026-08-09; the review doc's §6 carries the full candidate list,
including what was deliberately deferred.

Two constraints shape the batch. Everything must stay premise-clean: no new
dependency, no retained layout state, the `markdown` package stays stateless.
And wire changes are batched into one IDL-regen + Rust rebuild round, because
the first such change pays the round and the rest ride it.

## Design space

The contested forks, with kill reasons for the losers:

- **Image sizing.** A Rust-side `FIT_ASPECT_DOWN` mode is the cleaner
  cross-consumer answer but costs a wire change for a policy exactly one
  caller needs today — rejected until a second consumer wants fit-down.
  Clamping only when both axes are nonzero would preserve the documented
  zero-axis fill-available semantics, and with them the known
  collapses-to-0px trap in vertical scroll hosts — rejected as protecting a
  semantic nobody wants in a document reader.
- **Table height.** Always-flow (`vscroll(false)` unconditionally) is the
  purest document semantic but lets one pathological generated table lay out
  hundreds of rows per frame; keep-scroll-but-bounded never lets tables flow
  at all. The threshold policy takes flow for the common case and bounds the
  pathological one.
- **Inline gaps.** Trimming the spaces adjacent to link runs halves the
  word-gap but cannot fix detached trailing punctuation (`link .`) — there is
  no space to trim there; the gap is pure `item_spacing.x`. Rejected in
  favour of a scoped spacing op, whose wire cost is already paid by the table
  fix in the same batch.
- **Task checkboxes as real `Checkbox` widgets.** There is no
  render-geometry→source mapping (ADR-0178 rejected WYSIWYG), so a click has
  nowhere to write. Glyphs, not controls.
- **Rhythm.** A gradient proportional to heading size (0.5 × font size) was
  passed over for two token tiers — fewer magic numbers, and the tokens
  already carry density.

## Decision

Twelve items in three milestones.

**M0 — pure Go, no wire change:**

1. **Task-list checkboxes render.** `emitInline` gains the
   `east.TaskCheckBox` case: a Phosphor glyph atom (`icons.PhSquare` /
   `icons.PhCheckSquare`) ahead of the item text. Non-interactive by
   construction. Restyling checked text (Obsidian dims and strikes it) is
   deferred — it needs the checkbox to inject style into sibling nodes.
2. **The five doc drifts are corrected**: `WithFeatures`' footnote/task-list
   claim; the image wire-contract comment in `egui2_definition_d_image.go`
   (a non-empty buffer does **not** re-upload — Rust caches on
   `(id, version, w, h)`; the cost is wire memcpy) plus the two comments in
   the markdown widget citing it; EXPLANATION.md's pre-LayoutJob wrapping
   trade-off note; `play_detail_rich`'s "tables render as nothing" comment;
   the obsidian skill doc's `FeatureMath`/`FeatureAll` rows.
3. **Headings get vertical rhythm and density.** An `AddSpace` before each
   heading segment — `PaddingOuter(d)` for H1/H2, `PaddingDefault(d)` for
   H3–H6, skipped when the heading is the document's first segment — and
   `headingFontSize` wraps its tuned table in `ScaledPt(…, d)` so headings
   move with density like table rows already do. `AddSpace` consumes no
   widget id, so the id-derivation order is unchanged and no consumer
   scope-key bump is needed (the EXPLANATION invariant that tables tripped).
4. **Ordered-list markers align.** Markers render monospace, left-padded to
   the list's widest marker (computed at lowering time, cached on the
   segment). Exact alignment at every digit width; bullets unchanged.
5. **Images never upscale.** `renderImageRun` passes a bounding box of
   `min(cap, native)` per axis, and a zero axis in `WithImageMaxSize` now
   means "native size, no cap on that axis" instead of "fill available".
   This is a semantics change on an exported option, recorded here; it also
   removes the fill-available collapse trap for markdown consumers, all of
   whom render inside vertical scroll hosts.
6. **Obsidian embed size syntax stops breaking images.** The embed parser
   splits a `|suffix` off the target before resolution, so
   `![[img.png|300]]` is an image again; the size value itself is ignored
   (`// deferred:` — honouring it wants per-run caps). The wikilink
   `DisplayText` gains the empty-page branch, so `[[#heading]]` no longer
   renders a leading "> ".
7. **`FeatureMath` leaves `FeatureAll`.** The bit stays reserved with a
   comment; `FeatureAll` becomes all wired features. The flag is dead code
   today — enabling it changes nothing — so no behaviour moves; the exported
   constant's value does.
8. **The `Parse` thread contract is stated.** Package docs record that
   `Parse` is safe off the render goroutine and why (pooled retained
   builders, `unique` interning, the codeview memo's mutex — all
   deliberate); `docsections` and `sqlapplet_store` already rely on it.
   ADR-0178's "the parse cannot leave the render goroutine" gets a dated
   Update correcting the claim to what it meant: *rendering* is
   render-goroutine-only.

**M1 — the one wire round (IDL regen + rebuild both sides):**

9. **`table`'s `vscroll(false)` becomes real** — the apply gains its else
   arm (`builder.vscroll(false)`). Zero existing callers pass `false`, so
   nothing moves. `renderTable` then applies the threshold policy: documents
   flow their tables (`Vscroll(false)`) up to `tableFlowMaxRows` (100) body
   rows; above that the table keeps its internal scroll bounded by
   `MaxScrollHeight` = 24 × `tableRowHeight(hasHeader)`, so a pathological
   table costs a bounded viewport instead of the whole frame.
10. **A scoped item-spacing op.** `uiSetItemSpacing(sx, sy)` sets
    `spacing.item_spacing` on the current Ui (children inherit, siblings do
    not). `renderRuns` emits it inside the `HorizontalWrapped` row as
    `(0, GapItems(d))`: word gaps around links and images are then carried
    by the text's actual space characters, and trailing punctuation after a
    link sits flush. The op is generally useful (control rows, badge
    clusters) and carries no id.

**M2 — the nets:**

11. **The drop path becomes observable.** The lowering counts every AST node
    kind it skips; `Doc.Dropped()` reports per-kind counts. The book corpus
    tests assert zero drops across all committed books, which converts the
    silent-loss failure mode — the review's structural finding #1, and the
    mechanism behind the historical tag deletion — into a lintable property.
    A visible ⍰ marker option is deferred (trigger: an authoring surface
    such as mdedit's preview wants it).
12. **The test nets widen**: the two-tier parity test grows from
    "#tag exists" to per-category byte-range agreement over a small corpus,
    with an explicit allowlist for the lex tier's declared non-goals
    (indented code, reference links, setext, multi-line emphasis); and a
    `-race` test parses concurrently to pin item 8's contract.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `obsidian.FeatureAll` (exported const) | value shrinks by the dead `FeatureMath` bit | nothing in-repo (no non-test consumer); external consumers of the public module see a const value change |
| `markdown.WithImageMaxSize` semantics | zero axis: fill-available → native-no-cap; both axes now floored at native | the demo's small assets render native instead of stretched; option doc rewritten |
| `table` op apply (IDL + interpreter) | `vscroll(false)` honoured | nothing — zero callers pass false today; `renderTable` becomes the first |
| egui2 IDL | added — `uiSetItemSpacing(sx, sy)` | regenerated dispatch both sides; first consumer is `renderRuns` |
| `markdown.Doc` API | added — `Dropped()` per-kind skip counts | book corpus tests gain a zero-drops gate |
| every tour capture containing markdown | rhythm, marker, image-size changes shift pixels once | captures refreshed; A/B against a same-binary control per the established noise-floor recipe |
| ADR-0178 | dated Update on the parse-thread claim | none |

## Consequences

### Positive

- Reading fidelity: task state, embed-sized images, aligned lists, headings
  with air, tables that flow, links whose punctuation stays attached.
- The worst failure mode (silent content loss) becomes a tested property
  instead of a hope.
- Two wrong cost models stop being taught by comments.

### Negative

- `FeatureAll`'s value change is breaking for any external consumer that
  persisted the number rather than the expression.
- Every markdown-bearing capture shifts once; the tour A/B burden is real
  and the noise floor makes it a classify-by-bbox exercise, not a `cmp`.
- Monospace numerals sit beside proportional body text in ordered lists — a
  deliberate typographic trade for exact alignment.
- `tableFlowMaxRows` and the 24-row scroll bound are heuristics; a future
  measurement seam could replace them, a knob should not.
- The render path itself remains untested (deferred with ADR-0154); the M0/M1
  visual changes are verified by the live-drive recipe once more, by hand.

### Neutral

- Checked-text restyling, per-image size hints, styled table cells, the ⍰
  marker, slug dedup and the image cost model for screenshot-heavy books
  stay deferred with named triggers (review doc §6, items 13–16).

## Verification plan — Tier 1

- **Lane.** Default `go test`: a parse test per lowering change (checkbox
  runs, marker padding, embed pipe-strip, `DisplayText`, `FeatureAll`
  arithmetic, threshold policy as a pure function); the corpus zero-drops
  gate; the widened parity test; the `-race` concurrent-parse test.
- **Live.** The review's probe document driven through the gallery's Load
  section (headless compositor + egui-mcp): checkbox glyphs present, image
  at native size, heading gaps two-tier, marker columns flush, link
  punctuation attached — geometry read from the accessibility tree, not
  pixels. Tour A/B run against a same-binary control for the capture shift.
- **What would fail.** A future parser feature whose node the lowering does
  not know now fails the corpus gate instead of deleting text silently. A
  regression in the table else-arm shows as the policy test's `Vscroll`
  expectation flipping. `FeatureAll` regaining a dead bit fails its
  arithmetic test.
- **Gap.** Render-path emission is still exercised only live; that lane
  (op-capture or ADR-0154 carrier) remains the standing weakness, deferred
  on its own trigger.

## Status

Proposed 2026-08-09, from the design dialogue over the
[rendering review](../adr-background-work/markdown-rendering-review.md) §6.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way).

### Milestones

- **M0 — lowering + docs: checkboxes, drifts, rhythm, markers, image clamp, embed pipe, FeatureAll, thread contract.**
- **M1 — the wire round: table else-arm + threshold policy, uiSetItemSpacing + renderRuns.**
- **M2 — the nets: Doc.Dropped + corpus gates, parity broadening, race test.**

## References

- [Markdown rendering review](../adr-background-work/markdown-rendering-review.md) — the background work this decides on.
- [ADR-0178](./0178-mdedit-markdown-editor.md) — mdedit; owns the parse-thread claim this batch corrects.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — the tour lane whose captures shift.
- `imzero2/egui2/widgets/markdown/EXPLANATION.md` — the id-derivation invariant item 3 preserves.
