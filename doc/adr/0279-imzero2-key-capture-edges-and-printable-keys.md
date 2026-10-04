---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0279: Key releases, repeats and printable keys in imzero2 key capture

## Context

[ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) gave a focused
widget its own keys: a mask declares them, the interpreter consumes those
key presses while the widget has focus, and register R26 hands them to Go
one frame later. It was shaped for navigation. Its vocabulary (§SD4) holds
the fourteen keys a tree or a contact sheet needs, and it reports presses
only, because for those widgets a key is an event, not a state.

A downstream adopter needs more than that. shadow-boxer's HP 48GX emulator
(its ADR-0017, proposed, §SD8 and §M3a) draws the calculator in a window.
The calculator scans a key matrix and acts on what is *down*, so it needs
to know when a key goes up as well as when it goes down. It also needs the
keys a person types numbers and names with, not only arrows and Enter. With
presses alone the emulator can only hold each key for a fixed time. That
misreads a key held on purpose (auto-repeat, a game polling the matrix),
and the PC keyboard cannot type a digit at all.

Three properties of the existing design bound the change:

- **Adopters expect presses only.** The tree widget and the image viewer act
  on every captured event. If releases arrived in their captures, each
  keystroke would act twice. Whatever adds releases must leave a widget that
  does not ask for them exactly as it is.
- **The mask is 64 bits, and code 0 is reserved.** Fourteen codes are taken.
  §SD4 already chose an extendable subset over a mirror of `egui::Key`, so
  the question is which keys, not whether.
- **Keys are not characters (§SD10).** `*` on a US layout is Shift+8, and
  egui 0.35 has no `Asterisk` key. A capture of physical keys with
  modifiers is what §SD10 permits; a text channel is still out of scope.

## Decision

We will extend ADR-0177's capture in two ways, both opt-in at the point of
use:

- **SD1 — Edges are reported to a widget that asks for them.** A new Frame
  method `.captureKeyEdges()` makes the capture take key releases as well as
  presses, consuming both. R26 gains a fourth column, an edge byte per event:
  bit 0 set for a press, clear for a release, and bit 1 set for an
  auto-repeat press. A widget that does not call `.captureKeyEdges()`
  receives what it received before, presses including repeats, now with the
  edge byte filled in. In Go, `CapturedKey` carries the byte, read through
  `Down()`, `Up()` and `Repeat()`.
- **SD2 — The vocabulary gains digits, letters and a short list of
  punctuation.** These are codes 15–60: `Num0`…`Num9`, `A`…`Z`, and `Plus`,
  `Minus`, `Equals`, `Period`, `Comma`, `Slash`, `Colon`, `Quote`,
  `OpenBracket`, `CloseBracket`. Codes 61–63 stay free. They are physical
  keys, named after egui's variants and generated from the one Go table
  as before. A shifted character such as `*` arrives as its key with Shift
  in the modifier byte, and mapping it is the adopter's business. A mask
  that names none of the new codes captures exactly what it captured
  before.
- **SD3 — Focus loss is the adopter's release.** A key released after the
  widget lost focus is not captured, because capture is gated on focus
  (ADR-0177 §SD1). A widget that tracks held keys must treat losing focus
  as every key going up. This ADR states that rather than synthesising
  releases. The interpreter does not know which keys a widget considers
  held, and a synthetic event would be a second, quieter way for a key to
  go up.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL — Frame `.captureKeyEdges()` | added | regenerated `methods.out.go`, interpreter region, API reference |
| egui2 IDL — `fetchR26KeyCaptures` | a fourth return column, `edges` | `fetchers.out.go`, `StateManager.Sync`'s R26 drain, the interpreter's fetcher body |
| Interpreter register R26 | `r26_key_capture_edges` added; `r26_key_capture_push` takes it | `prepare_next_frame`'s clear arm, the TextEdit capture path |
| Named registry — the key-code vocabulary (ADR-0177 §SD4) | codes 15–60 added | `keycodes.Table`, the generated `keycodes.rs`; the drift test |
| Exported Go API | `CapturedKey.Edges`, `Down`, `Up`, `Repeat`; new key-code constants | none breaks |

## Alternatives

- **Releases for every capturing widget.** Simpler, one code path, but
  every existing adopter would act on each key twice until changed. A
  per-widget opt-in costs one flag in the construction code.
- **Held-key state instead of edge events** (a register of keys down, read
  each frame). It reads well for a matrix, but it loses presses shorter
  than a frame, which a fast typist produces. Edges carry both, and a
  widget that wants the state folds them.
- **A text channel** (`egui::Event::Text`). It delivers `*` and layout-aware
  characters, but has no release and brings IME and composition with it
  (ADR-0177 §SD10). It is right for typing text, wrong for a key matrix.
- **Widening the mask to 128 bits.** That would make room for every
  `egui::Key` at once, against §SD4's subset. It can come when the three
  spare codes run out.
- **Synthesising releases on focus loss.** Rejected in SD3.

## Consequences

### Positive

- A widget can follow a key matrix exactly: down, auto-repeat, up.
- Digits, letters and the common punctuation can be captured, so a focused
  widget can take typing that is not text.
- Existing adopters are untouched, whether or not they recompile.

### Negative

- R26 carries a fourth column, and the fetcher's wire shape changes. As with
  any FFI change, the Go host and the Rust client must be rebuilt together.
- The vocabulary is nearly full. The next addition beyond three keys is a
  change to the mask's width.
- Focus loss is a release the adopter must remember. A widget that forgets
  leaves a key held after the person clicked elsewhere.

### Neutral

- Modifiers still ride each event as ADR-0177 §SD5 defines them, so the
  layout-dependent mapping of shifted characters stays with the adopter.

## Migration — Tier 1

- **Breaks.** Nothing at the Go API: the existing fields and methods keep
  their meaning. The fetcher's wire shape changes, so a Go host and a Rust
  client built from different sides of this change do not interoperate.
- **Path.** Regenerate (`app egui2gen generate rust|go|doc`) and rebuild the
  client in the same step.
- **Regeneration.** Both sides of the FFI.
- **Old shape.** None kept: the column is added for every event, and widgets
  that do not opt in see presses only.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the vocabulary's drift test rebuilds
  `keycodes.rs` from the table, and a test pins the edge-byte accessors and
  the mask bits of the new codes.
- **What would fail.** A code that drifts between the Go table and the Rust
  match fails the drift test. A wrong edge decoding fails the accessor test.
- **Gap.** The interpreter's consume-and-push of releases runs only with a
  live client. The adopter's integration (shadow-boxer's astrolabe) is where
  it is first seen working, and a headless scene driving key-up events is
  worth adding when the scene driver can send them.

## Status

Proposed — awaiting review by the code owner.

## References

- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — the capture this extends; §SD4 vocabulary, §SD6 R26, §SD10 text out of scope.
- [ADR-0214](./0214-launcher-as-app-summary-detail-recall.md) §SD9 — the TextEdit capture path that shares R26, added for the launcher.
