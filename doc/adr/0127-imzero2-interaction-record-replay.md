---
type: adr
status: proposed
date: 2026-07-17
---

> **Status: proposed — pre-human-review.** Not verified; do not cite as
> authoritative.

# ADR-0127: imzero2 interaction record/replay — semantic capture over the inspection seam

## Context

The desktop imzero2 host already contains the *replay* half of an interaction
record/replay facility, none of the *record* half. Since the egui 0.35 upgrade,
the `egui_inspection` plugin ships in the desktop default build (env-gated by
`EGUI_INSPECTION`, see [the egui-mcp how-to](../howto/egui-mcp.md)): an external
tool can read the AccessKit tree and inject input over a TCP port, and the
`egui-mcp` server exposes that as agent tools (click / type / drag / scroll /
screenshot / `wait_for`) with locator-based targeting. The protocol is strictly
request/response — `GetInfo`, `GetTree`, `GetScreenshot`,
`ApplyEvents { Vec<egui::Event> }`, `Resize` — so nothing today observes what a
*human* does in the app.

Substrate facts, verified against the egui / `egui_inspection` 0.35.0 sources,
that bound the design:

- **Plugin seam.** `egui::Plugin` provides `input_hook` (every raw input event,
  before processing — the same hook the inspection plugin injects through) and
  an output hook that sees `FullOutput`, including `PlatformOutput.events`.
  Plugins are registered per-`Context` (`ctx.add_plugin`); the imzero2 client
  constructs the eframe app and can register its own plugin without forking
  anything.
- **Semantic effect stream.** Stock widgets push
  `OutputEvent::{Clicked, DoubleClicked, TripleClicked, FocusGained,
  ValueChanged}` carrying a `WidgetInfo` (type, label, current/previous text,
  value) into `PlatformOutput.events` unconditionally (`response.rs`,
  `Response::widget_info`). `WidgetInfo` does **not** carry the sender widget
  id.
- **Public interaction ids.** `Context::interaction_snapshot` is public and
  exposes per-frame `clicked`, `long_touched`, `drag_started`, `dragged`,
  `drag_stopped` (each `Option<Id>`) plus `hovered` / `contains_pointer`
  id-sets (`interaction.rs`). `Memory::focused()` gives the keyboard target.
  Event→widget attribution therefore needs no egui patch.
- **Coordinate-free replay verbs.** `ApplyEvents` carries arbitrary
  `egui::Event`s, and egui honors injected `AccessKitActionRequest`s:
  `Click` and `Focus` by widget id, `SetValue` on `Slider` and `DragValue`,
  `ScrollIntoView`, `SetTextSelection`. Pointer synthesis at tree-resolved
  coordinates remains available for everything else.
- **Deterministic ids.** egui2's Go side computes every widget id itself — an
  XOR-fold of `xxh3(label)` / `splitmix64(seq)` hashes down the scope stack
  (`egui2/bindings/egui2_id_handling.go`) — and AccessKit node ids are the raw
  egui id value (`Id::accesskit_id`). A tree node id is a pure function of the
  widget's path: identical across runs, resolutions and machines, and XOR
  (not ordinal) means unrelated sibling insertions shift nothing.
- **Coordinate frame.** Tree bounds, injected events and default screenshots
  all use logical points; `pixels_per_point` is reported per tree snapshot.

Why record at all (motivation, in decreasing weight):

1. **Demonstrations become agent-executable scripts.** Agent-driven live
   verification through egui-mcp currently pays a per-session exploration tax
   (tree queries, guessed navigation). A human demonstration captured in the
   same locator vocabulary is a grounded navigation script — and, further, raw
   material for LLM teach-in: traces as in-context examples, then distilled
   into parameterized, committed skill files (demonstrate → distill → replay →
   verify).
2. **Flight-recorder repros.** Semantic events fire only on actual
   interaction, so an always-on ring buffer is cheap; a panic exports the last
   N steps as a deterministic repro. imzero2's characteristic failures (e.g.
   id-stack mismatches that panic only at render) need exactly this
   interaction history.
3. **Interaction regression tests** with near-zero authoring cost, extending
   the screenshot tour ([ADR-0057](./0057-demo-registry-and-drivers.md)) from
   "renders right" to "flows still work".
