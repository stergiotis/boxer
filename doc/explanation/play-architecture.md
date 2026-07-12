---
type: explanation
audience: contributors working on or embedding the play app
status: draft
---

> **Status: draft — pre-human-review.** Living overview of the current shape.
> This page decides nothing; where it and an ADR disagree, the ADR is the
> record. The primary record is
> [ADR-0097](../adr/0097-play-reactive-query-graph.md), whose dated Updates
> carry the journey this page deliberately flattens.

# The play architecture

`play` is boxer's graphical ClickHouse SQL playground. Architecturally it is
a **reactive query-graph**: one SQL buffer splits into nodes, nodes execute
on demand-driven lanes, panels observe node results through typed channels,
and a signal store carries the live values — selections, viewports, time
extents — that flow between them as ordinary query parameters. This page
explains that shape and why it is this way; the end-user story lives in the
app's own help book (`apps/play/help/`), and each decision lives in its ADR.

## The premise: one buffer is the artifact

Everything derives from a single SQL text. The buffer — including its
`SET param_* = …;` prelude — is the reproducible artifact: copy it, paste
it, run it elsewhere, get the same query. The graph is *recovered* from the
buffer by static analysis (a nanopass CTE-lift and statement split), never
authored beside it; there is no separate pipeline definition to drift out of
sync. Explicit multi-cell authoring stays deferred until the single buffer
demonstrably fails someone (ADR-0097 SD12).

```
                ┌───────────────────────────────────────────────┐
                │          the editor buffer (one text)         │
                │                                               │
                │   SET param_lim = 50;      ── constants (D1)  │
                │   WITH recent  AS (…),                        │
                │        by_kind AS (… FROM recent)             │
                │   SELECT … FROM by_kind    ── the sink        │
                └───────────────────────┬───────────────────────┘
                                  Run   │   nanopass split
                                        ▼
      ┌───────────────────────  query graph  ─────────────────────┐
      │                                                           │
      │     recent ──▶ by_kind ──▶ sink         node = CTE        │
      │        ▲                                edge = FROM ref   │
      │        └── {lim:UInt64} ← signal edge (unbound param)     │
      └───────┬──────────────┬───────────────┬────────────────────┘
              │ fuseNode     │ fuseNode      │ fuse-to-sink
              ▼              ▼               ▼
      ┌─────────────┐ ┌──────────────┐ ┌────────────────┐
      │ bound lanes │ │ observe lane │ │   main lane    │
      │ (one per    │ │ (all-panels  │ │  (Run-gated;   │
      │ bound node) │ │  override)   │ │  owns history, │
      └──────┬──────┘ └──────┬───────┘ │  the FSM, the  │
             │               │         │  status bar)   │
             │               │         └───────┬────────┘
             │               │                 │      ┌────────────────┐
             │               │                 │      │ panel-authored │
             │               │                 │      │ nodes: the Map │
             │               │                 │      │ raster, the    │
             │               │                 │      │ Timeline bands │
             │               │                 │      └───────┬────────┘
             └───────────────┴───────┬─────────┴──────────────┘
                                     │  laneView: rec · schema ·
                                     │  loading · err · executed
                                     ▼
      ┌──────────────────────── tab registry ────────────────────────┐
      │  one dock loop; each tab renders from a per-tab frame view   │
      │  (a bound tab's frame carries its node's lane view)          │
      │                                                              │
      │  chrome (nil PanelI): Editor Preview History Snippets Graph  │
      │                       Diagnostics Map                        │
      │  panels (PanelI):     Table Projection Timeline Detail World │
      │                       Schema — typed channels, accept/reject │
      └──────────────────────────────────────────────────────────────┘
```

A CTE **is** a node — the node↔CTE isomorphism (SD13) means any CTE is
observable, and unobserved same-engine ancestors fuse back into one
pushed-down query for execution. Running the buffer executes the fused sink;
nothing is materialized server-side, and a node observed by several
consumers is recomputed per lane. That recompute-over-materialize stance
holds until shared observers measurably hurt (the SD13 trigger).

## Execution: demand-driven lanes

