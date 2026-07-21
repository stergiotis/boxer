---
type: adr
status: proposed
date: 2026-07-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0138: streaming query transport and run observability

## Context

Information-retrieval queries in play are synchronous today: one HTTP
request, one complete ArrowStream response. The direction this ADR
commits to is asynchronous delivery — a run's results and its progress
stream to the recipient as they exist, with live display of inflight
state — while keeping the synchronous path for what it serves well.

Most of the parts already exist, on opposite sides of the seam:

- The `/query` endpoint is an HTTP façade over a bus request
  (`chlocalbroker.ExecOnPool`), but the reply is one-shot — the runner
  reads one body to completion. Streaming replies are the genuinely new
  wire work.
- Live progress exists client-side:
  [ADR-0115](./0115-query-observability-data-plane-strategy.md) plane A
  reads in-band `X-ClickHouse-Progress` headers off the raw connection —
  deliberately scoped to plain `http://` endpoints, transient by design.
- Terminal observability exists as data: plane B turns terminal
  `system.query_log` events into QueryRun facts
  (`queryrunfacts`/`queryrunsvc`), and play already mints a stable
  `query_id` per lane, explicitly so runs are addressable in
  `system.processes`/`query_log`.
- [ADR-0050](./0050-clickhouse-observability-pipeline.md) (superseded by
  ADR-0115, never implemented) surveyed this design space — progress
  ticks, terminal log rows, and result batches over messaging transports.
  Its QOC remains useful; its O1 (a ticker on `system.processes` by
  `query_id`) is adopted here in a changed context: the observer is no
  longer the connection holder.

This ADR extends [ADR-0115](./0115-query-observability-data-plane-strategy.md)
rather than superseding it: plane B's facts pipeline is reused as-is, and
plane A's in-band capture remains the sync path's and the
`clickhouse-local` plane's mechanism. What changes is where progress is
captured for server placements, and how results travel for asynchronous
runs.

## Decision

A run is a stream with a durable identity; progress is observed by a
per-server poller and republished as frames; terminal truth comes from
the result path and `query_log`, never from inference.

- **SD1 — Run identity.** The ClickHouse `query_id` *is* the run
  identity. Play's minted per-lane ids already serve as the
  observability join key; this SD promotes the minted id to name the
  run's stream subject, its QueryRun fact, its pins, and its cancel
  address. One key joins live frames, terminal facts, server log tables,
  and results.

- **SD2 — Frame contract.** A run's stream carries typed, sequenced
  frames: data, progress, and exactly one terminal frame (success,
  error, or *truncated* — a `max_result_rows`/overflow cap is a distinct
  terminal state, not a smaller success). A subscription that ends
  without a terminal frame renders as *incomplete*; a plausible-looking
  short result must be impossible to mistake for a complete one. This is
  the streaming generalization of
  [ADR-0136](./0136-play-query-dispatch-resolver.md) §SD8.

- **SD3 — Progress source for server placements: a per-server poller.**
  One poller per server queries `system.processes` once per tick for all
  watched `query_id`s and fans the rows out as progress frames on the
  per-run subjects — the
  [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) single-writer
  pattern with `system.processes` in the role of `/proc`. Pull beats the
  in-band headers here on three counts: observation is decoupled from
  the query connection (any subscriber can watch any run, over any
  transport), the fields are richer (in-flight ProfileEvents, peak
  memory, `is_cancelled`), and the same addressing doubles as the cancel
  surface. The poller excludes its own `query_id`s. On clusters the
  poller watches the coordinator the balancer chose
  ([ADR-0137](./0137-query-placement-clusters-balancing.md) §SD3/§SD4).

- **SD4 — Polling caveats, stated.** A query faster than the tick never
  appears: absence of progress frames is not absence of a query. And a
  run *vanishing* from `system.processes` is ambiguous (finished, killed,
  or errored) — the terminal frame always comes from the result path or
  `query_log`; the poller contributes ticks only.

- **SD5 — The `clickhouse-local` plane keeps in-band capture.** Broker
  workers are one-shot processes with no listener; there is no
  `system.processes` to poll from outside, and their own system tables
  die with the process. In-band progress remains that plane's only
  witness, and the republished stream its only durable observation
  point. The honest cost mirrors
  [ADR-0134](./0134-adhoc-datasets.md)'s two decrypt paths: two capture
  paths, split by placement.

- **SD6 — Terminal results are first-class.** The terminal frame carries
  the run's totals and (on failure) the exception, in the shape
  `queryrunfacts` already encodes; it feeds the existing plane-B facts
  pipeline. The dispatch decision that routed the run
  ([ADR-0136](./0136-play-query-dispatch-resolver.md) reason, placement,
  chosen member) is recorded on the QueryRun fact, making routing
  auditable with plain SQL.

