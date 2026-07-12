---
type: adr
status: accepted
date: 2026-06-27
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-27
---

# ADR-0097: `play` as a reactive query-graph — panels as observers over a nanopass-split dataflow DAG

## Context

`play` is a graphical ClickHouse SQL playground: a `DockArea` of tabs over one
query result, pulled back as Arrow (ADR-0094 wires the in-process keelson
`/query` endpoint). The window's tabs are the thing this ADR calls **panels**.
There is no `Panel` concept in the code — only hand-written `render*Tab`
methods with mismatched signatures threaded ad-hoc from `Render()`:

```
renderEditorTab()                              // input
renderPreviewTab()  renderHistoryTab()         // input/derived
renderTableTab(rec, schema, numRows, err)      // result consumer
renderProjectionTab(rec, err)                  // result consumer
renderTimelineTab(rec, schema, executed, err)  // result consumer
renderDetailTab(rec, schema, row)              // result consumer + selection
mapDriver.Render()                             // self-driven, own query lane
```

Four structural facts describe the *de facto* panel concept today, and each is a
rigor gap this ADR closes:

1. **One shared result.** `QueryStore` (`play_store.go`) is single-flight: one
   `Snapshot()` per frame fans out to every consumer. Table/Projection/Timeline/
   Detail are pure views over it.
2. **Panel-local lanes are the unowned escape hatch.** Timeline-bands
   (`play_timeline_bands.go`) and the ADR-0096 Map each run their *own* async
   query — deliberately **not** `QueryStore`, because the shared store would feed
   them garbage and flood history. This split is a convention, not a concept.
   ADR-0096 spends most of its length working *around* the single-flight model
   (SD1, SD2, SD9) rather than against a model that admits it.
3. **Selection is a raw shared `int64`.** `inst.selectedRow` is written by Table,
   Timeline (`*selectedRow = …`), and Projection; read by Detail. No typed cursor.
4. **Capability negotiation exists in exactly one place.** Timeline's `Mode +
   Reject`-reason pattern (`play_timeline.go`) — declare the column shape you
   accept; if the result does not fit, surface *why* as an empty-state — is the
   one rigorous contract. Detail auto-detects leeway-vs-ad-hoc; Map needs reserved
   `vp_*` params. Each reinvents the negotiation.

ADR-0096 also surfaced a powerful latent idea: **reserved params** (`vp_*`) are
how an interactive panel *drives* a query — pan/zoom become typed param mutations
on a panel-local lane. That is the seed of a general interaction model, but it is
never generalized beyond the one map panel.

We want to strengthen "panel" into a rigorous concept: a precise notion of what a
panel consumes, how it declares fitness to render, and how interaction re-drives
data — and to do so in a way that *explains* facts 1–4 as instances of one model
rather than special cases. The decided direction (design dialogue, 2026-06-27) is
a **reactive query-graph**: the editor program splits into multiple independent
query nodes, each node compiled by a nanopass pipeline, with parameter mutations
propagating through the graph and re-issuing dependent nodes. Scope is **`play`-
local** (no extracted package). A survey of classical reactive-DAG prior art
preceded the decision and supplies the vocabulary below.

## Design space (QOC)

**Question.** Where does a `play` panel's data come from, and how does
interaction re-drive it?

**Options.**

- **O1 — One shared result; panels are views.** Status quo: a single
  single-flight result; interactive own-query panels stay narrow, sanctioned
  exceptions (today's Map/bands).
- **O2 — Named data sources.** Generalize the shared-result-vs-lane split into
  named sources; a panel binds a source by name; panel-local queries become
  first-class. No reactive propagation.
- **O3 — Params/selection reactive spine, single result.** Interaction mutates
  typed signals that re-drive *the* query; generalizes ADR-0096's reserved params
  but keeps one result set.
- **O4 — Reactive query-graph (chosen).** The editor program is split by a
  nanopass pass into a DAG of independent query nodes; params are signals;
  panels are demand-driven observers; re-evaluation is incremental (minimal +
  early-cutoff) and glitch-free. Subsumes O1–O3 as special cases.

**Criteria.**

- **C1 — Resolves the single-flight tension** that ADR-0096 fights.
- **C2 — Formal semantics** — glitch-freedom, minimality, early cutoff, demand
  are nameable laws, not per-panel habits.
- **C3 — Reuse of existing seams** (`QueryStore`, param-injection, nanopass,
  `contentVersion`, the Timeline contract).
- **C4 — Authoring cost / new surface** in `play` (lower is better).
- **C5 — Cost discipline** — queries that nothing observes do not run.
- **C6 — Forward path** — linked brushing, live tailing, more panels.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | −  | ++ |
| C2 | −  | +  | +  | ++ |
| C3 | ++ | +  | +  | +  |
| C4 | ++ | +  | +  | −  |
| C5 | +  | +  | −  | ++ |
| C6 | −− | +  | +  | ++ |

O4 is the most capable and the only option that makes the semantics a set of
laws (C2) and lets a query that nothing observes simply not run (C5). Its one
real cost is C4 — a multi-node program is more machinery than one buffer. The
**nanopass auto-split** decision (SD3) is precisely what bounds that cost:
ordinary single-statement SQL stays a single node and behaves exactly as today,
so the new surface is opt-in, paid only when a query actually splits.

## Decision

We will model `play`'s body as a **reactive query-graph**: a small demand-driven,
memoized, glitch-free dataflow runtime, with **ClickHouse as the compute engine**
and **nanopass as the per-node compiler**. Every term is borrowed from prior art
and is load-bearing:

- A **node** is a query: `node SQL → nanopass pipeline → param substitution →
  execute → Arrow result`. The pipeline is a pure function of its inputs, so a
  node is a memoizable cell (Salsa/Adapton *query*).
- A **signal** is an **unbound, typed parameter** — an `{name:Type}` slot with no
  `SET param_*` binding (`env`'s *unresolved* param). A node's signals are exactly
  its unbound params; a *bound* param is a constant. Params **unify by name**
  across nodes, so a shared name is a global variable (the Grafana model). The one
  axis of variation is who writes a signal's value: a typed **widget** (human
  input) or a **panel** via `Emit` (selection, viewport, brush). Signals are graph
  inputs (Reactive Vega *signals*; Observable `viewof`). See SD8.
- A **panel is an observer bound to exactly one node** (SD7 fixes this
  denotation). A visible panel *demands* its node; a hidden panel demands
  nothing, so its query never runs (Adapton *observer*). This is the principled
  form of today's ad-hoc panel-local lanes.
- The **propagation law** (SD4): a signal change dirties dependents transitively;
  they re-clean **on demand**, in topological/height order (**glitch-free**); a
  node re-executes only if a dependency changed (**minimality**); and if a
  re-run yields an unchanged result, dependents do **not** re-run (**early
  cutoff** — `play`'s existing `contentVersion` promoted to a graph law).
- The **graph is derived by static analysis** that nanopass already provides
  (SD3): CTE/table references give data edges, unresolved param slots give signal
  edges. No new authoring surface for the common case.
- Every panel implements a **typed accept/reject contract** (SD6): given the
  bound node's schema and the signal environment, return a claim or a
  human-facing reject reason — Timeline's `Mode/Reject` generalized to all panels.

This subsumes the single-flight constraint ADR-0096 fights: the Map is just a
node whose viewport signal is published by its own observer; the shared result
is the `main` node; panel-local lanes are ordinary sibling nodes.

### The interface (the contract to ratify)

The rigorous shape, `play`-local. Names follow house conventions (`I`-suffixed
interfaces, `E`-suffixed enums, receiver `inst`):

```go
// A node in the query-graph: compiled SQL + the signals it reads.
type NodeID string

type Node struct {
    ID       NodeID
    BaseSQL  string         // the slice of editor text this node owns
    Pipeline nanopass.Pass  // the per-node compiler (canonicalize, qualify, FORMAT ArrowStream, …)
    DependsOn []NodeID      // data edges (CTE/named-result references)
    Reads     []SignalID    // signal edges (unresolved param slots)
}

// A signal IS an unbound param; its id is the param name (SD8). Same name across
// nodes ⇒ one shared input. Mutations bump the graph revision.
type SignalID = string // a param name; unifies signals across nodes by name
type SignalEnvI interface {
    Get(id SignalID) (env.Param, bool) // the unbound (unresolved) params + current values
    Revision() uint64                  // monotone; identifies a consistent generation (SD4)
}
type SignalEmitterI interface {
    // A panel writes a param's value (selection, viewport, brush) — the same
    // param a widget would otherwise bind. Nodes referencing it depend on it.
    Emit(id SignalID, value any)
}

