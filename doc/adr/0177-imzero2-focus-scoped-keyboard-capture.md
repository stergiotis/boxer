---
type: adr
status: proposed
date: 2026-08-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0177: Focus-scoped keyboard capture for imzero2 widgets

## Context

imzero2 has no way for a widget to read the keyboard. The whole surface is
three things, and none of them is a channel:

- `StateManager.GetModifiers()` — R17, the modifier bitfield, unscoped and
  non-consuming. It says Shift is down, never that a key was pressed.
- `fetchF1KeyPressed` — one hardcoded `(Modifiers::NONE, Key::F1)`
  `consume_key`, drained by a fetcher.
- `fetchCommandEnterPressed` — one hardcoded Ctrl/Cmd+Enter and
  Ctrl/Cmd+Shift+Enter pair, same shape.

The two shortcut fetchers are deliberately hardcoded. `fetchF1KeyPressed`'s own
comment states the position: "a single hardcoded binding rather than a
parametric 'any key' fetcher … Future runtime-level shortcuts (debugger,
command palette) would each add their own fetcher to keep the consumed-event
ownership explicit per binding." That reasoning holds for *runtime-global*
shortcuts, where there is exactly one owner and the fetcher is it.

It stops holding for a widget. A tree ([ADR-0176](./0176-native-tree-widget.md)
SD8) wants roughly ten keys — ↑ ↓ ← → Home End PageUp PageDown Enter Space —
and wants them only while it has focus. One fetcher per key would be ten
fetchers, each globally consuming, each stealing the key from every other tree
and from any focused `TextEdit`. `fetchF1KeyPressed` already carries the
warning this produces: "widgets inside an app should NOT poll, since they'd
race the carousel for the same consumed event."

Three further facts shape the design:

- **Focus is already readable.** `HAS_FOCUS` / `GAINED_FOCUS` / `LOST_FOCUS`
  are populated in `fenums.rs:62-64` and reach Go as
  `ResponseFlagsE.HasFocus()` and friends (`egui2_enums.go:136-143`). The
  ownership question needs no new plumbing — only something to hang it on.
- **Nothing can take or request focus.** `TextEdit.LockFocus` is the only
  focus-related op. There is no `requestFocus`, and no way to make a block or
  a canvas focusable at all.
- **Fetchers cannot do the scoping.** Per the `imzero2-fetchers` skill they run
  only from `StateManager.Sync()` at frame end, after every widget has rendered
  and every deferred-block buffer has flushed. A `consume_key` there has no
  widget context to be scoped by. `fetchCommandEnterPressed` documents the
  consequence from the other side: plain Enter "would NOT be safe here — it is
  consumed by the TextEdit before any fetcher runs."

There is a precedent for exactly this shape of problem.
[ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md) faced it for the mouse
wheel: a canvas that should own the wheel only while hovered, and should fence
egui-native `ScrollArea`s out of the same gesture. Its answer was a
**hover-gated, consuming capture** declared per widget (`paintCanvas`
`.CaptureScroll()` / `.CaptureZoom()`), pushing into a per-id register (R23),
read back as `StateManager.GetCanvasWheel(handle)`. Keyboard is the same
problem with focus substituted for hover.

## Design space (QOC)

**Question.** How does a widget read keys without stealing them from every
other widget?

**Options.**

- **O1** — One hardcoded fetcher per key, extending today's pattern.
- **O2** — One global fetcher returning every unconsumed key event this frame;
  Go arbitrates using the focus flags it already has.
- **O3** — Focus-gated, consuming capture declared per widget with a key mask,
  pushed to a per-id register — the ADR-0140 analogue.
- **O4** — Delegate to egui: make each row a focusable widget and rely on
  egui's built-in focus navigation.

**Criteria.**

- **C1 — Ownership.** Two trees on screen, or a tree beside a focused
  `TextEdit`: does exactly one act on the key?
- **C2 — Fencing.** Does ↑/↓/PageUp/PageDown reach the widget without also
  scrolling the enclosing `ScrollArea`?
- **C3 — Virtualisation.** Does it survive only the visible rows existing?
- **C4 — Breadth.** Does it stay sane at ten keys, and at a second adopter?
- **C5 — New generated surface.** How much IDL, register and Rust.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ |
| C2 | −  | −− | ++ | +  |
| C3 | +  | +  | ++ | −− |
| C4 | −− | +  | ++ | −  |
| C5 | +  | ++ | −  | ++ |

