---
type: adr
status: proposed
date: 2026-11-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0360: kernels — composite data products as keelson calls with relation arguments, under a governor the deployment picks

## Context

In data-mesh terms boxer has two of the three product kinds. A windowed app
is a consumer-facing product: it declares operations and an agent drives
them under a task grant ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).
A headless service, a watchbill handler, a daemon such as queryrunsd are
infrastructure: long-running, governed by the host or by systemd
([ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md),
[ADR-0223](./0223-watchbill-durable-work-on-facts.md),
[ADR-0115](./0115-query-observability-data-plane-strategy.md)). There is no
unit for the third kind, the composite product: a declared function from
relations and scalars to a relation, registered so a consumer, a window, an
agent or a schedule can call it, with a record of what it read and wrote.
[units-of-work-design-space](../adr-background-work/units-of-work-design-space.md)
places every unit the tree runs in a sixteen-dimension box and finds this
cell empty (its gap G1), with the pieces around it already built:

- A keelson table takes typed named arguments, resolved where the query's
  parameters are, and answers Arrow in every lane: natively, where the
  engine snapshots each call into a temporary table before the statement
  runs; in a browser tab, through the trivial evaluator
  ([ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md)).
  Its arguments are scalars only, so it can be a source and never an
  interior product.
- An ad-hoc dataset is the output port a window already has: an Arrow
  record under an alias, sealed to one process, readable as `keelson(alias)`
  under a type gate and a per-dataset quota
  ([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md)).
- System apps join the operations contract with no mechanism of their own
  ([ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md));
  an operation declares an effect, and a command's read-only twin is its
  `DryRun` ([ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md)).
- Durable work is a row; an agent-caused job runs in a child of its own,
  one per job, from the running executable, bounded by a procpool slot
  ([ADR-0344](./0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md),
  [ADR-0285](./0285-procpool-pre-spawned-workers-behind-a-spawner.md)).
- A long-lived listener worker was withdrawn before acceptance: a second
  worker shape with a security boundary of its own, for a gain no consumer
  needed ([ADR-0333](./0333-listener-pools-a-long-lived-gated-clickhouse-local-behind-ch-local-exec.md)).

The tree also has more than one process governor, and no record says so.
Systemd governs processes: a daemon is a subcommand of the one binary with
no manifest, no row in `keelson.apps`, no grant, its authority the unit's.
Keelson governs apps: a service is a headless app with a manifest, an
instance key and lifecycle rows, and nothing restarts it. The host governs
the lane's children through a pool. A SQL engine governs the scripts it
spawns. A kernel must be declarable once and runnable under any of these,
or it becomes four units.

