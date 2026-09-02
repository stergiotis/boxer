---
type: adr
status: accepted
date: 2026-08-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-08
---

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
4. **The first cut has no file I/O** (M0–M3; M4 adds it, below). Text entered
   by native egui paste into the focused TextEdit — no broker, no capability —
   and left through a `clipboard.write` request under a declared Cap. This is
   the input idiom `writingstylescope` already established for markdown, and it
   kept the app off the fsbroker read/write handle asymmetry entirely until
   there was a reason to take it on.
5. **The buffer survives the window.** The document is persisted to the app's
   own keelson store under a `PersistedKeys` entry, which the host turns into
   the `runtime.persist.{ownAlias}.>` cap. Without it a no-file-I/O editor
   loses its content on close; with it the app is a durable scratchpad. This
   is the app's own store, not the filesystem — the Powerbox is untouched.
   M4's file handles do NOT replace it: a handle dies with the bus client, so
   the store remains the thing that survives a close, and the file is where the
   reader deliberately put a copy.
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
export, and any edit sets it. Through M3, with no file to be out of step with,
that was the only honest reading of the marker. M4 adds two more checkpoints —
a successful save and a successful open — without changing the rule: the
checkpoint is the last time the buffer reached somewhere the reader put it, and
what it records is the snapshot that actually landed, not the buffer as it
stands when the acknowledgement arrives.

Seven, added by M4:

