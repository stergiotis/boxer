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

Deferred (with triggers, per SD12): **3f** re-expressing the ADR-0096 Map as a
node (retiring the first bespoke panel-local lane); operator-level IVM (SD1);
cross-query materialization of shared intermediates over HTTP (SD13's hard part —
the first cut recomputes per observer); explicit multi-cell authoring (SD12).

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
