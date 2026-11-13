---
type: explanation
audience: platform designer taking the kernel decision
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Provenance.** Compiled 2026-11-07. Nothing here is a decision. The
> in-tree rows were placed by reading the ADR each row names and the code it
> points at, on that date. The outside rows come from public product
> documentation, unchecked against source, and are marked **[2nd]**; they
> are in the box to stress the dimensions, not to describe those products.

# Units of work: a morphological box over what boxer runs

Boxer runs several kinds of unit — windows, services, jobs, daemons, SQL
functions, passes, libraries — and a kernel for composite data products
would be one more. Before an ADR names its options, this page asks which
dimensions distinguish the units at all. The method is a morphological box:
dimensions as rows, every known unit placed in a cell, and three tests that
say when the dimension list is done. The tests are the point; the list is
their output.

## 1 Method and stopping rule

- **Separation.** Two units known to differ must land in different cells.
  If they do not, a dimension is missing. This is how unlisted dimensions
  are found.
- **Independence.** Two dimensions perfectly correlated over every placed
  unit are one dimension. One is dropped or marked derived.
- **Occupancy.** An empty cell is either impossible, with a one-line reason,
  or a gap. The kernel should be one gap; several gaps mean the ADR's scope
  is wrong.

The dimensions reuse vocabularies the tree already has rather than new
ones: the effect ladder of ADR-0269, the stateful-tool contract of
ADR-0315, the entry frames of ADR-0344, the component mark of ADR-0126, the
boundary tiers of the [sandbox survey §7](./agent-sandbox-compute-design-space.md),
and the product roles of [data-product-composition §2.1](../explanation/data-product-composition.md).
Concerns every unit must answer but that do not distinguish units — labels,
cost, cancellation, suspension — are not dimensions; they are the
checklist of §5.

## 2 The dimensions