4. **Deterministic demo re-capture** — record a walkthrough once, re-capture
   at any resolution after cosmetic UI changes (feeds a planned demo-video
   pipeline).
5. **Reproducible interaction workloads** for frame-time A/B comparison with
   the existing metrics overlay.

End-user macros — the "AutoHotkey" framing — are deliberately *not* a driver:
boxer's answer to repetitive work is a programmable surface (a play query, a
CLI), and demand for UI macros is unproven here.

## Design space (QOC)

**Question.** At what level should interactions be recorded and replayed so
recordings survive minor app changes and resolution changes?

**Options.**

- **O1** — raw input log: record `egui::Event`s verbatim plus the viewport
  size; replay via `ApplyEvents` unchanged.
- **O2** — semantic steps: attribute each gesture to an AccessKit node at
  record time; store dual-anchored steps (node id + role/label/ancestry +
  bounds-fraction offset + observed effect); resolve anchors against the live
  tree at replay time.
- **O3** — Go-side response log: instrument the generated widget wrappers to
  record polled responses ("widget X reported clicked"); replay by injecting
  synthetic responses into the app.
- **O4** — screen-level capture: coordinates + pixels (the AutoHotkey class).

**Criteria.**

- **C1** — robustness to app changes (renames, moved widgets, added siblings).
- **C2** — resolution / layout / scroll-position independence.
- **C3** — fidelity: replay exercises the real input path (hit-testing,
  occlusion, focus), producing only states a user could produce.
- **C4** — legibility: humans and LLMs can read, edit and generalize a
  recording (the teach-in requirement).
- **C5** — implementation weight and fork-freedom (no egui patch, no protocol
  change, no codegen coupling).

**Resolution.** O2 on all criteria except weight, where it is moderate rather
than minimal; O1 is retained *inside* O2 as a raw annex per step (cheap to
keep, useful for debugging the recorder and as the last-resort fallback for
node-less widgets). O1 alone fails C1/C2/C4 — it is exactly the AutoHotkey
failure mode. O3 fails C3 structurally (fabricated responses can encode
impossible states) and couples recording to the egui2 code generator. O4
fails every criterion but implementation familiarity. Kill details in
[§Alternatives](#alternatives).

## Decision

Build a recorder as an imzero2-side `egui::Plugin` plus an offline script
emitter, and replay through the *existing* inspection seam. No egui fork, no
protocol change, no new network listener.

### SD1 — Recording seam: a second plugin beside the inspection plugin

A `RecorderPlugin` in the imzero2 client crate, registered via `ctx.add_plugin`
next to eframe's inspection plugin, env-gated (working name
`IMZERO2_RECORD=<path>`; like `EGUI_INSPECTION` it is Rust-owned and therefore
documented in a how-to, not in the ADR-0009 Go env registry — naming open).
Taps, all public API:

- `input_hook` — raw gestures (pointer, keys, wheel, text).
- output hook — `PlatformOutput.events` (semantic effects with labels/values).
- `interaction_snapshot` + `Memory::focused()` — gesture→widget-id
  attribution.
- AccessKit tree snapshots — anchor context (role, label, ancestry, bounds)
  and a start-of-recording snapshot as the precondition record.

The recorder is passive (writes a local file; no listener), so it does not
extend the inspection port's attack surface. Recordings capture labels and
values — i.e. potentially real data. They are data artifacts: keep them out of
the repo unless scrubbed.

### SD2 — Step vocabulary: dual-layer, egui-mcp-aligned

One recording = an ordered list of semantic steps (`click`, `type`,
`set_value`, `drag`, `scroll`, `wait`, `note`) in a JSONL container (format
open: JSONL vs a single JSON document; JSONL favors the ring buffer). Each
step carries two layers:

