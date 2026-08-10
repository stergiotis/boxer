---
type: adr
status: accepted
date: 2026-08-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-09
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

## Alternatives

- **Stage the twelve items as separate, smaller ADRs/PRs** instead of one
  batch — the review doc's own size tiers (small / medium / larger, §6)
  would support it. Rejected: the design dialogue settled all twelve the
  same day the review landed, and the wire round (IDL regen + Rust rebuild)
  is a fixed cost either of its two M1 items pays alone — splitting them
  across future ADRs pays that cost twice for changes that are already
  zero-existing-caller and low-risk on their own.
- **Ship only the small tier (review §6 items 1–7) now, defer the medium
  tier (8–12) to a later pass.** A real fork the review's own tiering
  invites. Rejected: item 8 there — `Doc.Dropped()` and the corpus
  zero-drops gate, Decision item 11 — is the direct fix for the review's
  structural finding #1, the silent-drop failure mode behind the historical
  tag-deletion incident. Deferring it behind the cosmetic items (rhythm,
  markers) would ship polish ahead of the regression net for the worse bug
  class.
- **Fold the four larger items (review §6 items 13–16) into this batch
  too.** Rejected per the review's own framing: each needs its own dialogue
  independent of this one — render-path smoke coverage has no harness yet,
  styled table cells want a new op, the image cost model is speculative
  until screenshot-heavy books exist, and slug dedup wants corpus lint
  landed first so it doesn't silently move existing anchors. Recorded with
  named triggers (Consequences — Neutral) rather than folded in.

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

- **Lane — done, green.** Default `go test`: a parse test per lowering change
  (checkbox emission, marker padding, embed pipe-strip, `DisplayText`,
  `FeatureAll` arithmetic, image fit-axis, heading gap tiers, threshold policy
  as a pure function); the corpus zero-drops gates; the widened parity test;
  the `-race` concurrent-parse test. The regenerated Rust builds and is
  rustfmt-clean; `go mod tidy --diff` reports no drift.

  One shape worth recording, because it constrains every future test here: a
  finished paragraph run is an opaque retained FFFI blob with no way to read
  its text back, so "the checkbox glyph reaches the document" is asserted
  against the inline builder's pending buffer, and the regression net for the
  whole class is the zero-drops gate rather than a text comparison.
- **Live — done.** The probe document driven through the gallery's Load
  section, under a private headless weston (`--idle-time=0`) over egui-mcp,
  with geometry read from the accessibility tree. Measured at standard
  density:

  | Claim | Measurement |
  | --- | --- |
  | Heading rhythm is two-tier | gap above H1/H2 = **20 px**, above H3–H6 = **16 px**, against an **8 px** paragraph-to-paragraph baseline — i.e. the token step (12 / 8) on top of `item_spacing.y`. Every one of those gaps was 8 px before. |
  | Marker columns flush | ordered items 8, 9, 10, 11, 12 all start at **x = 101.3125**. The review measured 84.3 vs 91.75 across the same boundary; the step is now 0. |
  | Link punctuation attached | the three runs of one paragraph abut exactly — text ends at 161.875, link starts at 162.0, link ends at 232.8, trailing run starts at 232.0. The style gap is **zero**; the word gap is the author's own space. |
  | Images at native size | inline image width read as the x-gap between its neighbouring runs: **127.5 px** for the CommonMark path and **127.3 px** for the embed path, against a native 128 and a configured cap of 200×140. Row height 80 = native. Before, `FitAspectMax` scaled 128×80 up by 1.5625 to fill the cap. |
  | Tables flow, and stop flowing at the threshold | a 40-row table lays out **1385.6 px** inline (41 × 33.6) with prose directly beneath it; a 150-row table is bounded to **628 px** instead of 5040. Both arms of the policy, and proof the `vscroll(false)` else-arm is real. |
  | Checkbox glyphs present | in one list, task items carry a leading Phosphor glyph and the plain bullet beside them carries none; checked and unchecked render as visibly different squares. |

  Images carry no AccessKit node (the op allocates and paints rather than
  going through `apply_widget`), so their size is derived from the bounds of
  the text runs on either side rather than read directly — the only claim
  here not taken from a node of its own.