7. **The app is never told which file it has.** `DialogReply` carries a handle
   subject prefix; the path stays inside the broker ([ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD7).
   So the editor can say that a document is bound to a file and not which one
   — no title, no directory, no way to tell two documents apart. The name it
   puts in the save dialog is a suggestion derived from the first heading, and
   it is deliberately not remembered afterwards, because the user may have
   typed something else and echoing it back would present a guess as a fact.
8. **Saving asks once.** A read handle can never write (§SD7 again), so opening
   a document does not give the app anywhere to save it: the first Save raises
   a dialog and every later Save reuses the granted handle in silence. This is
   the portal-style behaviour §Alternatives chose over asking fsbroker for a
   read-write mode, and the reuse works because a handle lives until it is
   closed rather than being spent on one write.
9. **The wildcard handle cap is declined.** A granted handle is addressed under
   `fs.handle.{uuid}.>`, and the obvious move — which a sibling app makes — is
   to declare `fs.handle.>` statically. It is not needed: the broker adds the
   narrow per-handle cap to the app's live client at the moment the USER
   approves, and revokes it on close. Declaring the wildcard would convert a
   user-approved, per-file, revocable grant into standing authority over every
   handle the broker ever mints. The end-to-end tests run on the manifest's own
   caps, which is what makes this a demonstrated claim rather than a hope.

### Milestones

- **M0 — the editing surface: split panes, live preview, caret-follows-preview, dirty marker, clipboard export, session restore.** ✓
- **M1 — source-offset markdown highlighting, wired through `textEdit.highlightJob`.** ✓
- **M2 — editing affordances: a formatting bar over `insertAtCursor`, an outline from `Doc.Headings()`, a word and reading-time readout.** ✓
- **M3 — find and replace: matches painted through `sectionStyled`, replace-all as a whole-buffer rebind.** ✓
- **M4 — file I/O through fsbroker.** ✓ Open, Save and Save as over the two
  dialog subjects, portal-style. Two things the plan did not anticipate, both
  from the broker rather than from the app. The handle cap turned out to be
  granted dynamically on approval, so the manifest declares the two dialog
  subjects and nothing else — narrower than the sibling app this flow was
  modelled on. And the read op answers with the file's raw bytes on success but
  a CBOR refusal on failure, sharing one channel with no framing, so something
  had to tell them apart; the discriminator is a decode attempt, sound because
  the encoded refusal begins with a UTF-8 continuation byte that no valid text
  can start with. A test pins that leading byte, since the argument is the only
  thing holding the two apart.

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
| `app.BusI` (M4, shipped) | added — `RequestWithTimeout`, because a dialog request is bounded by a person and the transport default failed it while the picker was still open. Additive; no existing caller changes | the three implementations (`inprocbus`, `natsbus`, `NoopBus`) and five test fakes. Recorded on [ADR-0026](./0026-app-runtime-and-capability-subjects.md) (2026-08-08), which owns the seam |
| `fsbroker` exported API (shipped) | added — `DialogTimeout` / `HandleOpTimeout`, the waits a client should pass. Beside the subjects because which request waits on a person is a property of the subject, not of the caller | mdedit's local constants, which now alias them, and every other consumer in the repo — `sqlappletcreator`'s export, `capdemo`'s read and watch pickers, play's Load — all of which had the same defect and are fixed with them |

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
  and save. Same reasoning, and out of M0's scope entirely. M4 took the
  portal-style behaviour instead: the first save raises a write dialog
  pre-filled through the existing `SuggestedName` hint, and every later save
  reuses the granted handle. That rests on the handle outliving a single
  write, which it does — `handleWrite` does not close, so reuse needed nothing
  from the broker.
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
- **The editor cannot name the file it is bound to** (M4). The broker hands
  over a handle and keeps the path, so the bar can show that a document has a
  file and never which one, and two windows editing two files look identical.
  Changing that means widening `DialogReply` with a display name, which is a
  Powerbox decision and not one an app should make on its way past. Until then
  the honest surface is a badge that says "file bound" and nothing more.
- **A save is a whole-file truncate.** `handleWrite` is `os.WriteFile`
  create-or-truncate, and the payload is the entire buffer, so there is no
  partial write and no incremental save. Fine at the sizes an editor sees, and
  the same assumption the reparse policy already makes.
- **A file binding does not survive the window.** Handles die with the bus
  client, so reopening a persisted document and pressing Save asks where to put
  it again — even though the reader saved it to a file minutes earlier. The
  persisted buffer is not lost, only the knowledge of where it came from, and
  recovering it would mean persisting a path the app is never given.
- **An open replaces the buffer with no undo.** It is a whole-buffer rebind
  outside the widget's edit path, and the autosave carries the new document
  over the persisted one within seconds, so there is nothing to go back to.
  Guarded rather than merely documented: on a modified document the first Open
  refuses and says so, and a second click proceeds. Two clicks rather than a
  confirmation dialog because the only dialog facility here is the picker
  itself, and rather than a flat refusal because a scratch buffer should not
  have to be saved somewhere before it can be discarded. The arming is disarmed
  by the reader typing, so it cannot latch and turn a later, unrelated Open
  into the silent discard it exists to prevent.
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
- The app declared exactly one capability through M3 (`clipboard.write`) and
  three from M4 (adding `fs.dialog.read` and `fs.dialog.write`), plus the
  persist cap the host injects from `PersistedKeys` and the per-handle cap the
  broker grants and revokes around each approved dialog. Every one of them
  authorises ASKING rather than reaching: none names a path, and the three
  static ones cannot reach a file without a user approving a picker first.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the pure-Go logic — the char-to-byte caret
  conversion and its clamping, caret-offset-to-heading resolution including
  the line-start normalisation, the slug-changed scroll guard, the checkpoint
  transitions, and the manifest (it validates, it registers, and its cap list
  is asserted exactly). Plus an ADR-0057 registry `Demo` in
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
  M4 goes further than the lane's usual reach and runs against a REAL
  `fsbroker.Service` over the in-process bus, wired with the manifest's own
  caps and nothing else. That is deliberate: what is most likely to be wrong in
  file I/O is not this app's arithmetic but its beliefs about the broker, and a
  stub would agree with whatever the app assumed. So the tests assert the
  seam's behaviour rather than a mock of it — that the two dialog caps suffice
  (which is what demonstrates the wildcard handle cap is unnecessary), that a
  bound document raises no second dialog, that a cancelled "Save as" keeps the
  binding it already had, and that the checkpoint records what landed rather
  than what the buffer holds when the ack arrives.
- **What would fail.** A caret-to-heading regression turns the scroll guard
  into either a preview that never follows or one the reader cannot scroll by
  hand; both are assertable from the resolved slug alone, without rendering.
  A capability creeping into the manifest fails the cap-list test — which
  through M3 was the tripwire on file I/O arriving at all, and from M4 is the
  narrower tripwire on the app's standing authority growing past the two
  dialog subjects; a static `fs.handle.>` has its own test refusing it. If the
  broker ever grants a read-write handle mode, the read-handle-cannot-write
  test fails, which is the signal to revisit the portal-style save flow rather
  than discover the change by surprise. And if the read op's refusal frame ever
  becomes something a text file could begin with, the leading-byte test fails
  before a refusal can be mistaken for a document. A layout or lowering
  regression shows up as a changed tour capture.
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

Accepted 2026-08-08, with M0–M4 shipped. Changes now arrive as dated
`## Update` sections rather than in-place edits.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-09 — The outline renders through the native tree widget

M2's outline was a flat list of `SelectableLabel`s indented by `AddSpace`. It
now renders through `widgets/tree` ([ADR-0176](./0176-native-tree-widget.md)),
making mdedit that widget's fourth adopter after `schemaview`, `configview` and
`fieldview`, and following the same four-step shape they do: build the columnar
hierarchy, sync host state into the widget's, render, apply the result back.

Three things the flat list could not do, and one it did that this gives up.
Headings nest by level, so the outline shows the structure the document already
has rather than a run of indents. A section folds away, with the count of what
it hides on the closed row. And only the rows on screen build widgets, where
before every heading in the document did on every frame.

What it gives up is horizontal scrolling. A tree row is a table cell of declared
width, so a long heading truncates where it used to be reachable by dragging the
column sideways; the full text is carried as the row's tooltip instead. The same
change retires the reason the pane needed `Hscroll(true)` at all — the heading
labels no longer push their own width back out into the window's minimum,
because the column's width is declared rather than derived from content.

Four decisions worth recording, because each has a failure mode that is quiet:

- **Collapse state is keyed by `slug#ord`, not by node index.** `tree.State`
  keys on the node's index in the columnar input — the only identity such an
  input has — and this app rebuilds its hierarchy from a fresh parse on every
  edit that changes the text. Typing a new heading above a collapsed section
  renumbers everything below it, so an index-keyed collapse would transfer to
  whichever heading slid into the vacated slot. The host therefore owns a
  `map[string]bool` and rewrites the widget's State from it every frame. This is
  the hazard `schemaview.syncNav` exists for; it is not specific to mdedit, but
  mdedit hits it on every keystroke rather than on every filter change.
- **The zero value is fully expanded.** Absent from the map means open, so a
  reader who never collapses anything sees exactly what M2 showed.
- **Selection is derived from the caret, never remembered by the tree.** The
  highlighted row is whichever section the caret is in, recomputed each frame,
  so the outline cannot come to disagree with the editor about where the writer
  is. A click therefore highlights by moving the caret, not by selecting.
- **Revealing the caret's section opens its ancestors in the HOST's map.**
  `tree.State.Reveal` opens them in the widget's own State, which the sync
  above overwrites one frame later; opening them where they will last is what
  makes the reveal survive. It fires only when the caret's section CHANGES —
  the same guard the preview's scroll target has — which has the consequence
  that collapsing the section the caret is already in leaves it collapsed. A
  click also marks its own destination as revealed, or the caret arriving there
  next frame reads as a change and scrolls the outline to centre a row the
  reader had already put their pointer on.

The hidden-heading count is a column of its own rather than a chip after the
label. A truncating label in a horizontal row takes the whole width it is
offered, so anything emitted after it is pushed out of the cell — which for an
outline, where long headings are common, would drop the count exactly on the
rows with most to hide.

Verified by unit tests over the pure half (nesting under skipped levels, a
document with no level 1, key stability across an insert, the reveal and click
paths) and by driving the gallery scene live over egui-mcp: collapse-all leaves
one row carrying its count, expand-all restores, clicking a row scrolls both
preview and source, and a truncated heading's tooltip carries its full text.
That last one also settles a question this pane raised — a `HoverText` wrapping
a tree cell's label does not steal the row's own click.

### 2026-08-09 — Correction: the PARSE can leave the render goroutine; the RENDER cannot

The Context section above states that "the parse cannot leave the render
goroutine" because `parseAndLower` builds the document tree "out of FFI
opcodes", and the Design-space entry rejecting a `bgjob` worker rests on the
same reading. The claim is wrong as written, and
[ADR-0180](./0180-markdown-rendering-fidelity-pass.md) item 8 corrects it at
the source.

`markdown.Parse` never touches the wire. `c.Atoms()` / `.Keep()` write into a
`sync.Pool`-backed Go buffer and intern the result through `unique.Make`;
only `Send()` on the render path reaches the FFFI sink. The one piece of
shared mutable state involved — codeview's package-level prepared-job memo —
already takes a mutex, and says in its own comment why: the documented
retain-once idiom is a package-level `var doc = markdown.Parse(...)`, which
runs at init on whatever goroutine gets there. Two shipping consumers
(`docsections`, `sqlapplet_store`) have been parsing off the render goroutine
all along. The `markdown` package now states the contract, and a `-race` test
pins it.

What is render-goroutine-only is `Doc.Render` — it emits into the current Ui
scope like any other widget call. That is what the original sentence meant to
say.

Nothing in mdedit changes. The reparse-per-keystroke policy was licensed by
the measured cost curve, not by the impossibility of moving the work, and the
measurements stand. What changes is that the deferred quiescence gate is no
longer the only remedy available for a document large enough to show up as
typing latency: a `bgjob` parse with the previous `Doc` rendered until the new
one lands is now on the table, at the cost of a stale-preview window. It stays
deferred, on the same trigger.

### 2026-08-09 — the outline's collapse map moved into the widget

[ADR-0176](./0176-native-tree-widget.md) took up the `Keys` column this app's
port argued for, so the tree widget now files expansion under a caller-supplied
key instead of a node index. The outline's own `outlineCollapsed` map,
`outlineIsCollapsed` and `outlineSetCollapsed` are gone: `outlineModel` hands
the widget a `keys` column of `slug#ord` and `tree.State` is the store.

Three consequences worth naming, none of which change what the pane does:

- `syncOutline` no longer pushes expansion, only the caret-derived selection
  and the default. The default is set through `SetDefaultExpanded(true)`, which
  is what keeps the pane's promise that a document arrives fully open.
- `outlineReveal` lost its parent walk. It used to open the caret's ancestors
  in the host map because the widget's own `ExpandAncestors` was overwritten a
  frame later by `syncOutline`; with nothing rewriting expansion, asking for
  the reveal is enough. `PendingReveal` is what its test asserts on now.
- `outlineCollapseAll` stays a loop over the current nodes rather than becoming
  `tree.State.CollapseAll`, because that also flips the default: a heading
  written after the fold has never been closed by anyone and has to arrive
  showing. A test pins it.

The reparse still renumbers every node on every edit that changes the text —
that has not changed, and it is why the key column exists at all.

### 2026-09-01 — find leaves its checkbox; Enter walks the matches

M3 shipped find behind a checkbox in the state row. The checkbox is gone: the
query field is now always in the bar, with a clear button, the case toggle,
navigation and the count readout beside it — the empty query already meant
"not finding", so the toggle was a second way to say the same thing, and the
field being visible is what makes find discoverable. Replace stays behind a
toggle in the group, because its two buttons rewrite the buffer and a row of
rewrite gestures should be asked for rather than ambient. The bar reflows
around this: row one is document gestures and formatting, row two view state
and readouts, and the replace row appears third when asked for.

The "Enter to advance" deferral in §Alternatives is closed: its trigger — a
key channel — arrived as [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md)'s
focus-scoped capture, and the query field now captures Enter alone
(`textEdit.captureKeys`, this repo's first use of it outside the widget
packages). Enter steps forward, Shift+Enter back — the mask matches the key
alone and the modifier is read from the capture — and the step deliberately
does NOT pull focus into the source, or the second Enter would type a newline
into the document; the painted matches and the preview scroll show the
landing. Ctrl+F stays deferred on a different trigger: the ADR-0177 key
vocabulary has no letter keys, and a capture mask consumes the bare key, so
capturing `F` would eat every `f` typed.

### 2026-09-01 — view modes: Split, Source, Read

A segmented control leads the state row: Split is M0's layout and the zero
value, Source renders the editor alone in the central region, Read the
preview alone. Each mode only ever SKIPS panels and lets the central region
absorb what they released — no width math changes, because §Decision 6's
derive-every-frame sizing means nothing retained can clamp. What a mode hides
goes legibly with it: the formatting bar and the replace row follow the
editor (rewriting text the reader cannot see is a trap), while the find
query, its count and its navigation stay in Read mode — the preview scroll
half of `gotoMatch` still works, and the colour job the hidden editor would
consume is simply not built. The outline stays in every mode; in Source mode
it still navigates the caret. The mode is session-scoped on purpose:
persisting a layout preference would spend a persist key on low-value state.

### 2026-09-01 — heading numbering from the outline

The outline pane gained a numbering pair: Renumber inserts or refreshes
numeric section prefixes ("## 2.1 Title") across the whole document, Clear
removes them — the way back, since the rewrite is a rebind outside the
editor's undo (M3's standing caveat, restated in the tooltips). The numbers
come from `Doc.Headings()` — the same set the outline and preview render, so
they match the tree the reader is looking at by construction: the same
nesting rule for skipped levels, setext headings included (the source lexer
deliberately reads those as prose), nothing inside fences. `ByteOffset`
pointing at the heading TEXT is exactly the splice point, so no marker
hunting. A prefix must contain a dot and end in a space ("1. ", "2.3 "), so a
title starting with a bare year survives Clear; one starting with a decimal
("3.14 constants") does not — the accepted edge of any textual convention,
stated at the button. The gesture is stashed and applied at the top of the
NEXT frame: the outline renders after the source pane, and a rebind issued
after the editor's emit would miss the frame's databinding override. Slugs
move with the text, so collapse state and anchors move too.