// A panel is an observer bound to exactly one node.
type PanelI interface {
    ID() PanelID
    BoundNode() NodeID

    // Capability negotiation (SD6): can this panel render the node's output?
    // A non-empty claim means yes; otherwise reason is the empty-state text
    // (the Timeline Reject idiom). Pure — no side effects, no rendering.
    Accept(schema *arrow.Schema, sig SignalEnvI) (claim PanelClaim, reason string)

    // Render the node's result. Called only when Accept returned a claim, and
    // only while the panel is visible (demand). May publish signal mutations
    // back into the graph via emit — the viewof producer/consumer duality.
    Render(rec arrow.RecordBatch, claim PanelClaim, emit SignalEmitterI)
}
```

`PanelClaim` is the panel's interpretation of the columns — Timeline's `Mode` +
slot bindings, Detail's leeway-vs-ad-hoc choice, Map's framebuffer mapping —
computed once in `Accept` and consumed in `Render`, mirroring today's Timeline
`classify → render` split.

### Subsidiary design decisions

- **SD1 — node-level incrementality, not operator IVM.** The runtime decides
  *which queries to (re)issue*, memoizes whole result sets, and applies early
  cutoff; ClickHouse does all compute. Operator-level incremental view
  maintenance (DBSP/Materialize: a param delta yields a result delta) is
  explicitly **out** — we do not own ClickHouse's execution and talk to it over
  HTTP. IVM is the forward-path reference (a future engine boundary), not this
  cut. This boundary is what keeps `play` a playground, not a stream processor.

- **SD2 — demand-driven (pull), not eager (push).** A node runs only when a
  visible panel observes it (or a transitive dependency of one does). Switching
  to a hidden tab triggers its node's first run; this is the correct, principled
  version of the lazy panel-local lanes that exist today. Rationale: ClickHouse
  queries cost real time and engine resources — never spend one on a result
  nothing is showing. The Adapton inner/observer separation is the model. Hex took
  the opposite default — eager push downstream plus an aggressive query cache
  (same query within ~60 min returns cached) — trading "always pre-warmed" for
  computing cones the user may never look at; its background-refresh-on-app-open is
  the hybrid. We keep demand-driven (first look at a tab pays its latency once,
  nothing unseen runs) and get the cache for free as early-cutoff memoization (SD4).

- **SD3 — the graph is recovered from SQL by nanopass static analysis; the user
  writes ordinary SQL.** Splitting is two levels, both leaning on existing
  machinery:
  1. **Statement split** — a quote/comment-aware split on top-level `;` (the one
     new primitive; it mirrors the existing quote-aware discard-marker scan).
     Each statement is a candidate node.
  2. **CTE lift** — `BuildScopes` exposes each statement's `CTEDef`s (with `Name`
     and body `Scopes`); a CTE *reference* surfaces as `TableSource.IsCTE`. A
     top-level CTE may be promoted to its own node; the reference becomes a data
     edge. `env.Extract` already harvests `{name:Type}` slots and the `SET
     param_*` prelude into `env.Params`, so a node's **signal edges are its
     unresolved params** for free. `analysis.ExtractTables` corroborates
     cross-node references.

  Consequence: a single ordinary statement with no promotable CTEs is exactly one
  node — today's behaviour, unchanged. Splitting is opt-in and emergent, never
  imposed. The splitter is an *analysis* over the CST (it emits the node/edge
  structure), distinct from the per-node *compile* pipeline.

- **SD4 — glitch-freedom via revisions; minimality + early cutoff.** Each signal
  mutation bumps a monotone **revision** (Salsa's "revision"; Vega's consistent
  propagation). A node's cached result records the revision it was computed at and
  a content hash of its compiled SQL + bound signal values. On demand, the runtime
  re-cleans the dependency cone in topological/height order; a node re-executes
  only if a dependency's content hash changed (**minimality**), and a re-run whose
  result hash is unchanged does **not** dirty its dependents (**early cutoff**). A
  panel renders only a result whose whole dependency cone is consistent at one
  revision — no diamond glitch (Build Systems à la Carte: a *suspending* scheduler
  + a *verifying-trace* rebuilder).

- **SD5 — async via a suspending scheduler with supersession.** Nodes are async
  (HTTP to ClickHouse), so the scheduler suspends a node until its dependencies'
  results land (Observable's promise-aware runtime). A new inbound signal value
  while a node is in flight **supersedes** the running query: panel-owned context
  cancel + a stable `query_id` with `replace_running_query=1`, retaining the
  last-good result until the new one arrives. This is ADR-0096 SD9 generalized
  from the one map panel to every node.

- **SD6 — typed accept/reject for every panel.** `Accept(schema, sig) → (claim,
  reason)` generalizes Timeline's `Mode/Reject`: a non-empty claim renders; a
  reason becomes the uniform, self-documenting empty-state. Detail's leeway-vs-
  ad-hoc detection and Map's framebuffer-shape check become two `Accept`
  implementations rather than two bespoke idioms. `Accept` is pure and cheap so
  it can run every frame before render.

- **SD7 — "panel" denotes an observer bound to a node.** Editor, Preview,
  History, and Snippets are **not** panels — they are *graph chrome* (authoring
  and inspection surfaces) with their own role, outside `PanelI`. This narrows
  the concept to exactly "a typed view over one node," which is the rigor the
  request asks for: the dock's tab inventory splits cleanly into observers
  (panels) and chrome.

- **SD8 — signals are unbound params; one namespace, shared by name.** A signal is
  an `{name:Type}` slot with no `SET` binding (`env`'s unresolved param); a bound
  param is a constant, not an input. There is **no separate "ambient context"** and
  **no human-vs-panel namespace split** — Kibana's unified-search context and
  Grafana's dashboard variables both reduce to this one rule. The only variation is
  who writes the value: a typed **widget** or a **panel** via `Emit`.
  `inst.selectedRow` becomes a `selection` param; ADR-0096's reserved `vp_*` are
  just *panel-written* params; a dashboard time range is an unbound
  `{t_min:DateTime}` / `{t_max:DateTime}` referenced wherever the author means it
  (no column-guessing auto-injection into arbitrary SQL — the `%context%` magic
  Kibana users keep filing issues about). **Sharing is by name** (global-by-name):
  two nodes referencing the same name share one input; distinct names are distinct
  inputs. Author-chosen names make collisions controllable (the Grafana default);
  node-local scoping of a name is descoped (SD12).

- **SD9 — acyclicity is enforced; cycles are a reject.** The recovered graph must
  be a DAG. A reference cycle across nodes is rejected at split time and surfaced
  the same way a panel reject is (an empty-state on the affected panels), never a
  silent hang. ClickHouse recursive CTEs stay *inside* a single node and are not
  a graph cycle.

- **SD10 — node identity is stable across edits.** A node's identity is its name
  (CTE name / statement index-or-label) plus the content hash of its compiled SQL
  and bound signal values. Editing node B leaves node A's hash unchanged, so A's
  cached Arrow result is reused (minimality across edits — the verifying-trace
  property). This is what makes the multi-node editor cheap to iterate in.

- **SD11 — per-node state replaces the single query FSM.** Each node carries
  Idle/Loading/Ready/Failed (`play_querystate.go` generalized per node); the
  status bar aggregates. History becomes per-node (or main-node only), resolving
  the "panel-local lanes flood history" problem (ADR-0096 SD2) by construction.

- **SD12 — descope (deferred with triggers).** Operator-level IVM (SD1); explicit
  multi-cell authoring (a user-named-node surface — the auto-split path is the
  decided one, explicit authoring lands only if implicit graphs prove
  surprising); live tailing via generator nodes (Observable's generator idiom maps
  to streaming results); cross-panel brushing beyond a shared selection signal;
  node-local param scoping (the relief valve for SD8's global-by-name default).
  Each lands additively when a real need appears; none is foreclosed.

- **SD13 — fusion vs materialization: the node↔CTE isomorphism (the Hex lesson).**
  Splitting one buffer into nodes (SD3) and folding nodes back into one CTE query
  are inverse transforms over the same equivalence — Hex's "chained SQL" runs it in
  the *execution* direction, combining same-connection SQL cells into CTEs pushed
  down to the warehouse. So a node graph is **not** one round-trip per node. A
  demanded node is compiled by **fusing** its unobserved, same-engine ancestors into
  a single pushed-down CTE query; a node is **materialized** (executed standalone,
  its Arrow result memoized per SD1/SD10) only at a boundary: it is observed by a
  panel, it is a shared dependency of ≥2 downstream nodes (materialize once, reuse),
  it crosses an engine/source boundary (a different connection, a `url()` /
  introspection source, a dataframe), or it is explicitly pinned. Fusion is itself a
  nanopass transform (the splitter's inverse), so the graph has two compile
  directions — *split* for authoring/observation, *fuse* for execution. Early cutoff
  and minimality (SD4) apply at materialization points; a fused intermediate has no
  separate result to cut on, so the **materialization policy is the choice of where
  observation/caching granularity is worth a round-trip**. Same-engine is the gate
  (Hex's "same data connection" caveat); cross-source nodes always materialize.

## Alternatives

- **O1 — one shared result, views only.** Rejected: it *is* the status quo whose
  rigor gaps (single-flight tension, unowned lanes, raw selection, one-off
  contracts) prompted this ADR. Kept as the behaviour of the degenerate
  single-node graph.
- **O2 — named sources, no reactivity.** Rejected as the spine: it names the
  sources but leaves interaction and re-issue informal — it answers "where" but
  not "how does pan/zoom re-drive it." Its naming survives inside O4 (a node *is*
  a named source).
- **O3 — reactive single result.** Rejected: re-driving one shared result on
  every interaction reintroduces exactly the garbage-to-other-panels and
  history-flood problems the panel-local lanes were invented to avoid (C5/C1).
  O4 keeps the reactive spine but gives each consumer its own node.
- **Operator-level IVM (DBSP / differential dataflow).** Rejected for this cut
  (SD1): we do not own the engine; full incrementalization of arbitrary
  ClickHouse SQL over HTTP is out of scope. The forward-path reference, not a
  foreclosed option.
- **Eager/push propagation.** Rejected (SD2): simpler to reason about, but it runs
  hidden panels' queries — the wrong default when each query costs real engine
  time. Demand-driven is the cost-correct choice.
- **Explicit multi-cell authoring (dbt-style `ref`, Observable cells).** Not the
  first cut (SD3, SD12): the auto-split path keeps ordinary SQL working as one
  node and adds no surface. Explicit naming is the escape hatch if implicit graphs
  prove hard to read.
- **Kibana's model — per-panel `expressions` pipelines + one global unified-search
  context, RxJS push.** Studied as the mature, at-scale comparison; rejected as the
  *linkage* model. Panels share no node and link only through a single global bus
  (filters + query + time), so a scoped "panel X drives only panel Y" needs a
  drilldown hack, and its eager push needs a manual "Apply changes" gate to be
  affordable — the tell that demand-driven + early cutoff (SD2/SD4) is the
  cost-correct default. *Borrowed, not rejected:* the pure-serializable-pipeline-
  per-node unit (independent validation of the node model + memoization), **one
  data currency** (Arrow only — their `datatable`/`kibana_datatable`/`lens_multitable`
  fork is the warning), and **uniform emission** (their brush sometimes not
  emitting a select-range event is the cautionary tale SD6/SD8 avoid by
  construction). Their decoupled UI-Actions trigger/action registry is exactly the
  deferred drilldown story (SD12); its context-menu disambiguation tax is why it is
  deferred, not adopted.
- **Apache Superset cross-filtering — scoped linking via injected filters + a
  scoping subsystem.** The closest prior art to scoped panel-to-panel linking, and
  it *confirms scope is essential* (Superset added cross-filter scoping precisely
  because filtering all compatible charts is too blunt). But its linkage is
  **injected by column/dataset matching**, not referenced: a click filters every
  *compatible* chart (same dimension + dataset), so it needs a whole scoping
  subsystem to rein that in — a tree-UI to pick charts, global+per-chart overrides,
  tab scoping, dataset-compatibility guards — plus per-chart-type emit support
  (Calendar Heatmap silently doesn't emit; the Kibana brush problem again). In our
  model **scope *is* the reference graph** (a node reacts to a param iff it
  references it), so none of that subsystem exists; a cross-filter is sugar for
  adding a param reference (SD6 opt-in binding over the existing injection seam),
  not a broadcast that must then be scoped down.

## Consequences

### Positive

- **Facts 1–4 become one model.** Shared result = the `main` node; panel-local
  lanes = sibling nodes; selection = a signal; the Timeline contract = the panel
  contract. ADR-0096's machinery (reserved params, panel-local lane,
  supersession, `contentVersion`) is re-read as the general case, not a special
  one.
- **The semantics are laws, not habits** — glitch-freedom, minimality, early
  cutoff, demand, acyclicity each have a name and a place to test.
- **Cost discipline by construction** (SD2): a query nothing observes never runs.
- **Kibana bolts on as features what fall out here as instances of one primitive** —
  chained controls (a node edge), scoped panel-to-panel linking (a signal edge),
  and per-field reactive state (per-node state) are not three subsystems, and the
  "global context" is just unbound params shared by name (SD8), not a fourth.
- **Validated by Hex shipping the same model** — reactive DAG, dependencies inferred
  from references, input parameters that trigger only their downstream cone, and
  parallel independent branches. The node↔CTE isomorphism (SD13) is the same one
  Hex's "chained SQL" exploits in reverse, so the split/fuse correspondence is
  production-proven, not speculative.
- **No new authoring surface for the common case** (SD3): one ordinary statement
  is one node; splitting is opt-in and emergent.
- **Leans on existing seams**: nanopass (`BuildScopes`, `CTEDef`,
  `TableSource.IsCTE`, `env.Params`, `analysis.ExtractTables`), the param-
  injection lane, the `contentVersion` texture cache, the Timeline classify/render
  split.

### Negative

- **A reactive runtime is real machinery** — a scheduler, a dirty/clean pass, a
  revision counter, per-node memo + supersession. More than one `QueryStore`.
- **Implicit graphs can surprise** — a user may not expect their CTE to become an
  independently-cached node. The split must be inspectable, and explicit authoring
  (SD12) is the relief valve. Hex ships exactly this mitigation as a first-class
  **Graph view** and a per-cell **View compiled** — both validated and well-liked;
  treat them as required chrome (a node Graph view + a per-node "view the fused,
  param-substituted SQL", generalizing today's Preview tab), not optional polish.
- **The split adds an analysis hop** before execution; for a single trivial query
  it is pure overhead (bounded — one parse, no rewrite).
- **Acyclicity and identity are new invariants to get right** (SD9/SD10); a wrong
  identity hash silently serves stale results.

### Neutral

- The per-node compile pipeline is an ordinary nanopass `Sequence`
  (canonicalize, qualify, `SetFormat(ArrowStream)`, param injection) — no new
  pass framework, one new quote-aware statement splitter.
- The interface is `play`-local (decided scope); nothing is exported. If a second
  app later wants panels, extraction is a future ADR, not this one.

### Derived practices

- **A panel is an observer bound to one node** implementing `Accept/Render` — the
  shape every new panel follows; the Map and Timeline become two instances.
- **Unbound params are the one input channel** — human widgets and panel `Emit`
  write the *same* named params; interaction is a typed param write on the graph,
  never a bespoke per-panel lane nor a separate "global context" layer.
- **Fix the split at the splitter, fix canonicalization at the canonicalize
  pass** — the nanopass discipline (ADR-0002) extends to the new analysis.
- **Ship the graph as inspectable chrome** — a node Graph view plus a per-node
  "view compiled" (the fused, param-substituted SQL) are required, not optional;
  Hex's Graph view and View-compiled are the proof and the template.
- **Scope is the reference graph — don't build a scoping subsystem.** Superset's
  cross-filter scoping UI and dataset-compatibility guards are the symptom of
  filters injected by column-matching; because our nodes *reference* params, the
  scope is the graph itself, and a "cross-filter" is just an opt-in param reference,
  never a broadcast-then-scope mechanism.

## Status

Accepted — 2026-06-27 (reviewed-by: p@stergiotis). The two ratification gates are
ratified as the spec: (a) the `PanelI`/`Node`/`SignalEnvI` interface shape above and
(b) the split contract (statement split + CTE-lift, the data/signal edge
derivation). Implementation proceeds in slices, each buildable on its own:

- **Slice 1 (in progress) — the single-node runtime + contract.** Model today's
  behaviour as one `main` node with the `Accept/Render` contract and a
  demand-driven, memoized, revision-based runtime, proving the laws (minimality,
  early cutoff, demand) on a mock executor — *without* splitting or touching the
  live render path. Landed as `apps/play/play_graph.go` (+ `play_graph_test.go`).
- **Slice 2 — panel conversion + async.** Convert the `main`-result panels
  (Table/Projection/Timeline/Detail) to `PanelI` observers; wire the suspending
  async lane + supersession (SD5) and the per-frame demand set into `Render()`.
- **Slice 3 — splitting + fusion.** Enable CTE-lift + the statement splitter so a
  second node appears, fusion/materialization (SD13), and re-express the ADR-0096
  Map as a node whose viewport signal it emits — retiring the bespoke panel-local
  lane.

Descoped items (SD12) carry explicit triggers.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-06-28 — Slices 1–2 shipped; slice-3 first cut landed

Implemented on `main`, behaviour-verified live against ClickHouse:

- **Slice 1** — the `Node`/`Signal`/`PanelI` contract and a demand-driven,
  memoised, revision-based runtime (`apps/play/play_graph.go`), with the law
  tests (minimality, demand, early cutoff).
- **Slice 2** — all four result panels (Timeline, Detail, Table, Projection) are
  `PanelI` observers with the typed `Accept`/reject contract (SD6); selection is a
  panel-written signal (SD8, producer Timeline/Table/Projection → consumer
  Detail). The graph owns the `main` node's execution: the standalone
  `QueryStore` is retired into a graph-internal lane reached through a facade
  (`RunMain`/`MainSnapshot`/…), so the top bar, status bar, History tab and FSM
  read `main` through the graph. Smoke-tested end to end.
- **Slice-3 first cut** (3a+3b+3c, *fuse-to-sink*, *split-all-CTEs*) — the split
  contract `splitGraph` (statement-split + CTE-lift, data/signal edges,
  acyclicity); the suspending async node lane `nodeLane` (SD5: non-blocking
  demand, generation-tagged supersession, last-good retention — built and
  `-race`-tested, not yet on the live path); and the live Run path routed through
  `fuseToSink` (for a single statement the fused SQL is the original, so it is
  behaviour-identical — verified with a CTE query rendering 50 rows). The `main`
  node's lane stays `QueryStore` (Run-triggered, with history).
- **Slice 3 — Graph view (3e)** — a Graph dock tab (chrome, not a panel, SD7)
  renders the split graph from the last-run buffer: each node as a default-open
  header with its data edges, signal edges, and compiled SQL. The implicit graph
  is now visible (the Hex-validated implicit-graph-surprise mitigation). Smoke-
  tested on the CTE-chain snippet.
- **Slice 3 — observe-a-node (3d)** — generalized `fuseNode` (any node → its
  fused executable: the sink's whole statement, or a CTE's `WITH <transitive
  deps> <body>`); clicking "observe in panels" on a Graph-view node makes the
  result panels render THAT node's result — the canonical "a panel observes a
  node" model. An observed intermediate materialises its fused SQL on its own
  lane: a second `QueryStore` (like main's; the lean `nodeLane` stays reserved
  for the future many-node / streaming case). Smoke-tested: observing an
  intermediate CTE renders its 50 full leeway rows in Table/Detail, not the
  sink's aggregation.
- **Slice 3 — Map as a node (3f)** — the ADR-0096 geo-raster Map is now a graph
  node executed on a `nodeLane`; its bespoke panel-local fetch (`runFetch` /
  `maybeFetch` / `inFlight` + staleness guard) — a reimplementation of the lane's
  non-blocking demand / supersession / last-good — is retired, the first bespoke
  lane folded into the graph runtime. The Map keeps its specialised parts
  (viewport→bbox SQL, RGBA packing, the `mapRaster` overlay); only the async
  machinery moved. This makes `nodeLane` (3b) live. `nodeLane` gained a
  per-execution timeout (remote sources need ~60s). Smoke-tested against a
  synthetic mercator table — a 960×560 raster overlay rendered end to end. **With
  3f, slice 3 (the splitter, lanes, fusion, Graph view, observe-a-node, Map-as-
  node) is complete.**

Still deferred (with triggers, per SD12): operator-level IVM (SD1);
cross-query materialization of *shared* intermediates over HTTP (SD13's hard
part — the first cut recomputes per observer); explicit multi-cell authoring
(SD12).

### 2026-06-28 — Slice 4 (design): panels gain typed input channels (amends SD6, SD7, the interface)

A design amendment ratified for implementation; the slices land as their own
shipped-milestone Updates. Exploring how the Timeline really works surfaced a
refinement of the panel contract. The Timeline already has two data inputs —
foreground **events** (`_tl_*`) and background **bands** (`_tl_band_*`, a disjoint
contract authored as a separate SQL on a bespoke panel-local lane). SD6/SD7
modelled a panel as an observer of *one* node, which cannot express this; the
bands stayed a hand-rolled lane — the twin of the Map lane retired in 3f.

**Decision: a panel declares one or more typed input _channels_; each is filled
by an eligible node.** This amends two SDs and the interface:

- **SD6 (accept/reject) → per-channel.** `Accept(schema, sig)` becomes
  `AcceptForChannel(channel, schema, sig)`: eligibility is asked per (node ×
  channel); a node may be eligible for zero, one, or several channels. The claim
  (Timeline `Mode`, Detail's leeway-vs-ad-hoc, …) is per-channel; the Points/
  Intervals/Annotations modes stay claims *within* the events channel — channels
  are the higher axis.
- **SD7 (one node per panel) → a binding per channel.** A panel binds a node per
  channel, not one total. Single-channel panels (Table, Detail, Projection: one
  `{main, required}` channel) are behaviour-identical to the shipped versions. The
  Timeline declares `{events, required}` + `{bands, optional}`; *renderable* = all
  required channels filled (bands missing → events still draw).

Revised interface (supersedes the single-`BoundNode` shape above):

```go
type PanelI interface {
    ID() PanelID
    Channels() []ChannelSpec
    AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (ChannelClaim, reason string)
    Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI)
}
type ChannelSpec   struct{ ID ChannelID; Required bool; Label string }
type ChannelResult struct{ Node NodeID; Rec arrow.RecordBatch; Claim ChannelClaim }
```

**Assignment is hybrid (auto-suggest + override).** Eligibility auto-fills each
channel — the disjoint `_tl_band_*` contract makes a bands node an unambiguous
auto-match — and the binding is overridable from the Graph view. The binding
generalizes 3d's single global `observedNode` into a `(panel, channel) → node`
map; the first cut keeps `observedNode` driving every main/events channel (no
regression) and *adds* the bands channel, with per-channel reassignment of the
main/events channels as a later generalization.

**The Timeline's events→bands coupling becomes a signal.** Today the bands SQL
carries `_time_data_min` / `_time_data_max` placeholders textually replaced with
the events result's time extent. In the channel model the Timeline `emit`s
`tl_extent` when it renders the events channel (it already computes that extent
for the axis); the bands node reads it as bound params and re-runs reactively. The
placeholder hack retires into the SD8 signal path — no new mechanism.

So three bespoke mechanisms fold into the model already built: the bands become a
**node** (retiring the last panel-local lane, after 3f's Map), the panel contract
generalizes to **multi-channel**, and the events→bands coupling becomes a
**signal→param** edge.

**Terminology — "channel", not "role".** `play` is downstream of leeway, where
*role* already means membership-role (ADR-0073); a panel-input "role" would
collide nominally across the two domains. "Layer" fit the Timeline's z-order but
was weak for non-stacking panels. "Channel" carries no such collision and reads as
a typed data feed.

Delivery: **4a** the channel contract (redefine `PanelI`; migrate all four panels,
three trivially single-channel; behaviour-identical); **4b** bands as a node + the
`tl_extent` signal (retire the bands lane); **4c** Graph-view channel UI (per-node
eligibility badges + assignment, generalizing "observe in panels").

### 2026-06-28 — Slice 4 shipped (channels; the Timeline is multi-channel; the model is visible)

- **4a — the channel contract.** `PanelI` is now `Channels()` +
  `AcceptForChannel(ch, schema, sig)` + `Render(filled map[ChannelID]ChannelResult,
  emit)` (`PanelClaim` → `ChannelClaim`); a `dispatchPanel` helper runs the
  per-channel accept and renders when every required channel is filled. All four
  panels are single-channel (Table/Projection/Detail `main`, Timeline `events`),
  so behaviour-identical. Smoke-tested (Table + Detail render through the dispatch).
- **4b — the Timeline is the first multi-channel panel.** *4b-1*: the bands'
  bespoke async (fetch goroutine, LRU cache, staleness guard, `bandsView` + mutex)
  retired into a `nodeLane` — the second and last panel-local lane folded into the
  graph runtime, after 3f's Map (net −138 lines). *4b-2*: the Timeline declares
  `{events, required}` + `{bands, optional}`; `renderTimelineTab` demands the bands
  node and offers its `_tl_band_*` result as the `chBands` input, `Render(filled)`
  maps it before the events render. Bands lag the events extent by one frame
  (absorbed by the fetch latency). The `tl_extent` coupling is handled by
  substituting the driver's live extent into the bands node's compiled SQL; a
  *global* `tl_extent` signal other nodes could read awaits the SD8 signal store
  (the current `playSignals` is a `selection` strangler). Unit-tested (the
  demand/map path + memo-hit, the 2-channel contract; `-race` clean); smoke-tested
  (events + bands render through the 2-channel dispatch). The bands SQL keeps its
  editor (option (a) — a panel-authored node, not folded into the main buffer).

- **4c — the Graph-view channel UI.** The Graph view shows a panel × channel
  inventory (read off `Channels()`) and a per-node eligibility badge (`also fills:
  Timeline·events`), inferred statically from the node's SQL (a `_tl_time`
  projection ⇒ events, `_tl_band_from` ⇒ bands; the universal main channel
  omitted — a text heuristic, not an executed-schema check). The 3d "observe"
  gesture stays the assignment.

With 4c, **slice 4 is complete** — panels declare typed input channels, the
Timeline composes events + bands, and the channel model is visible. ADR-0097
(slices 1–4) is shipped end to end. The remaining items are the long-horizon
deferrals: SD1 operator-level IVM; SD13 materialization of *shared* intermediates
over HTTP; the SD8 global signal store (which a global `tl_extent` other nodes
could read awaits); explicit multi-cell authoring (SD12); and per-channel
independent reassignment (a lane per bound node — beyond the 4c UI slice).

### 2026-07-05 — Adversarial-review remediation (split contract, forces, SD5 server half, teardown)

A deep adversarial review of `apps/play` (2026-07-04) confirmed six defects by
repro test plus several promise-vs-implementation gaps; all were remediated in
six commits. The corrections that amend this ADR's record:

- **Split contract hardened (3a).** The synthetic sink id steps aside when a
  user CTE claims the default name (`main (sink)`, …): a CTE literally named
  `main` previously aliased the sink — unreferenced, `fuseToSink` executed the
  CTE body instead of the statement (wrong results); referenced, the duplicate
  id read as a bogus "dependency cycle". Duplicate node ids are an explicit
  reject now. Buffers with more than one non-SET statement are rejected at
  split — previously only the last statement ran, silently; the Run path falls
  back to the raw buffer (the server reports its native multi-statement error)
  and the Graph tab surfaces the split failure instead of silently degrading.
  Statements-as-sibling-nodes stays the SD3 future slice.
- **The lane forces actually force.** `nodeLane.forget` now supersedes an
  in-flight run (generation bump + cancel): a completion landing after the
  force used to restore the memo, and the forced re-fetch never ran. The Map's
  Refresh reset only its request-dedup key, not the lane memo — a no-op since
  3f; it now forgets the lane too.
- **Early cutoff is live at the observers (SD4).** The Map repack and bands
  re-map guards key on the served result's content fingerprint instead of its
  SQL text: identical bytes re-derive nothing, and a same-SQL re-fetch with
  new data — the case the SQL-text guard missed — always re-derives.
- **Empty results keep their schema.** Zero-batch streams lost the stream
  schema in both `QueryStore` and `clientExecutor`; "ran, empty" is now
  distinguishable from "no result", and an empty bands fetch reads "0 bands"
  instead of "pending". Status lines stopped latching stale errors: lane
  errors are mirrored on every demand (nil clears), mapping/packing errors
  live in their own field.
- **SD5's server half shipped.** Supersession was client-side only (context
  cancel); ClickHouse does not kill read-only HTTP queries on connection close
  by default, so superseded raster/bands queries piled up server-side. Every
  lane/store now sends a stable labelled `query_id`
  (main/intermediate/map/bands) with `replace_running_query=1` — the promised
  ADR-0096 SD9 semantics, generalized. (The 2026-06-28 entry's
  "generation-tagged supersession — built and -race-tested" described the
  client half only.)
- **Teardown exists.** Unmount closes the graph (including the main lane), the
  intermediate/map/bands lanes, and detaches the projector; `QueryStore`
  gained `Close` (a late finish on a closed store is dropped); lane/store
  contexts are cancelled on completion (armed-timer leak).
- **One fewer bespoke mechanism.** The observed-intermediate drive
  (`driveIntermediate` + a dedicated `QueryStore` — a third hand-rolled
  supersession machine) folded onto a `nodeLane`; node results carry
  `Summary`/`executedAt`/`elapsed` so the status bar keeps parity. Two
  mechanisms remain by design: `QueryStore` for the Run-triggered `main` node
  (history), `nodeLane` for every demand-driven node.
- **Deliberate non-fixes.** Memoized failures do not auto-retry: demand runs
  per frame, so auto-retry would hammer the engine; the recovery affordances
  are the (now working) explicit forces plus a changed input. The
  `playSignals` strangler and the SD8 global signal store remain this ADR's
  standing deferral. The statement splitter stays unaware of dollar-quoted
  strings — grammar1 does not parse them, and the nanopass discipline says fix
  the grammar first, not the consumer.

### 2026-07-11 — Second adversarial review: split-contract edge fidelity, SET classification, loading provenance, lane corners (+ SD9 correction)

A second adversarial review confirmed seven defects, all in the seams this ADR
introduced; each is fixed with a regression test. The corrections that amend
this ADR's record:

- **Fusing a CTE whose body opens its own `WITH` no longer emits invalid
  SQL (3d).** `fuseNode` prepended a second `WITH` clause when the observed
  node's body had one (a nested CTE or scalar alias) — `WITH a AS (…) WITH x
  AS (…) SELECT …` — which the observe-in-panels gesture then executed. The
  transitive dep definitions now *continue* the body's own WITH list
  (comma-merged, deps first — the body's items may reference them, never the
  reverse). Whether a body opens a WITH is read off the token stream at split
  time, not the text.
- **Data edges are derived by resolution, not name matching (3a/3e).** Two
  fidelity gaps in the recovered graph the Graph view exists to make
  trustworthy: the sink's edges came from the root scope only, so a CTE
  referenced solely inside a derived table or a later UNION member drew no
  edge; and a CTE body's edges name-matched every CTE-flagged reference, so a
  body's *nested* CTE surfaced as a phantom edge to a nonexistent node — and a
  nested name shadowing a lifted one would have fused the wrong definition.
  Each reference is now resolved in the scope where it occurs (`ResolveCTE`)
  and contributes an edge only when it binds to a lifted top-level definition;
  the derivation descends FROM/expression subqueries and the node's own nested
  CTE bodies, but not sibling lifted bodies (those references are the sibling
  nodes' own edges).
- **SET classification is grammar-based.** The splitter classified prelude
  SETs with a textual prefix check while the client's `ExtractParams`
  classifies by grammar — the two disagreed on comment-prefixed or
  newline-broken SETs, and the splitter falsely rejected such buffers as
  multi-statement (execution stayed correct via the raw-buffer fallback, but
  the Graph tab mis-diagnosed the reason and the graph was lost). The splitter
  now parses the fragment and looks for a SET statement. One grammar fact
  shapes the probe: grammar1 accepts a SET only as the prelude of a following
  statement (a lone `SET …;` does not parse), so the fragment is completed
  with `;\nSELECT 1` — only a genuine single SET parses in that shape.
- **The result tabs' loading gate reads the ACTIVE snapshot (3d).** The
  spinner was gated on the `main` lane, but an observed intermediate loads on
  the intermediate lane — its first fetch rendered the "0 rows" empty-state
  instead of the spinner. The tabs now receive the loading flag from the same
  `activeSnapshot` the frame renders.
- **The picker load is a render-thread handoff.** `loadFromPicker` assigned
  the editor buffer from its goroutine while the render loop reads and writes
  it unlocked — a data race. The goroutine now stashes the loaded buffer under
  the pick mutex and `Render` consumes it once per frame; the editor buffer is
  render-thread-only.
- **SD9 correction — recursive CTEs.** SD9 states ClickHouse recursive CTEs
  "stay inside a single node and are not a graph cycle". In practice grammar1
  does not parse `WITH RECURSIVE`, so such a buffer fails the split entirely
  and takes the raw-buffer fallback (the server executes it verbatim; there is
  no graph) — it never reaches the cycle check. The acyclicity guard stays for
  contract honesty; the fix belongs in grammar1 (the nanopass discipline,
  ADR-0002), recorded as a trigger alongside the dollar-quoted-string gap.
  *Resolved same day — see the SD9-realized entry below.*
- **The lane honours minimality on flip-back (SD4/SD5).** A demand returning
  to the SQL the memo already serves — while a superseding run is in flight
  (A→B→A: pan away and back on the Map, or a bands-extent flip) — re-executed
  A although its memo was current. The lane now cancels the in-flight run and
  serves the memo; a forced re-fetch still re-executes (forget clears the
  served SQL, so a force never takes this path).
- **`nodeLane` gained the closed guard `QueryStore` already had.** A demand
  landing after `close` (a straggler frame during Unmount) started a query
  nothing would consume; closed lanes now drop demands and forgets.
- **Law-coverage note.** The slice-1 memoized, revision-based runtime
  (`queryGraph.demand`/`setSignal`/`beginFrame`) remains dormant on the live
  path: the law tests prove minimality/demand/early-cutoff there, while the
  live path's coverage is the lane tests plus the per-observer fingerprint
  guards. The two lane fixes above are those laws enforced at the lane, where
  the live path actually runs.

### 2026-07-11 — SD9 realized: `WITH RECURSIVE` lands in the grammar and the split contract

The trigger recorded above is executed. grammar1 and grammar2 now parse
`WITH RECURSIVE` (the modifier survives canonicalisation and the AST — it is
semantics, not sugar), `BuildScopes` makes a recursive definition visible
inside its own body (`CTEDef.Recursive`), and the split contract implements
SD9 as specified:

- A recursive CTE lifts as an ordinary node. Its self-reference resolves to
  its own lifted definition and is **skipped as a data edge** — the recursion
  stays inside the node, exactly as SD9 promised, so the split succeeds and
  the acyclicity guard is not involved.
- Observing a recursive node cannot execute the bare body (it references the
  node's own name); `fuseNode` materialises it as
  `WITH RECURSIVE <deps…,> <id> AS (<body>) SELECT * FROM <id>`. A node
  inlining a recursive dep gets the clause-wide `RECURSIVE` keyword; a
  non-recursive node whose body opens its own `WITH RECURSIVE` also takes the
  wrap form (the comma-merge cannot continue such a list). All three fused
  shapes verified executing on the server.
- The Graph view labels recursive nodes (`CTE (recursive)`).

The dollar-quoted-string gap remains the one open splitter trigger.

### 2026-07-11 — Slice 5 (design): the signal store — SD8's standing deferral ratified for implementation

A design amendment ratified in dialogue (2026-07-11); the slices land as their
own shipped-milestone Updates. SD8 defined the contract (a signal IS an unbound
param; one namespace shared by name; widgets and panels write the same names;
revisions for consistency) and the slice-1 runtime already implements the
mechanics (`setSignal`, copy-on-write env, monotone revision) — dormant. What
this amendment fixes is the store's relationship to the two systems that
already own parameter state: the editor buffer's `SET` prelude and the lanes'
memo keys.

**Four decisions taken:**

- **D1 — two-tier truth model.** A name **with** a `SET` in the buffer is a
  *constant*: buffer-owned, human-authored, run-gated, reproducible — today's
  semantics, untouched. A name **without** a `SET` is a *signal*: store-owned,
  live, revisioned, panel-writable. Adding a `SET` *pins* a signal into a
  constant (the constant shadows the store value, with a UI hint); deleting it
  frees the name back to live. This is SD8 read literally, and it preserves
  the buffer as a self-contained, reproducible artifact for everything the
  human bound. Rejected: *store-is-truth* (all values leave the buffer —
  breaks buffer-as-artifact; history, persistence, and copy-paste
  reproducibility would all need a parallel snapshot to mean anything);
  *buffer-is-truth with panel write-through* (a panning viewport rewrites the
  editor at interaction rate — the debounce, undo, and staleness churn that
  drove ADR-0096 panel-local in the first place).
- **D2 — liveness is a per-node policy bit.** Demand-driven nodes (Map raster,
  bands, observed intermediates, panel-authored nodes) re-drive automatically:
  they compile per frame, changed inputs supersede in flight (SD5). The `main`
  node stays Run-gated; a referenced signal write flips the query FSM to the
  `*Stale` twin (the staleness witness grows from "buffer changed" to "buffer
  or referenced-signal revision changed"). Preserves SD2's cost discipline;
  the Map's "live" checkbox is the template for a later opt-in toggle on
  `main`.
- **D3 — widgets stay prelude-authoring in v1.** Humans produce constants
  (unchanged `SyncParamPrelude` path); panels produce signals (`selection`,
  `vp_*`, `tl_extent`). A referenced name that is neither SET-bound nor
  signal-written is an *unfilled input* — the bound panel shows a "set
  parameter {x}" empty-state (the SD6 idiom) instead of the server error.
  Signal-writing widgets arrive later together with the live toggle, as one
  coherent step.
- **D4 — reproducibility from day one.** `HistoryEntry` gains an additive
  signal-env snapshot (name→raw); restoring a history entry seeds the store
  alongside the buffer. Signals do not persist across sessions (live state);
  constants persist via the buffer as today.

**Mechanics (subsidiary, ratified with the above):**

- **Wire channel** — signal values ride the same `param_*` URL channel as
  bound params (one input currency; ClickHouse does the typed substitution; no
  literal-encoding surface). A node compiles to `(sql, params)`; the lane memo
  key becomes that pair (today: SQL text only); `ExecuteArrowStream` gains a
  direct params argument beside `BuildStatement`'s harvested ones. The
  "as sent" preview names signal values in its caption.
- **Frame-snapshot consistency** — emits apply to the store immediately, but
  each frame compiles against one env snapshot taken at frame top: every panel
  in a frame sees a single revision; an emit lands next frame (the one-frame
  lag the bands already accept). This is SD4's glitch-freedom operationalized
  without a scheduler.
- **Ownership** — the store lives on the graph (the dormant slice-1 signal API
  promoted to live); render-thread writes, immutable snapshots for async
  readers.
- **Conflicts are visible, never silent** — the same name declared with
  different types across nodes: each node encodes with its own declared type
  plus a Graph-view warning; a bound param shadowing a live signal gets the
  same hint.
- **Chrome** — a read-only Signals section in the Graph view (name, type,
  value, last writer, revision): the implicit-input surface made visible, same
  rationale as the node Graph view.

**Delivery slices** (each additive, buildable): **5a** the store + frame
snapshot + compile-with-params on the lanes + FSM staleness + history
snapshot; **5b** `selection` becomes a real signal — `playSignals`,
`selectedRowEmitter`, and `emptySignals` retire, behaviour-identical; **5c**
the Map returns to the param seam — the raster becomes a panel-authored node
with `{vp_*}` slots, the viewport emits signals, `buildRasterSQL` retires
(closing the ADR-0096 SD6 divergence recorded in that ADR's 2026-07-10
Update); **5d** the bands' `_time_data_min/max` textual substitution becomes
`{tl_min}/{tl_max}` slots fed by a Timeline-emitted extent signal; **5e**
(deferred) signal-writing widgets + per-node live toggles. End state retires
four bespoke mechanisms into one primitive: the two strangler `SignalEnvI`
stubs, the selection bridge, the Map literal rebuild, and the bands
placeholder hack.

### 2026-07-11 — Slice 5a shipped (the signal-store substrate)

The substrate is live; with an empty store it is behaviour-identical (the
first writers arrive in 5b):

- **The store is the slice-1 signal env, promoted**: `setSignalRaw` writes a
  bare name → raw value (revision bumps only on change), and `Render` takes
  one immutable snapshot per frame — every compile and consumer in a frame
  sees a single revision, an emit lands next frame.
- **Nodes compile to `(sql, params)`** — the new `compiledNode`, whose
  order-insensitive `key()` is the memo identity on the runtime memo AND the
  lanes: the same SQL under a moved signal value re-executes; an unchanged
  pair memo-hits. `ExecuteArrowStream` gained the signals argument; values
  ride the `param_*` URL channel and the harvested SET constants are applied
  second, so a bound name shadows a same-named signal (D1) at the wire too.
- **Run resolution**: a fresh parse of the Run buffer yields the slot list
  and the SET-bound set; unbound names with a store value ship as URL params
  and are recorded (with the bound set) for the staleness witness and the
  observed intermediates, which resolve their split `Reads` against the frame
  snapshot on the intermediate lane. The raw-fallback Run resolves nothing —
  the server reports, as for the SQL itself.
- **Staleness (D2)**: `observeQueryState` flips to the `*Stale` twin when the
  buffer's current resolution diverges from what the run shipped, and clears
  when it moves back — symmetric with edit/revert. O(#slots) per frame off
  the debounced caches, no parse.
- **History (D4)**: `HistoryEntry.SigParams` snapshots the run's signal
  inputs; restoring an entry seeds the store alongside the buffer.
- **Chrome**: the "as sent" preview captions the would-be signal resolution
  (`signals on URL: …`), refreshing on store-revision moves as well as
  buffer edits.

Regression tests cover the key identity, lane re-execution on a moved
signal, the wire channel with D1 shadowing, the history snapshot + seed
round-trip, unbound-only resolution (parse-failure resolves nothing), the
staleness flip/clear, and the observed-intermediate resolution end to end
against a capture server.

### 2026-07-11 — Slice 5b shipped (selection is a real signal; the stranglers retire)

The `selection` signal now lives in the store and the three strangler types
are gone:

- **One emit path.** `graphEmitter` (any-name, typed-value encoding via
  `encodeSignalValue`; an unencodable value is dropped with a warning) writes
  the store; every panel dispatch publishes through it and reads the frame
  snapshot (`frameSig`) as its env — Table, Projection, Timeline, Detail,
  Schema, and the ADR-0114 World panel, which had adopted the strangler
  pattern and migrated with the rest.
- **`PlayApp.selectedRow` is gone.** The per-frame clamp became
  `syncSelectionClamp`: an absent or out-of-range selection resets to row 0
  in the store (a fresh result still auto-selects its first row); an
  in-range one writes nothing, so the steady state does not churn the
  revision. The reset is visible from the next frame — this frame's panels
  guard out-of-range rows themselves, so the one-frame window is benign.
- **Propagation is uniformly next-frame** (frame-snapshot consistency, 5a).
  Previously a click propagated same-frame only to tabs rendered after the
  emitting tab — order-dependent; now every panel in a frame sees one
  revision. At interactive frame rates the one-frame lag is imperceptible.
- **The Timeline driver lost its PlayApp back-reference** (the `selectedRow`
  pointer fallback): selection publishes only through the per-frame emitter.
- **Retired:** `playSignals`, `selectedRowEmitter`, `emptySignals`, and the
  `selectionParam` helper. Panel tests now exercise the live store env
  (`sigWith`/`sigNone` build real snapshots) instead of stubs.

A consequence worth naming: `selection` is now an ordinary named signal, so a
buffer referencing `{selection:Int64}` participates in the D2 staleness
witness and ships the clicked row on the next Run — the first cross-filter
falls out of the model with no new mechanism, exactly as SD8 intended.

### 2026-07-11 — Slice 5c shipped (the Map on the param seam; ADR-0096 SD6 realized)

The third strangler-adjacent mechanism retires: the Map's literal-rebuilt
query. The raster is now a panel-authored node compiled against the signal
store:

- `rasterTemplateSQL` replaces `buildRasterSQL`: the node's SQL carries the
  six reserved `{vp_*:UInt32}` slots (ADR-0096 §SD6) and is **stable across
  pans** — the settled viewport is EMITTED as `vp_*` signals (uint32 mercator
  bbox + clamped output dims), the per-frame compile resolves the template's
  parsed Reads against the frame snapshot, and the lane's `(SQL, params)` key
  supersedes in flight on any change. Server-verified on the FILL bound
  (`WITH FILL … TO toUInt64({vp_w:UInt32}) * {vp_h:UInt32}` substitutes fine —
  the wiring check ADR-0096 left open).
- The overlay pin is **self-describing**: `laneView` exposes the served
  compiled params, and repack recovers dims + lat/lon bounds from the served
  `vp_*` values by inverse Web-Mercator — the demanded-SQL→bounds side table
  (`sqlMeta`/`rasterMeta`/`mapFetchKey`) retired with it. A served result
  lacking the vp set is a pack error, never a mis-pin.
- The request-dedup key is gone: the store dedups unchanged emits (a still,
  settled camera is write-free), and Refresh keeps its lane-forget force.
- `table`, `sampling`, and the colour render stay panel controls spliced into
  the template (the 2026-07-10 ADR-0096 shape); a control change is a
  template change. Custom colour blocks outside Grammar1 fall back to the
  reserved six for Reads derivation, so the viewport always resolves.
- The `vp_*` names are ordinary signals (SD8): any node referencing
  `{vp_min_x:UInt32}` now reacts to the map viewport — cross-filtering
  against the visible extent costs nothing new.

Regression tests: template well-formedness + Grammar1 parse + full vp Reads
per render mode; viewport emission values against the forward projection;
inverse-mercator round-trip; repack-from-served-params (including the
missing-param error path); Refresh re-execution; and the seam end to end
against a capture server — pan changes only `param_vp_*` on the wire, never
the SQL. The live-ClickHouse map test now exercises the template + params
path. Remaining slice-5 work: **5d** (bands extent onto `{tl_min}/{tl_max}`
slots) and deferred **5e**.

### 2026-07-11 — Slice 5d shipped (the bands extent rides `{tl_min}`/`{tl_max}` signals)

The last textual-substitution mechanism retires: `substituteBandsRange`
(string-replacing `_time_data_min`/`_time_data_max` with `toDateTime64`
literals) is deleted. The Timeline now **publishes** its events extent after
each rebuild — `tl_min`/`tl_max` signals carrying UTC-formatted
`DateTime64(3)` raw values — and the bands node reads them as ordinary
`{tl_min:DateTime64(3, 'UTC')}` / `{tl_max:…}` param slots, compiled against
the frame snapshot like any other node (server-verified: `DateTime64(3,
'UTC')` param slots substitute fine inside `WITH … AS lo`).

Semantics this sharpens beyond parity:

- **Extent gating is per-reference, not blanket**: the per-edit parse caches
  the SQL's slot names and SET-bound set; only a bands query that references
  an *unbound* `tl_min`/`tl_max` waits for the first events render. An
  extent-free bands query runs immediately — under the substitution regime it
  was needlessly extent-gated. A SET-bound `tl_*` pins as usual (D1).
- **A moved extent supersedes in flight** via the lane's `(SQL, params)` key;
  an unchanged (SQL, extent) pair stays a lane memo hit. The special-cased
  "bands input + extent" hash retired with the substitution.
- **Legacy SQL gets a migration hint**: a bands query still using the retired
  `_time_data_*` tokens would now reach the server unsubstituted and fail
  opaquely; the parse detects the tokens and the status line says what to
  write instead (highest-precedence hint, before lane/map errors).
- `tl_min`/`tl_max` are ordinary signals (SD8): any node — not just bands —
  can react to the events extent, e.g. a main-editor query windowed to
  `{tl_min:DateTime64(3, 'UTC')}`.

Regression tests: extent formatting; publish-gated-on-validity; the
pending→publish→demand→memo-hit→moved-extent-supersedes flow; the extent-free
immediate run; the legacy-token hint; and the seam on the wire against a
capture server (`param_tl_min`/`param_tl_max` on the URL, no legacy channel).
The help snippet migrated to the param form. Remaining slice-5 work: deferred
**5e** only.

### 2026-07-11 — Slice 5e shipped (signal-writing widgets + the `main` live toggle); slice 5 complete

The two halves the design deferred as "one coherent step" (D3) land
together, closing slice 5:

- **The Signals chrome is writable** — the read-only section the mechanics
  ratified arrives directly as the signal-writing widget, in the Graph tab.
  One row per held-or-referenced name: declared type(s), an editable raw
  value (a human write is `signals-editor` provenance, distinct from D1's
  prelude constants — it does NOT pin), a discard (frees the name; the
  staleness witness flips exactly as for a changed value), and a footer
  that adds an arbitrary name — e.g. one only a panel-authored node
  references. Value drafts are reseed-guarded: an idle draft follows
  external writes (live values stay live), typing wins while the store
  holds still.
- **Write provenance** — the store records each signal's last *changer*
  (writer id + revision; deduplicated re-sets update neither).
  `dispatchPanel` stamps the panel's ID onto the shared emitter, so panel
  writes carry provenance with no per-call-site plumbing; non-panel writers
  (`selection-clamp`, `history`, `map`, `signals-editor`) stamp explicitly.
- **Type conflicts are visible, never silent** (the ratified mechanics
  bullet): the chrome collects every slot occurrence's declared type across
  the buffer (no dedup — the per-buffer slot cache keeps first-wins) plus
  the reserved panel-signal types (`vp_*` UInt32, `tl_*` DateTime64,
  `selection` Int64), and warns on divergence — one shared value read
  through different casts.
- **The `main` live toggle** (D2's policy bit — `main` is its only
  Run-gated holder, so the surface is a single topbar checkbox, offered
  when the buffer has an unbound slot): with Live on, a referenced-signal
  move re-runs the unchanged buffer through the ordinary Run path.
  Buffer edits stay human-gated; an observed intermediate already re-drives
  on its lane; an in-flight run defers to its completion frame, so signal
  churn coalesces latest-wins at completion rate, not interaction rate.
  Auto-runs skip the SQL persist (the persistence point stays anchored to
  user intent).
- **Unfilled inputs block at the Run gate** (D3's empty-state, applied
  where it is cheapest): a referenced name that is neither SET-bound nor
  signal-written can only fail server-side, so Run — manual or live — is
  refused with an actionable reason (status bar, beside the FSM chip)
  and a standing topbar hint names the unfilled slots while typing. The
  raw-fallback path (unparseable buffer) still defers to the server.

Regression tests: provenance (writer/revision/dedup), deletion semantics,
the dispatcher stamp, the occurrence-level type collector, the chrome row
model (pinned/held/unfilled/conflict), the unfilled caches, the auto-run
decision gate by gate, and the blocked→filled→run and
run→diverge→auto-run→settle loops against a capture server.

**Slice 5 is complete**; the four bespoke mechanisms named in the design
Update are retired and both deferred halves are now built. Still deferred
(SD12 triggers): SD1 operator-IVM, SD13 shared-intermediate
materialization, explicit multi-cell authoring.

### 2026-07-12 — Slice 6 (design): first-class tabs — the panel registry

A design amendment settled in dialogue; the slices land as their own shipped
Updates. The PanelI contract (slice 4) made panels well-described — channels,
accept/reject, emit — but everything around it is hard-wired: the dock tabs
are a `uint64` const enum, each tab body a hand-written case in Render's dock
block, the scripted-focus knobs six hand-permuted copies of the tab order,
the channel inputs assembled inline per call site, and the Graph view's panel
inventory a hand-maintained list. One extension seam exists and is used in
production by an out-of-tree embedder that reuses PlayApp whole:
`SetDetailContent`, a body-level override on the Detail pane (the pluggable-
detail how-to). The registry generalizes the surround the same way slice 5
generalized the stranglers — and the repo already holds the idiom four times
(env ADR-0009, help books, pass registry ADR-0108, widget demos ADR-0057);
tabs are the odd subsystem still enum-wired.

**Five decisions taken:**

- **D1 — one registry for all dock tabs.** A tab registers
  `TabSpec{ID, DockID, Title, NoScroll, Panel, Render}`: `ID` a stable human
  slug (`"table"`, `"map"`), `Panel` a PanelI for result panels and nil for
  chrome — SD7's chrome/panel split survives structurally, as a field, not a
  parallel mechanism. The dock block becomes one loop; the panel inventory,
  the focus knobs, and the future binding UI fold over the same enumeration.
  Rejected: a result-panels-only registry — it leaves the tab enum, the
  hand-written dock cases, and the six focus permutations alive, which is
  most of the smell.
- **D2 — behaviour first, state ownership later.** Slice 6a registers the
  built-ins as closures over today's PlayApp state; `Render` takes a
  per-frame view (`TabFrame`: rec/schema/numRows/loading/err/executed +
  frameSig + emit) so the loop decouples even while state stays put.
  Per-panel state ownership (factory + panel context) migrates
  opportunistically, per tab, when something real needs it — an embedder
  tab, or 6c's per-panel lanes. Rejected: a big-bang ownership migration —
  wide churn through every panel with no behaviour change to show for it.
- **D3 — two-level identity, dock ids frozen.** The slug keys the
  human-facing surfaces (focus knobs, persistence namespaces, binding);
  the `uint64` DockID keys the Rust-side persisted dock layout and is
  frozen: built-ins keep their current 1..13, embedder tabs allocate ≥64,
  the registry rejects duplicates. Existing saved layouts survive
  unchanged. Rejected: derived/hashed dock ids — they orphan every
  persisted layout and are opaque in the dock-state debugging story.
- **D4 — instance-scoped, frozen at first render.** The tab set belongs to
  the PlayApp instance — `Tabs().Add/Replace/Remove` between construction
  and the first Render, frozen after — because the shell hosts multiple
  PlayApp embedders in one process, so a package-global mutable registry
  is wrong by construction. Initial layout comes from a `Zone` hint
  (editor / body / side / preview); new tabs append to the body zone;
  the focus knobs collapse to one reorder over the enumeration.
  Rejected: package-global registration (two embedders fight); runtime
  add/remove (no consumer, and it churns the persisted dock state).
- **D5 — granularities stay distinct.** The registry is tab-level.
  Body-level seams — `SetDetailContent` and the how-to's patterns — remain
  panel-owned extension points, unchanged: a tab replacement that existed
  only to append a section would have to re-implement the pane's gating.
  The non-tab core stays fixed: the editor buffer and Run lifecycle, the
  topbar, the status bar, the signal store. Rejected: subsuming the detail
  hook into `Replace("detail", …)`.

**Mechanics (subsidiary):** the per-tab ScrollArea wrappers move inside the
tab bodies so the dock loop is uniform (`NoScroll` covers the Map's
wheel-gesture opt-out); the `BOXER_PLAY_FOCUS_<ID>` env specs derive from
the built-in tab definitions at package init — embedder tabs declare their
own at their init, so ADR-0009's registration story is unchanged;
`resultPanels()` and the Graph view's channel inventory read the registry.
The TabSpec is deliberately where later work attaches: per-channel node
bindings (6c) and, eventually, contracts-as-data for accurate eligibility.

**Delivery slices:** **6a** the registry + the single dock loop + focus
collapse, behaviour-identical (tests: enumeration, focus reorder, dock-id
stability, unchanged channel inventory); **6b** the out-of-tree embedder
adds a domain tab through the API — the cross-repo proof — while its
`SetDetailContent` use stays untouched; **6c** per-panel/per-channel node
binding over the enumeration, with its own design refinement when reached
(binding lifetime across Runs, sink semantics when nothing observes it,
per-panel staleness presentation).

## References

Internal:

- [ADR-0096](0096-play-geo-raster-map-panel.md) — the geo-raster Map panel whose
  reserved-param channel, panel-local lane, supersession, and `contentVersion`
  this generalizes (its SD1/SD2/SD9 become the general case).
- [ADR-0094](0094-keelson-introspection-tables.md) — the in-process `/query`
  endpoint `play` reads.
- [ADR-0043](0043-imzero2-timeline-widget.md) — the Timeline whose `Mode/Reject`
  contract becomes the panel `Accept` contract.
- [ADR-0002](0002-nanopass-discipline.md), [ADR-0006](0006-nanopass-environment-and-first-class-pass.md),
  [ADR-0084](0084-nanopass-antlr-dfa-cache-bounding.md) — the nanopass substrate
  the splitter and per-node compiler use.
- [ADR-0057](0057-demo-registry-and-drivers.md) — the screenshot-tour path any
  new panels must remain capturable through.
- `apps/play/play_store.go`, `play_querystate.go`, `play_timeline.go`,
  `play_timeline_bands.go`, `play_param_inject.go`, `play_map.go` — the seams
  re-read by this model.

External prior art (the survey that informed the decision):

- Mokhov, Mitchell, Peyton Jones, *Build Systems à la Carte* (ICFP 2018) —
  scheduler × rebuilder decomposition; **minimality** and **early cutoff** as
  named properties; suspending scheduler + verifying trace (SD4).
- Satyanarayan et al., *Reactive Vega: A Streaming Dataflow Architecture for
  Declarative Interactive Visualization* (InfoVis 2015) — interaction events as
  first-class streaming sources; **signals**; linked-view brushing (the signal
  model, SD8).
- Observable reactive runtime — a dependency graph evaluated in dataflow order;
  promise/generator-aware; `viewof` producer/consumer duality (SD5, the panel
  duality).
- Hammer et al., *Adapton: Composable, Demand-Driven Incremental Computation*
  (PLDI 2014) — inner computations vs outer **observers**; demanded computation
  graph; dirty-then-reclean-on-demand (SD2).
- `salsa` (Rust; powers rust-analyzer) — memoized queries over inputs;
  **revisions** and red/green validation (SD4, SD10).
- Budiu et al., *DBSP: Automatic Incremental View Maintenance for Rich Query
  Languages* (VLDB 2023) — operator-level IVM; the forward-path reference
  deliberately **not** adopted (SD1).
- Elastic **Kibana** — the mature at-scale comparison (§Alternatives): the
  `expressions` pipeline (pure serializable per-panel dataflow), the embeddable
  framework + unified search (RxJS push, one global context), UI-Actions triggers
  (`VALUE_CLICK` / `SELECT_RANGE` / `APPLY_FILTER`) + drilldowns, and Controls
  (chained controls = a dependency edge). The global-bus ceiling and eager-push
  gate are what this ADR's fine-grained, demand-driven DAG avoids.
- **Grafana** dashboard variables — the model SD8 reduces to: a typed,
  dashboard-scoped input referenced by name across panels. Our unbound params
  *are* this, with no separate declaration step (the reference is the
  declaration).
- **Hex** — our model shipped as a product: a reactive cell-DAG over SQL with
  dependency inference from references, input-parameter cells (= unbound params), a
  Graph view, per-cell "View compiled", query caching + staleness, and **chained
  SQL** (same-connection cells fused into pushed-down CTEs) — the node↔CTE
  isomorphism of SD13, run in the execution direction. The strongest validation and
  the source of SD13.
- **marimo** — open-source reactive Python notebook that builds its dataflow graph
  by static analysis of variable references; corroborates dependency-inference +
  downstream-only re-execution as the reactive-notebook consensus.
- **Apache Superset** cross-filtering + native-filter scoping — the closest prior
  art to scoped panel-to-panel linking; confirms scope is essential but pays for a
  scoping subsystem because filters are injected by column/dataset matching, not
  referenced (§Alternatives). The thing it builds, our reference graph gives free.
