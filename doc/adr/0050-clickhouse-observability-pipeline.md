---
type: adr
status: proposed
date: 2026-04-20
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0050: ClickHouse Observability Pipeline via URL Engine, On-Disk Batches, and NATS Notifications

## Context

This repository needs a ClickHouse client + observability layer: a way
to *run* a ClickHouse query, stream its result record batches, and
expose its progress. No existing package covers it. We need that
surface for two user-facing goals:

1. **Graphical UIs** that show a query's progress (rows read, bytes
   read, total-approx, memory), its result record batches as they
   arrive, and its final accounting (exceptions, profile events, peak
   memory).
2. **Machine-consumable exports** of the same signal to NATS so
   downstream tools (dashboards, loggers, distributed agents) can
   subscribe without each re-implementing ClickHouse protocol.

The Go-side contract is fixed by upstream need:

```go
type ClickHouseClientI interface {
    ExecuteArrowStream(ctx context.Context, sql string, alloc memory.Allocator) (rdr *ipc.Reader, body io.Closer, err error)
}
```

— a reentrant, Arrow-IPC-returning submission primitive. Everything
else in the pipeline builds on top of it.

**Deployment constraints.** ClickHouse, the NATS broker, and the Boxer
Go process that consumes observability data run **on the same host**.
The host has NVMe storage; memory is not the scarce resource. Arrow is
the canonical in-memory and on-wire data format throughout the rest of
the repository. No existing NATS dependency; the project can adopt one.

**Non-goals.** Multi-host fan-out, cross-datacenter durability, long-term
archival of result batches. When those appear as requirements, an
object-store-backed variant (see O6 below) is the upgrade path.

## Design space (QOC)

**Question.** How should Go consumers receive a ClickHouse query's
progress ticks, terminal `query_log` row, and result record batches,
given the single-host deployment constraint and the repository's
Arrow-first conventions?

**Options.**

- **O1 — Polling.** Go wraps `ClickHouseClientI`; a ticker queries
  `system.processes` by `query_id`, results arrive on the primary Arrow
  stream. Single process, no broker.
- **O2 — NATS engine, per-kind subjects.** Three ClickHouse
  `NATS` table engines (progress / query_log / result), three subjects.
  Per-query DDL for the result sink (schema varies per query).
- **O3 — NATS engine + RawBLOB + `formatRow` trick.** Static single-subject
  sink; `formatRow('ArrowStream', …)` encodes each row into a self-describing
  blob. Avoids per-query DDL.
- **O4 — Direct URL engine to Boxer HTTP.** CH pushes Arrow IPC stream
  bodies via URL engine to HTTP endpoints on the Boxer Go process. No
  broker, no intermediate format. Point-to-point.
- **O5 — URL engine → HTTP→NATS bridge.** CH → bridge → NATS subjects.
  Bridge chops Arrow at message boundaries to fit `max_payload`. Adds
  decoupling and fan-out.
- **O6 — URL → bridge → JetStream Object Store.** Inline small bodies on
  NATS, claim-check large bodies into JS ObjectStore. Durable, replayable,
  multi-host-ready.
- **O7 — URL → bridge → NVMe files + NATS notification *(chosen)*.**
  Bridge writes each Arrow record batch as a self-contained `.arrow` file
  on local NVMe, publishes a small reference message on NATS. Consumer
  mmaps files for zero-copy reads.

**Criteria.**

- **C1 — Per-row wire overhead.** Does the format repeat the Arrow schema
  per row (`formatRow`) or ship it once per batch?
- **C2 — Per-batch size ceiling.** Is there a hard cap that requires
  producer tuning to avoid runtime failure?
- **C3 — Consumer decode cost.** How cheaply does the consumer reach
  record-batch buffers — full decode, copy, or zero-copy?
- **C4 — Producer/consumer decoupling.** Can CH submit without reaching
  the Boxer Go process directly?
- **C5 — Fan-out to multiple observers.** Can a GUI and an exporter
  subscribe to the same query's output independently?
- **C6 — Durability / replay.** Are events retained for late attachers?
- **C7 — Deployment complexity.** Number and independence of runtime
  components the operator must run.
