---
type: adr
status: accepted
date: 2026-07-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

# ADR-0153: `play` SQL flow-graph panel — a clause-level dataflow view of the active node

## Context

`play` has three graph-shaped surfaces, each with a distinct job:

- the **Graph** tab draws the *inter-node* reactive surface — split nodes,
  signals, tabs and their edges ([ADR-0097](./0097-play-reactive-query-graph.md));
- the **Network** tab draws a *query result* as a node-link graph
  ([ADR-0129](./0129-play-layered-graph-panel.md));
- the **Passes** tab draws the nanopass *pass catalog* as a schematic pipeline
  ([ADR-0119](./0119-imzero2-pipelineview-widget.md)).

None of them shows what happens *inside* one statement. Reading a non-trivial
SELECT means mentally reconstructing its dataflow — which sources feed which
joins, where rows are filtered, where aggregation happens, what the result
passes through on the way out. That reconstruction is exactly a graph, and the
machinery to recover it already exists: the split contract (ADR-0097 slice 3a,
`play_split.go`) parses the buffer with grammar1 and `BuildScopes`, which
classifies every table source (base table, CTE reference, subquery, table
function) and exposes each clause of a SELECT as a typed CST accessor.

A second motivation is forward-looking: ClickHouse can describe its own view
of a query (`EXPLAIN AST`, `EXPLAIN PLAN`, `EXPLAIN PIPELINE`). Those outputs
are also graphs, and a panel that renders "a dataflow graph of the current
query" is the natural place for them to land later. Today the repo uses
`EXPLAIN AST` only as a verdict probe
([ADR-0141](./0141-play-endpoint-dispatch-seam.md)) and `EXPLAIN SYNTAX` only
in an integration test; nothing parses EXPLAIN output.

The existing passthrough-table classifier (ADR-0117) is not reusable here: it
deliberately collapses a statement to the base tables it reads "1:1 as
stored", which erases the intra-statement structure this panel exists to show.

## Decision

Add a **Flow** tool tab to `play`: a clause-level logical dataflow graph of
the **active node's** SQL, derived statically from the grammar1 CST and drawn
with the existing `layeredgraph` widget. No server round-trip, no new IDL/FFI,
no new dependencies.

### SD1 — scope and granularity

The graph covers **one statement**: the active node (the observed node, else
the sink — `activeNodeID()`), the same resolution the result panels use.
Inter-node structure (CTE nodes, signals, tabs) stays the Graph tab's job;
the Flow tab begins where the System graph's boxes end.

Granularity is the **clause-level logical plan**: source nodes (base tables,
CTE references, subqueries, table functions) feed a left-deep join chain,
whose output passes through the clause stages in ClickHouse's logical order —
ARRAY JOIN → PREWHERE → WHERE → GROUP BY → HAVING → projection (SELECT list)
→ QUALIFY → DISTINCT → ORDER BY → LIMIT BY → LIMIT → result. Stages absent
from the statement are absent from the graph. UNION/EXCEPT/INTERSECT members
each get their own chain, merged by an operator node. Column-level lineage is
out of scope (see Alternatives).

### SD2 — derivation: static, play-local, IR with no CST types (the lens seam)

A pure, UI-free builder (`play_flow_model.go`, the `play_split.go` precedent)
maps SQL text to a small IR:

- `flowNode{ID, Label, Detail string; Kind flowNodeKind}` — kinds: the four
  source kinds, join, filter (PREWHERE/WHERE/HAVING/QUALIFY, distinguished by
  label), aggregate, project, distinct, sort, limit, union, result. `Detail`
  carries a whitespace-collapsed, truncated source snippet of the clause
  (via `ParseResult.SourceRangeOf`).
- `flowEdge{From, To, Label string}` — labels mark join inputs (`l`/`r`).
- `flowGraph{Nodes, Edges, Capped}`.

Derivation is `nanopass.Parse` + `nanopass.BuildScopes` plus a structural walk
of the union/join/clause accessors. CTE references stay **opaque** source
nodes — they are other split nodes, already drawn by the System graph; sibling
CTE names come from the split node's `DependsOn`. FROM-subqueries recurse into
their own chains (flat — layeredgraph has no clusters), bounded by a depth cap
and a node cap; beyond the cap a subquery stays opaque and the graph is marked
capped.

