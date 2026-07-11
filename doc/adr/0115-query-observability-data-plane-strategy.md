---
type: adr
status: proposed
date: 2026-07-11
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0115: Query-observability data plane — ELT/ETL architecture and technology strategy

## Context

Boxer needs ran ClickHouse queries — their executions, performance accounting
and, later, results — to become first-class data. The full goal, entity model
(QueryDef / ParamEnv / QueryRun / RunProfile / ResultSet), planes and slices
live in
[doc/explanation/query-observability.md](../explanation/query-observability.md);
this ADR settles only the **data-plane architecture and technology
strategy**: where transformation runs, what carries the data, and which
technologies are the decided defaults, escalations, and deferrals.

The concrete first instantiation is the capture pipeline for query runs
(`system.query_log` → `runtime.facts`, kind QueryRun), carried in the
implementation outline.

What exists (relevant substrate): a single client chokepoint in play that
already stamps per-lane `query_id`s (ADR-0097 SD5) and applies registered
pre-execute rewrites (ADR-0108); `runtime.facts` with generated leeway DML
builders emitting Arrow IPC (ADR-0026 §SD6, `chstore.InsertArrow`); the
loopback introspection HTTP plane for `url()` consumption (ADR-0094); the
standalone-service anatomy and NATS-core bus (`natsbus`, `inprocbus`,
`Bridge`) of ADR-0090.

Constraints measured on ClickHouse 26.6.1 during this design (single-host
reference deployment):

- `system.query_log` fills **asynchronously** (rows absent immediately after
  completion; seconds-order flush; `SYSTEM FLUSH LOGS query_log` forces in
  ~100 ms), carries a short TTL and a CH-version-owned schema that drifts
  across upgrades, and is **created lazily** on first flush.
- Query **parameters never reach `query_log`** (no column; not in the
  `Settings` map) — environment capture is client-side by necessity.
- A materialized view attached to `system.query_log` fires on the internal
  flush; a broken MV skips its own rows but never poisons the flush; skipped
  windows are recoverable by anti-join while the source TTL holds.
- The `url()` table function exhibits **read amplification** (more than one
  GET per query — a pipeline-construction pass and a data pass, observed) —
  any HTTP source it reads must be stateless and idempotent.
- Refreshable materialized views work (stable feature), including
  destination-derived watermark subqueries in the view body; their refresh
  inserts log to `query_log` (self-noise needs tag-filtering), while internal
  system-log flushes do not self-log.
- WASM UDFs exist (experimental server gate, wasmtime; modules registered by
  `INSERT INTO system.webassembly_modules`; `BUFFERED_V1` block transforms
  over any types) — demonstrated end-to-end with both Rust-SDK and TinyGo
  freestanding kernels, including a MV-on-flush calling the UDF.

Prior art: [ADR-0050](0050-clickhouse-observability-pipeline.md) designed a
push pipeline (MVs → URL-engine sinks → bridge binary → NVMe files + NATS)
for three planes at once; its results and progress planes have since been
answered by shipped reality (inline Arrow responses; in-band progress
headers), and this ADR replaces its `query_log` plane —
ADR-0050 is superseded with its option analysis preserved as the kill-reason
record. [ADR-0051](0051-query-categorization-provenance.md) (result
categorization) is dormant; its requirements are tracked in the explanation
page and a successor design ADR is expected at the weave slice.

## Design space (QOC)

**Question.** Where should the transform of ClickHouse-resident operational
data into leeway-shaped boxer data run, and over which transport — given that
the encoding must be leeway-correct (~180 generated wire columns; hand-written
SQL against them is the drift failure leeway exists to prevent) and the
deployment is single-host?

**Options.**

- **O1 — Client-seam recorder.** In-process ring + introspection provider +
  Go-side enrichment poller; no server-side pipeline.
- **O2 — Generated-SQL transform MV.** Event-MV on the source whose SELECT is
  emitted by a new leeway backend (write-side dual of the ADR-0066 readback
  generator).
- **O3 — WASM UDF kernel.** The event-MV calls a `LANGUAGE WASM` UDF running
  a generated encoding kernel inside the server.
- **O4 — Executable UDF / table function.** The real Go encoder as a
  server-spawned subprocess (`user_scripts` + server config).