- **C8 — Fit for co-located single-host deployment.** Does the architecture
  exploit the actual deployment shape (shared page cache, NVMe,
  no network)?

**Assessment.** `++` strong positive, `+` positive, `−` negative,
`−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 | O6 | O7 |
|----|----|----|----|----|----|----|----|
| C1 | ++ | +  | −− | ++ | ++ | ++ | ++ |
| C2 | ++ | −  | ++ | ++ | −  | ++ | ++ |
| C3 | +  | +  | +  | +  | +  | +  | ++ |
| C4 | −  | ++ | ++ | −  | ++ | ++ | +  |
| C5 | −  | ++ | ++ | −  | ++ | ++ | +  |
| C6 | −  | −  | −  | −  | −  | ++ | +  |
| C7 | ++ | +  | +  | +  | −  | −− | −  |
| C8 | +  | +  | +  | +  | +  | −  | ++ |

O7 is Pareto-optimal among configurations suited to the stated
single-host deployment: native Arrow batching with zero per-row
overhead (C1), no practical size ceiling (C2), zero-copy consumer
access via `mmap` (C3), and natural use of NVMe + shared page cache
available on the host (C8). It gives up multi-host decoupling and
inherent replay (C4/C5/C6 vs O6) — a loss the deployment constraint
makes acceptable.

## Decision

We adopt architecture **O7**:

1. **ClickHouse producer.** Two long-lived materialized views publish
   `system.processes` and `system.query_log` rows via URL-engine sink
   tables to a bridge service; dictionary encoding is disabled at the
   producer. User queries are rewritten by the Go executor into
   `INSERT INTO FUNCTION url(…) SELECT '<qid>' AS query_id, …`
   with `SETTINGS query_id='<qid>'` so the `query_id` is set on both
   the connection and as an explicit column.
2. **Bridge.** A small stateless HTTP service with three endpoints
   (`/ingest/progress`, `/ingest/query_log`, `/ingest/result`). Progress
   and query_log bodies are republished inline on their NATS subjects.
   Result bodies are chopped at Arrow IPC message boundaries; each
   `{schema + one record batch}` pair is written as a self-contained
   Arrow file on NVMe, and a small `ResultRefLw` reference is published
   on `boxer.ch.result.<qid>`.
3. **Reaper.** A separate idempotent janitor applies a configurable
   eviction policy against the results directory.
4. **Consumer (Go package `chstream`, path TBD within this repository).**
   Subscribes to NATS, dispatches by subject + `query_id`, `mmap`s
   referenced files, and exposes per-query event iterators
   (`QueryHandleI.Events() iter.Seq2[QueryEvent, error]`) to downstream
   UIs and exporters.

The Go package itself holds **only** the consumer side. Bridge and
reaper are separate binaries outside `chstream`'s scope and may live
alongside it (e.g. `chbridge/` and `chreaper/` sibling packages) once
implemented.

## Alternatives

The QOC matrix carries the comparative assessment. Nuance:

- **O1 — Polling.** Works without a broker, but every observer has to
  hold its own CH connection and its own progress-query ticker.
  Exports outside the process (the second goal above) have no natural
  seam. Rejected.
- **O2 — NATS engine, per-kind subjects.** Requires per-query result
  DDL on the hot path (each query creates and eventually drops its own
  sink table because result schema varies per query). Rejected as
  unnecessary operational overhead.
- **O3 — RawBLOB + `formatRow`.** Per-row schema serialization dominates
  for high-row-count results; the `max_block_size` / dictionary-encoding
  tuning that makes it tolerable is equally available in O5/O7 with
  lower overhead. Rejected as the primary path.
- **O4 — Direct URL → Boxer HTTP.** Tighter coupling than O5/O7: CH
  reaches the Boxer Go process directly, so restart semantics and
  fan-out become the Go process's problem. Rejected.
- **O5 — URL → bridge → NATS (no blob store).** Good middle ground,
  but every result batch has to fit under NATS `max_payload`; requires
  aggressive `max_block_size` tuning and leaves a runtime failure mode
  for pathological single-row blobs. Rejected in favour of O7's
  unbounded-per-batch story.
- **O6 — URL → bridge → JetStream Object Store.** Durable and
  multi-host-ready; the natural answer when deployment fans out beyond
  one host. Rejected *for this deployment* because co-located NVMe +
  `mmap` delivers lower per-batch latency (zero-copy reads, shared
  kernel page cache) than OBJ's chunked `Get`, and the reaper plus
  NVMe storage gives enough observability-grade durability without a
  broker-backed store. O6 is retained as the documented upgrade path
  when multi-host requirements appear.

## Consequences

### Positive

- Native Arrow IPC framing end-to-end; no per-row schema repetition,
  no nested Arrow-in-Arrow encoding.
- Consumer record-batch access is `syscall.Mmap` + `ipc.NewReader`
  over `bytes.Reader`: zero-copy into the kernel page cache, shared
  across multiple in-process observers.
- The bridge is Arrow-semantics-unaware — byte-level message framing,
  `write()` to disk, `nats.Publish` of a small notification. Trivially
  reimplementable in any language.
- Unbounded per-batch result size; only the reaper's disk budget
  constrains it. NATS `max_payload` does not apply to result bodies.
- Disabling dictionary encoding at the producer
  (`output_format_arrow_use_dictionary = 0`) keeps each batch
  self-contained, so chopping at message boundaries is correct
  without cross-batch state.
- Bridge, reaper, and consumer are independent processes with trivial
  restart semantics — bridge is stateless, reaper is idempotent,
  consumer re-attaches to queries via NATS.
- Core NATS is sufficient for notifications; JetStream is optional and
  can be enabled later for subject-level replay without touching the
  data plane.

### Negative

- Architecture is pinned to single-host deployment. Multi-host fan-out
  requires re-introducing an object store (path O6) or a shared
  filesystem (NFS/Ceph); both widen the operational envelope.
- No out-of-the-box durable replay; once the reaper evicts a batch
  file, a subscriber that attached late cannot retrieve it.
- One additional deployable — the bridge. Small and stateless, but
  still something to package, monitor, and roll.
- Cross-user permission layout must be planned at provisioning time
  (bridge writes files; reaper + consumer read/delete; all three may
  run under different system users). Runtime permission surprises are
  painful; this is addressed explicitly below.
- Generated ClickHouse DDL (materialized views, sink tables) is
  deployment state outside the Go code's direct ownership. Migrations
  and version-skew handling need their own home.

### Neutral

- Leeway canonicaltypes will define `ProgressInfoLw`, `QueryLogInfoLw`,
  and `ResultRefLw`, feeding Go type generation and Arrow schemas.
  Leeway does not yet emit ClickHouse DDL; the MV and sink-table
  templates are hand-written for v1 and re-converted to codegen when
  that backend exists.
- The bridge implementation is intentionally outside this ADR's
  primary Go package. It may sit as a sibling package (e.g.
  `chbridge/cmd/ch-bridge/`) once the consumer side
  stabilises.
- `INSERT INTO FUNCTION url(…, 'ArrowStream', '<schema>', …)` declares
  the result schema at the ClickHouse-function call site. The consumer
  reads this schema from the Arrow IPC header of each file and caches
  it per query — no separate control message required.

## Implementation outline

> Informational appendix. Implementation details may evolve without
> superseding this ADR provided the decision above stands. When a
> detail below turns into a cross-cutting choice of its own, spin it
> out as a new ADR and link back here.

### Topology

```text
+------------+    HTTP POST    +--------+   write    +-----------+
| ClickHouse | --------------> | bridge | ---------> | NVMe FS   |
+------------+   ArrowStream   +--------+   .arrow   +-----------+
      |                           |                        ^
      |                           | publish                | mmap (read-only)
      |  INSERT / KILL QUERY      v                        |
      +-----------------------> +-------+   subscribe   +---------+
                                | NATS  | ----------->  | Boxer   |
                                +-------+               | chstream|
                                                        +---------+
                                                             ^
                                                             | (observes)
                                                        +---------+
                                                        | reaper  |
                                                        +---------+
