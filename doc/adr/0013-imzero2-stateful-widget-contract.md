---
type: adr
status: proposed
date: 2026-04-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0013: ImZero2 — Stateful-Widget API Contract

## Context

ImZero2 widgets fall into two interaction classes: **event-only** (Button, NodeLeaf, SelectableLabel, Hyperlink) which emit a click signal but hold no internal state Go-side, and **stateful** (Checkbox, RadioButton, Slider, DragValue, TextEdit, DatePickerButton) which round-trip a typed value back to a Go-side `*T` via the FFFI2 r9_*/r10 databindings. The two classes need different orchestrators on the Go side (`SendResp() ResponseFlagsE` vs `SendRespVal(*T) ResponseFlagsE`) and different apply-block shapes on the Rust side (plain `apply_widget` vs `apply_widget` followed by a gated push).

The contract was implicit until [commit 7a664db9](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_codeblocks.go) — captured by convention rather than by tooling. RadioButton drifted: its spec at [`egui2_definition_d_widgets.go:251`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go) hand-rolled the apply block via `ir.MergeVerbatimCode` instead of routing through the standard `applyCodeWidgetRustOnChange` helper. The hand-rolled form omitted the `if resp.is_some() && resp.unwrap().changed() { … }` gate, so the r10 push ran unconditionally every frame writing `checked || clicked` to the bound bool.