### 2026-09-01 — LLM transformations

mdedit gained an env-gated transform surface — a prompt picker, a bgjob-run
one-shot completion, and a preview-then-apply result pane. The decisions live
in [ADR-0216](./0216-mdedit-llm-transformations.md): the sibling package
owning the LLM dependency, the embedded prompt-book format, the endpoint
gate, and why apply refuses a buffer that moved. On this ADR's side of the
line: the result pane is a bottom panel declared before the side panels, both
for full width and so an Apply's rebind precedes the editor's emit in the
same frame; and the clipboard export machinery grew a non-checkpointing
variant, because copying a transformation result is not exporting the
document and must not clear the dirty badge.

### 2026-09-02 — the bar names the file

Contract 7's "the app is never told which file it has" is revised at its
recorded seam: the Powerbox now widens `DialogReply` with the file's
BASENAME ([ADR-0026](./0026-app-runtime-and-capability-subjects.md) Update
2026-09-02), taken as a deliberate broker decision rather than on an app's
way past. The bar's "file bound" badge becomes the save target's name, an
opened-but-unbound document shows the name it was opened from, and the
statuses say "opened notes.md" / "saved out.md". Still true: the path stays
inside the broker (no directory, no location); a silent save through the
kept handle learns no name and must not clobber the one its dialog gave; and
the names are not persisted — like the handles they describe, they die with
the window. The suggested-filename derivation is unchanged, and the reason
it was never echoed back still stands — the difference is that the badge now
carries the broker's truth instead of nothing.