The IR deliberately contains **no CST types**: it is the seam future *lenses*
target. An EXPLAIN AST / PLAN / PIPELINE parser produces the same `flowGraph`
and the panel needs no change (PLAN/PIPELINE run against the server, so those
lenses ride a node lane and inherit the Run-hook invalidation the Network tab
needed). A lens interface is deliberately **not** introduced with one
producer; the trigger is the second one.

### SD3 — rendering: layeredgraph, cached layout, left-right default

The graph renders through the ADR-0069 stack: `GraphModel` →
`goccyengine.Shared()` (Graphviz-WASM `dot`) → `view.Render` with pan/zoom
`ViewState`. Layout is cached on a topology fingerprint (the Network tab's
`networkModelKey`, reused as-is — same package) so clicks and repaints never
re-run the layout engine. Rank direction defaults to **left-right** — a clause
spine reads like a pipeline, and the System graph made the same choice — with
the Network tab's segmented top-down/left-right toggle. Sources and stages are
boxes; union and result nodes are ellipses; stage kinds get subtle background
tints, the selected node the accent fill.

### SD4 — interaction: selection is local; snippet detail instead of editor highlight

Clicking a node toggles a **panel-local** highlight and shows the node's
clause snippet in a detail line under the canvas. Nothing is published to the
shared `selection` signal: ADR-0129 established that a panel whose nodes are
not observable split nodes cannot drive the node-scoped selection (the clamp
sends unbound cursors home), and a *clause* is not a row cursor at all.
Highlighting the clause's text range in the editor is the natural next
affordance but is deferred until the ADR-0130 TextEdit highlight seam has a
consumer path; the flow node's source range is retained in the model to keep
that door open.

### SD5 — inputs are Run-gated

The panel derives from `currentSplit` — the node graph of the **last Run** —
exactly like the Graph tab, so the two stay mutually consistent and node
identity is stable. It does not re-derive per keystroke. Live-on-edit and
caret-follow (the statement under the caret, the Docs-pane pattern) are
deferred; both are compatible with the memo design (the key is the SQL text).

### SD6 — tab wiring and caps

A tool pane, not a result panel: registered with no `PanelI`, no signal
writes, slug `flow`, title "Flow", lazy, in the body zone after Docs;
`dockTabFlow = 18` (dock ids are frozen; embedders start at 64).
`BOXER_PLAY_FOCUS_FLOW` derives automatically from the tab definition.
Caps: `flowMaxNodes = 160`, `flowMaxDepth = 4` (subquery/union nesting);
exceeding either stops expansion, keeps an opaque node, and reports in the
status line rather than truncating silently.

## Alternatives

- **`egui_graphs` binding** (interactive graph widget, used by godepview's
  live mode). Works, but holds retained Rust-side graph state that would need
  re-syncing on every topology change, and its force-directed interaction
  vocabulary differs from the sibling tabs. play's precedent for derived
  static DAGs is the cached-layout painter; rejected for this panel, not in
  general.
- **`pipelineview`** (ADR-0119). Its model is a series/parallel *stage tree*
  with closed port classes — it cannot express an arbitrary join DAG, and
  bending it would forfeit the layout guarantees that make it good at
  pipelines. It remains the natural renderer candidate for a future
  `EXPLAIN PIPELINE` lens, whose processor graph *is* pipeline-shaped.
- **Whole-buffer expansion** (every split node opened to clause level in one
  drawing). Duplicates the System graph's top level, needs clusters the
  widget does not have, and grows unreadable exactly when the buffer is
  interesting. The active-node scope composes with the observe gesture
  instead.
- **Sources-only graph** (tables/CTEs/subqueries feeding the statement).
  Strictly less informative than the clause plan and mostly duplicates edges
  the System graph already draws.
- **EXPLAIN-first** (parse ClickHouse EXPLAIN output instead of the CST).
  Needs a server, a round-trip policy, and output-format parsing before any
  picture exists; the static lens is free, works offline, and is exact for
  the logical shape. EXPLAIN becomes additional lenses later (SD2).
- **Column-level lineage in v1.** The most valuable long-term cut, but the
  derivation (expression → source column attribution through aliases and
  scopes) is a project of its own and the graphs need interaction design
  (folding) to stay readable. Deferred, not killed.

## Consequences

- A fourth graph surface with a clearly distinct job; the tab comment must
  say which surface owns what (Network = result, Graph = reactive surface,
  Flow = inside one statement, Passes = pass catalog).