This interacted with a second, pre-existing quirk: `StateManager.Sync()` at [`egui2_statemanagement.go:244`](../../src/go/public/thestack/imzero2/egui2/bindings/egui2_statemanagement.go) populates `responseFlags` via `UpsertBatch` and never clears the map between frames. Entries persist until overwritten. Buttons self-heal on hover (response carries hover flag → r7 push → upsert overwrites with current frame's flags), but a RadioButton whose cursor moves away after a click receives no further r7 entries, so `responseFlags[id]` retains `PrimaryClicked` indefinitely. `RadioButton(...).SendResp().HasPrimaryClicked()` consequently returned true on every subsequent frame, with no user input.

The fix has to land at the *contract* level, not just for RadioButton: the same drift can recur for any future stateful widget whose spec author hand-rolls the apply block. Codifying the contract (and enforcing it at codegen-time) is the only durable defence.

## Decision

We adopt a three-part contract for stateful widgets, enforced by tooling:

1. **Parametric gated-apply helper.** `applyCodeWidgetRustOnEvent(hasId bool, event respEventE, onEventCode ir.VerbatimCodeI)` replaces the prior `applyCodeWidgetRustOnChange`. The new `respEventE` typed-string admits `respEventChanged`, `respEventClicked`, and is open to extension (`gainedFocus`, `dragStarted`, …) without further helper proliferation.
2. **Go-side method shape.** Every stateful widget exposes `SendRespVal(*T) ResponseFlagsE` in `egui2_methods.go`, identical in shape to `CheckboxFluid.SendRespVal(*bool)`: queue the opcode, register the r-slot databinding, return the response-flag cache. Event-only widgets keep `SendResp() ResponseFlagsE` (no databinding registration). RadioButton's previous `SendResp()` is removed.
3. **Drift guard.** `TestStatefulWidgetsAreGated` walks `definitionsWidget()` and asserts every spec whose apply code emits `r10_push` / `r9_*_push` also contains the gate substring `if resp.is_some() && resp.unwrap().` — i.e. went through `applyCodeWidgetRustOnEvent`. Hand-rolled apply blocks that bypass the helper fail at `go test` time, before any rendering bug surfaces.

For RadioButton specifically, the apply block now reads:

```go
WithApplyCodeClientRust(applyCodeWidgetRustOnEvent(true, respEventClicked,
    rustClientCode("self.r10_push({{Id}}.value(), true);\n"))).
```

The push value is the literal `true`: inside the `clicked()` gate the radio is by definition newly selected, so the previous `checked || clicked` collapses to `true`. The bound `*bool` becomes `true` on the click frame and the user reads the rising edge against the current `walkersTileSrcIdx == i` predicate (or analogous). The walkers tile-server selector at [`egui2_hl_walkers_demo.go:225`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_walkers_demo.go) is the canonical migration: a persistent `walkersRadioBound []bool` plus a two-pass loop (edge-detect, then render) that avoids the one-frame visual artifact when selection changes.

## Alternatives

- **Sibling-per-event helpers (`applyCodeWidgetRustOnChange`, `applyCodeWidgetRustOnClicked`, …).** Rejected: known second event (`clicked`) already justifies parametric form; future events (`gainedFocus`, `dragStarted`) would multiply the helper count without improving safety. The typed `respEventE` keeps callers compile-time-checked just as well.
- **Fix `responseFlags` stickiness in `StateManager.Sync()`.** Tempting, since the stickiness is itself a latent bug that masks the radio symptom. Rejected as the primary fix because (a) it does not address the structural drift in apply-block specs, (b) clearing `responseFlags` between frames may break consumers that read responses lazily across the frame boundary, and (c) the stateful contract is independently necessary regardless of cache lifecycle. The cache stickiness can be addressed separately if and when it surfaces in another widget.
- **Encode interaction class as a builder option (`WithStatefulPush[T](rSlot, BoundExpr, GateExpr)`).** Rejected for now as over-abstracted: the surface area is five-ish stateful widgets, and the apply-code DSL already composes verbatim Rust strings. A first-class option introduces a parallel mini-IR for one corner of the spec and obscures rather than clarifies the underlying apply-block shape.

## Consequences

### Positive

- The historical RadioButton spurious-fire is gone at the source: the r10 push only fires on `.clicked()`, and the bound `*bool` carries a clean rising edge readable across frames.
- Future stateful widgets cannot drift the contract silently: the drift guard rejects any spec that emits a state push outside an event gate, at codegen time. The next "I'll just MergeVerbatimCode it inline" attempt fails CI.
- The parametric helper extends naturally to other egui response events (`gained_focus`, `drag_started`, etc.) without API churn — add a `respEventE` constant, done.
- Symmetry between Checkbox and RadioButton at the API surface (`SendRespVal(*bool) ResponseFlagsE` for both) reduces the cognitive load identified in the broader imzero2 contract analysis: stateful widgets now share a single mental model.

### Negative

- Callers reading clicks via `RadioButton(...).SendResp().HasPrimaryClicked()` break at compile time and must migrate to `SendRespVal(&bound)` plus rising-edge detection. This is intended (the previous form was structurally broken), but is a hard break for any out-of-tree consumer.
- The radio-group idiom requires a persistent bound slice indexed by option, not a single shared pointer. This is more code than `if Foo().SendResp().HasPrimaryClicked() { selected = i }` and is the natural cost of an honest signal: the previous one-liner was reading latched cache, not user input.
- The `responseFlags` stickiness remains. `HasPrimaryClicked()` on stateful widgets is still unreliable as a click signal; users must rely on the bound value, not the response flags. We accept this for now; an ADR-0014 successor may revisit.

### Neutral

- The `SendResp()` / `SendRespVal()` method-name split now mirrors a real type-level distinction (event-only vs stateful). Newcomers must learn the two forms; the drift test and the migration in `egui2_hl_walkers_demo.go` serve as the executable specification.
- A future `RadioGroup[T comparable]` helper (deferred from this ADR) would abstract the persistent-bound-slice + rising-edge pattern for radio groups specifically. Out of scope here — the primitive is now correct; an ergonomic wrapper can land independently.

## Status

Proposed — awaiting review by @stergiotis.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- Commit landing the contract: `7a664db9` — *fix(imzero2): gate RadioButton state push on .clicked() event*.
- Helper: [`egui2_definition_d_codeblocks.go`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_codeblocks.go).
- Drift guard: [`egui2_definition_d_widgets_test.go`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets_test.go).
- Migration example: [`egui2_hl_walkers_demo.go`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_walkers_demo.go).
- Related: [ADR-0059 — declarative layouting](0010-imzero2-declarative-layouting-over-visual-builder.md), [ADR-0012 — collapsible retained bodies](0012-imzero2-collapsible-retained-bodies.md).
