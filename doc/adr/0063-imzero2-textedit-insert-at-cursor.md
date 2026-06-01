---
type: adr
status: accepted
date: 2026-05-31
reviewed-by: "@spx"
reviewed-date: 2026-05-31
---

> **Status: accepted 2026-05-31 by @spx.** Implemented on both sides (Go binding + Rust interpreter), unit-tested (the char-indexed splice), and shipped in play's snippet library.

# ADR-0063: ImZero2 — Programmatic Insert-at-Cursor for TextEdit

## Context

ImZero2 is a Go-driven, Rust-rendered UI over [egui](https://www.egui.rs/). A
`TextEdit` widget round-trips a whole `String`: Go sends the current buffer in
the `textEdit` opcode, the user edits it, and on egui's `.changed()` the Rust
interpreter pushes the new buffer back to Go through the `r9_s` databinding,
which `StateManager.Sync` writes into the bound `*string`. The Go side never
sees the caret — egui's `TextEditOutput.cursor_range` is not plumbed across the
FFI, and a search of the Rust client found no existing use of `TextEditState`,
`CCursorRange`, or `cursor_range`.

The [`play`](../../apps/play/) SQL playground grew a snippet library (a help
doc whose fenced SQL blocks are surfaced via
[`markdown.Doc.RenderActions`](../../public/thestack/imzero2/egui2/widgets/markdown/markdown.go),
the same mechanism [ADR-0026](0026-app-runtime-and-capability-subjects.md) wires
to "Copy"). The requirement was that clicking a snippet **insert it into the
editor at the caret**, like an IDE — not copy it to the clipboard for a manual
paste, and not clobber the buffer.

Nothing in the current API can do that. The realistic options split on *where
the splice happens and who knows the caret*, and a codegen constraint rules out
the most obvious shape: the generated `textEdit` handler constructs the egui
widget (`egui::TextEdit::multiline(&mut text)` — a `&mut text` borrow) **before**
the builder-method loop runs, so a builder method cannot mutate `text` while the
widget holds it. (The same "no outer-scope room ahead of the egui call" note is
recorded on `datePickerButton` in the widget definitions.)

## Design space (QOC)

**Question.** How does a snippet land at the editor's caret, given the whole-
string round-trip, the absent cursor readback, and the construct-before-methods
ordering?

**Options.**

- **O1 — Go-side append / replace-buffer.** On click, Go mutates the bound
  string (append, or replace when empty). No FFI work. Not at-cursor; ignores
  selection; an append mid-edit reads as wrong.
- **O2 — Clipboard + manual paste.** Reuse the existing `clipboard.write`; the
  user presses Ctrl+V. Already possible; not a *direct* insert, and pollutes the
  system clipboard.
- **O3 — Cursor readback to Go.** Add an opcode + register to ship
  `cursor_range` back to Go, splice in Go, push the new text *and* a new cursor
  back. True at-cursor, but two-way: a new readback register, char↔byte (UTF-8)
  handling in Go, and a second opcode to set the caret. Largest surface.
- **O4 — Builder method + Rust-side splice via persisted state _(chosen)_.** A
  new `insertAtCursor(snippet)` builder method stashes the snippet on a scratch
  slot of the interpreter; the `textEdit` **apply** code — which runs after
  `apply_widget` releases the `&mut text` borrow — splices the snippet at the
  editor's persisted caret and force-pushes the new buffer back over the
  existing `r9_s` path. No new register, no Go-side cursor handling.

**Criteria.** C1 faithfulness to "at the caret" · C2 FFI surface added · C3 fit
with the construct-before-methods ordering and the `&mut text` borrow · C4
robustness (culled/never-focused editor) · C5 reversibility.

**Assessment.**

| | C1 | C2 | C3 | C4 | C5 |
|---|----|----|----|----|----|
| O1 append/replace | −− | ++ | + | + | ++ |
| O2 clipboard | − | ++ | + | + | ++ |
| O3 cursor readback | ++ | −− | − | + | − |
| **O4 method + splice** | **+** | **+** | **+** | **+** | **+** |

O4 gets C1 a `+` rather than `++` because it splices at the *persisted* caret
(last frame's), not a live readback — imperceptible for a click-driven insert,
and the snippet button lives in another tab where the editor is unfocused
anyway.

## Decision

Add an `insertAtCursor(snippet string)` method to the `textEdit` widget
definition and fold the splice into its apply code (both authored in
[`egui2_definition_d_widgets.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go),
emitted by egui2gen). The interpreter gains one scratch field,
`text_edit_pending_insert: Option<String>`.

**Mechanism.**

1. **Stash (method arm).** `insertAtCursor` reads the snippet and sets
   `self.text_edit_pending_insert = Some(snippet)`. It does not touch `text`
   (still borrowed by the widget), sidestepping the ordering constraint.
2. **Splice (apply code).** After `apply_widget` returns (the `&mut text` borrow
   released), if the scratch slot is `Some`, load `TextEditState` for the
   widget id, take its caret/selection as a sorted char range (or the buffer end
   when there is none — an editor never focused), `delete_char_range` +
   `insert_text` via the `egui::TextBuffer` trait (char-indexed, UTF-8-safe),
   set the caret to just past the inserted text, and `state.store`.
3. **Force-push.** A programmatic edit never sets egui's `.changed()`, so the
   push is gated on a single `changed` flag set by *either* a user edit *or* an
   insert, and `text` is moved into `r9_s_push` exactly once at the end (pushing
   twice would move it twice).

The slot is `take()`-n in the same handler invocation for the same widget id, so
it cannot leak to another widget — and because the apply code runs even when the
widget is culled, a click while the editor tab is hidden still consumes the
snippet (appending at end, since there is no live Ui/caret).

**Semantics.** One-frame latency (the editor emitted before the click was
recorded, so the splice lands next frame); insert at the persisted caret, or
append at end when the editor was never focused; selection is replaced.

## Alternatives

The QOC options above are the alternatives weighed; the three rejected ones, and
why:

- **O1 — Go-side append / replace-buffer.** On click, Go mutates the bound
  string (append, or replace when empty), needing no FFI work. Rejected: it is
  not at-cursor, it ignores any selection, and an append mid-edit reads as wrong
  (C1 `−−`).
- **O2 — Clipboard + manual paste.** Reuse the existing `clipboard.write` and let
  the user press Ctrl+V. Already possible today, but it is not a *direct* insert
  and it pollutes the system clipboard (C1 `−`).
- **O3 — Cursor readback to Go.** Add an opcode plus register to ship
  `cursor_range` back to Go, splice in Go, and push the new text and caret back.
  True at-cursor (C1 `++`), but the largest surface (C2 `−−`): a new readback
  register, char↔byte (UTF-8) handling in Go, and a second opcode to set the
  caret. Not deleted — it remains the path if reading the caret is ever needed
  (see Consequences).

## Consequences

- First consumer: `play`'s Snippets tab (`renderSnippetsTab` →
  `pendingSnippetInsert` → `sqlTextEditField(...).InsertAtCursor(...)`). The tab
  is a sibling of the bottom body tabs, not of the editor, so the editor stays
  visible and the splice reads a real caret.
- The capability is generic — any `TextEdit` consumer can drive programmatic
  insertion (macro expansion, completions, templates) without new FFI work.
- No cursor *readback* to Go: callers still cannot read the caret, only insert
  at it. O3 remains the path if reading is ever needed.
- Extends the stateful-widget contract of
  [ADR-0013](0013-imzero2-stateful-widget-contract.md) with a second writer of
  egui's persisted `TextEditState`; complements the code-block action surface of
  [ADR-0026](0026-app-runtime-and-capability-subjects.md) (Copy → now also
  Insert).

## Status

Accepted 2026-05-31 by @spx. Implemented on both sides (Go binding + Rust interpreter), unit-tested (the char-indexed splice), and shipped in play's snippet library.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).