- **Readable line** — role, label, scope path, observed effect ("click Button
  'Add filter' in 'Options'; a new row appeared"). This is the surface humans
  and LLMs read, edit and generalize; vocabulary aligned with the egui-mcp
  tool names so a trace is directly actionable by an agent.
- **Machine block** — node id (u64), ancestry ids, bounds-fraction offset,
  the raw `egui::Event` annex, frame/timing info, recorded effect values.

Coalescing at emit time: press+release → `click`; keystream → `type` with
final text; slider/drag-value gestures → `set_value` with the end value
(from `ValueChanged`); pointer trajectories collapse to endpoints. A
user-triggered annotation marker (hotkey, exact binding open) inserts `note`
steps — narration is the cheap fix for "traces show what, not why", and it is
in scope from v1.

### SD3 — Attribution: snapshot ids first, tree hit-test second

Primary: `interaction_snapshot.clicked` / `drag_started` / `dragged` /
`drag_stopped` for pointer gestures (egui's own layer-aware hit test, so
occlusion is correct); `focused()` for keyboard-driven changes. Secondary:
layer-aware hit-test of the pointer position against the tree snapshot, for
hover/scroll targets and for regions with no interactive widget. Attribution
of same-frame multi-source events (keyboard edit + pointer click on different
widgets in one frame) is heuristic by event type; an upstream PR adding the
sender id to `WidgetInfo` (~15 lines, stamped in `Response::output_event`,
where the id already keys the AccessKit builder) would make events
self-identifying and delete the heuristic. The PR is worth filing; it is
explicitly **not** a prerequisite, and no locally patched egui will be
carried for it.

### SD4 — Replay: anchor ladder, semantic verbs first

Anchor resolution per step, in order: exact node id → unique role+label →
ancestry-scoped label (+ ordinal) → bounds-fraction offset with a loud
warning. Execution prefers coordinate-free verbs (`ScrollIntoView` then
AccessKit `Click` / `SetValue` / `SetTextSelection` via `ApplyEvents`),
falling back to pointer synthesis at tree-resolved coordinates, then to the
raw annex (canvas widgets). Heal-on-green: when a fallback rung matched and
the step's recorded effect verified, the replayer may rewrite the stale
anchor in the script (off by default in CI, on in interactive use).

### SD5 — Waits and assertions

`ApplyEvents` acks only after a frame has applied the events, which removes
most wall-clock flake by construction. The emitter auto-inserts `wait` steps
on the next step's target (exists + enabled); recorded effects double as
opt-in assertions with two strictness levels — *navigate* (effects ignored)
and *verify* (effects asserted). Postcondition capture beyond the target
widget (tree diffing) is deferred.

### SD6 — Replay executors, in adoption order

1. **Via egui-mcp** (day one, zero new code): a trace is agent-readable and
   uses the mcp tool vocabulary; an agent replays it directly. This is also
   the teach-in surface: traces as in-context examples; distillation into
   parameterized, committed skill files; demonstrate → distill → replay →
   verify as the definition of done for a taught skill.
2. **`app imzero2 replay <trace>`** (M3): a small Go client speaking the
   `egui_inspection` protocol directly (4-byte BE length + MessagePack
   framing), enabling deterministic, assertable, CI-able replay. CI runs the
   desktop build under headless weston, the arrangement the screenshot tour
   already uses; the headless host cannot compile inspection, by design.

### SD7 — Non-goals and deferrals, recorded

- **End-user macro UI** (record/replay buttons in-app): deferred until demand
  is demonstrated; the operator-facing story stays programmable surfaces.
- **Model fine-tuning on traces**: descoped. The in-context / skill route
  captures the value at ~zero cost; the flight recorder accumulates the
  dataset passively, so the option stays open.
- **Canvas sub-targets**: widgets that paint without AccessKit nodes
  (timeline, treemap, world map internals) record only as bounds-fraction
  offsets — resolution-proof, not data-layout-proof. The fix is per-widget
  AccessKit sub-node exposure, a separate accessibility work track that this
  ADR motivates but does not gate on.
- **Upstream protocol record channel** (streaming events over the inspection
  port): a candidate later contribution; v1 writes locally and needs no wire
  change.
- **Native dialogs / window-manager interactions**: out of scope; the seam
  sees only egui.

### Milestones

- **M1** — `RecorderPlugin`: JSONL semantic log with dual layers, raw annex,
  start-of-recording tree snapshot, annotation marker; ring-buffer mode with
  export-on-panic.
- **M2** — script emitter: coalescing, dual anchors, auto-waits,
  egui-mcp-aligned vocabulary; a recorded demonstration replays via egui-mcp.
- **M3** — `app imzero2 replay`: Go protocol client, anchor ladder, navigate /
  verify modes; CI wiring under weston.
- **M4** — heal-on-green + the teach-in how-to (demonstrate → distill →
  replay → verify, skill file conventions).

## Alternatives

- **Raw input replay only (O1).** Breaks on any resolution, layout, scroll or
  ordering change — the recording encodes coordinates and wall-clock, which
  is precisely the brittleness the feature exists to avoid; illegible to
  humans and LLMs, so no teach-in value. Kept only as the per-step annex.
- **Go-side response recording (O3).** Replay by fabricated responses can
  express states no user can reach (clicking occluded or scrolled-away
  widgets), bypasses the real input path entirely, requires instrumenting the
  generated wrapper layer, and misses gestures the app does not poll. Noted
  as a possible *complement* for headless logic tests; rejected as the
  primary mechanism.
- **Screen-level capture (O4).** Strictly dominated: an introspectable,
  deterministic tree exists; pixels add fragility and remove semantics.
- **Patch egui first (`WidgetInfo.widget_id`).** The attribution gap it
  closes is narrow (same-frame multi-source events) and public API covers the
  rest; carrying boxer's first egui fork for it inverts the cost/benefit.
  File upstream, adopt when released.
- **Gate on the upstream protocol extension.** Recording over the wire is not
  needed for any v1 consumer; gating on an external crate's release cadence
  violates descope-over-gate.
- **OS-level automation (AT-SPI etc.).** Already explored during the egui-mcp
  work and abandoned; the in-process AccessKit seam is strictly better here.

## Consequences

### Positive

- One artifact class serves five consumers (agent scripts / teach-in, repros,
  regression tests, demo capture, perf workloads); the costly capture step is
  amortized across all of them.
- Fork-free and patch-free: everything rests on public egui API plus the
  already-shipped inspection seam.
- The deterministic id stack finally pays a second dividend: anchors that are
  pure functions of widget path.
- Recordings are inspectable text in git-friendly form; taught skills are
  reviewable files, not opaque state.

### Negative

- Recorded regression tests rot on intentional UI change; mitigation is a
  deliberately small suite plus the healing ladder, and it remains a
  discipline cost.
- Desktop-only, permanently: inspection cannot compile into headless builds,
  so CI replay requires the weston arrangement.
- A new maintenance surface against egui's still-young `Plugin` API across
  upgrades.
- Attribution heuristics can mislabel rare same-frame multi-source events
  until (if ever) the upstream `WidgetInfo` id lands.
- The canvas-widget ceiling: flows through painter-only widgets replay
  weakly and teach an LLM nothing generalizable until those widgets grow
  AccessKit sub-nodes.

### Neutral

- Recordings are best-effort across app versions by design — the anchor
  ladder degrades gracefully rather than guaranteeing replay.
- A recording may embed real data (labels, typed text, values); handling
  discipline is the same as for any data artifact.

## Status

Proposed. Decision dialogue captured 2026-07-17; next step is review of the
SD carve and, if accepted, M1.

## References

- [How to drive imzero2 from an AI agent with egui_mcp](../howto/egui-mcp.md)
  — the existing seam this ADR builds on.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — demo registry, tour
  driver, and the headless-weston CI arrangement.
- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless
  host's separate input/pixel path (why headless replay is out of scope).
- egui 0.35.0 sources: `plugin.rs` (`Plugin` trait), `interaction.rs`
  (`InteractionSnapshot`), `response.rs` (`widget_info` / `output_event`),
  `id.rs` (`accesskit_id`), `context.rs` (AccessKit action handling),
  `widgets/slider.rs` / `widgets/drag_value.rs` (`SetValue`).
- `egui_inspection` 0.35.0 `protocol.rs` — the wire protocol (request/response
  set, framing, versioning).
- <https://github.com/rerun-io/kittest_inspector> — the `egui-mcp` server.
- Allen Cypher (ed.), *Watch What I Do: Programming by Demonstration*, MIT
  Press 1993 — the classic PbD program and its generalization problem, which
  the teach-in path (SD6) addresses with an LLM as the generalizer.
- Playwright locators and auto-waiting — prior art for the anchor ladder and
  wait insertion.

### Related ADRs

- [ADR-0009](./0009-environment-variable-registry.md) — why `IMZERO2_RECORD`
  (Rust-owned) stays out of the generated env-vars doc.
- [ADR-0092](./0092-adr-overview-tool.md) — status/evidence tracking that
  will pick this ADR up.
