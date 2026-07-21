---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Why GraphNode colors were silently dropped, and the set_color mirror fix

Documents a downstream change to `reconcile_graph_state` in
[`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs),
made 2026-07-19 from the `hackathon_2026` consumer, companion to
[`imzero2-graph-coincident-spawn.md`](./imzero2-graph-coincident-spawn.md).

## Symptom

`c.GraphNode(id, label).Color(col).Send()` has no visible effect: every node
renders in the theme's default stroke color (white on the dark theme),
regardless of the color passed. Edge colors work. Observed in the
hackathon_2026 `egonet` app; `godepview`'s per-class node coloring is
affected the same way.

## Root cause

egui_graphs (0.31.0) resolves a node's fill in `DefaultNodeShape`
(`src/draw/displays_default/node.rs`): `effective_color` uses the shape's
`color`, which is copied from **`node_props.color()`** — the `Node`'s own
`color: Option<Color32>` slot, settable only through `Node::set_color`
(`src/elements/node.rs:160`). With that slot `None`, it falls back to
`style.fg_stroke.color`.

The imzero2 reconcile stored the Go-side color only in the **payload**
(`GraphNodeUserData.color`) and never called `set_color`. Nothing reads a
node payload's color — the analogous gap on edges was closed with the custom
`PayloadColorEdgeShape`, but nodes kept `DefaultNodeShape` and the payload
color was dead data. The old comment on `GraphNodeUserData` even said so:
"picked up only if a style hook is wired (not in v1)". The `graphNode` IDL
surface has advertised `.Color(...)` since v1, so the wire carried colors
that the renderer discarded.

## The fix

`reconcile_graph_state` now mirrors the color into the node's slot at both
sites: on **add** (right after `add_node_with_label_and_location`) and on
**update** (next to the payload label/color refresh), via
`node.set_color(col)` when the Go declaration carries a color.

- A declaration **without** a color leaves the slot untouched (egui_graphs
  has no public "clear color" — `set_color` only writes `Some`). In practice
  Go apps either always or never color a given graph's nodes, so the
  asymmetry is unobservable; noted here for completeness.
- The payload keeps carrying the color too — it is the value a future
  payload-aware node shape (mirroring `PayloadColorEdgeShape`) would read,
  and removing it would touch more call sites than it saves.

## Why fix it here

- **The Go side cannot reach the node slot.** The only data path from the
  `graphNode` opcode into egui_graphs goes through this reconcile; the
  payload is all it wrote.
- **Alternative considered — a `PayloadColorNodeShape`** mirroring the edge
  shape: strictly more code (a full `DisplayNode` impl) for the same result,
  and it would still leave the stale-payload comment wrong. `set_color` is
  the API egui_graphs provides for exactly this.

## Upstreaming note

Nothing to upstream to egui_graphs — the library behaves as documented; the
gap was in this consumer. For boxer upstream: the change is the two
`set_color` calls plus the corrected `GraphNodeUserData` comment; the
`graphNode` IDL and Go bindings are untouched (the wire format already
carried the color).

## Verification

- Build: `cargo build --release --features puffin` in `rust/imzero2`.
- Any Graph consumer that calls `.Color(...)` per node: nodes render in the
  declared colors instead of uniform theme white; colors update live when a
  node's declared color changes between frames (the update path re-mirrors).
  The hackathon_2026 `egonet` app (schema-colored nodes + violet center) is
  the originating repro.