O2 fails C2 outright: a fetcher at frame end cannot consume on behalf of a
widget that already rendered, so the enclosing `ScrollArea` has scrolled by the
time Go sees the key. O3 is the only option positive on C1 and C2 together, and
it pays for it in C5 alone.

## Decision

We will add a **focus-scoped, consuming key-capture primitive**, modelled on
ADR-0140's wheel capture: a widget declares the keys it wants; the interpreter
consumes exactly those, and only while that widget has focus; the captured
events land in a per-id register that Go reads back by widget handle.

### Subsidiary decisions

- **SD1 — Focus is gated on the widget's own egui response, at apply time.**
  The same shape as ADR-0140's `resp.contains_pointer()` gate, with
  `resp.has_focus()` in its place. Capture happens in apply code, inside the
  widget's own scope, not in a fetcher — that is what makes it scopable at all.

- **SD2 — Capture is consuming, and that is the point.** `consume_key` removes
  the event from egui's input queue, so an enclosing `ScrollArea` does not also
  scroll on ↑/↓/PageUp/PageDown and a sibling widget does not double-handle it.
  This mirrors ADR-0140's `.CaptureScroll()` zeroing `smooth_scroll_delta`. A
  widget that captures nothing pays nothing and changes no behaviour.

- **SD3 — The widget declares a key mask, rather than capturing everything.**
  This is the parametric form of the existing per-binding ownership stance, not
  a departure from it: the widget states what it eats, so `F1` and Ctrl+Enter
  keep reaching their runtime-level owners even while a tree has focus. A
  capture-all primitive would silently break both the moment any widget adopted
  it.

- **SD4 — A shared key vocabulary crosses the FFI as a `u8` code.** Nothing
  today maps `egui::Key` into Go — there is no key enum in the bindings at all.
  This ADR defines one, deliberately as a **subset**: the navigation and
  activation keys widgets need (↑ ↓ ← → Home End PageUp PageDown Enter Space
  Escape Tab Backspace Delete), not a transcription of `egui::Key`. A subset is
  a registry we can extend on demand; a full mirror is a maintenance obligation
  against an upstream enum for keys nobody has asked for. The Go constants and
  the Rust match arm are generated from one IDL-side table so they cannot drift.

- **SD5 — Modifiers ride each event; the mask matches modifier-agnostically.**
  A tree wants ↓ and Shift+↓ to be the same capture
  with a different meaning, so the mask names keys and the event carries the
  modifier byte. This deliberately differs from `consume_key`'s
  `matches_logically`, whose extra-modifier tolerance is what forced
  `fetchCommandEnterPressed` to register Shift-first; matching on the key alone
  and reporting modifiers sidesteps the ordering hazard rather than
  re-encountering it per adopter.

- **SD6 — The register is R26, per widget id, drained each frame.** The next
  free number after R25 (etable column widths). Shape follows R23: parallel
  arrays of `(id, keyCode, modifiers)`, one row per captured event, read via
  `StateManager.GetCapturedKeys(handle)`. Unlike R15's retained cameras this is
  a per-frame drain — a key press is an event, not a state, and replaying last
  frame's would repeat it.

- **SD7 — `requestFocus(id)` is part of this decision, not a follow-on.**
  Capture is useless if nothing can take focus: clicking a row must focus the
  tree. The op is small, but it carries a trap worth stating — an imzero2
  widget id is only the r7 read-back key, since egui allocates its own
  interaction id from `ui.next_auto_id()`. `request_focus` on an id egui never
  registered silently does nothing. It therefore works only against a widget
  registered through SD8's interact rect.

  Implementation found a second silent-failure path in the same family, and it
  cost more to find than the one above. The op must reach memory through the
  interpreter's `egui::Context`, **not** the outer `Ui`: focus lives in
  `Memory`, which hangs off the Context, so a `Ui` is not needed — and reaching
  it as `ui.ctx()` means writing the body under `if u.is_some()`, which turns
  every dispatch where the interpreter holds no `Ui` into a no-op with nothing
  logged. `interpret_inner`'s Context parameter is never optional, so that form
  cannot be skipped. Both spellings compile, vet clean, and differ only in
  whether focus ever moves.

  A consequence for adopters: **the `GAINED_FOCUS` / `LOST_FOCUS` edges are not
  reliable when focus moves programmatically.** `requestFocus` is applied at
  the end of a pass, after the widget has already run, and egui snapshots
  `id_previous_frame` from the focus state at the end of that same pass — so by
  the time the widget next runs it "always had" focus and `gained_focus()` is
  false. Clicking does produce the edge, because that request happens during
  the widget's own interaction. A consumer that keys off the edge flags
  therefore works when a user clicks and silently misses every code-driven
  focus change. Derive transitions from the `HAS_FOCUS` level instead; the
  `focus` demo does.

