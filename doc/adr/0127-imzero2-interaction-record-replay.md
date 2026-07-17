---
type: adr
status: proposed
date: 2026-07-17
---

> **Status: proposed — pre-human-review.** Not verified; do not cite as
> authoritative.

# ADR-0127: imzero2 interaction record/replay — semantic capture over the inspection seam

## Context

The desktop imzero2 host has the *replay* half of interaction record/replay
and none of the *record* half. Since egui 0.35 the `egui_inspection` plugin
ships in the desktop default build (env-gated by `EGUI_INSPECTION`, see
[the egui-mcp how-to](../howto/egui-mcp.md)): external tools read the
AccessKit tree and inject input over TCP, and `egui-mcp` exposes that as
locator-targeted agent tools. The protocol is strictly request/response —
`GetInfo`, `GetTree`, `GetScreenshot`, `ApplyEvents { Vec<egui::Event> }`,
`Resize` — so nothing today observes what a *human* does in the app.

Substrate facts, verified against the egui / `egui_inspection` 0.35.0
sources:

- **Plugin seam.** `egui::Plugin` provides `input_hook` (every raw input
  event, pre-processing) and an output hook seeing `FullOutput` including
  `PlatformOutput.events`. Plugins register per-`Context`
  (`ctx.add_plugin`), so the imzero2 client can add its own without forking.
- **Semantic effect stream.** Stock widgets push `OutputEvent::{Clicked, …,
  ValueChanged}` with a `WidgetInfo` (type, label, text, value) into
  `PlatformOutput.events` unconditionally (`Response::widget_info`).
  `WidgetInfo` does **not** carry the sender widget id.
- **Public interaction ids.** `Context::interaction_snapshot` exposes
  per-frame `clicked` / `long_touched` / `drag_started` / `dragged` /
  `drag_stopped` ids plus `hovered` / `contains_pointer` sets;
  `Memory::focused()` gives the keyboard target. Attribution needs no egui
  patch.
- **Coordinate-free replay verbs.** `ApplyEvents` carries arbitrary
  `egui::Event`s; egui honors injected `AccessKitActionRequest`s — `Click`,
  `Focus`, `SetValue` (`Slider`, `DragValue`), `ScrollIntoView`,
  `SetTextSelection`. Pointer synthesis at tree-resolved coordinates covers
  the rest.
- **Deterministic ids.** egui2's Go side computes widget ids as an XOR-fold
  of `xxh3(label)` / `splitmix64(seq)` hashes down the scope stack
  (`egui2/bindings/egui2_id_handling.go`); AccessKit node ids are the raw
  egui id value (`Id::accesskit_id`). A node id is a pure function of widget
  path — stable across runs and resolutions — and XOR (not ordinal) means
  unrelated sibling insertions shift nothing.
- **Coordinate frame.** Tree bounds, injected events and default screenshots
  share logical points.

Motivation, in decreasing weight:

1. **Demonstrations become agent scripts.** Agent live-verification via
   egui-mcp pays a per-session exploration tax; a human demonstration
   captured in the same locator vocabulary is a grounded navigation script,
   and raw material for LLM teach-in — traces as in-context examples, then
   distilled into parameterized, committed skill files.
2. **Flight-recorder repros.** Semantic events fire only on interaction, so
   an always-on ring buffer is cheap; a panic exports the last N steps as a
   deterministic repro (id-stack panics need exactly this history).
3. **Interaction regression tests** at near-zero authoring cost — the tour
   ([ADR-0057](./0057-demo-registry-and-drivers.md)) checks "renders right",
   replay adds "flows still work".
4. **Deterministic demo re-capture** at any resolution after cosmetic UI
   changes (feeds a planned demo-video pipeline).
5. **Reproducible interaction workloads** for frame-time A/B comparison with
   the existing metrics overlay.

End-user macros — the "AutoHotkey" reading — are not a driver: boxer's
answer to repetitive work is a programmable surface, and demand for UI
macros is unproven here.

## Design space (QOC)

**Question.** At what level are interactions recorded and replayed so that
recordings survive minor app changes and resolution changes?

**Options.**

- **O1** — raw input log: verbatim `egui::Event`s plus viewport size; replay
  via `ApplyEvents` unchanged.
- **O2** — semantic steps: attribute each gesture to an AccessKit node at
  record time; store dual-anchored steps (node id + role/label/ancestry +
  bounds-fraction offset + observed effect); resolve anchors against the
  live tree at replay time.
- **O3** — Go-side response log: instrument the generated widget wrappers;
  replay by injecting synthetic responses.
- **O4** — screen-level capture: coordinates + pixels (the AutoHotkey
  class).