The box also names the cells this record does not take: a wasm boundary
(G2), a scheduled unit under keelson (G3), and a session-long stateful unit
written at run time (G4), which is what "the agent lives in the kernel"
would be and which [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
O4 refused.

## Design space (QOC)

**Question.** How is a composite data product declared, invoked and run, so
that one declaration serves SQL, agents, durable work and every governor?

**Options.**

- **O1** — A keelson table with relation arguments: ADR-0290's provider
  extended with inputs that name other keelson tables, a declaration that
  carries schema, effect and version, and a worker contract the governors
  share.
- **O2** — Engine-side only: ClickHouse executable table functions and
  UDFs, declared in server configuration and run by the server.
- **O3** — A sandboxed executor: code the caller supplies, run per call in
  an interpreter or a wasm guest that calls back into the host.
- **O4** — SQL applets as the product unit: a document-declared window
  whose result is published as a dataset.
- **O5** — Watchbill kinds as the product unit: every composite product is
  a job whose output lands in facts.

**Criteria.**

- **C1** — Reach: callable from SQL natively, in a tab without ClickHouse,
  and from an external reader.
- **C2** — Agent-drivable under a grant, with an effect declared before the
  call, not inferred after it.
- **C3** — Governor-neutral: one declaration under keelson, the host pool,
  the engine and systemd.
- **C4** — Self-serve: declarable without a build, by a document.
- **C5** — Lineage as a query: what a run read and wrote is a fact.
- **C6** — New mechanism: what has to exist that does not.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | ++ | −  | −  | −  | −− |
| C2 | ++ | −− | −  | +  | +  |
| C3 | ++ | −− | −  | −− | −  |
| C4 | +  | +  | ++ | ++ | −− |
| C5 | ++ | −  | −  | −  | +  |
| C6 | +  | ++ | −− | +  | +  |

O2 is kept as one of O1's governors, not rejected. O3 is the refused cell
G4 under another name: no reviewer can see a program's effect before it
runs. O4 is a consumer-facing product and stays one. O5 makes every call
durable, which most calls are not.

## Decision

We will add **kernels**: a kernel is a declared, stateless table function
with scalar and relation arguments and an Arrow output schema, registered
as a keelson table and listed in `keelson('kernels')`. It is called from
SQL as `keelson('<name>', <scalar> = …, <relation> = '<table>')` in every
lane ADR-0290 serves, driven by agents as operations on one `kernels`
system app, run durably as a watchbill kind, and executed under whichever
governor the deployment picks through one worker contract. Every run
writes one lineage fact.

### Subsidiary design decisions

- **SD1 — The declaration.** A kernel declares a name, a version, scalar
  arguments as ADR-0290 §SD1 declares them, relation arguments each with an
  Arrow schema the input must satisfy by name and type, an output schema, an
  effect class from ADR-0269 §SD2 (`none` for a pure kernel, `run` for one
  that writes facts), and an implementation kind: a Go function, or a SQL
  statement with `{slot:Type}` parameters that reads its relation arguments
  as keelson tables. A wasm kind is deferred (Q1). Registration goes
  through the introspection registry, so `keelson('kernels')` is a provider
  like `keelson('services')`, with `origin = declared`; which kernels a
  process offers is a predicate over it, composed with the launch limit as
  ADR-0352 §SD3 composes services.

- **SD2 — A relation argument names a keelson table.** The value is the
  name of a provider, an ad-hoc alias, or another kernel call. Natively the
  engine snapshots the named tables first and the kernel after them, in
  dependency order, and refuses a cycle; a subquery is not an argument —
  the caller publishes it as an ad-hoc dataset and names the alias. ADR-0240's
  type gate and per-dataset quota bound every relation input, so a kernel
  cannot receive what a dataset cannot hold. In a tab, the trivial
  evaluator of ADR-0290 §SD3 admits as a relation argument any source it
  can evaluate, a `WITH` query over keelson calls included. *By leaning:*
  a relation argument that is a native `WITH` query is deferred (Q2), since
  the engine cannot read a query's rows before the statement runs.

- **SD3 — The governor is a deployment property.** A kernel's declaration
  says nothing about where it runs. Four governors are recorded, and
  authority, boundary, attribution and failure follow the governor:

  | Governor | Runs as | Authority | Boundary | Attribution | Failure |
  | --- | --- | --- | --- | --- | --- |
  | caller | the Go kind, on the caller's goroutine | the caller's | entry frame | the call | returned to the caller |
  | host pool | a child from the running executable, one call each, a procpool slot | the task grant, attested as ADR-0344 §SD2 | own process | job id and run id | at-least-once under a row; returned otherwise |
  | engine | a script the SQL server spawns, in a pool it sizes | the server's user | own process | none | the server's |
  | systemd | a daemon answering a `url()` pull on loopback, as queryrunsd does | the unit's | unit hardening | component mark | `Restart=` |

  On demand is the default: a call takes a slot and runs. Pre-spawning is
  the predicate of SD1 with a pool size per kernel. A kernel that must be
  always on belongs behind a unit, because only systemd restarts. The
  engine governor is recorded, not built, until a measurement says what a
  script costs against the host pool (M7).

- **SD4 — One worker contract, no listener.** A worker is the running
  executable started as `kernel run --name <name>`, one call per process,
  as the lane's children are. The scalar arguments, the output schema and
  the lineage header travel as one JSON line; each relation argument
  arrives as an Arrow IPC stream on stdin, in declared order; the result
  leaves on stdout as one Arrow IPC stream. ADR-0333's reasons stand: a
  long-lived worker is a second shape with a boundary to maintain, and the
  warm one-shot child is within the latency target. Because the contract is
  a pipe, the same binary serves the host pool, an engine's executable pool
  and a daemon behind a pull.

- **SD5 — Surfaces, by caller.** Four, none new in kind:

  | Caller | Surface | Record |
  | --- | --- | --- |
  | SQL, any lane | `keelson('<name>', …)` | ADR-0290; the gate of ADR-0253 keeps refusing calls with arguments, as ADR-0290 §SD2 decided |
  | an agent | the `kernels` system app: a query per pure kernel, class `query`, effect `none`; a `run` command for kernels with effect `run`, declared `Async` under ADR-0358; `drain`, `resize`, `stop` as commands with effect `run` | ADR-0345's three rules; ADR-0315 `DryRun` is the pure twin of `run` |
  | durable work | the watchbill kind `kernel`, args naming the kernel, its arguments and the output: an ad-hoc alias or a facts table | ADR-0223; agent-caused rows take the lane of ADR-0344 |
  | an external reader | `url()` against a daemon's `/pull`, the refreshable-view shape | ADR-0115 §S1 |

  No command is an `INSERT`: a row that bypassed the enqueue service would
  bypass the attestation ADR-0344 §SD2 put at the door.

- **SD6 — One lineage fact per run.** A facts-bound record store
  ([facts-bound-record-stores](../explanation/facts-bound-record-stores.md))
  with one kind, `kernelRun`: kernel name and version, governor, the
  attribution key of SD3, a digest per relation input and the scalar
  arguments, the output's alias or table, start, end and outcome. With it
  the structural and economic sealing tests of
  [data-product-composition §2.1](../explanation/data-product-composition.md)
  are queries over `boxer.facts`.

- **SD7 — Labels and rights.** A kernel never runs with rights wider than
  its caller's. Under the caller and host-pool governors its output carries
  the join of its inputs' labels (ADR-0311), computed by the dispatcher
  that recorded the call. The engine and systemd governors carry no label,
  so a kernel placed there declares the one label its outputs get, and a
  deployment does not place a kernel there whose inputs can carry more.

- **SD8 — A document declares a SQL kernel.** The SQL kind is declared by
  one markdown document in a book, on ADR-0132 §SD1's convention:
  frontmatter carries the name, version, arguments and output schema, the
  first `sql` fence is the statement. Books are committed; a kernel
  authored at run time follows ADR-0132 O4's deferral and its trigger. *By
  leaning:* this is in the first cut (M5) because it is the cheapest kind
  and the one a person can write without a build.

### Deferred and open

- **Q1 — A wasm kind.** The box's G2. Its triggers are ADR-0162 option
  (c)'s and the harness study's §6 #13: a small shared package, instruction
  bounded, never load-bearing in a prunable predicate. The worker contract
  of SD4 is what it would plug into.
- **Q2 — A native `WITH` query as a relation argument.** Needs the engine to
  run the query as a statement of its own before the call; ADR-0291's
  statement sets are the precedent. Left until a caller cannot publish an
  alias instead.
- **Q3 — A reused worker.** A kernel with state across calls is D6
  `declared` in the box and needs the five-part contract of ADR-0315 §SD2.
  Not in this record.
- **Q4 — Scheduled kernels under keelson.** The box's G3; ADR-0308 §SD11
  keeps window-less runs out. A cadence is the engine's refreshable view or
  a systemd timer until an initiation record exists.
- **Q5 — Relation arguments over `url()`** for an external ClickHouse:
  ADR-0290's Q4, unchanged.
- **Q6 — The engine governor's shape.** Which script location, format and
  pool settings; decided with M7's measurement.

### Milestones

- **M1 — Declaration and the SQL surface.** The registry, `keelson('kernels')`,
  the Go kind in-process, relation arguments natively and in the tab.
- **M2 — The `kernels` system app** and its catalog.
- **M3 — The lineage fact** and the sealing queries as a sqlapplet book.
- **M4 — The host-pool worker** and the watchbill kind.
- **M5 — Document-declared SQL kernels.**
- **M6 — The daemon surface:** a `kernel serve` subcommand behind a pull.
- **M7 — The engine governor,** after a trial comparing it with M4.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `introspect.ArgSpec` (ADR-0290) | reshaped: a relation argument type beside the scalar ones | `keelsonsql.ExpandWithArgs` snapshots in dependency order; the trivial evaluator's source rule |
| the introspection registry | added: `keelson('kernels')`, `keelson('kernel_workers')` | the launch-limit predicate, the app center's tables |
| the facts schema and keelson vocabulary | added: the `kernelRun` kind and its ids | the generated record store, `boxer.facts` DDL |
| the operations contract (ADR-0269) | added: the `kernels` system app's catalog | ADR-0345's table gains a row |
| watchbill kinds | added: `kernel` | the worker's linked handlers; the lane |
| the main binary's commands | added: `kernel run`, `kernel serve` | the lane coordinator's child program |
| environment variables (ADR-0009) | added: `KEELSON_KERNELS_WHERE`, pool sizes under `KEELSON_KERNELS_*` | env-vars documentation |

## Alternatives

The QOC carries the assessment; the kill reasons:

- **Engine-side only (O2).** Reaches no tab, no grant and no label; kept
  as one governor.
- **A sandboxed executor (O3).** The effect of supplied code is not
  declarable before it runs; ADR-0269 O4's reason, and the box's G4.
- **Applets as the unit (O4).** A window; its grant dies with it, and it
  cannot be an argument to another product.
- **Jobs as the unit (O5).** Every call would be a row; a pure call that
  answers in milliseconds has no reason to be durable.
- **A long-lived kernel worker.** ADR-0333's reasons.
- **A relation argument as a subquery.** The engine has no rows before the
  statement runs; an alias costs the caller one publish and nothing else.
- **A new registry beside introspection.** A kernel is a table; a second
  catalog would be a second predicate, a second app-center page, a second
  gate.

## Consequences

### Positive

- The composite product exists as a unit, and the mesh's three kinds are
  three things the tree can name.
- One declaration runs under four governors; moving a kernel between them
  is a deployment change.
- Lineage, sealing and the dead stratum are queries, which is what
  [data-product-composition](../explanation/data-product-composition.md)
  asked of the platform.
- A person writes a SQL kernel in a document; a consumer writes a Go kernel
  as a provider; neither touches the host.

### Negative

- A relation argument materialises in memory under the dataset quota; a
  kernel over a table larger than that runs as a job or reads the table
  itself, which the declaration cannot express.
- The engine and systemd governors lose labels and grants; SD7 limits what
  may be placed there by hand, and nothing checks it mechanically.
- One more system app, one more watchbill kind, one more facts kind, two
  more subcommands.

### Neutral

- "Kernel" is also the word ADR-0315 uses for an interpreter in a
  downstream app. This record's kernel is a table function; the collision
  is noted, not resolved.
- Whether a kernel's code moves to the data or the data to the code is the
  governor's reading: engine moves code, host pool moves data, caller moves
  nothing. The box's D16 says this for every unit; here it is a deployment
  choice.

## Migration — Tier 1

- **Breaks.** Nothing. Every surface above is additive; a provider with
  scalar arguments is unchanged.
- **Path.** None for existing callers. ADR-0290 gains a dated entry for the
  relation argument type; ADR-0345's table gains the `kernels` row.
- **Regeneration.** The facts record store for `kernelRun`; the keelson
  vocabulary for its ids.
- **Old shape.** Kept.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the declaration, the dependency-ordered
  snapshot and the refused cycle; the integration lane for the native and
  tab paths answering the same rows for one kernel over one alias, as
  ADR-0291 holds its two statement sets; a headless scene for the `kernels`
  app's catalog; the trial under `doc/trials/` for M7.
- **What would fail.** A kernel call whose rows differ between the engine
  and the tab; a run with no `kernelRun` row; a lane job whose child runs
  under the `Human` frame; a call with a relation argument passing the
  `keelson.query` gate.
- **Gap.** SD7's placement rule is a reviewer's check; no test reads a
  deployment's predicate against its labels.

## Status

Proposed — awaiting review by the keelson runtime owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates
Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [units-of-work-design-space](../adr-background-work/units-of-work-design-space.md) — the box this record's row comes from; gaps G1 to G6.
- [data-product-composition](../explanation/data-product-composition.md) — the product kinds and the sealing tests SD6 makes queryable.
- [ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) — named arguments; the provider SD1 extends.
- [ADR-0291](./0291-a-precomputed-vector-field-family-of-keelson-tables.md) — a family of argument tables as a product; the parity test SD4's plan copies.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) — the output port and the input bound.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md), [ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md), [ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md), [ADR-0358](./0358-asynchronous-operations-a-pending-outcome-and-a-turn-a-completion-wakes.md) — the agent surface.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md), [ADR-0344](./0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md), [ADR-0285](./0285-procpool-pre-spawned-workers-behind-a-spawner.md), [ADR-0333](./0333-listener-pools-a-long-lived-gated-clickhouse-local-behind-ch-local-exec.md) — the durable and pooled homes.
- [ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md), [ADR-0115](./0115-query-observability-data-plane-strategy.md), [ADR-0126](./0126-appliance-topology-as-data.md) — the governors.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the document convention SD8 reuses and the run-time authoring it defers.
- [ADR-0162](./0162-leeway-co-ragged-function-pack.md), [agent-harness §6](../adr-background-work/agent-harness.md) — the wasm deferral Q1 inherits.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md), [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md), [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — labels, the gate, and what stays out.