```

### NATS subjects

| subject                         | payload                                                    |
|---------------------------------|------------------------------------------------------------|
| `boxer.ch.progress`             | Arrow IPC stream; rows of `ProgressInfoLw`                 |
| `boxer.ch.query_log`            | Arrow IPC stream; rows of `QueryLogInfoLw`                 |
| `boxer.ch.result.<query_id>`    | `ResultRefLw` envelope (Arrow IPC, one row) per batch      |

### Bridge HTTP endpoints

| method/path                                | body                                          | side effect                                                  |
|--------------------------------------------|-----------------------------------------------|--------------------------------------------------------------|
| `POST /ingest/progress`                    | Arrow IPC stream (`ProgressInfoLw` rows)      | republish body on `boxer.ch.progress`                        |
| `POST /ingest/query_log`                   | Arrow IPC stream (`QueryLogInfoLw` rows)      | republish body on `boxer.ch.query_log`                       |
| `POST /ingest/result?qid=<qid>`            | Arrow IPC stream; first column `query_id`     | chop → write files → publish `ResultRefLw` per batch         |

### File layout

- Base directory: `/var/lib/boxer/results/` (configurable).
- Subdirectory per query: `<basedir>/<query_id>/`.
- One file per record batch: `<basedir>/<query_id>/<seq:010>.arrow` — a
  self-contained Arrow IPC stream with exactly one schema message and
  one record batch, followed by the end-of-stream marker.
- Directory permissions: owner = bridge user, group = shared `boxer`
  group, mode `2770` (setgid for inherited group).
- File permissions: mode `0640`, group `boxer`, written by the bridge
  user.

### Leeway-generated types (sketch)

```text
record ProgressInfoLw {
    query_id: string;
    elapsed_ns: uint64;
    read_rows: uint64; read_bytes: uint64;
    total_rows_approx: uint64;
    memory_usage: int64;
    result_rows: uint64; result_bytes: uint64;
    observed_at: timestamp;
}