A lane is the async execution slot for one node: non-blocking `demand`,
last-good results while a run is in flight, cancel-and-replace supersession.
Every lane keys its memo on the **compiled pair `(SQL, params)`** — the same
SQL under different signal values is a different execution, and an unchanged
pair is a memo hit, never a re-run (minimality, SD1/SD4). Four lane kinds
exist, all the same machinery: the `main` lane (Run-gated, and the only one
carrying run history and the status-bar FSM), the observe lane (the Graph
view's all-panels override), per-node bound lanes (slice 6c), and the
panel-authored lanes (the Map's raster template, the Timeline's bands).
Everything executes over ClickHouse HTTP; results arrive as Arrow.

## Signals: live values as unbound parameters

A `{name:Type}` placeholder the prelude does not bind is a **signal**: a
live, store-owned value shared by name across every query and panel (the
Grafana model, SD8). A `SET` for the same name *pins* it into a buffer-owned
constant that shadows the store — the two-tier truth model (slice-5 D1) that
keeps the buffer a self-contained artifact while panels write at interaction
rate.

```
        panels write as you interact         humans write
        ────────────────────────────         ────────────
        row click  → selection               the Signals editor (Graph tab)
                     (+ node, + id)          a SET prelude pins a name
        map settle → vp_min_x … vp_h         history restore re-seeds (D4)
        events     → tl_min, tl_max
                     │                                  │
                     ▼                                  ▼
             ┌─────────────────── signal store ───────────────────┐
             │  name → raw value · last writer · revision         │
             │  one immutable snapshot per frame; emits land the  │
             │  next frame (glitch-freedom as frame semantics)    │
             └───────────────────────┬────────────────────────────┘
                                     │ compile: resolve {name:Type}
                                     ▼
                   (SQL, params) — the memo identity of every lane
                                     │  a changed pair re-executes,
                                     │  superseding in flight
                                     ▼
                       ClickHouse (params ride the URL as param_*)
```

The store is deliberately thin: raw strings plus provenance (who last
changed a value, at which revision — visible in the Graph tab's Signals
section, which is also where a human sets, adds, or discards one). Types
live in the *reading* slots; ClickHouse does the typed substitution
server-side, so there is no client-side literal-encoding surface. Liveness
is a per-node policy (D2): demand-driven lanes re-drive automatically, while
`main` stays Run-gated with an opt-in **Live** toggle and a staleness
witness that covers both buffer edits and moved signals. A referenced name
nothing fills blocks Run with a hint rather than a doomed request (D3).
History entries snapshot the signal values a run shipped, and restoring one
re-seeds the store (D4) — signals otherwise do not persist.

Selection is three signals, not one. A row click writes the ordinal cursor
(`selection`), the node it indexes (`selection_node`), and — when the
clicked result carries a leeway `id:id:…` column — the row's id *value*
(`selection_id`). The dispatcher stamps all three; panels are unaware. Reads
are node-scoped (a panel sees the cursor only when it indexes that panel's
node), the Detail tab follows `selection_node` by default, and
`{selection_id:UInt64}` cross-filters correctly regardless of node or
ordering because it is a key, not a position.

## Tabs, panels, channels

Every dock tab is a registered `TabSpec`; the dock block is one loop (slice
6a). Result panels are the specs carrying a `PanelI` — typed input
channels, a pure per-channel accept/reject negotiation, and a render over
the filled channels (SD6) — while Editor, Preview, History, Snippets,
Graph, Diagnostics, and the Map register as chrome with no PanelI (SD7 as
structure; the Map is a driver over its own panel-authored node). The tab
set is instance-scoped and frozen at first render: an embedder customizes
it between construction and mounting via `Tabs().Add/Replace/Remove`, with
dock ids frozen so persisted layouts survive (built-ins 1..13, embedders
≥64). Two extension granularities stay deliberately distinct (D5): the
registry works at tab level; body-level hooks such as `SetDetailContent`
remain panel-owned seams.

Per-panel **bindings** (6c) point one tab at one split node: the tab's
frame view swaps to that node's lane view, its title names the node
("Table · recent"), and per-node loading/error present through the tab's
existing states. Bindings are presentation-side — Run still executes the
fused sink and the status bar keeps tracking `main`. They key on CTE names,
survive Runs, sit inert while a split lacks the name, and revive when it
returns.

## What is deliberately absent

- **A scoping subsystem.** Scope *is* the reference graph — a name is
  visible where a CTE reference reaches it. This guardrail is recorded with
  the prior-art survey in ADR-0097 (Superset's contrast).
- **Operator-level incremental view maintenance** (SD1). Node-level
  re-execution against ClickHouse is nowhere near the bottleneck.
- **Server-side materialization of shared intermediates** (SD13). Recompute
  per observer until it measurably hurts; the options are recorded.
- **Explicit multi-cell authoring** (SD12). The buffer-split carries the
  notebook feel without a second authoring surface.
- **A generalized wrappable-panel API.** One narrow body hook serves the
  one consumer; unification has a recorded trigger (slice-6 D5).

Each absence has a trigger in the ADRs; reaching for one of these means
re-reading the recorded reasoning first, not re-deriving it.

## Reading list

- [ADR-0097 — the reactive query-graph](../adr/0097-play-reactive-query-graph.md):
  the primary record — laws, prior-art survey, and every slice's dated
  Update (the split, lanes, channels, the signal store, the tab registry,
  bindings).
- [ADR-0096 — the geo-raster Map panel](../adr/0096-play-geo-raster-map-panel.md):
  the panel-authored node pattern and the reserved `vp_*` viewport slots.
- [ADR-0108 — the SQL pass registry](../adr/0108-keelson-sql-pass-registry.md):
  the pre-execute rewrite seam and the "as sent" wire preview.
- [ADR-0114 — the World choropleth](../adr/0114-play-world-choropleth-panel.md):
  a result panel built against the channel contract.
- [ADR-0009 — the env-var registry](../adr/0009-environment-variable-registry.md):
  where the `BOXER_PLAY_*` automation knobs are declared.
- The app's help book (`apps/play/help/`): the end-user description of the
  same features.
- [How-to: the ADS-B demo map](../howto/play-adsb-map.md) and
  [how-to: pluggable Detail](../howto/play-pluggable-detail.md): task-level
  entry points.
