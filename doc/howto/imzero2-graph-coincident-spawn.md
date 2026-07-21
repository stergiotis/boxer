---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Why imzero2 Graph nodes spawned coincident, and the deterministic-scatter fix

This documents a downstream change to `reconcile_graph_state` in
[`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs),
made 2026-07-18 from the `hackathon_2026` consumer, so the reasoning is
reviewable and the change can be offered upstream.

## Symptom

In a force-directed `Graph` widget, some nodes render **exactly on top of each
other** and never separate — the simulation converges with them stacked, and
zoom/drag on the cluster reveals two (or more) labels painted at one position.
Observed in the `egonet` sanctions ego-network app (hackathon_2026): a center
node with several leaf neighbours reliably produced stacked leaves. Any
consumer of the Graph binding with symmetric topology (godepview included) can
reproduce it.

## Root cause — three links, all verified in source (egui_graphs 0.31.0)

1. **Every new node spawns at the same point.** The reconcile add path called
   `Graph::add_node_with_label`, which routes through `Node::new`
   (`src/elements/node.rs:101`), and that sets
   `location: Pos2::default()` — exactly `(0,0)`. No layout performs an
   initial scatter: the force-directed `Layout::next`
   (`src/layouts/force_directed/layout.rs:19`) only calls `alg.step(..)`.

2. **Repulsion between exactly coincident nodes is zero.** The
   Fruchterman–Reingold repulsion pass
   (`src/layouts/force_directed/implementations/fruchterman_reingold/core.rs:212`)
   computes

   ```rust
   let distance = delta.length().max(epsilon);
   let force = c_repulse * (k * k) / distance;
   let dir = delta / distance;
   ```

   With `delta == (0,0)`, `dir` is the zero vector — the `max(epsilon)` guard
   prevents division by zero but not the zero *direction*, so coincident nodes
   exert **no repulsion on each other**.

3. **Structural symmetry locks the tie in.** Two nodes with identical
   neighbour sets (e.g. two leaf entities each connected only to the same hub)
   receive identical attraction, identical center gravity, and — by link 2 —
   zero mutual repulsion. Their net forces are identical every step, so they
   move in lockstep from the shared spawn point and remain exactly coincident
   forever. Nodes with *differing* neighbour sets escape, because different
   attractions separate them first and repulsion takes over from there. That
   is why only some nodes stack.

## The fix

`reconcile_graph_state` now adds nodes with
`add_node_with_label_and_location`, at a position produced by
`graph_spawn_location(go_key)`: a splitmix64 finalizer hash of the Go-side
node key mapped to polar coordinates on a disc (radius 30–150 layout units).

Properties that drove this shape:

- **Deterministic.** The position derives from the node's stable Go-side key,
  not from insertion order or a RNG — the same node spawns at the same place
  across frames, re-declarations, and sessions. No `rand` dependency added.
- **Collision-free in practice.** Distinct keys hash to distinct disc points;
  even a residual collision is broken by the layout once positions differ at
  all (link 2 only bites at *exact* coincidence).
- **Layout-neutral.** The scatter is only an initial condition; FR convergence
  and hierarchical/random layouts are unaffected beyond losing the degenerate
  start.

## Why fix it here and not elsewhere

- **egui_graphs upstream** is the deeper root (default spawn location +
  zero-direction repulsion tie). The ideal upstream fix is either a jittered
  default location or a symmetric tie-break in the repulsion pass for
  near-zero `delta`. Until that lands, the consumer-side add site is the
  choice point where the default-location API was selected, and it is
  hand-maintained code (not generated).
- **The Go app side cannot fix it.** The `graphNode` IDL surface carries only
  id/label/color — no position — and adding one would be a far larger change
  (IDL + codegen + Go bindings + wire format) for something no app should have
  to manage.

## Upstreaming note

If offering this upstream to egui_graphs rather than boxer: the minimal
equivalent is a deterministic jitter in `Node::new`'s default location, or
`delta = (i, j)`-indexed unit tie-break in the repulsion pass when
`delta.length() < epsilon`. Either removes the failure class for all
consumers, after which this reconcile-side scatter becomes redundant but
harmless.

## Verification

- Build: `cargo build --release --features puffin` in `rust/imzero2`.
- Repro before/after: in any Graph consumer, declare a hub node with ≥ 2
  leaf nodes connected only to the hub (identical neighbour sets). Before:
  leaves converge stacked at one point. After: leaves settle separated.
  The hackathon_2026 `egonet` app ("Slobodan Milosevic" ego network on the
  SECO import) is the originating repro.