record QueryLogInfoLw {
    query_id: string;
    type: string;                       // QueryFinish | ExceptionBeforeStart | ExceptionWhileProcessing
    query_start_time: timestamp;
    query_duration_ms: uint64;
    read_rows: uint64; read_bytes: uint64;
    written_rows: uint64; written_bytes: uint64;
    result_rows: uint64; result_bytes: uint64;
    memory_usage_peak: int64;
    exception_code: int32;
    exception: string;
    stack_trace: string;
    normalized_query_hash: uint64;
    profile_event_names:  []string;     // SoA pair flattens ProfileEvents map
    profile_event_values: []int64;
    observed_at: timestamp;
}

record ResultRefLw {
    query_id: string;
    path: string;                       // absolute path of the .arrow file
    seq: uint64;                        // per-query monotonic
    row_count: uint64;
    byte_size: uint64;
    blake3_hash: optional []byte;       // 32 bytes; populated when HashFiles=true
    observed_at: timestamp;
}
```

### Go interfaces (consumer, target package `chstream`)

```go
type ClickHouseClientI interface {
    ExecuteArrowStream(ctx context.Context, sql string, alloc memory.Allocator) (rdr *ipc.Reader, body io.Closer, err error)
}

type NATSSubscriberI interface {
    Subscribe(ctx context.Context, subject string) (msgs <-chan NATSMessage, cancel func(), err error)
    io.Closer
}

type NATSMessage struct {
    Subject string
    Payload []byte
    Seq     uint64
    When    time.Time
}

type QueryExecutorI interface {
    Run(ctx context.Context, sql string, alloc memory.Allocator) (handle QueryHandleI, err error)
    Attach(ctx context.Context, queryID string, alloc memory.Allocator) (handle QueryHandleI, err error)
    io.Closer
}

type QueryHandleI interface {
    QueryId() (queryID string)
    Events()  iter.Seq2[QueryEvent, error]
    Cancel()
}

