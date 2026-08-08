---
type: adr
status: proposed
date: 2026-08-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0178: mdedit — a markdown editor composed from the existing reading and editing seams

## Context

The repository holds a complete markdown *reading* stack and a complete text
*editing* seam, and nothing that joins them.

Reading: `public/semistructured/markdown/obsidian` parses the Obsidian dialect
through goldmark, and `imzero2/egui2/widgets/markdown` lowers the AST into a
segment tree of retained FFI blobs that `Doc.Render` splices per frame.
Editing: [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) put a
syntax-colouring layouter, styled overlays, a caret report and cursor-relative
insertion on the `textEdit` opcode, all of it span-source-agnostic. The three
existing consumers each use one half: `helphost` reads markdown and cannot
edit it, `writingstylescope` takes pasted markdown as measurement *input*, and
play's editor drives the ADR-0130 seam for SQL. There is no surface on which a
person writes markdown and sees what it renders to.

Four constraints shape what such a surface can be, and each of them cost
something to discover.

**The preview package is built for the opposite access pattern.** `Parse`'s
doc comment says to hoist the result to a package-level var and amortise it
across frames — retain-once, render-many. An editor is parse-often. Measured
on linux/amd64 (Ryzen AI MAX+ 395), `markdown.Parse` over synthetic documents
of headings, styled prose, lists, fenced Go and blockquotes:

| document | time | allocations | share of a 60 fps frame |
| --- | --- | --- | --- |
| ~350 B | 65 µs | 561 | 0.4 % |
| ~1.6 KB | 159 µs | 1 239 | 1 % |
| ~6.3 KB | 629 µs | 3 769 | 3.8 % |
| ~31 KB | 2.5 ms | 17 220 | 15 % |

Roughly linear at 10–12 MB/s. Reparsing on every keystroke is therefore
affordable at the sizes an editor actually sees, and the cost curve — not a
guess about it — is what licenses the simplest update policy below.

**The parse cannot leave the render goroutine.** `parseAndLower` calls
`c.Atoms()` and `c.CodeViewJob(...).Keep()` as it walks the AST, so the
document tree is built out of FFI opcodes. ADR-0130's L2 escape — move the
expensive pass to a `bgjob` worker, keep the render thread on the cheap tier —
is unavailable here. Whatever the update policy is, the work lands in the
frame; the only lever is how often, never where.

**The markdown highlighter is a viewer, by construction.**
`markdownhighlight.Highlight` recovers marker offsets by *re-emitting* a
canonical form rather than recovering them from goldmark's AST, which discards
them. Its spans therefore index that canonical text — lists normalised to `-`,
emphasis to `*`/`**`, frontmatter keys sorted — and its own package comment
says it is "for viewers, not for editor-source roundtripping". Fed to
`textEdit.highlightJob` it would colour the wrong bytes, and ADR-0130's
reconcile only shifts spans past a single edit region, not past a wholesale
re-emission.

The source tier M1 adds beside it (`HighlightLex`) is also the cheap one, by
the same margin the SQL seam found between its lex and parse tiers — measured
on the same machine, over synthetic documents of headings, styled prose,
lists, fenced Go and callouts:

| document | `HighlightLex` | `Highlight` (canonical) |
| --- | --- | --- |
| ~0.3 KB | 4.4 µs, 2 allocs | 65 µs, 484 allocs |
| ~2.6 KB | 30 µs, 8 allocs | 213 µs, 1 278 allocs |
| ~13 KB | 119 µs, 14 allocs | 928 µs, 4 774 allocs |

About eight times faster and two orders of magnitude fewer allocations — the
scanner's only allocation is its own span slice growing. At 13 KB it is 0.7 %
of a 60 fps frame, so the per-keystroke path is not where this app's cost
lives.