### 2026-09-02 — the buffer follows the file on disk

While an opened document is UNMODIFIED, the buffer follows the file: an
external write reloads it (a checkpoint move, not an edit), so mdedit reads
well beside a pipeline or another editor. The moment the buffer holds
unsaved edits the follow goes passive — a "changed on disk" badge, never a
reload — and a deleted or renamed-away file is a "gone on disk" badge with
the buffer untouched. Save as gives it a new home.

The mechanics revise two of M4's economies. The read handle is now KEPT
instead of closed after the read: it is what the file is followed through —
the broker watches the file for it (the read-handle watch seam, ADR-0026
Update 2026-09-02) and re-reads ride it; if the follow cannot start, the
close-immediately economy returns. And the app-side flow is the capdemo
ordering plus three rules of its own: the event handler only files mu-guarded
flags (the pump's publishes are synchronous — blocking it stalls the broker);
a ~250 ms debounce coalesces the burst a save is made of into one re-read,
single-flighted beside — not inside — the file-gesture slot, so Open and
Save stay usable during a reload; and disk-equals-checkpoint is read as "not
a foreign change", which is what keeps the app's own save from reloading
itself (an identical external write is indistinguishable and equally
harmless). One sequencing rule is load-bearing: uuids are stable per
(app, path, op), so re-opening the SAME file mints the same uuid — the old
handle's teardown runs ahead of the new dialog in the open goroutine, and
Unmount tears down synchronously (bounded) because the host closing the bus
client silences no broker-side pump.

One host constraint surfaced here: repaint is reactive, so a disk change
arriving with no input would sit unseen. While — and only while — a follow
is active, the frame keeps a low-rate `RequestRepaintAfter` tick alive; the
tick lives in the render body rather than the drain so the whole follow
state machine stays drivable from plain tests. Deferred: a follow on/off
toggle (always-on until someone asks), and re-arming a stale "changed on
disk" badge when hand-reverting edits makes the buffer clean again without a
new disk event.

### 2026-09-02 — the files pane: load from lading snapshots

A toggleable side pane browses the lading snapshot store (ADR-0198) through
the fsbrowser widget — this app is its third host after tally and play
(ADR-0200) — and activating a file loads it into the buffer. The source is a
pinned snapshot, so the contract differs from Open at every point a reader
could confuse them, and each is stated where it shows: the loaded document is
NOT file-bound (Save as gives it a home), there is no follow (a snapshot
cannot change), and the badge names `mount:path @ latest` under a tooltip
stating the snapshot contract rather than the Powerbox one. The load rides
the same two-click dirty guard as Open — the arming is factored into a
shared `confirmReplace` — and a size gate refuses rather than truncates.

Plumbing is deliberately tally's, copied rather than shared: `storeConn` and
the `lane` are app-local there by ADR-0200's own design, and the trim is
real (mounts and files, never audits or sizes). The connection is lazy and
its failure is a hint inside the pane; listings read on the render thread
through `ladingview.Locked` (the batch-shaped trade ADR-0198 records);
loads leave the frame on a lane and LAND in the render body's drain, before
the source pane — the same emit-ordering rule every buffer rebind here obeys.
The send-to-play pipeline keeps its own one-shot connection rather than
sharing this one: a generated store is single-goroutine, and sharing the
executor across the send goroutine and this pane's reads would cross that
line unguarded for the price of one HTTP connect. Ships latest-snapshot-only;
a snapshot picker is deferred — snapshot archaeology is tally's job.

### 2026-09-02 — send to play

A "Send to play" button persists the document as a `boxer.facts` row and
opens the playground on a query that reads it back, rendered as markdown in
play's Detail pane. The decisions — the mddoc vocabulary and kind, the
neutral package pair, the launch-query handover and its identity rules —
live in [ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md). On this ADR's
side of the line: the manifest grows one cap (Pub on `windowhost.open`, the
cap-list test updated), the pipeline is one goroutine draining into the
status line like every other gesture here, and mdedit gains its first — and
indirect — ClickHouse dependency, entirely inside that goroutine.

## References

- [ADR-0176](./0176-native-tree-widget.md) — the tree widget the outline
  renders through, and the `Keys` identity column its `State` files under.
- [ADR-0180](./0180-markdown-rendering-fidelity-pass.md) — the rendering
  fidelity pass that corrected the parse-thread claim above.
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