type QueryEventKindE uint8
const (
    QueryEventKindProgress QueryEventKindE = 1
    QueryEventKindBatch    QueryEventKindE = 2
    QueryEventKindFinal    QueryEventKindE = 3
)

type QueryEvent struct {
    Kind     QueryEventKindE
    QueryId  string
    Seq      uint64
    Progress ProgressInfo      // Kind == Progress
    Batch    arrow.RecordBatch // Kind == Batch; consumer owns Retain/Release
    Final    QueryLogInfo      // Kind == Final
}
```

### Semantics

- **Subscription-before-submission invariant.** `Run` opens NATS
  subscriptions and registers the handle *before* calling
  `ExecuteArrowStream`. No progress tick or result batch arrives before
  the handle is wired.
- **`Attach`** registers a handle for an already-submitted query.
  Observes events that arrive *after* the call; earlier events are
  lost unless JetStream replay is configured on the adapter.
- **Ordering.** Per subject, NATS preserves publish order. Across
  subjects the merged stream is in NATS arrival order; no wall-clock
  merge.
- **Termination.** The `QueryEventKindFinal` event from `query_log` is
  the authoritative terminal signal. The handle then stays subscribed
  to the result subject for `Config.ResultDrainGrace` (default 1 s)
  and counts received result rows:
    - closes early when `receivedRows >= Final.ResultRows`;
    - on grace expiry, closes the iterator;
    - yields `ErrResultUnderRun` if `receivedRows < Final.ResultRows`
      at close.
- **Record-batch lifetime.** A batch delivered to the consumer has
  buffers that point into an mmap'd region. The handle retains the
  mmap; `batch.Release()` drops the consumer's ref, and `munmap`
  executes when the last ref drops (or when the handle closes).
- **Backpressure (per-handle buffered channel).** Batch events block
  (data loss is unacceptable); progress events drop-oldest (keep the
  newest tick); final event blocks (exactly one, must not drop).
- **Cancel.** Best-effort `KILL QUERY WHERE query_id='<qid>'` via the
  injected `ClickHouseClientI`. Iterator closes with `ctx.Err()` or
  the kill's error, whichever applies.

### ClickHouse-side DDL (hand-written template for v1)

```sql
-- progress
CREATE TABLE ch_progress_sink (<ProgressInfoLw columns>)
ENGINE = URL('http://boxer-bridge:9090/ingest/progress', 'ArrowStream')
SETTINGS output_format_arrow_use_dictionary = 0;

CREATE MATERIALIZED VIEW mv_progress TO ch_progress_sink AS
SELECT query_id,
       toUInt64(elapsed * 1e9) AS elapsed_ns,
       read_rows, read_bytes, total_rows_approx,
       memory_usage, result_rows, result_bytes,
       now64() AS observed_at
FROM system.processes;

-- query_log
CREATE TABLE ch_query_log_sink (<QueryLogInfoLw columns>)
ENGINE = URL('http://boxer-bridge:9090/ingest/query_log', 'ArrowStream')
SETTINGS output_format_arrow_use_dictionary = 0;

CREATE MATERIALIZED VIEW mv_query_log TO ch_query_log_sink AS
SELECT query_id, type, query_start_time, query_duration_ms,
       read_rows, read_bytes, written_rows, written_bytes,
       result_rows, result_bytes, memory_usage AS memory_usage_peak,
       exception_code, exception, stack_trace, normalized_query_hash,
       mapKeys(ProfileEvents)   AS profile_event_names,
       mapValues(ProfileEvents) AS profile_event_values,
       now64() AS observed_at
FROM system.query_log
WHERE type != 'QueryStart';