**Criteria.** C1 robustness to app changes; C2 resolution / layout / scroll
independence; C3 fidelity (replay exercises the real input path and produces
only reachable states); C4 legibility to humans and LLMs (the teach-in
requirement); C5 implementation weight and fork-freedom.

**Resolution.** O2, with O1 retained *inside* it as a per-step raw annex
(recorder debugging; last-resort fallback for node-less widgets). Kill
reasons for O1/O3/O4 standalone in [§Alternatives](#alternatives).

## Decision

An imzero2-side recorder `egui::Plugin` plus an offline script emitter;
replay through the existing inspection seam. No egui fork, no protocol
change, no new network listener.

### SD1 — Recording seam: a second plugin beside the inspection plugin

A `RecorderPlugin` in the imzero2 client crate (`ctx.add_plugin`), env-gated
(working name `IMZERO2_RECORD=<path>`; Rust-owned like `EGUI_INSPECTION`,
so documented in a how-to rather than the ADR-0009 registry; naming open).
Taps, all public API: `input_hook` (raw gestures); the output hook
(`PlatformOutput.events`); `interaction_snapshot` + `Memory::focused()`
(attribution); AccessKit tree snapshots (anchor context, plus a
start-of-recording snapshot as the precondition record).

The recorder is passive — a local file, no listener — so the inspection
port's attack surface is unchanged. Recordings capture labels and values,
i.e. potentially real data: they are data artifacts, kept out of the repo
unless scrubbed.

### SD2 — Step vocabulary: dual-layer, egui-mcp-aligned

A recording is an ordered list of steps (`click`, `type`, `set_value`,
`drag`, `scroll`, `wait`, `note`) in JSONL (container choice open; JSONL
favors the ring buffer). Each step carries a **readable line** (role, label,
scope path, observed effect — the layer humans and LLMs read and
generalize, vocabulary aligned with the egui-mcp tools) and a **machine
block** (node id, ancestry ids, bounds-fraction offset, raw-event annex,
timing, effect values).

Coalescing at emit time: press+release → `click`; keystream → `type` with
final text; slider / drag-value gestures → `set_value` with the end value;
pointer trajectories collapse to endpoints. An annotation marker (hotkey,
binding open) inserts `note` steps — narration, in scope from v1, is the
counter to "traces show what, not why".

### SD3 — Attribution: snapshot ids first, tree hit-test second

Pointer gestures attribute via `interaction_snapshot` (egui's own
layer-aware hit test, so occlusion is handled); keyboard-driven changes via
`focused()`; hover/scroll targets and widget-less regions via hit-test
against the tree snapshot. Same-frame multi-source events (keyboard edit
plus pointer click in one frame) attribute heuristically by event type. An
upstream PR adding the sender id to `WidgetInfo` (small: stamp `self.id` in
`Response::output_event`) would delete the heuristic — worth filing, not a
prerequisite; no locally patched egui.

### SD4 — Replay: anchor ladder, semantic verbs first

Anchor resolution per step: exact node id → unique role+label →
ancestry-scoped label (+ ordinal) → bounds-fraction offset with a loud
warning. Execution prefers coordinate-free verbs (`ScrollIntoView`, then
AccessKit `Click` / `SetValue` / `SetTextSelection` via `ApplyEvents`), then
pointer synthesis at tree-resolved coordinates, then the raw annex (canvas
widgets). Heal-on-green: after a fallback rung matched and the recorded
effect verified, the replayer may rewrite the stale anchor — off by default
in CI, on interactively.

### SD5 — Waits and assertions

`ApplyEvents` acks only after a frame has applied the events, which removes
most wall-clock flake by construction. The emitter auto-inserts `wait` on
the next step's target (exists + enabled). Recorded effects double as
opt-in assertions: *navigate* mode ignores them, *verify* mode asserts
them. Postcondition capture beyond the target widget (tree diffing) is
deferred.

### SD6 — Replay executors, in adoption order

1. **Via egui-mcp** (day one, no new code): traces use the mcp vocabulary,
   so an agent replays them directly. This is also the teach-in surface:
   traces as in-context examples, distilled into parameterized committed
   skill files (demonstrate → distill → replay → verify).
2. **`app imzero2 replay <trace>`** (M3): a small Go client speaking the
   `egui_inspection` protocol (4-byte BE length + MessagePack), for
   deterministic, assertable, CI-able replay — desktop build under headless
   weston, as the tour already runs. The headless host cannot compile
   inspection, by design.

### SD7 — Non-goals and deferrals, recorded

- **End-user macro UI**: deferred until demand shows; the operator story
  stays programmable surfaces.
- **Model fine-tuning on traces**: descoped — the in-context / skill route
  captures the value, and the flight recorder accumulates the dataset
  passively regardless.
- **Canvas sub-targets**: painter-only widgets (timeline, treemap, world map
  internals) record as bounds-fraction offsets — resolution-proof, not
  data-layout-proof. The fix, per-widget AccessKit sub-nodes, is a separate
  accessibility track this ADR motivates but does not gate on.
- **Upstream protocol record channel**: candidate later contribution; v1
  writes locally.
- **Native dialogs / window-manager interactions**: the seam sees only
  egui.

### Milestones

- **M1** — `RecorderPlugin`: JSONL semantic log with dual layers, raw annex,
  start-of-recording tree snapshot, annotation marker; ring-buffer mode with
  export-on-panic.
- **M2** — script emitter: coalescing, dual anchors, auto-waits; a recorded
  demonstration replays via egui-mcp.
- **M3** — `app imzero2 replay`: Go protocol client, anchor ladder,
  navigate / verify modes; CI wiring under weston.
- **M4** — heal-on-green + the teach-in how-to (demonstrate → distill →
  replay → verify, skill file conventions).

## Alternatives

- **Raw input replay only (O1).** Encodes coordinates and wall-clock: breaks
  on any resolution / layout / scroll / ordering change, and is illegible to
  humans and LLMs. Kept only as the per-step annex.
- **Go-side response recording (O3).** Fabricated responses can encode
  states no user can reach (occluded or scrolled-away widgets), bypass the
  real input path, couple recording to the wrapper generator, and miss
  unpolled gestures. Possible later complement for headless logic tests;
  rejected as the mechanism.
- **Screen-level capture (O4).** Strictly dominated: a deterministic,
  introspectable tree exists; pixels add fragility and remove semantics.
- **Patch egui first (`WidgetInfo.widget_id`).** Closes only the narrow
  same-frame attribution gap; carrying boxer's first egui fork for it
  inverts cost/benefit. File upstream, adopt on release.
- **Gate on an upstream protocol extension.** No v1 consumer needs recording
  over the wire; gating on an external release cadence violates
  descope-over-gate.
- **OS-level automation (AT-SPI etc.).** Explored during the egui-mcp work
  and abandoned; the in-process AccessKit seam is strictly better here.

## Consequences

### Positive

- One artifact class serves five consumers: agent scripts / teach-in,
  repros, regression tests, demo capture, perf workloads.
- Fork- and patch-free: public egui API plus the shipped inspection seam.
- Anchors are pure functions of widget path, courtesy of the deterministic
  id stack.
- Recordings and taught skills are reviewable text, not opaque state.

### Negative

- Recorded regression tests rot on intentional UI change; the small-suite
  plus healing-ladder mitigation is a standing discipline cost.
- Desktop-only, permanently: CI replay requires the weston arrangement.
- A new maintenance surface against egui's still-young `Plugin` API.
- Attribution can mislabel rare same-frame multi-source events until (if
  ever) the upstream `WidgetInfo` id lands.
- Canvas ceiling: painter-only flows replay weakly and teach an LLM nothing
  generalizable until those widgets grow AccessKit sub-nodes.

### Neutral

- Recordings are best-effort across app versions: the ladder degrades
  gracefully, it does not guarantee replay.
- Recordings may embed real data; handled like any data artifact.

## Status

Proposed. Decision dialogue captured 2026-07-17; next step is review of the
SD carve and, if accepted, M1.

## References

- [How to drive imzero2 from an AI agent with egui_mcp](../howto/egui-mcp.md)
  — the existing seam this ADR builds on.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — demo registry, tour
  driver, and the headless-weston CI arrangement.
- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless
  host's separate input/pixel path.
- egui 0.35.0 sources: `plugin.rs`, `interaction.rs`
  (`InteractionSnapshot`), `response.rs` (`widget_info` / `output_event`),
  `id.rs` (`accesskit_id`), `context.rs` (AccessKit action handling),
  `widgets/slider.rs` / `widgets/drag_value.rs` (`SetValue`).
- `egui_inspection` 0.35.0 `protocol.rs` — the wire protocol.
- <https://github.com/rerun-io/kittest_inspector> — the `egui-mcp` server.
- Allen Cypher (ed.), *Watch What I Do: Programming by Demonstration*, MIT
  Press 1993 — the PbD generalization problem the teach-in path answers
  with an LLM.
- Playwright locators and auto-waiting — prior art for the anchor ladder
  and wait insertion.

### Related ADRs

- [ADR-0009](./0009-environment-variable-registry.md) — why `IMZERO2_RECORD`
  (Rust-owned) stays out of the generated env-vars doc.
- [ADR-0092](./0092-adr-overview-tool.md) — status/evidence tracking that
  will pick this ADR up.