| # | Dimension | Values | Vocabulary from |
| --- | --- | --- | --- |
| D1 | Origin — who defines the unit, and when | `compiled` · `document` (a row or a markdown definition, minted at load) · `runtime` (written in a session, by a person or a model) | ADR-0132 §SD1, ADR-0240 |
| D2 | Parametricity — what varies per call | `fixed` · `arguments` (typed, declared) · `generated` (emitted from a description) · `text` (the code is the argument) | ADR-0290 §SD1, ADR-0270 |
| D3 | Governor — who starts, bounds and stops it | `caller` (no supervision; the caller's goroutine) · `host-process` (hostboot, the in-host worker) · `app-model` (manifest, instance key, lifecycle rows) · `host-pool` (a procpool child) · `engine` (a SQL engine spawns it) · `systemd` · `framework` (an external runner) | ADR-0352, ADR-0285, ADR-0344 §SD4, ADR-0126 §SD2 |
| D4 | Lifetime | `call` · `pooled` (warm, one call each) · `session` (a window or a run) · `process` (boot to shutdown) | ADR-0285, ADR-0322 |
| D5 | Launch | `on-demand` (a call or a gesture) · `pre-spawned` · `boot` (all, or by predicate) · `cadence` (a schedule or a server pull) · `enqueue` (a durable row drained later) · `external` | ADR-0272, ADR-0352 §SD3, ADR-0115, ADR-0223 |
| D6 | State across calls | `stateless` · `declared` (footprint, epochs, rebuild and fork class) · `undeclared` | ADR-0315 §SD2 |
| D7 | Authority — whose rights it runs with | `person` (a gesture at the window) · `grant` (a task grant) · `declared` (manifest caps or typed bindings, cross-checked) · `ambient` (whatever the process holds; credentials from the environment) · `server` (the SQL engine's user) | ADR-0269 §SD6, ADR-0026 §SD10 |
| D8 | Boundary — what separates it from its caller | `none` · `frame` (an entry frame marks the stack) · `process` (own process, host environment inherited) · `unit` (systemd hardening) · `engine` (a read-only session with quotas) · `runtime` (wasm, an isolate) · `vm` (a VM or a container) | ADR-0344 §SD1, ADR-0333, sandbox survey §7 axis 1 |
| D9 | Surface — how it is invoked | `go` · `keelson()` · `sqlfn` (a server-side SQL function) · `op` (a bus operation; the person's gesture goes through the same handler) · `tool` (a coordinator tool) · `bus` (it subscribes) · `row` (a durable row) · `cli` · `http` · `pipe` (stdin and stdout) | ADR-0269, ADR-0290, ADR-0315, ADR-0252 |
| D10 | Input shape | `none` · `scalars` · `relation` · `bytes` · `code` · `events` | — |
| D11 | Output shape | `values` · `table` (an Arrow record) · `facts` (rows written) · `code` · `effects` · `pixels` | — |
| D12 | Effect | `none` · `view` · `document` · `run` · `consequential` · `mixed` | ADR-0269 §SD2 |
| D13 | Attribution — the key its work is recorded under | `none` · `instance` · `call` · `job` · `run` · `mark` | ADR-0188, ADR-0343, ADR-0126 §SD1 |
| D14 | Failure | `caller` (an error returned, nothing retried) · `left-out` (boot goes on without it) · `at-least-once` · `dead-letter` · `restart` | ADR-0352 §SD2, ADR-0223 §SD4, ADR-0252 |
| D15 | Mesh role | `source` · `composite` · `consumer` · `platform` (not a product) | data-product-composition §2.1 |
| D16 | Sharing — whether code moves or data moves | `code` (the unit reaches the consumer as code and runs on the consumer's data) · `serving` (the consumer's data goes to a running instance) · `both` (the consumer links the code and hands it data) | — |

## 3 The box

In-tree units, placed on the compile date. A cell with two values means the
unit takes both, by caller.

| Unit | D1 | D2 | D3 | D4 | D5 | D6 | D7 | D8 | D9 | D10 | D11 | D12 | D13 | D14 | D15 | D16 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| windowed app (ADR-0269) | compiled | arguments | app-model | session | on-demand | declared | person, grant | frame | op | events, scalars | pixels, table | mixed | instance, call | caller | consumer | serving |
| SQL applet (ADR-0132, ADR-0274) | document | arguments | app-model | session | on-demand | declared | person, grant | frame | op | scalars, code | pixels, table | mixed | instance, call | caller | consumer | both |
| headless service (ADR-0352) | compiled | fixed | app-model | process | boot | undeclared | declared | none | bus | events | effects, facts | run | instance | left-out | platform | serving |
| host service (`hostboot.RegisterHostService`) | compiled | fixed | host-process | process | boot | undeclared | ambient | none | bus | events | effects | run | none | left-out | platform | serving |
| watchbill handler, in-host (ADR-0223, ADR-0344) | compiled | arguments | host-process | call | enqueue | stateless | ambient, grant | frame | row | scalars | facts, effects | run | job | at-least-once | platform | both |
| lane child (ADR-0344 §SD4) | compiled | arguments | host-pool | call | enqueue | stateless | grant | process | row | scalars | facts, effects | run | job, run | at-least-once | platform | serving |
| stevedore handler (ADR-0252) | compiled | fixed | framework | process | external | stateless | ambient | process | pipe | bytes | facts | run | none | dead-letter | source | code |
| daemon, e.g. queryrunsd (ADR-0115) | compiled | fixed | systemd | process | boot | stateless | ambient | unit | http | none | table | run | mark | restart | source | serving |
| keelson table with arguments (ADR-0290) | compiled | arguments | caller | call | on-demand | stateless | declared | none | keelson() | scalars | table | none | none | caller | source | serving |
| SQL UDF pack (ADR-0162, ADR-0244) | compiled | arguments | engine | process | boot | stateless | server | none | sqlfn | scalars | values | none | none | caller | platform | code |
| nanopass pass (ADR-0108) | compiled | fixed | caller | call | on-demand | stateless | ambient | none | go | code | code | none | none | caller | platform | code |
| science package (ADR-0356) | compiled | fixed | caller | call | on-demand | stateless | ambient | none | go | values | values | none | none | caller | platform | code |
| ad-hoc dataset (ADR-0240) | runtime | fixed | app-model | session | on-demand | stateless | declared | none | keelson() | none | table | none | instance | caller | composite | serving |
| coordinator tool (ADR-0315) | compiled | arguments | app-model | call | on-demand | declared | grant | none | tool | scalars | values | mixed | call | caller | platform | serving |
| play SQL buffer (ADR-0270 `set_sql`, `run`) | runtime | text | engine | call | on-demand | stateless | person, grant | engine | op | code | table | run | instance, call | caller | composite | code |

Outside units **[2nd]**, placed to stress the dimensions.

| Unit | D1 | D2 | D3 | D4 | D5 | D6 | D7 | D8 | D9 | D10 | D11 | D12 | D13 | D14 | D15 | D16 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ClickHouse `executable_pool` table function | document | arguments | engine | pooled | pre-spawned | stateless | server | process | sqlfn | relation | table | none | none | restart | composite | code |
| Snowflake table function | document | arguments | engine | pooled | on-demand | stateless | server | vm | sqlfn | relation | table | none | none | caller | composite | code |
| dbt model | document | text | framework | call | cadence | stateless | ambient | engine | cli | relation | facts | run | run | caller | composite | code |
| Temporal activity | compiled | arguments | framework | call | enqueue | stateless | ambient | process | go | scalars | values | run | run | at-least-once | platform | both |
| systemd unit | document | fixed | systemd | process | boot | undeclared | ambient | unit | cli | none | effects | run | none | restart | platform | serving |
| Kubernetes Job | document | arguments | framework | call | on-demand | stateless | ambient | vm | http | none | effects | run | none | at-least-once | platform | code |
| Cloudflare Worker | document | text | framework | pooled | on-demand | stateless | declared | runtime | http | bytes | values | run | none | caller | consumer | serving |
| Jupyter kernel | compiled | text | framework | session | on-demand | undeclared | ambient | process | http | code | values, pixels | mixed | none | caller | consumer | serving |

## 4 What the tests did

**Independence — dropped or merged.**

- *Placement* (in-process, child, separate process, remote) was a function
  of D3 over every row. Dropped; D3 carries it.
- *GUI versus headless* was `pixels` in D11 over every row. Dropped.
- *Short-lived versus long-lived* is D4 under a better name.
- *Principal* and *capability model* never separated: declared caps came
  with a manifest or bindings, ambient rights with environment credentials,
  in every row. Merged into D7.
- *Labels* (ADR-0311) take one value for every unit but the coordinator's
  tool calls. Moved to the checklist. *Synchronous versus asynchronous* is
  an attribute of an operation (ADR-0358), not of a unit; moved likewise.
- *Sharing* (D16) follows D9 over most rows: `go`, `sqlfn`, `cli` and `pipe`
  move code, `op`, `bus`, `http`, `tool` and `keelson()` move data. Surface
  `row` breaks it: a watchbill handler is linked by the party that enqueues
  to it, which no surface value can say. Kept, with `both` as the value D9
  cannot carry. D16 is also distinct from D1, which says when the code was
  written, not where it runs relative to the data. A `platform` unit
  shared by `serving` is a substrate products run on; one shared by `code`
  is a vocabulary products are written in.
- *Boundary* correlates with *governor* over the in-tree rows alone
  (app-model gives `frame`, systemd gives `unit`). The outside rows break
  the correlation three ways, so D8 stays. That the in-tree rows cannot
  separate them is itself a finding: no boxer governor offers more than
  one boundary.

**Separation — what forced a dimension.** D1 by the SQL applet against the
windowed app, identical elsewhere. D2 by the play buffer against the keelson
table. D4 by `executable_pool` against the SQL UDF pack, same governor and
surface. D5 by the headless service, the lane child and a server pull,
which differ in little else. D6 by the Jupyter kernel against the Cloudflare
Worker. D14 and D15 are not derivable from D3: `host-process` carries both
`left-out` and `at-least-once`, and `framework` carries all of `source`,
`composite` and `platform`. D13 is nearly a function of D3 and D9 but not
quite, and a kernel must choose it, so it stays.

**Occupancy — cells that matter.**

Impossible, with the reason:

- D8 `none` × D3 `systemd`, `host-pool`, `framework`: a separate process is
  at least a `process` boundary.
- D4 `call` × D5 `boot`: a unit started at boot that ends after one call is
  a `cadence` or an `enqueue`.
- D11 `pixels` × D3 `engine`, `systemd`: `Frame` is an opcode stream the
  Rust client interprets; without a carrier it cannot run (ADR-0352 §SD6).
- D7 `person` × D4 `process`: a gesture cannot hold process-long authority;
  ADR-0352 §SD1 refuses a workingset to a service for the same reason.

Open, each with the record that already defers it:

| Gap | Cell | Nearest occupied | Deferred by |
| --- | --- | --- | --- |
| G1 the kernel | D2 `arguments` × D10 `relation` × D11 `table` × D3 `app-model` or `host-pool` | keelson table (no relation); `executable_pool` (engine governor); play buffer (`text`, `runtime`) | taken up by [ADR-0360](../adr/0360-kernels-composite-data-products-as-keelson-calls-with-relation-arguments-under-a-governor-the-deployment-picks.md), proposed |
| G2 a wasm boundary | D8 `runtime` × D3 any boxer governor | — | ADR-0162 option (c); ADR-0207 O4; [agent-harness §6 #13](./agent-harness.md) |
| G3 a scheduled unit | D5 `cadence` × D3 `app-model` | ClickHouse refreshable view (`engine`); systemd timer | agent-harness §2.7 S17; ADR-0308 §SD11 |
| G4 a session-long, stateful, text-parametric unit | D1 `runtime` × D4 `session` × D6 `undeclared` × D3 any boxer governor | Jupyter kernel | ADR-0269 O4 (rejected); agent-harness §6 #13 |
| G5 a pooled unit that is not clickhouse-local | D4 `pooled` × D3 `host-pool` | chlocalpool (ADR-0285) | ADR-0333 (withdrawn) |
| G6 a daemon declared by data | D1 `document` × D3 `systemd` | systemd unit | ADR-0126 §SD6, the R1 desired-state store |

G1 is the gap this page was built to show. G4 is what "the agent lives in
the kernel" would be, and it is the one cell a boxer governor has refused
rather than deferred. G2, G3 and G5 are each one ADR away and independent
of G1.

## 5 The kernel's row, and the §2.8 checklist

A kernel as the earlier dialogue shaped it occupies one row with a choice
left open in some columns. A column with one value is fixed by the gap; a
column with several is either a deployment choice that follows D3, or a
decision the ADR must take.

| Unit | D1 | D2 | D3 | D4 | D5 | D6 | D7 | D8 | D9 | D10 | D11 | D12 | D13 | D14 | D15 | D16 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| kernel (proposed) | compiled, document | arguments | app-model, host-pool, engine, systemd | call, pooled | on-demand, pre-spawned, enqueue | stateless | follows D3 | follows D3; `runtime` deferred (G2) | keelson(), op, row, http | scalars, relation | table, facts | none, run | follows D3 | caller, at-least-once | composite | both |

Follows D3: `app-model` gives `grant`/`frame`/`call`; `host-pool` gives
`grant`/`process`/`job`; `engine` gives `server`/`process`/`none`; `systemd`
gives `ambient`/`unit`/`mark`. The ADR decides D4, D5 and the surface set;
D6 `declared` is the deferral that would admit a reused worker. D16 reads
the governor choice: under `engine` the code moves to the data, under
`host-pool` the data moves to a worker on its stdin, and under `caller` a Go
kernel is a library and nothing moves.

The cross-cutting concerns of [agent-harness §2.8](./agent-harness.md),
each with the kernel's answer or the record that owes one:

| Concern | Kernel's answer |
| --- | --- |
| C1 identity | one run id per invocation; the attribution key of D13 |
| C2 order and time | none across invocations; a kernel does not order |
| C3 durability | only under `enqueue`: the job row is the intent (ADR-0223) |
| C4 authority | D7, by governor; never wider than the caller's |
| C5 confidentiality | the output carries the join of its inputs' labels (ADR-0311); the engine and daemon governors carry none, which the ADR must say |
| C6 cost | bytes in and out, wall time, one slot; folded like ADR-0355's usage vector |
| C7 context capacity | not applicable; the model sees a result reference, not the table |
| C8 prefix stability | not applicable |
| C9 provenance | the lineage fact: kernel, version, input digests, output alias, governor |
| C10 failure | D14; `caller` by default, `at-least-once` under `enqueue` |
| C11 concurrency | stateless, so commuting; cancellation by context or SIGTERM to the group (ADR-0344 §SD4) |
| C12 versioning | a version in the declaration; the catalog keeps retired names as ADR-0162 does |
| C13 variability | which kernels a process offers is a predicate, as ADR-0352 §SD3 |
| C14 determinism | declared per kernel; a nondeterministic kernel cannot be a `DryRun` twin (ADR-0315) |
| C15 suspension | an `enqueue` kernel is pending in the sense of ADR-0358 |
| C16 measurement | the pool table and the lineage fact |
| C17 placement | D3, inside the declared cell (ADR-0307); an `engine` kernel runs where the server runs |
| C18 tool state | `stateless`; the `declared` value is the deferral |

What the page does not do: it does not pick a value in any open column, and
it does not place units the tree gains after the compile date. A new unit
goes in the box before it goes in an ADR, and a row that needs a sixteenth
column is the signal this list was not complete.