- **SD7 — Control lane vs bulk lane.** The bus carries control, progress,
  and small result frames; bulk result bodies ride a side channel (the
  loopback patterns this repository already uses), with the stream's
  frames referencing them. Result wire format is kept separate from
  ingestion wire format ([ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md));
  frame encoding and progress cadence land in the
  [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)
  codec/cadence seam. A useful property falls out: the progress lane is
  the stream's heartbeat, so a stalled bulk channel, a stalled query, and
  a dead run are three distinguishable failures.

- **SD8 — Cancel.** Cancel is `KILL QUERY WHERE query_id = <run>`
  addressed via the run's placement, carried over the same audited
  request/reply discipline as other mutating bus capabilities. Play's
  existing kill-and-replace semantics is the precedent.

- **SD9 — Live display tiers.** Tier 1 (progress: rows/bytes/elapsed
  against estimates) and tier 2 (row-tail streaming for shapes that
  produce rows progressively) are in scope; rendering follows the
  existing subscribe-hold-poll UI pattern with append-only stability.
  Tier 3 — live *partial aggregates*, which require plan-level
  decomposition of a blocking aggregation into incremental pieces — is
  **deferred**, named here so "live display" is not read as promising it.

- **SD10 — One error taxonomy.** HTTP exception envelopes
  ([ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md)), bus
  errors, and side-channel aborts normalize into the frame contract's
  error terminal before reaching the UI; panels see one shape.

## Alternatives

- **In-band progress only, relocated to the executor.** Keeps one capture
  path. Rejected for server placements: visibility stays coupled to the
  connection holder, the raw-header trick does not survive TLS or other
  transports, and third-party observation (a second window, ops tooling)
  would still need a republish hop — at which point the poller is
  simpler and richer. Kept for the `clickhouse-local` plane (SD5), where
  it is the only option.
- **Per-run pollers.** One ticker per watched run. Rejected: N queries
  per tick against the same table; the per-server batch poll observes
  all runs for the same cost as one.
- **A log table as the live source.** Periodic per-query log tables
  (`query_metric_log` and kin) flush on an interval; they are the durable
  trail, not the live lane, and this repository's query-observability
  explanation already places them in the deep tier. Live frames come
  from `system.processes`; log tables stay one drill-down query away.
- **Deriving terminal state from process disappearance.** Rejected:
  ambiguous (SD4); a cancel and a success would be indistinguishable at
  the observer.
- **Per-kind messaging table engines** (progress / log / result as
  ClickHouse `NATS` engine sinks —
  [ADR-0050](./0050-clickhouse-observability-pipeline.md) O2/O3).
  Not re-evaluated here; the recorded kill-reasons (per-query DDL, server
  coupling) stand, and the poller needs no server-side configuration at
  all.

## Consequences

### Positive

- Progress and results become observable by parties other than the
  issuing connection — late joiners, second windows, ops tooling — keyed
  by one identity end to end.
- The failure modes of a live stream are distinguishable and loud
  (heartbeat vs bulk vs terminal), and truncation cannot masquerade as
  completion.
- Plane B and its facts pipeline are reused unchanged; routing decisions
  become queryable history.

### Negative

- Two progress-capture paths, split by placement (SD5) — the cost of the
  `clickhouse-local` plane's process model.
- A standing poller per watched server: small but real load, plus one
  more component with a lifecycle.
- Frame retention for late-joiner replay needs a bounded-buffer policy
  that does not exist yet; unbounded retention is not an option.

### Neutral

- Tick cadence bounds progress resolution; sub-tick runs simply have no
  live story (SD4), which their duration makes acceptable.
- The synchronous HTTP path remains fully supported; asynchronous
  delivery is an additional binding behind the
  [ADR-0136](./0136-play-query-dispatch-resolver.md) executor seam, not a
  replacement.

## Status

Proposed — awaiting review. Depends on
[ADR-0136](./0136-play-query-dispatch-resolver.md) (executor seam) and
[ADR-0137](./0137-query-placement-clusters-balancing.md) (placement,
affinity, cancel addressing). Extends
[ADR-0115](./0115-query-observability-data-plane-strategy.md); adopts
[ADR-0050](./0050-clickhouse-observability-pipeline.md) O1 in a changed
context without reopening that ADR's supersession.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — planes A/B this extends
- [ADR-0050](./0050-clickhouse-observability-pipeline.md) — prior design space; O1 adopted
- [ADR-0136](./0136-play-query-dispatch-resolver.md) — resolver seam and executor shape
- [ADR-0137](./0137-query-placement-clusters-balancing.md) — placement and affinity
- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) — single-writer scraper pattern
- [ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md) — wire-format separation
- [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md) — codec/cadence seam
- [ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md) — HTTP dialect and exception envelope