-- result (generated per-run by the Go executor; no DDL on the hot path)
INSERT INTO FUNCTION url(
    'http://boxer-bridge:9090/ingest/result?qid=<qid>',
    'ArrowStream',
    '<inline result schema>'
)
SELECT '<qid>' AS query_id, <user_cols>
FROM (<user_query>)
SETTINGS query_id='<qid>', output_format_arrow_use_dictionary = 0;
```

### Bridge (separate binary)

- Go, depends on `nats.go` + `net/http` + `lukechampine.com/blake3`
  only; does **not** import `arrow-go`.
- Per `/ingest/result` request: length-prefix-scans the Arrow IPC
  stream from the HTTP body, caches the schema message bytes, and for
  each record batch writes
  `<basedir>/<qid>/<seq:010>.arrow`
  (schema + batch as a mini self-contained stream). Optionally computes
  `blake3(filebytes)` when `Config.HashFiles=true`, then publishes a
  `ResultRefLw` on `boxer.ch.result.<qid>`.
- Per `/ingest/progress` and `/ingest/query_log`: republishes the
  request body unchanged on the corresponding subject.
- Failure modes: disk-full or NATS publish failure → HTTP `503`; CH
  retries the INSERT per its own settings. Operator sees the 503 rate
  and the reaper backlog.

### Reaper (separate binary)

- Scans the base directory periodically; deletes files according to
  the configured policy.
- v1 policy: **size-bounded LRU with age floor** — enforce a global
  byte budget (default 10 GiB) by evicting oldest files first, but
  never evict a file younger than `MinAge` (default 60 s). The age
  floor guarantees consumers have a mmap window.
- Future policy: ACK-driven — consumer publishes
  `boxer.ch.ack.<qid>.<seq>` after a successful `batch.Release()`;
  reaper removes acknowledged files immediately. Falls back to the
  age/size policy for unacked files. Out of scope for v1.
- Idempotent across restarts. Uses `unlink(2)` — open mmap'd file
  handles survive, so an in-flight consumer finishes reading even if
  the file vanishes from the directory.

### Integrity (optional blake3)

- Opt-in via `Config.HashFiles=true` on the bridge. When enabled, the
  bridge computes `blake3(filebytes)` and sets `ResultRefLw.blake3_hash`.
- Consumer verifies when the field is present; a mismatch is surfaced
  as an iterator error on the affected batch.
- Disabled by default: NVMe + same-host + `mmap` makes corruption
  implausible, and the hash is cheap-but-not-free.
- Hash algorithm fixed to `blake3` (`lukechampine.com/blake3`, already a
  dependency of this repository) per the sibling
  [boxer `CODINGSTANDARDS.md`](https://github.com/stergiotis/boxer/blob/main/CODINGSTANDARDS.md).

### Permissions plan (different users)

Two-or-three system users, one shared group:

- Users: `boxer-bridge`, `boxer-reaper`, `boxer-consumer` (any two may
  be consolidated in practice, e.g. reaper + consumer under one user).
- Group: `boxer`. All three users are members.
- Base directory: owned by `boxer-bridge:boxer`, mode `2770` (setgid so
  newly-created subdirectories inherit the group).
- Files: mode `0640`, group `boxer`, written by the bridge.
- Per-query subdirectories: mode `2770`, inherited group.
- Reaper has write access on subdirectories via the group bit; can
  `unlink(2)` entries even though it did not create them.
- Consumer has read-only access via the group; cannot modify or delete.
- If the deployment runs all three components under a single user, the
  plan collapses to `0700` / `0600` and a single uid — the rules above
  are a strict superset and remain correct.

## Status

Proposed — 2026-04-20. Awaiting review by `p@stergiotis`.
Implementation to follow once accepted.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [boxer `CODINGSTANDARDS.md`](https://github.com/stergiotis/boxer/blob/main/CODINGSTANDARDS.md)
  — sibling-project conventions for error handling, naming, packages,
  and the `blake3` hash selection, followed here by agreement.
- [boxer `doc/DOCUMENTATION_STANDARD.md`](https://github.com/stergiotis/boxer/blob/main/doc/DOCUMENTATION_STANDARD.md)
  — sibling-project ADR and Diátaxis conventions; this ADR is authored
  against its template.
- [NATS JetStream Object Store](https://docs.nats.io/using-nats/developer/develop_jetstream/object)
  — considered (option O6), retained as the documented upgrade path
  for multi-host deployments.
- Apache Arrow IPC format — the self-framed, length-prefixed,
  mmap-friendly wire format that makes the bridge's byte-level chopping
  correct without any Arrow-semantic decoding.