- The extractor leans on an **undocumented order invariant**: `BuildScopes`
  collects `scope.Tables` in the same DFS left-to-right order the join-tree
  walk visits source leaves. The invariant is pinned by a fixture test, and
  the extractor falls back to local classification if the counter overruns.
- grammar1 quirks are encoded rather than fought: OFFSET exists only inside
  `limitExpr` (rendered as detail on the LIMIT node), `WINDOW` admits a
  single named window (folded into the projection node's detail), and only
  single SELECT-shaped statements parse — which the split guarantees
  upstream, so the statement-kind gate is defensive only.
- Long identifiers widen Graphviz boxes; labels are truncated with the full
  text in the detail line. The caps bound layout cost; a capped graph says so.
- No new dependencies, no IDL/FFI change, nothing for the Rust side; the
  layeredgraph widget is consumed strictly through its existing API.

## Status

Accepted 2026-07-31 (reviewed-by p@stergiotis). Proposed, built and
verified the same day: **M1** IR + extractor + fixture tests → **M2**
driver + tab body (memoised derivation, cached layout, local selection,
width probe) → **M3** registration + registry-test updates + env-vars
regen → **M4** live verification. 25 extractor fixtures (including the
order invariant, the sibling-CTE contract, depth/node caps and id
determinism) plus 6 driver tests, full `apps/play` suite green. M4 ran the
scripted-screenshot path (`BOXER_PLAY_FOCUS_FLOW` on a private headless
weston): the capture shows the left-right chain, the `l`/`r` join fan-in,
the layout toggle, the status line and the result ellipse for a
CTE-self-join query. The click path (local highlight + detail line) is
covered by the driver tests, not yet driven interactively. The code landed
as `50902410`. Post-acceptance changes arrive as dated Updates.

## Updates

### 2026-07-31 — editor-range highlight and EXPLAIN lenses

Both deferrals landed together on their recorded triggers (a direct user
request; the lenses are the IR's second producer).

**Editor-range highlight (§SD4 realized).** Clicking a clause node on the
statement lens tints that clause's bytes in the SQL editor, through the
ADR-0130 `sectionStyled` channel (a `StyleBackground` section appended by
`editorStyledSections`, so it inherits the quiescence gate). Three coordinate
systems meet and every hop re-verifies: `flowNode` gained `Start`/`End`
(clause range within the derived SQL, captured from the CST); `splitNode`
gained `SrcOff` (the fragment's offset within its parsed statement, anchored
by an actual substring match against the CST source range and `-1` when the
token-stream text and the source disagree); the statement is located in the
*current* buffer by trimmed-text equality against the editor's own statement
split (`locateStatementStart`) — equality, not containment, is the staleness
gate, since the split describes the last Run and the buffer may have moved
on. A final slice-equality guard compares the actual bytes; any mismatch
anywhere declines silently rather than tinting plausible-but-wrong text. The
tint is info-toned alpha (the palette has no mid-tone background token, and
the accent alpha is already the subquery region's voice); its value is not
one the gutter recognises, so it earns no spurious gutter mark. Remote-lens
nodes carry no ranges — the highlight is statement-lens only.

**EXPLAIN lenses (§SD2 realized).** The controls row gained a lens selector —
statement (default) · ast · plan · pipeline. Remote lenses ship
`SELECT * FROM (EXPLAIN … <residual>)`: the subquery form keeps the outer
statement an ordinary SELECT, so FORMAT and URL params behave exactly as for
any node — verified against ClickHouse 26.7, including `{p:Type}`
substitution inside the explained statement. It does require a server recent
enough to accept EXPLAIN in a table subquery.

The lens lane compiles and routes the **plain fused statement**; the wrap is
wire-body-only, applied by the transport to the residual
(`ExecOptions.WrapStatement` — the ADR-0141 probe precedent, "resolve from
the statement it wraps", generalized to lanes). This is load-bearing, not
cosmetic: index structure and schema are endpoint-local, so the EXPLAIN must
interrogate the endpoint the statement itself routes to — a first cut that
wrapped before dispatch sent every lens query to the configured endpoint,
because the security classifier cannot parse the wrapper (grammar1 has no
EXPLAIN-in-subquery) and auto-routing declined. Wrapping late also lets the
pre-execute rewrites and the SET-prelude harvest see the statement (a pass
skips what it cannot parse, so an early wrap shipped the inner statement
unrewritten), and the demand memo keys on the statement itself. The wrapper
is applied between the rewrites and the FORMAT step — a FORMAT inside the
parens would bind to the inner statement — and the row cap is read from the
wire form, since an inner LIMIT bounds the explained query, not the
wrapper's result.

Output dialects: PLAN uses `json = 1` (a recursive document; the server's
`Node Id` becomes the node id), AST parses one-space-per-level indentation,
PIPELINE two-space indentation with `(PlanStep)` group markers folded into
the processors' detail. Edges point child→parent throughout, so storage sits
left and the output right, as on the statement lens; `ReadFrom*` steps
render as sources, everything else as a new generic `flowOp` kind. Each lens
runs on its own lane — forgotten on Run (the ADR-0129 memo-on-error lesson)
and closed in Close — and the parse is memoised on the lane's served key,
scoped by lens (all three lanes compile the same plain statement, so served
keys collide across lenses by construction); switching lenses clears the
shown graph and the selection (node ids do not carry across lenses) rather
than displaying the previous lens's while the new one loads.

Even at two producers no lens *interface* materialized: the seam is the
`flowGraph` IR itself plus one dispatch switch (`parseLensRecord`), and a Go
interface would wrap a single call site. The parsers are pinned by verbatim
fixtures captured from the live server, so a future output-dialect change
fails a test rather than a render.

Two further lenses landed the same day, closing the deferral above:
**estimate** (`EXPLAIN ESTIMATE`, the one tabular dialect — rows arrive
tab-joined from the multi-column result and render as one source node per
MergeTree table, carrying parts/rows/marks, draining into a read-estimate
terminal; a statement reading no MergeTree tables estimates empty and the
pane says so) and **indexes** (`EXPLAIN PLAN indexes = 1, json = 1`, riding
the existing PLAN parser — the `Indexes` entries fold into the ReadFrom
node's detail as selected-vs-initial parts and granules per index).

The column-lineage deferral closed the same day as a seventh lens —
**lineage**, local like the statement lens: output-column provenance of the
active node's SELECT list. Each item is a node fed by the source columns its
expression references, resolved with ClickHouse's own precedence — a
select-list alias shadows a column (the alias-in-WHERE behaviour), then the
FROM sources by qualifier or as the single source, and a bare name over
several sources is drawn flagged ambiguous rather than guessed. `*` renders
as one star per matching source (the column set is unknowable offline);
scalar subqueries are marked, not traced (nested SELECTs are pruned from the
identifier walk so inner scopes never read as outer lineage); a union
statement traces its first member and says so. Lineage nodes carry ranges —
an item's expression, an identifier's first occurrence — so the editor
highlight works on this lens exactly as on the statement lens. Deliberate
non-goals, each with its trigger recorded here: columns consumed by
WHERE/GROUP BY without being projected (filter provenance, not output
lineage), cross-node lineage through sibling CTEs (needs a whole-split
view), and schema-informed star expansion (needs the server).

Two affordances added on use: a remote lens gained a **view** toggle between
the parsed graph and the raw EXPLAIN text as the server returned it
(monospace, indentation intact — the graph is a reading of that text, the
text is the full detail), and an endpoint whose SQL surface has no EXPLAIN
(recognised by the introspection plane's `keelsonsql:` error namespace) gets
a plain-language notice instead of a relayed parser error — routing is
working as designed when that fires; only the lens has nothing to ask. The
panel is documented in the app's help book (`help/features.md`, "Flow").

## References

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the layeredgraph widget stack this renders through.
- [ADR-0097](./0097-play-reactive-query-graph.md) — split contract, active node, System graph.
- [ADR-0119](./0119-imzero2-pipelineview-widget.md) — pipelineview; kept for a future EXPLAIN PIPELINE lens.
- [ADR-0129](./0129-play-layered-graph-panel.md) — Network tab; the local-selection lesson and the layout-cache idiom.
- [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) — the deferred editor-highlight affordance's seam.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the existing EXPLAIN AST verdict probe.
- [ADR-0117](./0117-passthrough-table-classifier.md) — why the passthrough classifier is not reused.
- ClickHouse `EXPLAIN` statement: https://clickhouse.com/docs/en/sql-reference/statements/explain