**The Powerbox brokers are narrower than a classical editor assumes.**
`clipboardbroker` serves `clipboard.write` and has no read subject.
`fsbroker` mints read-mode and write-mode handles separately, and
`handleWrite` refuses a read handle by design ([ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD7),
so a document opened for reading cannot be saved back through the handle that
opened it.

The shape below was settled in a design dialogue on 2026-08-07.

## Design space (QOC)

**Question.** How does the source pane obtain markdown syntax colouring, given
that the existing highlighter's spans index a canonicalised re-emission rather
than the buffer being edited?

**Options.**

- **O1** — plain monospace source in the first cut; add a source-offset lexer
  (`markdownhighlight.HighlightLex`, spans into the original bytes) as a
  follow-up milestone.
- **O2** — write the source-offset lexer first and ship the app with colours
  already in place.
- **O3** — keep the existing `Highlight` and add a "Format document" action
  that rewrites the buffer into the canonical form the spans describe.

**Criteria.**

- **C1** — colour correctness against the bytes the user is editing.
- **C2** — cost of the first cut.
- **C3** — buffer integrity: whether the mechanism rewrites the user's text.
- **C4** — reuse beyond this app.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | +  | ++ | −− |
| C2 | ++ | −  | +  |
| C3 | ++ | ++ | −− |
| C4 | +  | ++ | −  |

O3 fails the criterion that matters most: after one keystroke its colours sit
on bytes that have moved, and the only way to keep them honest is to rewrite
what the user typed. Silently-wrong colour is worse than none. O1 and O2 differ
only in ordering; O1 is chosen because it puts a working editor in front of a
reader sooner and keeps the lexer's cost from gating the surface it serves —
the descope-rather-than-gate rule in AGENTS.md.

## Decision

We will add `apps/mdedit`, a windowed keelson app that composes the existing
reading and editing seams into a markdown editing surface, adding no new
dependency, no IDL method and no Rust.

Layout is three egui panels inside the host-owned window: a `PanelTopInside`
action bar carrying the document's dirty state and the clipboard export, a
`PanelLeftInside` holding the source, and a `PanelCentralInside` holding the
live preview. The source pane is
`c.TextEdit(...).CodeEditor().ReportCursor()` read back through
`SendRespValCursor`; the preview is a `markdown.Doc` rendered inside its own
`c.IdScope`, which the package's caller-provided-IdScope invariant requires.

Six contracts fix the behaviour:

1. **The TextEdit owns the text.** Go keeps a mirror for parsing and for the
   caret's byte arithmetic, never an authoritative copy. This is ADR-0130's
   first contract and the reason paste, undo, selection and accessibility work
   without this app implementing any of them.
2. **The preview reparses when the buffer differs from what the current `Doc`
   was parsed from** — a text-keyed gate, the same shape as play's
   `sqlTextEditField` job cache. The measured table licenses this: at the
   sizes an editor sees the reparse is a low single-digit percentage of a
   frame, and it degrades gracefully rather than cliff-edging, since only
   frames in which the text *changed* pay anything at all.
3. **The caret drives the preview's scroll, at heading granularity.**
   `ReportCursor` returns the caret as a packed char range; Go converts it to
   a byte offset against its own frame copy, clamped, and resolves it to the
   last `HeadingInfo` whose `ByteOffset` does not exceed it. That slug is
   passed to `Doc.Render` via `WithScrollToSection` **only on the frame the
   slug changes** — re-passing it every frame keeps re-scrolling and prevents
   the reader from scrolling away, which is the guard the option's doc comment
   names and `helphost` already implements. Line-for-line sync is not
   available: the preview is a segment tree with no byte-to-y mapping, and
   nothing in the IDL reports one.
4. **The first cut has no file I/O.** Text enters by native egui paste into
   the focused TextEdit — no broker, no capability — and leaves through a
   `clipboard.write` request under a declared Cap. This is the input idiom
   `writingstylescope` already established for markdown, and it keeps the app
   off the fsbroker read/write handle asymmetry entirely until there is a
   reason to take it on.
5. **The buffer survives the window.** The document is persisted to the app's
   own keelson store under a `PersistedKeys` entry, which the host turns into
   the `runtime.persist.{ownAlias}.>` cap. Without it a no-file-I/O editor
   loses its content on close; with it the app is a durable scratchpad. This
   is the app's own store, not the filesystem — the Powerbox is untouched.
6. **Every dimension of the editing surface is derived from a measurement, and
   none of it is left to egui's retained layout state.** Both halves of this
   are load-bearing, and both were found by driving the live app rather than
   by reading the code.

   A `TextEdit` that states neither width nor rows gets
   `spacing().text_edit_width` — 280pt — and four rows, which have nothing to
   do with the pane it sits in, so it does not fill either axis. The editor
   therefore states both, from a `CapturePaneSize` probe of its own pane
   divided by a *measured* monospace row height (`MeasureTextSizeBind`); the
   probe runs before anything is placed, so what it reports does not move when
   the editor draws into it and the sizing cannot ratchet. `CaptureAvailableSize`
   is not usable here: it is a single process-wide slot that the frame's last
   capture wins.

   The source/preview split is likewise recomputed every frame — a share of a
   window probe, applied with `ExactSize` — rather than left to a resizable
   `SidePanel`. A SidePanel keeps its width in retained state and clamps that
   state *destructively* when the window shrinks, while `DefaultSize` only
   seeds the first frame. So a window that was once narrow leaves the source
   pane stuck at its clamped width for good: widen it again and the editor
   stays a ribbon a few characters wide beside a preview that took everything.
   Deriving the width instead means every window size maps to a share and
   nothing is retained to get stuck.

   Deriving a width from the window is only safe if that width cannot flow
   back into the window, and by default it does. egui sizes each scroll-area
   axis by `(direction_enabled, auto_shrink)`: `(true, false)` keeps the size
   it was offered, but `(false, false)` reports `max(available, content)` — so
   an axis that cannot scroll pushes its content width back out into the
   layout. Both panes therefore enable horizontal scrolling, not to scroll
   (the editor wraps) but to sever that path. Left un-severed it closes a
   loop: the editor's desired width joins the window's MINIMUM width, that
   minimum is a share of the window, and the floor rises every time the
   window does — so widening once permanently prevents narrowing back.
   Measured before the fix: a widen-then-narrow cycle moved the floor 439pt →
   582pt, and repeating it walked the window off the screen. After: 104.5pt →
   105pt → 105.5pt over three cycles, which is layout rounding.

"Dirty" is defined against the last checkpoint rather than against a file: the
document is clean when restored and immediately after a successful clipboard
export, and any edit sets it. With no file to be out of step with, this is the
only honest reading of the marker.

### Milestones

- **M0 — the editing surface: split panes, live preview, caret-follows-preview, dirty marker, clipboard export, session restore.**
- **M1 — source-offset markdown highlighting, wired through `textEdit.highlightJob`.** ✓
- **M2 — editing affordances: a formatting bar over `insertAtCursor`, an outline from `Doc.Headings()`, a word and reading-time readout.** ✓
- **M3 — find and replace: matches painted through `sectionStyled`, replace-all as a whole-buffer rebind.** ✓
- **M4 — file I/O through fsbroker.**

M3 shipped as planned, plus two things the plan did not name. Replace-one is
there beside replace-all — a find bar without it is a strange tool, and it is
the same rebind with a one-element span list. And the colour job now depends on
the match set, which is the milestone's one structural surprise: a styled
overlay is merged into the format of every colour section it OVERLAPS, by
design, so that the colour tier alone decides where sections begin
(`text_edit_highlight.rs`, `apply_styles`). Since the markdown lexer coalesces
runs of one category, a paragraph of prose is a single section — and a match
inside it tinted the paragraph. The repair asked nothing of the seam: the app
splits its own colour spans at every painted match boundary before building the
job, so each match is its own section. Verified live, per-pixel: the background
covers the match's bytes and stops there.

Three limits M3 accepts rather than solves:

- **No keyboard shortcut.** There is no general key channel across the seam —
  only the dedicated `F1` and Ctrl+Enter drains — so the bar is opened from a
  toggle and stepped with buttons. `lostFocus` cannot stand in for Enter: it
  cannot tell Enter from a click away, and advancing the match because someone
  clicked elsewhere is worse than not advancing at all.
- **The overlay is windowed at 400 matches**, centred on the current one, since
  each painted match costs a styled section AND a split in the colour job.
  Navigation and replace read the full list, so "replace all" means all of
  them, and the readout says when the painting is bounded.
- **A replacement leaves the widget's own edit path.** What Ctrl+Z does after
  one is egui's undoer's business — it snapshots the buffer on a timer, and an
  externally rewritten buffer is just another state to it. Not arranged here,
  and not relied on.

## Surfaces — Tier 1

M0 reshapes no named contract; it is additive at every point it touches. The
later milestones do reach shared surfaces, recorded here so the reach is
visible before the work starts rather than discovered inside it.

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.DefaultRegistry` | added — one `RegisterFactory` under a new manifest | the carousel's side-effect import block, which is the single site pulling registered apps |
| `markdownhighlight` exported API (M1, shipped) | added — `HighlightLex`, returning spans into the *source* bytes; `Highlight` untouched | `codeview.BuildMarkdownLex`, resolving the same categories against the existing `mdColors` palette |
| `codeview` exported API (M2, shipped) | added — `BuildMarkdownFromSpans`, the sibling of `BuildSqlFromSpans` | nothing; it lets a caller that already lexed (for the word count) colour the same text without lexing twice |
| `textEdit` IDL (shipped, unblocking M3) | added — `setCursor(sel, focus)`, the inbound half of the caret channel, taking the packed word `reportCursor` emits | `PackCursorRange` beside the existing `UnpackCursorRange`; regenerated dispatch on both sides. Recorded on [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) (2026-08-08), which owns the seam. It positions the caret but does NOT reveal it — an off-screen match can be selected, not scrolled to |
| `sectionStyled` (M3, shipped) | reached, not changed — first sub-token consumer of a channel whose existing users (play's statement tint, its clause background) all paint regions | nothing on the seam. The consequence is the caller's: overlay boundaries have to exist as COLOUR boundaries, so mdedit splits its own spans rather than asking the layouter to split its sections. A second sub-token consumer would want the same helper, at which point it belongs in `codeview` rather than in an app |
| `markdown` widget default feature set (shipped) | added — `obsidian.FeatureTag`, so `#tag` renders as a tag rather than as prose. Reaches every consumer of the widget, helphost above all | the `tagext.Node` case in `emitInline`, without which the feature DELETES tags (an unenumerated inline node hits the default branch and is dropped, and a tag carries its text in a field with no children); the same case in `flattenInlineText` and in `headingPlainText` — the last of those keeps heading slugs from shortening, which would have moved existing scroll targets and fragment links |
| `headingPlainText` / heading slugs (shipped) | fixed — wikilinks, embeds and autolinks in a heading now reach its text, where they were dropped. A heading like `## See [[Some Page]]` sluggified to `see-`, which nothing could link to | `fieldCarriedText`, one enumeration of the nodes that keep their text in a field, now shared with `flattenInlineText`. That sharing is the actual fix: the set had grown twice and only one walker learned each time, and the failure is silent — a missing node contributes nothing rather than erroring. Measured before changing it: **zero** headings anywhere in the working tree, gitignored trees included, contain such a node, so no existing slug moved |
| `obsidian` tag parser (shipped) | narrowed — a word-boundary rule, Obsidian's not-purely-numeric rule, and `{`/`#` rejected as openers. Strictly declines more than before; nothing that parsed as a tag stops doing so except what was never meant to be one | the competence doc, whose "intentionally permissive" claim was true of both extensions and is now true only of `==highlight==`. Measured before changing it: across committed markdown, 76 `#` in flowing prose, of which ~70 were `#4`-style numeric references |
| `markdownhighlight` categories (shipped) | added — `CategoryTagMarker`, `CategoryTagText`, appended after the existing values | `codeview.mdColors`, sized by `CategoryCount`. Appending is load-bearing: inserting among the existing values renumbers the ones after it and silently repaints documents in the wrong colours rather than failing to compile |

## Alternatives

- **A dock-tab layout instead of panels.** `egui_dock` is the repo's idiom for
  apps whose panes are *alternatives* — writingstylescope, play. Source and
  preview are simultaneous by definition, and tabs would hide exactly the half
  that makes the other legible.
- **Line-level formatting buttons (heading, list, blockquote) in M2.** They
  prefix a line; `insertAtCursor` inserts at the caret, so they would only be
  right with the caret already at the line start. Doing them properly means
  rewriting the buffer Go-side, which contradicts §Decision 1 and costs the
  widget's undo history — a worse trade than typing `## ` by hand. Deferred to
  a line-aware seam, not rejected. The inline actions have no such problem:
  `insertAtCursor` REPLACES the selection, so handing it the selection wrapped
  in markers turns "insert" into "wrap" with nothing new required.
- **Regular expressions in the find bar.** Matching is literal. A regex find
  wants its own error reporting, its own capture-group syntax in the
  replacement, and a cost model for a pattern that matches everywhere — all of
  which is a feature beside the one M3 is, rather than a switch on it. Trigger:
  someone reaching for it on a real document.
- **Enter to advance to the next match.** Deferred for want of a channel, not
  by preference: there is no general keyboard drain across the seam, and the
  one signal a TextEdit does report — `lostFocus` — cannot distinguish Enter
  from a click somewhere else, so binding to it would advance the match
  whenever the reader clicked away. Trigger: a general key channel, which is
  the same thing a Ctrl+F toggle needs.
- **Preview reparse gated on quiescence.** Deferred rather than rejected: the
  measured curve says it buys nothing below a few tens of KB, and it costs a
  state machine plus a stale-preview window. Trigger to revisit: a document
  large enough for the reparse to show up as typing latency.
- **Moving the parse to a `bgjob` worker,** as ADR-0130 L2 did for the SQL
  semantic tier. Not available: `Parse` builds retained FFI holders, so it is
  render-goroutine-only. Recorded so it is not re-proposed.
- **Extending `clipboardbroker` with a read subject** to drive a "load from
  clipboard" button. Deferred: native paste already gets text in, and adding a
  read side to a shared Powerbox broker is a capability-model decision that
  should not ride in on an app. Trigger: a flow that must replace the whole
  buffer without the user's own paste gesture.
- **Adding a read-write handle mode to fsbroker** so one dialog serves open
  and save. Same reasoning, and out of M0's scope entirely; M4 will instead
  accept portal-style behaviour, where the first save raises a write dialog
  pre-filled through the existing `SuggestedName` hint and every later save
  reuses the granted handle.
- **A WYSIWYG or hybrid-preview editor.** The preview's segment tree has no
  inverse mapping from rendered geometry back to source bytes, and building one
  means either a second lowering that tracks provenance or a per-block editing
  model. Both are larger than the whole of M0.

## Consequences

### Positive

- A markdown editing surface for the cost of composition: no new dependency,
  no IDL method, no Rust, no generated code.
- The reading and editing seams gain their first shared consumer, which is
  where a mismatch between them would surface.
- M1's lexer is useful wherever markdown source is displayed for editing, not
  only here, and it fills the one gap the highlighter's viewer-only contract
  leaves open.

### Negative

- The preview's cost is paid on the render goroutine and cannot be moved off
  it. A large enough document turns typing latency into a visible property of
  the app, with only the deferred quiescence gate as a remedy.
- Every keystroke that changes the buffer interns a fresh set of retained
  blobs. They become collectable once the superseded `Doc` is dropped, but the
  package was not written against a churn-heavy caller and this app is the
  first one.
- The source tier is a second reading of markdown syntax, beside goldmark's.
  It is deliberately the shallower one — it recognises surface markers and
  claims spans, never builds a tree — but it is still a place where the editor
  and the preview can disagree about what a document says, and only the
  preview is authoritative. The gap is bounded by what M1 declines to guess:
  indented code blocks, reference links, setext headings and multi-line
  emphasis all read as ordinary prose in the source pane while the preview
  renders them.

  `#tag` was on that list and came off it, which is where the cost of the
  second reading showed itself: the rule had to be written twice, once in the
  goldmark parser and once in the scanner, and a rule added to one and not the
  other is exactly how the two panes come to disagree. A test asserts the two
  tiers reach the same verdict on the same input rather than each being
  checked alone. Nothing structural stops the next divergence — only that
  test.
- Without file I/O the app cannot open what is already on disk, and the
  clipboard round-trip is the only way in or out besides its own persisted
  session.
- **The split is not draggable.** Pinning the source pane with `ExactSize` is
  what makes it recover from a resize, and it costs the handle: the reader
  cannot widen the source at the preview's expense. A draggable split that
  also recovers needs its own retained fraction plus a splitter widget —
  bigger than the defect it would serve, and deferred until someone wants the
  handle back rather than merely the panes at sane sizes.
- The editor's width, its row count and the split all read a probe from the
  previous frame, so a continuous window drag has them trailing by one frame.
  Self-correcting and settled by the time the drag stops, but it is the cost
  of Go not being able to ask egui for a size inline.
- **Going to a find match selects it without revealing it.** `setCursor`
  positions the caret and cannot scroll to it — egui scrolls to a selection
  only when the widget changed it itself, and no byte-range-to-rect channel
  exists (ADR-0130, 2026-08-08). A match below the fold is selected where the
  reader cannot see it. The preview scrolling to the match's section is the
  partial compensation, and it is the only one available from this side.
- **The current match's background is mostly hidden while the editor has
  focus**, because egui paints its selection fill over the galley's own
  section background — and navigating to a match both selects it and takes
  focus. Observed live: only the underline and a pixel of the fill at the row
  edges survive. It matters where the selection is absent, which is every
  frame after a replacement (which does not take focus) and any time the
  reader is working in the bar rather than the buffer.

### Neutral

- Scroll sync is heading-granular by construction. A document without headings
  gets no sync at all, which is the correct degenerate behaviour rather than a
  gap to fill.
- The app declares exactly one capability in M0 (`clipboard.write`), plus the
  persist cap the host injects from `PersistedKeys`.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the pure-Go logic — the char-to-byte caret
  conversion and its clamping, caret-offset-to-heading resolution including
  the line-start normalisation, the slug-changed scroll guard, the checkpoint
  transitions, and the manifest (it validates, it registers, and it declares
  exactly one capability). Plus an ADR-0057 registry `Demo` in
  `mdedit_tour.go`, which the widgets TestDriver captures as a PNG and an SVG
  on every tour run.

  M3 adds to the same lane, and it is reachable there because every button
  calls a helper rather than editing state inline: the scan and its case fold,
  the index bookkeeping (wrapping, and the resume offset that stops a
  replacement from landing inside what it just wrote), the splice, the paint
  window, and the span split — whose assertion is that cutting changes where
  sections begin and never what colour they are or which bytes they cover. The
  tour scene opens the bar on a query matching twice, so a capture carries both
  match tones and a fixture test fails if that query ever stops matching.
- **What would fail.** A caret-to-heading regression turns the scroll guard
  into either a preview that never follows or one the reader cannot scroll by
  hand; both are assertable from the resolved slug alone, without rendering.
  A capability creeping into the manifest — an `fs.*` pattern above all —
  fails the manifest test, which is the tripwire on file I/O arriving without
  the decision that should precede it. A layout or lowering regression shows
  up as a changed tour capture.
- **Gap.** Three, all accepted for now. The measured parse costs are not
  regression-tested — no benchmark gate guards the table above, so a slowdown
  inside goldmark or the lowering would pass unnoticed; the numbers inform a
  policy choice, and that policy degrades gracefully rather than breaking if
  they drift. The tour scene is a static document, so it exercises the
  lowering and the layout but never the *editing*: typing, the reparse gate
  and the caret-driven scroll have no end-to-end coverage, only unit coverage
  of the logic beneath them — closing that needs a carrier-driven run
  ([ADR-0154](./0154-headless-carrier-tree-and-driver.md)), which is not
  built. And the sizing contract has no automated net at all: every defect it
  records — the unstated TextEdit dimensions, the SidePanel's destructive
  clamp, the scroll-axis push-back — is an egui layout behaviour that only
  appears in a live window under resize, and each was found by driving one.
  A regression would be caught the same way, by hand, which is the weakest
  part of this plan.

  M3 sits on the same fault line and was checked the same way, by driving a
  live window under `EGUI_INSPECTION` against a 60-match document: the overlay
  covers a match's bytes and stops there rather than tinting the prose run
  around it (read per-pixel, not by eye); stepping moves the warm tone;
  `setCursor` selects the match it names; folding finds 60 with the case
  toggle off and none with it on; and a replace-all round trip — every match
  rewritten and rewritten back — left the buffer byte-identical, which is what
  says the rebind and the databinding override hold rather than being quietly
  reverted at the next Sync. None of that is automated, and a regression in
  any of it would again be found by hand.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) — the TextEdit
  highlight seam this app edits through, and the reconcile contract that makes
  a canonicalising span source unusable here.
- [ADR-0063](./0063-imzero2-textedit-insert-at-cursor.md) — `insertAtCursor`,
  which M2's formatting bar drives.
- [ADR-0125](./0125-codeview-prepare-memo.md) — the `Prepare*` memo; the
  editor path deliberately stays off it, for the reason ADR-0130 §6 records.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD7 — the
  Powerbox grant model behind the clipboard and fsbroker constraints.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the headless tour lane.
- `imzero2/egui2/widgets/markdown/EXPLANATION.md` — the segment tree, the
  retained-holder lifetime rule, and the caller-provided-IdScope invariant.