- **O5 — Bus ETL.** Extract to NATS, transform in an attached processor
  (bare keelson service or Redpanda-Connect/Bento framework), insert back.
- **O6 — URL-engine transform service** *(chosen)*: a standalone loopback Go
  HTTP service runs the **actual** generated DML builders; a CH-owned
  refreshable materialized view pulls `url('…', ArrowStream, …)` and appends
  to the target table.

**Criteria.**

- **C1 — Encoder single-sourcing.** Does the one existing leeway encoder run,
  or does a second generated implementation need golden-equivalence first?
- **C2 — Pipeline autonomy.** Who must be alive for capture to advance?
- **C3 — Delivery semantics.** Loss, duplicates, replay/backfill under
  component failure.
- **C4 — Operational surface.** Deployables, frameworks, experimental gates,
  server-filesystem state.
- **C5 — Capability posture.** Ambient authority held by the transform
  (ADR-0026 hygiene).
- **C6 — Pipeline self-observability.** Is the pipeline's own state
  inspectable as data?
- **C7 — Single-host efficiency.** Serialization hops and scheduling
  overhead co-located with CH.

**Assessment.** `++` strong positive, `+` positive, `−` negative,
`−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 | O6 |
|----|----|----|----|----|----|----|
| C1 | ++ | −  | −  | ++ | ++ | ++ |
| C2 | −− | ++ | ++ | ++ | −  | +  |
| C3 | −− | +  | +  | +  | −  | +  |
| C4 | ++ | ++ | −  | −  | −− | +  |
| C5 | +  | ++ | ++ | −  | −  | +  |
| C6 | −  | +  | +  | +  | −  | ++ |
| C7 | ++ | ++ | ++ | −  | −− | +  |

O6 is the only option positive on every criterion. It concedes C2's top mark
to the in-database pair — capture pauses while its daemon is down — but
pull-shape semantics make that *deferred, not lost* (bounded by source TTL),
and a standalone unit decouples it from any GUI process. O2/O3 win autonomy
but pay C1 (a second encoding implementation plus its golden program before
anything ships; O3 additionally experimental-gated); they are retained as the
escalation, with O6's output as the golden corpus they would verify against.

## Decision

Adopt an **ELT-first strategy with URL-based transform services**, and fix
the technology set:

- **SD1 — Decision rule: paradigm per stage, by data locus.** Data **born in
  CH and destined for CH** is transformed in ELT successions — CH routes,
  stores and schedules; stage interfaces are tables. Data **born outside CH
  or that must leave it** (external I/O, non-CH sinks, cross-host fan-out)
  goes through a bus ETL stage (the ADR-0090 sysmetrics precedent). Neither
  paradigm is adopted globally; the locus decides.
- **SD2 — Transform tier: standalone loopback URL services, pull-shape.**
  Where an ELT stage needs the real Go encoder, the transform is a
  **stateless, idempotent, loopback-only HTTP service** consumed by a
  CH-owned refreshable materialized view (`REFRESH EVERY … APPEND`).
  Statelessness is mandatory (measured `url()` read amplification);
  watermarks derive from the **destination**; identities are deterministic
  (reserved id band: top bit set over a content hash) and the MV body
  anti-joins a recent destination window, so delivery is at-least-once with
  structural dedup. Push via URL-engine sink tables is the documented
  fallback for fire-and-forget stages (gap-on-down + anti-join backfill).
  The refreshable MV is a deliberate answer to *who drives the load*:
  ClickHouse owns both the schedule and the write, so the pipeline is a
  schema object — declared in DDL, drift-detectable via
  `create_table_query`, observable as data in `system.view_refreshes` — and
  the service holds no write authority: it only SELECTs and serves bytes.
  The daemon-driven-insert shape this rejects is assessed in §Alternatives.
- **SD3 — Wire format: ArrowStream.** Services serve exactly the Arrow IPC
  the generated DML builders emit; `url(…, 'ArrowStream', …)` consumes it.
  No re-encoding tier exists anywhere in the plane.
- **SD4 — Service form: subcommand + hardened unit.** Each service is a
  subcommand of the single Go binary (the `sysmetricsd` precedent) with env
  configuration from the ADR-0009 registry, and runs as a standalone
  systemd unit (canonical copies under `showcase/onbox/`) with a hardening
  profile stricter than the GUI carrier's (no proc access, loopback-only
  networking, no writable state — `DynamicUser` candidates). Each service
  owns its pipeline reconciliation at boot: target DDL, a forced flush to
  materialize lazily-created system sources, and create-or-recreate of its
  MV on definition drift.
- **SD5 — Interactive forwarding leg: NATS core.** When a live plane needs
  pushing to interactive keelson apps, services forward over NATS core via
  the established `natsbus`/`Bridge` pattern (consumers stay on the in-proc
  bus; the topology switch is producer-side). This is the decided
  technology; the leg is **built at consumer trigger**, not speculatively.
  Pull consumers are served without it (facts tables, `url()`
  introspection).
- **SD6 — Escalation: in-database kernels.** If a stage must advance with
  no boxer daemon installed, the transform moves into CH as a generated
  kernel — SQL backend or WASM `BUFFERED_V1` (demonstrated; experimental
  gate acknowledged) — verified against the URL service's output as golden
  corpus. Trigger-gated; not v1.
- **SD7 — Identity stamping at the client chokepoints.** Clients stamp
  `query_id` plus a compact JSON `log_comment`
  (`run_id`, `app`, `lane`, authored/sent/chain/env fingerprints per the
  explanation's entity model) so the server's own log is independently
  attributable and the capture pipeline can lift identity into memberships.
  Parameters never reach `query_log` (measured), so environment capture is
  client-side by design.

First instantiation: **`queryrunsd`** — the query-run capture pipeline
(`system.query_log` → `runtime.facts` kind QueryRun) — specified in the
implementation outline.

## Alternatives

The QOC matrix carries the comparison. Kill-reason nuance, with measured
evidence:

- **O1 — client-seam recorder.** Least data-centric: the record dies with
  the process, covers only boxer-issued queries, and needs its own poller to
  reach `query_log`. Its unique value — truths the server never sees
  (client-side failures, supersession, parameters) — returns as a thin
  emitter *into the same tables* in a later slice, not as the spine.
- **O2 — generated-SQL MV.** Best autonomy, zero deployables; pays a new
  leeway backend reproducing membership-role bookkeeping in SQL array
  algebra, golden-locked before first light. Escalation path.
- **O3 — WASM kernel.** Demonstrated on 26.6 (module registration via SQL,
  `BUFFERED_V1` block transform over real `query_log` rows, MV-on-flush
  firing; heap-free TinyGo, runtime TinyGo via a wasm start-section patch,
  and Rust SDK). Rejected for the spine: experimental server gate, module
  lifecycle, fuel sizing — and still a second implementation (C1).
  Escalation variant where imperative authoring beats SQL generation.
- **O4 — executable UDF.** Single-source like O6 but with server-filesystem
  deployment state and a subprocess per batch, and none of O6's
  observability. Dominated.
- **O5 — bus ETL.** For born-in-CH→for-CH stages the broker and framework
  buy nothing on one host: the domain-hard transform is the same Go either
  way, and intermediate state moves from queryable tables to opaque
  subjects. Framework licensing is fragmented (MIT core; most connectors
  Apache-2.0; enterprise connectors key-gated; a fully-MIT fork exists).
  Remains the right shape for born-outside-CH stages (SD1), with a bare
  keelson service preferred over a framework until connector breadth is the
  actual need.

### O6 sub-shapes — who drives the load

Within the chosen family, three drivers were weighed:

- **Refreshable-MV pull** *(chosen)*: CH schedules, reads the endpoint, and
  performs the write. Endpoint downtime degrades to catch-up — the next
  successful refresh reads everything newer than the destination watermark
  (measured: no gap).
- **Event-MV → URL-engine sink push.** Each flushed block is offered to the
  endpoint exactly once; endpoint downtime is a gap until an anti-join
  backfill (measured). No `url()` involvement, so none of its
  read-amplification duplicates. Retained as the fallback for
  fire-and-forget stages where a lost window is acceptable.
- **Daemon-driven direct insert (no MV).** The service extracts, encodes,
  and INSERTs on its own ticker — the `chstore.InsertArrow` pattern promoted
  to a unit, and the simplest shape on paper: no `url()` read amplification,
  no anti-join guard, one fewer DDL object. Rejected for the spine on four
  grounds. (1) *The pipeline stops being a schema object*: dataflow,
  cadence, and dedup guard live in Go instead of DDL, drift detection via
  `create_table_query` disappears, and pipeline health is only as observable
  as the daemon's logs — where the MV gets `system.view_refreshes` (status,
  exception, retry, duration) as a queryable table for free. (2) *Authority
  separation*: under the MV the database performs the write and the service
  needs no INSERT grant — its blast radius is bounded by the one declared
  target (ADR-0026 hygiene); daemon-insert puts the write on an imperative
  path behind a write-capable credential. (3) *Scheduling machinery*:
  refresh serialization (no overlapping runs), retry with backoff, and
  exception capture would be reimplemented as a hand-rolled ticker loop,
  each piece a small bug surface; under the MV the remaining Go is a
  stateless HTTP handler. (4) *Shape-stability*: the SD6 escalation and
  every later ELT stage (facts → marts) are MV-driven by construction, so an
  MV-driven first stage makes moving the transform in-database a
  transform-tier swap rather than a topology change. The honest cost of the
  chosen shape: the `url()` read-amplification class of duplicates exists
  *only* because of the pull — deterministic ids carry the dedup burden in
  either shape, and the anti-join guard is the price paid for reasons (1)
  through (4).

## Consequences

### Positive

- The leeway encoding runs exactly once, in its existing implementation; no
  golden-equivalence program gates v1.
- Stable ClickHouse features only on the spine; the experimental surface
  (WASM) is confined to a trigger-gated escalation.
- Pipelines are self-observing: refresh state in `system.view_refreshes`,
  source errors in `system.text_log`, and the captured data is ordinary SQL
  — explorable with play itself.
- Restart-lossless capture with automatic catch-up; durable history in
  schemas boxer owns, decoupled from system-table TTL and upgrade drift.
- Once clients stamp (SD7), the server's `query_log` is independently
  attributable by any tool, boxer running or not.
- The unit + reconciler pattern generalizes (a future `sysmetricsd` unit is
  a near-copy with the proc-access asymmetry inverted).

### Negative

- Capture pauses while a service is down; outages longer than the source
  TTL lose that window permanently.
- One long-running unit per pipeline to package, monitor, and roll.
- At-least-once delivery means rare duplicates under adversarial timing
  survive until the dedup backstop; readers must treat deterministic
  id/naturalKey as identity, not row count.
- `runtime.facts` grows with query traffic; retention/partitioning for the
  facts table remains an open concern that this plane sharpens (interim
  control: the capture scope knob).

### Neutral

- `log_comment` stamping is visible to every observer of a shared server —
  intended (attribution is the point), but stated.
- The loopback HTTP surface carries no auth while loopback-only (bind
  refusal is the control, as with the introspection plane).
- NATS core is a decided dependency of the *strategy* but not of any shipped
  slice until a live consumer exists.

## Implementation outline

> Informational appendix — the first instantiation. Details may evolve
> without superseding this ADR provided the decision above stands.

### queryrunsd (capture pipeline, slice S1 of the explanation page)

- **Fact shape.** One `runtime.facts` row per terminal `query_log` event
  (`type != 'QueryStart'`), kind QueryRun: naturalKey = `query_id`,
  timestamp = `event_time_microseconds`; scalar attributes (duration,
  read/written/result rows and bytes, peak memory, `normalized_query_hash`,
  `query_kind`, event type, exception code/text) on typed sections;
  `ProfileEvents` fanned out per event via the `LogField` mixed-ref pattern
  (event name as high-card parameter); identity lifted from `log_comment`;
  capped inline query text until interning lands.
- **Packages.** `runtime/queryrunfacts` (layout, vocab additions, extract
  SQL, row→entity encoding — pure library) and `runtime/queryrunsvc`
  (`/pull` + `/healthz`, boot reconciliation, loopback refusal);
  `public/app/commands/queryrunsd` registers the verb.
- **Pipeline objects** (reconciled at boot):

```sql
CREATE MATERIALIZED VIEW runtime.mv_queryruns
REFRESH EVERY 5 SECOND APPEND TO runtime.facts
AS SELECT * FROM url('http://127.0.0.1:8127/pull', 'ArrowStream', '<facts columns>')
WHERE `id:id:u64:2k:0:0:` NOT IN (
  SELECT `id:id:u64:2k:0:0:` FROM runtime.facts
  WHERE `ts:ts:z64:2k:0:0:` > now64() - INTERVAL 1 DAY);
