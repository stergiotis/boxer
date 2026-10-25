---
type: adr
status: proposed
date: 2026-10-24
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0334: mdedit edits markdown as blocks over the source — a block widget, a formatted source pane, and two seams

## Context

`apps/mdedit` ([ADR-0178](./0178-mdedit-markdown-editor.md)) is a markdown
editor for someone who already writes markdown: a monospace `TextEdit` over
the source beside a `markdown.Doc` preview, with an outline, find and
replace, file I/O, and a formatting bar that wraps the selection in markers.
The question this ADR answers is how it becomes an editor for someone who
does not — who expects bold to look bold while typing, a heading to be a
heading rather than `## `, a table to be a grid and an image to be a picture
— without the file on disk ceasing to be markdown that other tools read
unchanged.

The design space was surveyed first, under a clean-room procedure
([rich-text-editor-design-space-survey](../adr-background-work/rich-text-editor-design-space-survey.md),
2026-10-24). What it established, and this decision rests on:

- **Every product serving a mixed audience converged on a rich default with a
  persisted markdown toggle, markdown as the stored form.** The complaints
  that forced vendors to change were about keyboard predictability at seams
  — Enter, Backspace, the caret at a marker's edge — not about rendering.
- **Markdown is lossy from any richer model.** Every exporter from a block or
  document model documents the loss. The only ways to keep a file untouched
  where the user did not edit are to keep the source as the model, or to
  splice edits back into the source by byte range. goldmark reports the
  ranges; the tree's lowering drops them.
- **egui's `TextEdit` can vary font, size and line height per span, but
  cannot hide a byte range or embed anything in the text flow**, and its
  undo is a per-widget snapshot of the whole string. Every immediate-mode
  toolkit surveyed reaches the same place: the stock field is single-style,
  anything richer is a retained widget beside it. Tables and code fences are
  never edited as hidden-marker text anywhere; every product gives them a
  sub-editor.
- **The tree's seams stop short in four places**: one font per buffer in the
  highlight seam, a key vocabulary without letters and a mask that cannot
  say "Ctrl+B but not b", a write-only UTF-8 clipboard, and no channel from
  a byte range to a screen rect.

Four decisions were taken at the design dialogue of 2026-10-24 and bound
what follows: build the block editing mode directly rather than stopping at
a formatted source pane; both Rust seams (font, keys) may change; a fresh
window opens in the formatted source pane with the block mode one click
away; and clipboard read, frontmatter forms and cross-block text selection
are deferred with their triggers recorded.

## Design space (QOC)

**Question.** What is the editing surface's model, given that the file must
stay faithful and the toolkit cannot hide text or embed widgets in a text
flow?

**Options.**

- **O1** — a decorated source pane: one `TextEdit`, styled from the parse,
  markers dimmed, no in-place tables or images.
- **O2** — a block list over the source: the parse's top-level blocks, each a
  byte range; unfocused blocks rendered, the focused one a `TextEdit` over
  its slice; every gesture a splice into the one source string.
- **O3** — an owned text engine on the painter lane, with its own caret,
  selection, layout and IME.
- **O4** — a rich document model with markdown export on save.

**Criteria.**

- **C1** — reads as formatted to a non-programmer; tables, images and code as
  they expect.
- **C2** — bytes the user did not edit never change.
- **C3** — keyboard predictability at seams.
- **C4** — reuses the tree's seams; Rust change bounded to generic surfaces.
- **C5** — caret, IME and accessibility inherited from egui rather than
  rebuilt.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | ++ | ++ | ++ |
| C2 | ++ | +  | +  | −− |
| C3 | ++ | −  | +  | +  |
| C4 | +  | +  | −− | −− |
| C5 | ++ | +  | −− | −  |

O4 fails C2 outright and is the shape every lossy exporter has. O3 fails
ADR-0069's bar for reimplementation — neither isomorphism nor closed-loop
observability asks for it — and takes on the list the contenteditable
literature says a custom editor owes itself. O1 is not an alternative to O2
but its focused-block rendering, and is kept as the default pane. O2 is
chosen; its one negative, the seams, is exactly the documented failure mode
of its genre, and §SD4 is the answer to it.

