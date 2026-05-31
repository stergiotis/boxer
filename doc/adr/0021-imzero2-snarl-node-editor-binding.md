---
type: adr
status: accepted
date: 2026-05-04
reviewed-by: "@spx"
reviewed-date: 2026-05-04
---

> **Status: accepted 2026-05-04 by @spx.** M1 implementation underway in this branch.

# ADR-0021: ImZero2 node-editor binding via `egui-snarl`

## Context

ImZero2 has no node-editor affordance. Several in-flight directions need one: the Grafana-replacement scope (memory `project_grafana_replacement`) implies a visual builder for query/transform pipelines; the spinnaker/play "affordance" line (memory `feedback_affordance_term`) hints at a visual-programming surface for SQL-side compositions; the M4-in-SQL upstream lane benefits from a DAG view of derived signals. The user's reference points are `thedmd/imgui-node-editor` (the production-grade C++ reference) and `Fattorino/ImNodeFlow` (a simpler MIT alternative).

The existing `egui_graphs` binding (see [ADR-0056](0056-walkers-map-h3-binding.md) for the binding-pattern precedent; the graph widget itself is unADR'd) is **visualization-only** — read-only positions, force-directed layout, no edit affordances, no typed pins, no connect/disconnect events surfaced back to Go. A node editor needs the inverse: Go authoritative for topology, Rust authoritative for layout/interaction, with a bidirectional event flow.

Forces the design must respect:

- **FFFI2 register-drain + opcode-stream protocol.** Any binding cooperates with deferred-block capture, frame-level culling ([ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) drain-on-cull), and the stateful-widget contract ([ADR-0013](./0013-imzero2-stateful-widget-contract.md) gated `r10_push`). It cannot rely on synchronous call semantics or persistent stateful encoders on the Go side.
- **egui 0.34.1 pin.** `rust/imzero2/Cargo.toml` is on `egui >= 0.34.1` / `eframe 0.34.1` / glow backend. Any candidate must track current egui without forcing a backend switch.
- **CGO-free Go build.** `build_go.sh` sets `CGO_ENABLED=0` deliberately. Pure-Rust crate; no Go-side native deps.
- **Existing thick-client binding shape.** [`egui_dock 0.19`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_dock.go) (viewer-trait + retained `DockState<u64>` + deferred-block map for tab bodies) and the walkers binding ([ADR-0056](0056-walkers-map-h3-binding.md)) define the proven pattern: hand-written `render_*` apply on `ImZeroFffi`, `HashMap<u64, *State>` retained state, register-drain accumulators for child entities, fetcher node for events.
- **Screenshot-based testing.** Demos must tolerate the 4-frame `IMZERO2_SCREENSHOT_DIR` tour without animations stalling the capture (see memory `feedback_collapsingheader_tour`).
- **License ergonomics.** Per [ADR-0005](0005-streaming-persisted-kafka-from-connect.md), Apache-2.0 derivative tracking is non-trivial. Preference for MIT or MIT/Apache-2.0 dual-licensed deps where the choice exists.

The C++ references are not realistic ports. `thedmd/imgui-node-editor` owns its own painter and runs a smooth-zoom transform pipeline that doesn't compose with egui's coordinate model — multiple egui community attempts have stalled at the zoom layer. `ImNodeFlow` is portable in principle but simpler than the egui-native crates already available. The decision is therefore between **egui community crates** and a **ground-up port**.

## Design space (QOC)

**Question.** How should ImZero2 expose an editable node-graph widget so that Go owns topology and node identity, Rust owns layout and interaction state, edits flow back as discrete events, and the binding fits the existing thick-client pattern (egui_dock, walkers) without a backend or version flip?

**Options.**

- **O1 — `egui-snarl 0.9` viewer-trait binding (chosen).** Wrap `egui_snarl::Snarl<u64>` retained on Rust; expose a `SnarlEditor` widget with a deferred-block map for node bodies (mirroring `DockAreaRaw`'s `tabBody` map); register-drain `snarlNode` / `snarlConnection` accumulators for the initial topology push; `fetchR15SnarlEvents` for connect/disconnect/move events back to Go. Implement `SnarlViewer` as `FffiSnarlViewer` delegating bodies + pin sockets to the captured deferred blocks.
- **O2 — `egui_xyflow 0.4` viewer binding.** Same shape as O1 but with the React-Flow-derived crate. Ships with minimap, force-directed layout, viewport culling, snap-to-grid, animated edges out of the box.
- **O3 — Port `thedmd/imgui-node-editor` to egui directly.** Reimplement the C++ source against `egui::Painter` and `egui::Response`. Production-grade reference, smooth zoom, group dragging, copy/paste/delete shortcuts.
- **O4 — Ground-up node editor on ImZero2 painter primitives.** Build directly on `egui::Painter` + `Sense::click_and_drag` + the existing `PaintCanvas` widget (memory `feedback_r14_click_cascade`). No external crate.
- **O5 — `egui-graph-edit 0.7` binding.** The live successor to the abandoned `setzer22/egui_node_graph` lineage; powers Blackjack and Modal.

**Criteria.**

- **C1 — egui 0.34.1 compatibility.** Hard requirement. Misses imply a fork or a coordinated ecosystem upgrade.
- **C2 — Maintenance health.** Last release date, contributor count, real-world users, license clarity.
- **C3 — Binding-pattern fit.** Maps cleanly onto the established viewer-trait + retained-state + register-drain shape; no novel codegen surface.
- **C4 — Feature coverage.** Bezier wires, typed pins, multi-connect, context menus, serde, plus stretch features (minimap, undo, smooth GPU zoom, comments, group selection).
- **C5 — Dev cost.** Lines of IDL + apply + retained state; new fetchers; ADR-0012/0013 conformance work.
- **C6 — License.** MIT or MIT/Apache-2.0 strongly preferred.
- **C7 — Forward path.** Can the wire format absorb future features (typed pin polymorphism, undo, node templates) without a wire break?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (snarl) | O2 (xyflow) | O3 (port thedmd) | O4 (ground-up) | O5 (graph-edit) |
|----|------------|-------------|------------------|----------------|-----------------|
| C1 | ++         | +           | n/a              | ++             | −               |
| C2 | ++         | −−          | n/a              | ++             | +               |
| C3 | ++         | +           | −−               | +              | +               |
| C4 | +          | ++          | ++               | −−             | +               |
| C5 | +          | +           | −−               | −−             | +               |
| C6 | ++         | ++          | ++ (MIT)         | n/a            | ++              |
| C7 | +          | +           | ++               | ++             | +               |

O1 is the Pareto choice: only option that satisfies C1+C2+C3 simultaneously while keeping C5 bounded. O2 is feature-richer (the only crate offering minimap + force-layout + viewport culling out of the box) but the maturity gap is real — single author, 2★, no production users — and that risk is not justified before establishing an editor MVP exists at all. O3 is the highest ceiling on polish (C4, C7) but the dev cost is order-of-magnitude larger and the egui-zoom mismatch has defeated multiple prior attempts. O4 wins on independence but loses everything that motivates picking a community crate. O5 lags egui by several versions (egui 0.32 vs the repo's 0.34.1) and would either pin the rest of the repo back or require a fork.

## Decision

We bind `egui-snarl = "0.9"` (zakarumych, MIT OR Apache-2.0) as the ImZero2 node-editor widget. The integration follows the egui_dock thick-client pattern: a hand-written `ImZeroFffi::render_snarl_editor(...)` method on the interpreter, a retained `HashMap<u64, SnarlState>` keyed by editor id, a `FffiSnarlViewer` delegate implementing `egui_snarl::ui::SnarlViewer<u64>`, and a deferred-block map (`WithDeferredBlockMap("nodeBody", u64)`) so each Go-side node renders its body via re-entered IDL opcodes. Connect / disconnect / move / select events are captured via a `RefCell<Vec<SnarlEvent>>` analogous to the `egui_graphs` event-capture, drained by a `fetchR15SnarlEvents` fetcher node.

Initial topology (nodes + edges) is pushed each frame via two register-drain accumulator nodes — `snarlNode(id, x, y, kind)` and `snarlConnection(srcNode, srcPort, dstNode, dstPort)` — drained by the `snarlEditor` apply. **Go is authoritative for positions by default**: every frame the Rust-side `Snarl<u64>` is reconciled against the Go-supplied set (positions overwritten, new nodes inserted, missing nodes removed). User drags surface as `NodeMoved` events through the fetcher; Go updates its own model and pushes the new coordinates on the next frame. **Rust-side persistence is opt-in per editor** via `.PersistPositions(true)` on the widget — when set, Go-supplied positions are honoured only on first insertion of a given node id; subsequent Go pushes ignore `(x, y)` and Rust's drag-tracked coordinates win. Connections are diffed every frame regardless of mode (SD7). Edit events flow Rust→Go via the fetcher; position pushes back to Go are gated per [ADR-0013](./0013-imzero2-stateful-widget-contract.md).

Pin identity is a packed `u64`: high 32 bits = port index, low 32 bits = node id; connections are `(srcPin u64, dstPin u64)` tuples. Pin polymorphism (typed sockets) is **not** in scope for M1; all pins are monotyped at the IDL surface, with `kind: u32` carried alongside for the viewer to colour by type. Typed-dispatch plumbing is left as a forward path (SD8).

### Subsidiary design decisions

- **SD1 — `u64` node id is the canonical exchange unit.** Mirrors the `egui_graphs` and `egui_dock` precedent. Positions live on the Rust side after first frame; the binding does not roundtrip them through Go each frame. Rationale: fits the retained-state pattern without inflating the per-frame opcode stream; matches the way users expect a node editor to "remember" layout across frames.
- **SD2 — Node bodies use the deferred-block map (`WithDeferredBlockMap("nodeBody", u64)`).** Direct copy of `DockAreaRaw`'s `tabBody` mechanism. The viewer's `show_node` calls `interpret_outer(c, &mut deferred[node_id])` inside the body slot. Drain-on-cull per [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) is required: every body block apply must `else { interpret_outer(c, &mut None) }` when culled.
- **SD3 — Pin sockets render via a smaller deferred block (`pinSocket`).** Custom pin labels / icons / inline widgets (e.g. a tiny colour swatch for a Color pin) need IDL re-entry; the viewer's `show_input` / `show_output` map onto the captured pin-socket block. Optional — if absent, viewer falls back to a default circle + text rendering driven by the `kind: u32` on the pin record.
- **SD4 — Stateful contract: position pushes are gated on `SnarlEvent::NodeMoved`.** Per [ADR-0013](./0013-imzero2-stateful-widget-contract.md), `r10_push` must be inside `if let Some(SnarlEvent::NodeMoved { .. }) = …`. The drift guard `TestStatefulWidgetsAreGated` will cover this; no hand-rolled apply allowed.
- **SD5 — Single `fetchR15SnarlEvents` returns the full event batch each frame.** One fetcher, multi-event payload (`[]SnarlEventDTO`). Rejected per-event-kind fetcher fanout (one for moves, one for connects, one for selections) — multiplies wire surface for no Go-side ergonomic gain. Empty batch when no events; cheap to call unconditionally.
- **SD6 — Position authority is Go by default; Rust persistence is opt-in per editor via `.PersistPositions(true)`.** Default mode (flag unset, the typical case): every frame Rust copies the Go-supplied `(x, y)` into the retained `Snarl<u64>`. User drags fire `NodeMoved` events; Go updates its model and pushes the new coordinates next frame. Symmetric, easy to reason about, predictable for contributors, and matches every other "Go authoritative for state" widget in the stack. Opt-in mode (`.PersistPositions(true)`): Rust uses the Go-supplied position only when a node id is first inserted into the retained `Snarl<u64>`; subsequent Go pushes for that id leave `(x, y)` alone (kind/pin updates still applied). The flag is per-editor, not per-node — an editor either lets Go drive layout or lets Rust persist it. Use cases for opt-in: ephemeral panels where Go has no place to store layout state; demos that want the editor to "remember" positions across navigation away and back. Rationale: the originally-proposed "Go wins on first sight, Rust wins thereafter" was asymmetric and surprising; the symmetric default plus a single explicit flag covers both intents without forcing every consumer to reason about two-mode reconciliation.
- **SD7 — Connection reconciliation: full diff per frame.** Compute `(toAdd, toRemove)` between the Go-supplied connection set and the retained Snarl's connections; apply both. Cheaper than tracking edits incrementally for the expected scale (single-digit thousands of connections per editor) and avoids the "stale connection" bug class entirely.
- **SD8 — Pin types: `kind: u32` colour-coded only at M1.** No type-checking on connect (any pin connects to any pin); no pin-shape variation; no typed dispatch on the Snarl side. Typed-pin polymorphism is a forward path — Snarl's generic `Snarl<NodeData>` allows a richer node-type enum if a real consumer needs structural pin matching, but the FFFI surface for that hasn't been designed.
- **SD9 — Multi-connect / disconnect modifiers follow Snarl defaults.** Shift+drag and Ctrl+drag for multi-input behaviour are Snarl's UI affordance and are surfaced as configurable on the widget level (`.WireStyle(...)` / `.AllowedWires(...)` per `SnarlStyle`). Default values mirror Snarl's defaults; no custom rebind layer.
- **SD10 — Thick-client apply path, not single-expression template.** The `snarlEditor` apply does retained-state lookup, accumulator drain, topology reconciliation (SD6, SD7), viewer construction with deferred-block delegation, and event-batch capture. Same pattern as walkers SD11 and `egui_dock`'s `DockAreaRaw`. Codegen emits the dispatch site; the body is hand-written on `ImZeroFffi`.
- **SD11 — Out of scope at M1: minimap, undo/redo, comment groups, smooth GPU zoom.** None of these is provided by `egui-snarl`; replicating them inside the binding is a project-sized effort each. They are tracked as Negative consequences with named escape hatches, not as M1 work.

## Alternatives

- **O2 — `egui_xyflow`.** Strictly more ambitious — the only crate offering minimap, force-directed layout, viewport culling, snap-to-grid, animated edges, draggable edge anchors out of the box. Rejected at M1 because of maturity risk: 2★, single author (avinkrisv), v0.4.2 (2026-04-17), no production users. Worth revisiting once a Snarl-bound MVP is in production and the gap features (especially minimap + force-layout) are proven necessary by real users; the wire format defined here (`u64` ids, packed pins, event batch) is intentionally crate-agnostic so the bind target can swap.
- **O3 — Port `thedmd/imgui-node-editor`.** Highest ceiling on polish, smooth zoom, copy/paste/delete shortcuts, group dragging, selection rect. Rejected because the smooth-zoom transform pipeline doesn't fit egui's coordinate model — multiple ecosystem attempts have stalled there — and the dev cost is order-of-magnitude larger than O1. Holds open as the escape route if O1 + O2 both fail user requirements; would be its own multi-ADR programme.
- **O4 — Ground-up on ImZero2 painter.** Eliminates external crate risk and gives total control over the wire format. Rejected at M1 because every line of "node editor in 2026" written in this repo is a line that already exists in egui-snarl. Wins on long-term independence; loses an order of magnitude on time-to-MVP.
- **O5 — `egui-graph-edit`.** The Blackjack/Modal lineage; well-shaped for typed-pin dataflow. Rejected on C1: egui 0.32 vs this repo's 0.34.1. Would either pin the rest of the repo back or require maintaining a fork. Snarl tracks current egui without that cost.

## Consequences

### Positive

- **Existing binding pattern stretches to one more crate without new infrastructure.** No codegen surface change; no novel IDL syntax; no Cargo features wired up. The IDL definition file (`egui2_definition_d_snarl.go`) sits alongside `_d_dock.go` (119 lines) and `_d_walkers.go` (310 lines) at a comparable size.
- **Topology authority lives in Go.** Per-frame node + connection accumulators mean the editor is a pure view of Go-side state on the first frame; the user experience is "drop nodes from a Go-side palette and the editor renders them" without manual id management on the Go side.
- **Edit events flow back as a single batch.** One fetcher, one DTO type, one `RangeFunc` on the Go side. Matches `egui_graphs`' event capture shape; no surprises for contributors who've worked there.
- **Crate license is clean.** `egui-snarl` is dual MIT / Apache-2.0; ADR-0005-style derivative tracking does not apply (we depend, not derive). Cargo.toml line is one entry.
- **Forward path is preserved.** Wire format (`u64` ids, packed pins, event batch, kind colour code) is crate-agnostic; if a future user need pushes to swap to `egui_xyflow` or a port of `thedmd`, the binding layer changes but Go-side code does not.

### Negative

- **No minimap, no undo, no smooth GPU zoom, no comment groups (SD11).** The four features that distinguish a "professional" node editor from a "functional" one are not in `egui-snarl`. Workarounds: minimap could be a separate `mapMinimap` widget feeding off the same retained state in a future ADR; undo is a Go-side concern (track edit events, replay in reverse); smooth zoom is the hardest — likely not achievable without an O2 or O3 escalation.
- **Pin polymorphism deferred (SD8).** All pins connect to all pins at M1. Type-mismatched connections will be allowed; the consumer must validate downstream. Real impact is minor for the expected first use cases (Grafana-replacement query/transform graphs are mostly homogeneous), but a node-editor for a strongly-typed dataflow language would feel limited until SD8 is revisited.
- **Default mode round-trips drag events through Go.** Every node drag fires a `NodeMoved` event, Go updates its model, and the new position is pushed back next frame — at minimum two frames of latency between user input and rendered position. For high-drag UX (rapid layout exploration) this is more work than persisting locally on the Rust side. The `.PersistPositions(true)` opt-in (SD6) is the escape hatch when this matters.
- **`egui-snarl` ≥ 0.9 pinned.** Earlier versions had a different `SnarlViewer` trait shape; the binding cannot easily downgrade. Symmetric to walkers' ≥ 0.53 pin (ADR-0056 SD4).
- **One more retained `HashMap` on `ImZeroFffi`.** `snarl_states: HashMap<u64, SnarlState>`; cleared at process exit, not at frame boundary. Same shape as `dock_states`, `walkers_states`, `graph_states` — small marginal cost.
- **Multi-editor frames hit the same `last_*` register problem as walkers (ADR-0056 SD3).** The fetcher returns events for the editor id passed in the call, so this is mitigated for events; but if a future `fetchR15SnarlCamera`-style register is added it inherits the single-slot constraint. Documented up front; not a current bug.

### Neutral

- **Snarl's default `SnarlStyle` is acceptable for M1.** Bezier curvature, wire thickness, and node corner radius use crate defaults. A future demo polish pass may surface widget-level setters (`.WireCurvature(...)`, `.NodeFrameRadius(...)`) — the IDL is structured to admit them additively.
- **Connection diffing is O(n) per frame (SD7).** For thousands of connections this is fine; for millions it would dominate the frame. Not a current scale concern.
- **Event batch is FIFO, not priority-ordered.** If a frame contains both a NodeMoved and a NodeRemoved on the same id, the consumer sees them in user-action order. Rust drains events in the order Snarl produced them. Documented expectation.

### Derived practices

- **New node-editor widgets follow the same shape.** Register-drain accumulators for declarative content; deferred-block map for child-rendered widgets; retained `*State` HashMap; `FffiSnarlViewer`-style delegate; single fetcher for events. The same shape works for any future "viewer-trait + retained state" egui crate (`egui_dock`, `egui-snarl`, hypothetical future). [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) and [ADR-0013](./0013-imzero2-stateful-widget-contract.md) conformance is non-negotiable; the drift guards cover it automatically.
- **Crate-bind ADRs follow this template.** Context (forces) → QOC (≥3 candidates) → SDs covering wire format / reconciliation / state ownership / out-of-scope → C7 forward-path criterion if a swap is plausible. ADR-0056 (walkers) and this ADR are the two reference shapes.

## Status

Accepted — 2026-05-04 by @spx. M1 implementation underway in this branch: `egui-snarl 0.9` added to `rust/imzero2/Cargo.toml`, IDL definition file `egui2_definition_d_snarl.go` alongside the other `egui2_definition_d_*.go` files, Rust apply (`render_snarl_editor`, `FffiSnarlViewer`, `SnarlState`) on `ImZeroFffi`. Default `PersistPositions=false` per the SD6 revision adopted at acceptance.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0056 — Slippy map + H3 cell overlays via `walkers` + `h3o`](0056-walkers-map-h3-binding.md) — sibling external-egui-crate binding ADR; same template shape.
- [ADR-0012 — ImZero2 collapsible retained bodies](./0012-imzero2-collapsible-retained-bodies.md) — drain-on-cull invariant for the `nodeBody` deferred-block map.
- [ADR-0013 — ImZero2 stateful widget contract](./0013-imzero2-stateful-widget-contract.md) — gated `r10_push` rule for node-position events.
- [ADR-0005 — Streaming persisted Kafka from Connect](0005-streaming-persisted-kafka-from-connect.md) — Apache-2.0 derivative-tracking precedent; not load-bearing here (snarl is dual-licensed).
- [`egui-snarl` on crates.io](https://crates.io/crates/egui-snarl) — v0.9.x; egui 0.34 compatible; MIT OR Apache-2.0.
- [`egui-snarl` on GitHub](https://github.com/zakarumych/egui-snarl) — repo, demo, viewer-trait reference.
- [`thedmd/imgui-node-editor`](https://github.com/thedmd/imgui-node-editor) — C++ reference cited in the request; not ported (see O3 in QOC).
- [`Fattorino/ImNodeFlow`](https://github.com/Fattorino/ImNodeFlow) — simpler C++ reference cited in the request; covered by Snarl on functionality (see O3/O4 in QOC).
- [`egui_xyflow`](https://github.com/avinkrisv/egui_xyflow) — alternative crate held in reserve as the O2 escape hatch.
- [`public/thestack/imzero2/egui2/definition/egui2_definition_d_dock.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_dock.go) — viewer-trait + retained-state binding precedent that this ADR mirrors.
- [`public/thestack/imzero2/egui2/definition/egui2_definition_d_walkers.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_walkers.go) — register-drain accumulator + thick-client apply precedent.