- **SD8 — Focusability is opt-in via an overlay interact rect.** A widget that
  wants focus registers `ui.interact(rect, {{Id}}, Sense::click())` after its
  body, the same post-body overlay pattern `HoverText` and the `Frame`
  sense-click already use. This is required for containers: `egui_table`'s
  `Table::show` returns `ui.scope_builder(…).response` (`table.rs:444`), which
  is hover-sense only and, registered at child-ui construction, sits *behind*
  its own cells in hit-test order. It cannot take focus.

- **SD9 — One focus stop per widget, not per row.** A container that captures
  keys is a single tab stop and moves its own internal cursor. Making each row
  focusable and leaning on egui's navigation (O4) breaks under virtualisation:
  only visible rows exist, so egui's focus order changes as you scroll and Tab
  walks a set that is a function of scroll position. This is a constraint the
  capture design inherits from ADR-0176 SD4, and it is the reason O4 is
  rejected rather than a side effect of rejecting it.

- **SD10 — Text input and type-ahead are out of scope.** Jump-to-typing wants
  `egui::Event::Text`, a different event class with its own IME, composition
  and repeat semantics. Keys are not characters, and conflating them would put
  a half-working text channel in a keyboard-navigation ADR. Deferred until a
  widget asks.

### Milestones

- **M0 — Key vocabulary + `requestFocus` + focusable interact rect.** ✓ SD4's
  generated table, SD7's op, SD8's `.Focusable()` method. No register yet;
  verifiable on its own by focusing a widget and reading `HasFocus()` back.
  Shipped with `surrenderFocus` alongside `requestFocus` — without it a widget
  that takes focus on click cannot give it back on Escape except by requesting
  focus on some other id it may not have. Demo `focus`; a headless trace
  asserts click-to-focus, `RequestFocus` and `SurrenderFocus`.
- **M1 — R26 capture register + fetcher + `GetCapturedKeys`.** The capture
  path end to end, with a gallery demo that echoes captured keys.
- **M2 — Fencing verification.** A demo placing a capturing widget inside a
  `ScrollArea` and asserting ↑/↓ do not scroll the parent — the observable
  SD2 exists for.
- **M3 — Tree adoption.** ADR-0176's widget moves its cursor from captured
  keys, with `ScrollToRow` following it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL — new `requestFocus` proc, new `.Focusable()` / `.CaptureKeys(mask)` methods | added | regenerated `bindings/factories.out.go` + `methods.out.go` + `enums.out.go`, regenerated region of `interpreter.rs`; Go binary and Rust renderer rebuilt in the same commit |
| egui2 IDL — new `fetchR26KeyCaptures` fetcher | added | `bindings/fetchers.out.go`, `StateManager.Sync` drain, `GetCapturedKeys` |
| Interpreter register file | added: `r26_key_*` parallel arrays | `prepare_next_frame` clear arm; the register table in the imzero2 skill §13.1 |
| Named registry — the key-code vocabulary (SD4) | added | Go constants and the Rust match arm, both generated from the IDL table |
| Exported Go API under `public/` | added: `StateManager.GetCapturedKeys`, key-code constants | nothing downstream yet |

## Alternatives

- **A fetcher per key (O1).** Ten fetchers for one widget, each globally
  consuming and each stealing from every other adopter. The pattern is right
  for a runtime-global shortcut with exactly one owner and wrong for a widget.