```

  The service's extract (tagged `log_comment='queryrunsd-extract'`) selects
  `query_log` rows newer than the newest KindQueryRun fact, excluding its own
  tag and the refresh inserts.
- **Environment** (ADR-0009 registry): `IMZERO2_QUERYRUNS_LISTEN`
  (`127.0.0.1:8127`), `IMZERO2_QUERYRUNS_CH_URL`, `IMZERO2_QUERYRUNS_CADENCE`
  (`5s`), `IMZERO2_QUERYRUNS_SCOPE` (`all` | `stamped` | `off`; default
  `all`).
- **Unit** (`showcase/onbox/queryrunsd.service`): `ExecStart=<current
  release>/main_go queryrunsd`, `Restart=always`, `DynamicUser=yes`,
  `ProtectSystem=strict`, `ProtectProc=invisible` + `ProcSubset=pid`,
  `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`,
  `IPAddressAllow=localhost` + `IPAddressDeny=any`,
  `SystemCallFilter=@system-service`,
  `After=network-online.target clickhouse-server.service`.
- **Stamping** (SD7): `play.Client` sets `log_comment` with
  `{run_id, app, lane, authored_fp, sent_fp, chain_fp, env_fp}`; `chclient`
  follows when it adopts recording.
- **Tests.** Unit tests on extract SQL and row→entity encoding; integration
  gated on a live CH (`Ping`-skip): fresh-server reconciliation (lazy
  creation), one query → one fact row, endpoint-down catch-up,
  duplicate suppression under forced double-read. No encoder goldens — it
  *is* the existing builder.

Later slices (glass surfaces, result tiers, interning, forwarding leg) are
mapped in the explanation page; slices that need a real decision (weave
semantics) get their own ADR when reached.

## Status

Proposed — 2026-07-11. Awaiting review by the code owner.

Supersedes [ADR-0050](0050-clickhouse-observability-pipeline.md) (flipped
`superseded` 2026-07-11; never accepted or implemented — its results and
progress planes were answered by shipped reality, its `query_log` plane by
this ADR). [ADR-0051](0051-query-categorization-provenance.md) remains
dormant-proposed; its successor is expected at the weave slice.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.

## References

- [doc/explanation/query-observability.md](../explanation/query-observability.md)
  — the vision, entity model, planes, slices, and ADR bindings this ADR
  serves.
- [ADR-0026](0026-app-runtime-and-capability-subjects.md) — runtime,
  capabilities, `runtime.facts` (§SD6).
- [ADR-0050](0050-clickhouse-observability-pipeline.md) — superseded;
  kill-reason record for the push/broker family.
- [ADR-0051](0051-query-categorization-provenance.md) — dormant; theory
  record for shape classification.
- [ADR-0090](0090-sysmetrics-pubsub-data-plane.md) — standalone-service
  anatomy; bus-for-born-outside-CH precedent; `natsbus`/`Bridge`.
- [ADR-0094](0094-keelson-introspection-tables.md) — loopback `url()`
  serving precedent.
- [ADR-0097](0097-play-reactive-query-graph.md) — per-lane `query_id`
  stamping; signals as unbound parameters.
- [ADR-0108](0108-keelson-sql-pass-registry.md) — the pre-execute rewrite
  chain captured by SD7's fingerprints.
- [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md) — ref
  tuples (lineage encoding for later slices).
- [ADR-0111](0111-identity-technology-neutral-leased-id-generation.md) —
  future unification for the reserved id band.
- [ADR-0112](0112-dimensionstore-interned-facts-additive-memberships.md) —
  interning substrate for definitions, chains, and large values.
- ClickHouse documentation:
  [query_log](https://clickhouse.com/docs/operations/system-tables/query_log),
  [url table function](https://clickhouse.com/docs/sql-reference/table-functions/url),
  [refreshable materialized views](https://clickhouse.com/docs/materialized-view/refreshable-materialized-view),
  [WebAssembly UDFs](https://clickhouse.com/docs/sql-reference/functions/wasm_udf).
- Framework licensing (O5 input):
  [Redpanda Connect licensing](https://docs.redpanda.com/connect/get-started/licensing/),
  [benthos core (MIT)](https://github.com/redpanda-data/benthos).