## Decision

We will add a semi-retained widget, `widgets/mdblocks`, that edits a
markdown document as a vertical list of blocks over the one source string,
and give `apps/mdedit` a fourth view mode, Blocks, that hosts it. The
existing source pane becomes proportional and sized through an extended
highlight seam, and formatting shortcuts arrive through an extended key
vocabulary. Two Tier 1 seams move, each recorded on the ADR that owns it.

### SD1 — The source string is the only model

The document is the markdown bytes. Presentation — blocks, styled runs, the
rendered view of an unfocused block — is derived from them by parse and is
throwaway. Every editing gesture is a replacement of one byte range of the
source by new bytes, followed by a reparse; nothing ever serialises a model
back to markdown. Bytes outside the replaced range are never rewritten.

Two exceptions are stated at the widget that makes them: the table grid
re-pads the cells of the row it edits (a GFM table is lines, and a changed
cell changes its row's spelling), and ordered-list renumbering stays the
explicit outline gesture it already is (ADR-0178, 2026-09-01). Frontmatter
is one block edited as text; a form over line-level patching is deferred
(§Alternatives), because the object round-trip every product uses is the
survey's one "very high" fidelity risk.

### SD2 — A block is a top-level block of the parse

Blocks are goldmark's top-level block nodes — paragraph, heading, list,
blockquote or callout, fenced or indented code, table, thematic break,
frontmatter — each with a `[start, stop)` byte range taken from the node's
position and its last descendant's lines, snapped to line ends, the recipe
`mdspan` ([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md) §SD5)
already uses, written again beside the lowering rather than exported from
`mdspan`, whose helpers work over its own line index. A fence's closing
line and a setext heading's underline, which goldmark keeps out of the
node's lines, are part of the range. Frontmatter, which the parser consumes
without a node, is found the way `mdspan` finds it. Source the parse turns
into no block — an HTML block, a link reference definition, a node the
lowering drops — is a raw block: edited as text, never merged into or
deleted along with a neighbour, so the blocks and the blank lines between
them are the whole document and no gesture can reach bytes the view does
not show. A list is one block: Enter inside it continues the item (§SD4)
rather than splitting the list, which is what every surveyed product does. Block identity across reparses is positional
plus content-keyed, the way the outline keys collapse state by `slug#ord`:
the focused block is re-found after a splice by the range the splice
produced, never by index.

The nested block list inside a callout body is deferred; the first cut edits
a callout's body as one text field with the `> ` prefixes stripped for
editing and restored on splice.

### SD3 — The focused block is a `TextEdit` over its slice

One block at a time is a `TextEdit` whose buffer is the block's source
slice, coloured and sized by the highlight seam (headings large, emphasis
italic or bold, code monospace, markers dimmed). Unfocused blocks render
through the markdown widget, one block at a time, under the widget's own id
scope. Reveal granularity is therefore whole-block: the focused block shows
its own markers, as Logseq does. A `TextEdit` cannot hide bytes, so this is
the only granularity available, and it is also the one that avoids the two
caret complaints the survey found (reflow on reveal, caret inside a closing
marker).

Four blocks get sub-editors instead of a text field: a **code fence** (a
bordered monospace field for the body, a language field, Tab indents, no
markdown behaviours inside), a **table** (a grid of cell fields in resizable
columns, Tab and Enter between cells, Tab in the last cell adds a row, a
handle before every row and above every column whose menu inserts, moves,
deletes, aligns and sorts — the operations the productivity tools converge
on that a GFM table can carry — and a paste of tab-separated text into a
cell expanding into cells; no merged cells, no multi-line cells), an **image** (the picture, with alt text and the
Obsidian `|WxH` size as fields), and a **callout** (a type menu and a fold
toggle over the rendered callout, the body as a text field). Everything else
focuses as text.

### SD4 — What the keys do at the seams

The gestures, and their answers, chosen from what the surveyed products
converged on:

| Gesture | Answer |
| --- | --- |
| Enter at the end of a paragraph or heading | split: a new paragraph block below, focus moves into it |
| Enter inside a paragraph | split at the caret |
| Enter inside a list, quote or callout | continue the line's marker on the next line; Enter on an empty item removes the marker and exits |
| Shift+Enter | a soft line break inside the block |
| Backspace at the start of a block | a heading or list item demotes to a paragraph first; a paragraph merges into the previous text block; a non-text neighbour (image, rule, table) is selected instead |
| ↑ with the caret at the block's start, ↓ at its end | focus moves to the neighbouring block, caret at its end or start. The row the caret is on is not known to Go (§SD9, byte-range-to-rect), so the edge of the text stands in for the edge row |
| Escape | leaves text editing and selects the block; Shift+↑/↓ extends the block selection; Delete removes it; Enter opens the block; Escape again clears the selection |
| Tab, Shift+Tab inside a list | indent or outdent the item by one tab, the unit Obsidian's default writes; elsewhere Tab inserts a tab |
| `# `, `- `, `1. `, `> `, `[] `, a fence at line start | the line-start shortcuts, applied inside the focused block as text |
| Ctrl+B / I / K / E | wrap the selection in `**`, `*`, a link, a code span — the formatting bar's actions, from the keyboard |
| Ctrl+Z / Ctrl+Y | the document-level undo and redo (§SD5). Ctrl+Y rather than Ctrl+Shift+Z: a widget captures one chord modifier set (§SD7), and Ctrl+Y is the Windows and Linux convention the host runs on |
| In a table cell: ↑/↓, ←/→ at the text's edges | the cell above or below; the previous or next cell, wrapping at a row's ends; ↑ from the header and ↓ from the last row leave the table for the neighbouring block |
| In a table cell: Ctrl+↑/↓, Ctrl+←/→ | move the row or the column the caret is in, the header staying first; the word jump Ctrl+←/→ would be in a field is given up in a cell. Typora's Ctrl+Enter for a row below is not taken: the window host polls the command-Enter fetcher every frame as its sign of input, and whoever polls it owns the chord for the process; a row below is the row handle's menu, or Enter in the last row |

A split and a merge are inverses and land as one undo entry each. Pasting
multi-paragraph text into a block splits it into blocks after the splice,
because the reparse does. Cross-block *text* selection is deferred behind
block selection, the order Notion took.

### SD5 — Undo is the document's, in Go

A history of inverse splices — range, old bytes, new bytes, caret before and
after — owned by the widget, with the coalescing rules the frameworks
document: contiguous typing at the caret within 500 ms joins the open
entry; a caret move, a structural gesture, a paste or a formatting command
closes it. egui's per-widget `Undoer` would fight this history, so Ctrl+Z
and Ctrl+Y are captured in the focused block (§SD7) before egui sees them.
Undo restores the focused block and its caret. The first text typed into a
gap is recorded with the blank lines that make it a block, and the typing
that follows still joins that entry.

### SD6 — Per-section fonts in the highlight seam

The `textEdit` styled channel gains a font vocabulary: `sectionFont` sets a
section's family (inherit, proportional or monospace) and a size factor of
the editor's base font, on the register `sectionStyled` already uses, under
a flag of its own. The colour `Section` shared with the codeview producers
does not change shape, for the reason ADR-0130 §Alternatives gave, and
neither does `section`'s wire. A font cannot be overlap-merged into one
shaping run the way a decoration can, so the layouter splits the colour
sections at font boundaries and the narrowest font section covering a piece
wins — which lets a producer list one body font for the whole buffer and
smaller spans over it. Weight is not in the vocabulary: the host embeds
regular faces only, so strong stays a colour. Rows size themselves to their
tallest section, so a heading line and a body line share one galley. The
source pane and the focused block use it; the derivation from lexer
category to font lives in `codeview` beside the colour palette. Recorded on
ADR-0130 (2026-10-24).

### SD7 — Shortcuts in the key vocabulary

ADR-0279 had already put the letters in ADR-0177's vocabulary; what no
capture could say was "this letter only with its modifier". `Frame` and
`textEdit` gain `captureKeyChords(mask, mods)`: a masked key is taken only
from an event whose own modifiers are the chord — the platform command
modifier held (Ctrl on Linux and Windows, Cmd on macOS), Shift exactly as
stated, Alt not — and only that event leaves the queue, so a plain letter in
the same frame still types. Letters with the command modifier are shortcuts
by every platform's convention, and a `TextEdit` that would otherwise
interpret one (egui's own Ctrl+Z) loses it to the capture. Recorded on
ADR-0177 (2026-10-24).

### SD8 — mdedit hosts it as a mode, and the source pane is formatted

Blocks is the fourth entry of the Split / Source / Read control. A fresh
window opens in Split with the source pane formatted (§SD6); the mode is
session-scoped like the others. The existing features keep working because
they act on the string: find paints matches in the source pane and scrolls
the preview; the outline follows the caret and renumbers through its
rebind; proposals apply through `rebindBuffer`; follow-on-disk reloads the
string. In Blocks mode the format bar and replace row hide with the source
pane, as they do in Read mode, and the block widget's own gestures replace
them.

### SD9 — What is deferred, and what triggers it

- **Clipboard read and an HTML flavour** — the ADR-0026 extension point.
  Paste stays egui's own text paste into the focused block. Trigger: the
  first user pasting from a browser or a word processor into Blocks mode and
  losing structure.
- **Cross-block text selection.** Trigger: block selection proving
  insufficient for copying a passage.
- **Frontmatter as a form** over line-level patching. Trigger: a user asking
  for properties they cannot type.
- **Nested blocks inside callouts and list items.** Trigger: a callout body
  that needs a table or a code fence.
- **Byte-range-to-rect**, for a bubble toolbar, a link popover, and ↑/↓
  by the caret's row rather than the text's edge. Trigger: the formatting
  bar's distance from the selection being the complaint, or ↑ from a
  wrapped line's second row being expected to stay in the block.
- **Windows line endings.** Block ranges keep a `\r`; a gap the editor pads
  gets `\n` alone. Trigger: a CRLF document edited in Blocks mode.
- **A URL pasted over a selection becoming a link.** Paste is egui's own
  text paste; nothing tells a pasted URL from typed text. Trigger: a user
  asking for it.
- **What a GFM table cannot carry**: merged cells, multi-line cells,
  several header rows, captions, cell colours. MultiMarkdown spells a
  colspan as trailing pipes and markdown-it-multimd-table a rowspan as
  `^^` and a line break as a trailing backslash; Pandoc's grid tables take
  block content in a cell. None parses in goldmark, Obsidian or GitHub, so
  a document using them renders broken everywhere but here. Even the
  GFM-legal `<br>` in a cell stays out while the renderer drops raw HTML.
  Trigger: a reader asking for one of these in a document that stays in
  Blocks mode.
- **Dragging a row or column handle to reorder.** The bindings carry no
  drag source or drop zone; the handle's menu and Ctrl+arrows move. Trigger:
  a table long enough that moving by steps is the complaint.
- **A table's dragged column widths across sessions.** The grid's columns
  resize and egui keeps the widths for the session; the deferred-body
  table op has no width read-back, so nothing reaches the width store the
  record grid uses. The rendered table does not honour the delimiter row's
  alignment either, for want of an alignment knob on its op. Trigger: a
  column width or an alignment that has to survive a restart.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `styledSections` IDL node | added — `sectionFont` beside `section` (§SD6) | the Rust `StyledSection` struct and layouter, regenerated dispatch on both sides, `codeview.StyledSection` and `MarkdownFontSections`, the browser host digest |
| `Frame` and `textEdit` IDL nodes | added — `captureKeyChords(mask, mods)` beside `captureKeys` (§SD7) | the shared capture helper's match rule, `keychord.rs`, `keycodes.Mods`, the browser host digest |
| `widgets/markdown` exported API | added — per-block source ranges and kinds, and a render of one top-level block | `widgets/conformance` (a new exported symbol set), the EXPLANATION's invariants |
| `widgets/` | added — `mdblocks`, semi-retained | the demo registry's gallery, `widgets/conformance` with the allowlist still empty |
| `apps/mdedit` view modes | added — Blocks; default pane formatted | the help page, the tour scene |

## Alternatives

- **A rich document model with markdown export** (O4). Rejected: every such
  exporter documents loss, and the file is the contract with every other
  reader of the vault, this tree's own tooling included.
- **An owned text engine** (O3). Rejected for the first cut, recorded so it
  is not re-proposed without a trigger: it is the only shape that can hide
  markers and inline an image, and it owes caret, selection, IME anchoring
  and the accessibility tree in return.
- **Span-level reveal of markers inside the focused block.** Not available:
  a `TextEdit` lays out every byte. Whole-block reveal is chosen, with the
  font overlay narrowing the gap.
- **Keeping egui's undo** and recording Go's history beside it. Rejected:
  two histories over one buffer disagree after the first structural gesture;
  capturing Ctrl+Z is cheaper than reconciling them.
- **Changing `Section` to carry a font** instead of a parallel channel.
  Rejected for ADR-0130's reason: the struct is shared with the read-only
  codeview producers, and a sparse overlay leaves them untouched.
- **A general key channel** instead of a command-modifier capture. Rejected:
  ADR-0177 §SD10 keeps text input out of the key vocabulary on purpose, and a
  shortcut is the one case where a letter is a key, not a character.
- **Blocks as the default mode.** Deferred by the dialogue: the formatted
  source pane is the default until the block mode's seams have earned it.

## Consequences

### Positive

- An end user sees a formatted document and edits it in place, and the file
  on disk changes only where they typed.
- The block widget, the font overlay and the shortcut capture are each
  removable or reusable on their own: the widget is a package with one
  consumer, the seams are generic.
- mdedit's existing features need no knowledge of blocks, because every
  gesture is a splice into the string they already act on.

### Negative

- The seams are where this genre fails, and the first cut has them all:
  focus handoff between fields, split and merge, block selection without
  text selection across blocks. The scenes in §Verification plan are the
  guard, and the complaints will come from exactly there.
- Two Rust changes move the browser host digest and add to two registries
  (an IDL method, key codes) that are permanent until retired.
- A document-level undo is a second undo in the tree, and the first that
  spans widgets; the capture that keeps egui's out of the way is a
  dependency between §SD5 and §SD7.
- Reveal is whole-block, which the survey found some users reject in
  Logseq's case; nothing short of O3 changes that.

### Neutral

- The reparse-per-edit policy ADR-0178 measured is inherited unchanged; only
  the frame in which it runs now also re-derives the block list.
- Spell-checking is absent from the tree and remains so; the ADR names it so
  the absence is a decision.

## Migration — Tier 1

- **Breaks.** Nothing: every surface change is additive. Existing `textEdit`
  callers see no new behaviour without the new method; existing
  `captureKeys` masks match as before.
- **Regeneration.** `app egui2gen generate` for the IDL; the Rust host is
  rebuilt; `tabhost/browserhost.sum` is refreshed in the same commit as each
  Rust change.
- **Old shape.** Kept; nothing is deprecated.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the block model (splice, reparse, undo
  coalescing, block identity after a splice), the Rust unit tests beside the
  highlight seam's reconcile, `widgets/conformance`, and headless scenes
  under `apps/mdedit/scenes` driving the Blocks mode: Enter at a
  paragraph's end opens a gap and typing pads it; Ctrl+Z and Ctrl+Y undo
  and redo it; a click on another block leaves the open one; Ctrl+B
  inserts its wrap; the sub-editors round-trip a fence and a table; and the
  source pane read at the end shows the bytes outside the edited range
  equal to the originals. The default lane's unit tests cover the rest of
  the §SD4 table: list continuation and exit, Backspace demoting then
  merging, the arrows, Escape, the soft break, and the raw blocks.
- **What would fail.** A seam gesture doing something other than the §SD4
  table; a byte outside a splice changing.
- **Gap.** IME composition inside a block, and the accessibility tree across
  blocks, are not asserted headlessly.

### Milestones

- **M0 — the font overlay** (§SD6): IDL, Rust, bindings, `codeview`'s
  category-to-font map; ADR-0130 update; digest. ✓
- **M1 — the shortcut capture** (§SD7): the command-modifier method;
  ADR-0177 update; digest. ✓
- **M2 — the source pane**: formatted through M0; Enter list continuation
  and exit, Tab indent, Ctrl+B/I/K/E through M1. ✓ The line gestures live
  in `mdline`, shared with the block widget so the two cannot disagree.
  `BOXER_MDEDIT_DOC` seeds a window with a document of its own, treated as
  handed over, so a scene never touches the person's draft.
- **M3 — `mdblocks` core**: document model, splice and reparse, undo,
  paragraph and heading blocks, the §SD4 seams, block selection; a gallery
  demo; mdedit's Blocks mode (§SD8) and its scene. ✓ Three things the
  design did not say. A click on a rendered block lands on a focusable
  frame above the block's own text, read as the frame gaining focus, and
  puts the caret at the block's end — there is no channel from a click
  position to a byte; the open is applied after every block is drawn, since
  an editor emitted later in the same frame would otherwise consume the
  caret it queued. What the TextEdit writes back at a frame's end already
  carries that frame's insert, cursor and rebind, so nothing is ever skipped
  as stale; only a caret the host or a gesture queued since the report wins
  over it. And a write-back moves the binding's range along with the text,
  so plain typing never rebinds the widget.
- **M4 — sub-editors**: code fence, table grid, image, callout. ✓ The
  grid's handles, menus, keys, sort and paste expansion followed as the
  survey of the productivity tools' tables (PowerPoint's Layout tab,
  Google Docs, Notion, Obsidian 1.5, Typora, Advanced Tables) sorted by
  what the GFM source can carry: the structural operations yes, widths and
  the like as presentation the source never holds, spans and multi-line
  cells not at all (§SD9). Each sub-editor
  parses the block into fields on open, composes the block from them after
  every write-back and splices only the bytes that differ — and nothing
  while the fields still compose to what they composed to on open, so a
  spelling the composer would normalise stays as written until a field is
  edited; a table re-emits
  only the rows whose cells changed and keeps the others verbatim, padding
  and all. A structural key gesture — Tab past the last cell adding a row —
  splices in the same pass, since the fields are reloaded only when the
  block's text moved past what they were loaded from. A fence body has no
  syntax colours of its own in this cut, and a rendered code block carries
  no text to click on in the accessibility tree, so the scene reaches it
  with ArrowDown from the block above — the seam §SD4 names.
- **M5 — a help page** for the Blocks mode and its keys. ✓ mdedit had no
  inline help for any mode, so the book (`apps/mdedit/help`, through
  `Manifest.Help` as ADR-0047 has it) is two pages: the app, and the
  Blocks mode with its keys as the §SD4 table reads to a user.

## Status

Proposed — awaiting review by the code owner. Implementation began the day
the dialogue settled the shape; a milestone's `✓` records what shipped.

## References

- [rich-text-editor-design-space-survey](../adr-background-work/rich-text-editor-design-space-survey.md) — the clean-room survey this decision rests on; §9 maps the shapes, §10 the forks.
- [ADR-0178](./0178-mdedit-markdown-editor.md) — mdedit, and the WYSIWYG alternative it declined for its first cut.
- [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) — the highlight seam §SD6 extends.
- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — the key capture §SD7 extends.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget shapes; `mdblocks` is semi-retained.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the clipboard extension point §SD9 defers to.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — the scene lane the verification plan names.