- **Tour A/B — done, and it needed the clean-worktree form.** Run first from
  the working tree, the A/B reported 77 of 81 captures shifted, which is not a
  finding but an artefact: that tree also carried two other sessions'
  unfinished work. Re-run between two throwaway worktrees at this batch's tip
  and its parent, with a same-binary control for the noise floor, 90 captures
  each:

  - **4 captures are nondeterministic run to run** (`fibscope-exhaust`,
    `sccmap-bytes-tests`, `sccmap-treemap`, `splashscreen-about`) — the noise
    floor, and the reason this is a classify-by-bbox exercise rather than a
    `cmp`.
  - **2 captures changed for real, both markdown-bearing**: `markdown`
    (14.4 % of pixels — images at native size, heading rhythm, markers) and
    `mdedit-split` (4.5 %, confined to the preview pane — the heading rhythm
    reaching a second consumer).
  - **18 captures differ only inside one 135×17 box**, identical in all of
    them: the window title, shifted a uniform −8 px. Every differing pixel
    lies inside that box. The cause is the gallery window being 16 px
    narrower from the markdown demo onward, because its images no longer
    claim 200 px of width — so the whole downstream effect traces to the
    image clamp and touches no demo's own content.
  - **64 captures are byte-identical.**
- **What would fail.** A future parser feature whose node the lowering does
  not know now fails the corpus gate instead of deleting text silently. A
  regression in the table else-arm shows as the policy test's `Vscroll`
  expectation flipping. `FeatureAll` regaining a dead bit fails its
  arithmetic test.
- **Gap.** Render-path emission is still exercised only live; that lane
  (op-capture or ADR-0154 carrier) remains the standing weakness, deferred
  on its own trigger. What the live pass above bought is one dated
  measurement, not a gate — nothing re-runs it.

  A second gap the live pass surfaced: an A/B on a shared working tree
  measures whatever else is in that tree. The clean-worktree pair is the only
  form of this check worth reporting, and it should be the documented one.

## Status

Accepted 2026-08-09, from the design dialogue over the
[rendering review](../adr-background-work/markdown-rendering-review.md) §6,
with M0–M2 shipped and both verification lanes closed the same day. Changes
now arrive as dated `## Update` sections rather than in-place edits.

The code landed ahead of acceptance, which is worth recording rather than
smoothing over: the batch is a correctness pass over an existing surface
rather than a new subsystem, so the diff was the cheapest way to read what the
decision actually meant, and the live measurements below could not be taken
until it existed.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way).

### Milestones

- **M0 — lowering + docs: checkboxes, drifts, rhythm, markers, image clamp, embed pipe, FeatureAll, thread contract.** ✓
  All eight items as decided. Two things worth recording beyond the decision
  text. The heading ladder turned out to sit BELOW body text at H6 (12.5 pt
  against the IDS 13 pt step): it was tuned against egui's default 12 pt body,
  and `ScaledPt` moves it without correcting it. Pinned by a test rather than
  retuned — retuning is a typographic decision of its own and moves every
  capture a second time. And the ordered-list marker is padded and rendered
  monospace, which is a `LabelAtoms` where it was a `Label`; bullets stay a
  plain `Label`, per the decision.
- **M1 — the wire round: table else-arm + threshold policy, uiSetItemSpacing + renderRuns.** ✓
  One `egui2gen generate` round, Rust rebuilt and rustfmt-clean. The
  threshold policy is factored as a pure `tableFlows(rows, hasHeader)` so the
  decision is testable without a live FFFI sink — the render path around it
  still is not.
- **M2 — the nets: Doc.Dropped + corpus gates, parity broadening, race test.** ✓
  `Doc.Dropped()` counts every skipped node by AST kind; comments are
  deliberately not counted, since an author who wrote `%%…%%` got what they
  asked for. The zero-drops gate went in per corpus-owning package rather than
  as one central test — play, capinspector, helphost, logviewer, and all ten
  sqlapplet applet books — because `help.BookI` already exposes everything the
  gate needs and adding a method to that interface was a surface change the
  decision did not license.

  The parity test compares merged per-category byte ranges over 32 cases, with
  a per-case allowlist that must stay live: a category listed as differing
  which has since come into agreement fails, so the list cannot rot. Two
  constraints shaped it. `Highlight`'s spans index the canonical form it
  re-emits, so a case is only comparable when that form is byte-identical to
  its source — 22 of 32 cases agree exactly, and the ten that do not are
  documented divergences (the `***x***` delimiter split, the space after `>`,
  callout titles, wikilink aliases, HTML blocks, frontmatter values). Nine
  categories cannot be compared at all, because every input carrying them is
  rewritten by canonicalisation; they are named with reasons and asserted as
  an exact set. The lex tier's four declared non-goals get their own test:
  they must stay plain.

## References

- [Markdown rendering review](../adr-background-work/markdown-rendering-review.md) — the background work this decides on.
- [ADR-0178](./0178-mdedit-markdown-editor.md) — mdedit; owns the parse-thread claim this batch corrects.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — the tour lane whose captures shift.
- `imzero2/egui2/widgets/markdown/EXPLANATION.md` — the id-derivation invariant item 3 preserves.