- **A global unconsumed-key fetcher (O2).** Cheapest to build and the tempting
  option, since Go already has the focus flags to arbitrate with. Rejected on
  fencing: a fetcher runs at frame end, so by the time Go could decide the key
  was "for the tree", the enclosing `ScrollArea` has already scrolled on it.
  Consumption has to happen where the widget is, or it is not consumption.
- **Per-row focus via egui's own navigation (O4).** Free, and correct for a
  short static list. Rejected because it is incompatible with the
  virtualisation ADR-0176 is built on — see SD9.
- **Mirror `egui::Key` wholesale.** Rejected as an unbounded maintenance
  obligation against an upstream enum, for keys no widget has asked for; SD4's
  subset is extendable when one does.

## Consequences

### Positive

- A widget can own the keyboard while focused without stealing from anything
  else, which is what makes ADR-0176's keyboard navigation possible at all.
- The fencing problem is solved once rather than per adopter — an
  arrow-key-driven widget inside a `ScrollArea` stops fighting its parent.
- `requestFocus` and `.Focusable()` are independently useful: any widget that
  wants a focus ring or click-to-focus gets them, not just trees.
- The existing global shortcuts keep working unchanged, because SD3's mask
  means a capturing widget declares what it takes.

### Negative

- Another per-id register and fetcher, with the drain cost and the
  `prepare_next_frame` clear arm that implies. R26 is the fourth of this shape
  (R21, R23, R24, R25) and the pattern is not getting cheaper.
- SD4's vocabulary is a registry, so it acquires the usual obligation: adding a
  key is a change in two generated places and a rebuild of both FFI sides.
- Capture is one frame lagged, like every register — pressed during render N,
  drained at end of N, read in N+1. Imperceptible at interactive cadence, but
  it means a widget cannot make a same-frame decision from a keypress.
- A consuming capture is invisible to the widget it takes from. A key that
  "does nothing" because a focused sibling declared it in its mask is a
  plausible future bug report with no obvious log line.

### Neutral

- This does not give imzero2 keyboard shortcuts in general — no chords, no
  command palette, no rebinding. It gives a focused widget its own keys.
- Autorepeat comes free: egui delivers held keys as repeated events, so a held
  ↓ produces one captured event per frame with no extra machinery.

## Migration — Tier 1

- **Breaks.** Nothing. Every addition is new surface; `GetModifiers`,
  `fetchF1KeyPressed` and `fetchCommandEnterPressed` are untouched and keep
  their current semantics.
- **Path.** Nothing to migrate. A widget opts in by calling `.Focusable()` and
  `.CaptureKeys(mask)`; one that does neither is unaffected, including in
  whether its keys are consumed.
- **Regeneration.** `app egui2gen generate` at M0 and M1. Both are
  FFI-boundary changes: rebuild the Go binary **and** the Rust renderer
  together, or the stream desynchronises.
- **Old shape.** The two hardcoded shortcut fetchers stay. They serve
  runtime-global owners with no focused widget to hang on, which is the case
  this primitive does not cover — F1 must work whatever has focus.

## Verification plan — Tier 1

- **Lane.** The gallery demo from M1 plus a headless scene
  ([ADR-0154](./0154-headless-carrier-tree-and-driver.md)) driving keys through
  the `type` / `press_key` path; default `go test` for the mask encode/decode
  and the key-code table.
- **What would fail.** Three distinct observables, because the failure modes
  differ: a broken vocabulary or mask fails the Go unit test; a broken focus
  gate shows as the M1 demo echoing keys while unfocused (or not echoing while
  focused); a broken *consume* shows only as M2's parent `ScrollArea` scrolling
  when it should not — which is why M2 is a milestone rather than a note, since
  nothing else in the plan would catch it.
- **Gap.** Two adopters competing for focus is not covered — the scenes have
  one capturing widget. The multi-adopter case is where SD1's gate would be
  wrong in a way single-widget tests cannot see, and it is worth a scene once a
  second widget adopts. Autorepeat timing and IME behaviour are untested;
  the latter is out of scope per SD10.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md) — the hover-scoped consuming wheel capture this mirrors, register and all.
- [ADR-0176](./0176-native-tree-widget.md) — the first adopter; its SD8 defers here and its SD2 carries the cursor this moves.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the headless driver the verification plan uses.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) — why apply-time capture and frame-end drains are one frame apart.
