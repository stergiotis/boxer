---
type: adr
status: proposed
date: 2026-05-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0028: Low-latency local SQL via pre-spawned clickhouse-local workers

## Context

[ADR-0026](./0026-app-runtime-and-capability-subjects.md) introduces the app runtime, the in-proc bus, and the capability-as-subject taxonomy. Its §SD3 reserves the `ch.query.{db}` / `ch.stream.{db}` / `ch.schema.{db}` family for ClickHouse access; those subjects are the eventual route to the shared CH server that hosts `runtime.facts`, `spinnaker.facts`, and business data. They are not yet bound (M2 wires `fs.*` first), but the contract is clear: those subjects talk to a long-lived multi-tenant server.

In parallel, a second SQL workload has accumulated and does *not* fit the server family:

- **Interactive scratch.** The regex explorer at [`public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_chlocal.go`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_chlocal.go) shells out to `clickhouse-local --query <SQL> --format ArrowStream` once per query. Cold subprocess fork costs ~50–60 ms on a warm filesystem cache; for debounced typing this is tolerable, for a panel that emits a query per keystroke or per repaint it is the dominant latency. Future apps in the same shape — play scratchpad expressions, schema-conversion utilities, ad-hoc Pretty-format peeks — would multiply the cost.
- **Format flexibility.** The CH server's native protocol (and `chclient`'s HTTP wrapper) is primarily a `Native`/`RowBinary`/`Arrow` carrier. Apps using `clickhouse-local` for *data-conversion* — read parquet, emit JSONEachRow; read CSV, emit Pretty for a debug pane; produce Markdown from a `system.*` table — need the full CH `FORMAT` surface (Pretty, JSONEachRow, CSV, TSVWithNamesAndTypes, Markdown, Vertical, …) that the server protocols do not naturally expose.
- **No shared state with the server.** Local scratch queries read from `file()`, `url()`, `s3()`, ad-hoc `engine=Memory` tables, and `system.*`. None of it touches `runtime.facts` or `spinnaker.facts`; mixing it onto the `ch.query.*` family would conflate two trust boundaries (server-side ACLs vs. local-only) and two latency budgets.

Stakeholder direction: **optimize for latency, not throughput.** This is interactive UI work, not a high-QPS service. We are willing to spend process resources to keep a few warm workers ready; we are not willing to manage a reuse lifecycle to extract every last query/sec from each process.

Forces that constrain the design:

- **CGO-free build** (ADR-0026 invariant). Rules out embedding CH as a linked library.
- **Capslock hygiene** (ADR-0026 §SD10). Apps must not import `os/exec` directly. Whatever spawns the worker process lives in the runtime, hidden behind the bus.
- **NATS forthcoming** (ADR-0026 §SD4, M4). The cap design should not paint itself into a corner that needs reworking when the in-proc bus is replaced by NATS. NATS message size caps (1 MB default, ~64 MB practical) are a real constraint for any payload-over-the-wire design.
- **ImZero2 frame discipline.** The host owns frame cadence; cap calls must not block the render goroutine for tens of milliseconds. Either they complete inside one frame, or the result is delivered asynchronously to a future frame.

The memory note `reference_clickhouse_local` records that `/usr/bin/clickhouse-local` is installed and is the preferred offline-SQL path; this ADR formalises a runtime-mediated, low-latency use of that binary.

## Design space (QOC)

**Question.** How should the runtime expose `clickhouse-local` as a capability so that (a) small SQL roundtrips cost a small number of milliseconds, (b) every ClickHouse `FORMAT` is supported, (c) worker processes do not pin on slow callers or leak on cancelled requests, and (d) the cap/audit/capslock machinery from ADR-0026 carries through unchanged?

**Options.**

- **O1 — Status quo: per-query subprocess.** Each query forks a fresh `clickhouse-local`. ~50–60 ms cold spawn. The current regex_explorer pattern. Audit/capslock handled by hand at each call site.
- **O2 — Pool of reused workers, server mode over UDS.** `clickhouse-local` invoked with `--http-port` (and `<listen_host>unix:…</listen_host>`); pool of long-lived processes; each query is a `POST /?default_format=…` over an HTTP/UDS connection from `chclient`. Reuse amortises spawn cost across many queries.
- **O3 — Pool of one-shot pre-spawned workers, stdin/stdout (chosen).** Pre-spawn N `clickhouse-local` processes blocked on stdin. On each request, write `<SQL> FORMAT <fmt>;` to a worker's stdin, close stdin, drain the result from stdout into an in-memory buffer, let the worker exit. No reuse; pool refills asynchronously to keep MinIdle warm.
- **O4 — Embed CH via cgo.** Link the CH executor into the monolith. Eliminates spawn cost entirely but violates the CGO-free invariant and pulls a multi-million-line C++ dependency into the build.
- **O5 — Long-lived clickhouse-server bound to a UNIX socket.** Run a real `clickhouse-server` instance for local scratch. Heaviest option; pulls in persistence, configuration, and security surface for a workload that needs none of it.

**Criteria.**

- **C1 — Latency floor at low load (warm pool hit).** Time from request submission to first byte readable when a warm worker is available.
- **C2 — Format flexibility.** Can the caller request Pretty / JSONEachRow / CSV / Arrow / Markdown / Vertical interchangeably without transport-layer changes?
- **C3 — Cap/audit/capslock integration cleanliness.** Does this option compose with ADR-0026's subject filter / audit row / runtime-only-EXEC discipline without bespoke escape hatches?
- **C4 — Worker lifecycle bound.** Can a slow caller pin a worker process / FDs / tmpdirs? Can a forgotten Close() leak the process?
- **C5 — Implementation surface.** Lines of code, third-party dependencies, packages touched.
- **C6 — NATS forward-compatibility.** Does the design force re-architecture when the in-proc bus is replaced by NATS (M4), or is it a transport swap with no payload-shape change?
- **C7 — Worst-case memory.** A query returning hundreds of MB of Pretty text — is the design bounded, or can one rogue request pin huge buffers indefinitely?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O5 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | +  |
| C2 | ++ | +  | ++ | +  |
| C3 | −  | +  | ++ | +  |
| C4 | ++ | −− | ++ | −  |
| C5 | ++ | −  | +  | −− |
| C6 | +  | +  | +  | −  |
| C7 | +  | −  | +  | +  |

O3 dominates on C1 without losing on C2–C4. O2's apparent benefit (worker reuse) carries a cost ledger that defeats the premise: a reused worker exposes the engine=Memory leakage problem (state persists across consumers in a shared pool), needs health-check machinery (`SELECT 1` pings, dead-worker reaping), needs format-framing logic (how does the broker know one query's output has ended and the next can be sent?), and needs cancellation semantics (mid-query SIGKILL invalidates the worker, defeating reuse). One-shot workers (O3) avoid all of these by construction: stdin EOF → CH executes → stdout EOF → process exits. The framing problem becomes trivial; the engine=Memory cross-talk is impossible; the health check reduces to "did the spawn succeed."

The remaining trade in O3 is the cost of the spawn that O2 amortises. The premise of this ADR — *low latency, not high throughput* — makes that trade favourable: MinIdle warm spares cover interactive bursts, and an excess burst that drains the pool degrades to O1's cold-spawn latency (~60 ms), which is the floor today anyway.

O1 is rejected on C1 (the entire reason for the ADR) and C3 (each call site reimplements audit/capslock discipline). O4 is rejected on the CGO-free invariant. O5 is rejected on C5/C7 — running a real `clickhouse-server` for local scratch imports a service-management problem that the design does not need.

## Decision

We will introduce the **`ch.local.*`** subject family as a sibling of ADR-0026 §SD3's `ch.query.*`. The runtime hosts a pool of pre-spawned `clickhouse-local` worker processes blocked on stdin; on each request to `ch.local.exec.<pool>`, the broker hands a worker its SQL, drains stdout into a tier-pooled in-memory buffer, and returns the result to the caller as an `io.ReadCloser` plus a `ContentType`. Workers are single-use; the pool refills asynchronously to maintain MinIdle warm spares. The cap subject filter, audit row, and capslock cross-check inherit unchanged from ADR-0026.

Two opt-in flags on the request widen the design without complicating the default:

1. **`Streaming bool`** — bypass the in-memory buffer; reader is the worker's stdout pipe directly. Caller takes ownership of close-discipline. For unbounded results and progressive consumption.
2. **`Cacheable bool`** — broker maintains a per-pool LRU result cache; a hit returns the cached bytes without touching a worker. For deterministic queries against stable local data sources.

## Subsidiary design decisions

### SD1 — Subject family `ch.local.exec.<pool>`

The cap surface is one subject pattern per pool name:

```
ch.local.exec.<pool>            request/reply: SQL → result bytes
ch.local.admin.<pool>           future: warmup / flush-cache / inspect
```

`<pool>` is an opaque app-declared identifier — typically `scratchpad`, `play`, or an app-specific name. Apps name the pool in their `Manifest.Caps`:

```go
Caps: []app.SubjectFilter{
    {Pattern: "ch.local.exec.scratchpad",
     Reason:  "interactive SQL evaluation",
     Direction: app.CapDirectionPub,
     Sticky: true},
}
```

Two apps that name the same pool share the same warm-worker pool and the same result cache. Apps with distinct names get isolated pools. Pool identity is the only knob the caller has for sharing/isolation; the runtime does not introduce per-call pool selection.

The `exec` verb (not just `ch.local.<pool>`) is deliberate: it leaves room for `ch.local.admin.<pool>` to land later for cache flush / pool warmup / per-pool inspection without re-shaping the subject taxonomy. Symmetric in spirit with ADR-0026 §SD3 where the verb distinguishes `query` / `stream` / `schema`.

**No `ch.local.stream.<pool>` subject.** Streaming is a request flag (`Streaming bool`), not a subject. Justification: there is only one *request shape* (send SQL, receive result); the streaming-vs-buffered distinction is purely how the *response* is delivered. A separate subject would duplicate the cap-grant surface (apps would need both `ch.local.exec.<pool>` and `ch.local.stream.<pool>` in their manifest) for the same underlying permission.

### SD2 — Worker invocation: `clickhouse-local`, stdin/stdout pipes

Each worker is spawned as:

```
clickhouse-local --path <tmpdir> [--max_memory_usage <cap>] [--logger.console 0]
```

with `cmd.Stdin` set to a pipe and `cmd.Stdout`/`cmd.Stderr` set to pipes. No `--query`, no `--queries-file`. The process loads its executor and blocks reading stdin.

On request, the broker writes the SQL — postfixed with `FORMAT <fmt>;` and a trailing newline — to the worker's stdin, then closes stdin. The worker runs the statement, emits the result to stdout, then exits naturally on stdin EOF. The broker reads stdout to completion, calls `cmd.Wait()`, and stamps `{exitErr, stderrTail}` onto the reply.

Format is in the SQL, not in the transport. Pretty / JSONEachRow / CSV / Arrow / Markdown / Vertical / TSVWithNamesAndTypes / Parquet — anything CH supports, including format-specific options via `SETTINGS output_format_… = …` clauses, works without broker code changes. The broker is byte-blind; it echoes a `ContentType` it derives from the requested format ("text/plain; charset=utf-8" for Pretty, "application/json" for JSONEachRow, "application/vnd.apache.arrow.stream" for Arrow, etc.) for caller convenience.

`INTO OUTFILE '/tmp/…'` is accepted from the caller as a way to ask CH to write directly to a path (useful for very large data conversions); when present, stdout is mostly empty and the reply carries the output path. The runtime does not synthesise `INTO OUTFILE`; it is opt-in by the caller only.

`--max_memory_usage` is set per worker from `Config.MaxMemoryPerWorker` (default 1 GiB) as a guard against runaway queries; the caller can request a tighter override via the request's `Settings` map but cannot raise it above the runtime cap.

### SD3 — Pool design: one-shot, refill-to-MinIdle

The pool (`runtime/chlocalpool/`) maintains a free list of warm workers. Configuration:

```go
type Config struct {
    BinaryPath          string // default: "/usr/bin/clickhouse-local"
    BaseTmpDir          string // per-worker --path roots under here
    MinIdle             uint8  // pre-spawned spares; default 2
    MaxConcurrent       uint8  // hard ceiling on simultaneous live workers; default 8
    SpawnConcurrency    uint8  // bound on parallel refill spawns; default 2
    MaxMemoryPerWorker  uint64 // --max_memory_usage; default 1 GiB
    SpawnTimeout        time.Duration // bail if spawn doesn't reach "ready" by this; default 2s
    WatchdogMaxLifetime time.Duration // SIGKILL workers older than this; default 60s
}
```

`Acquire(ctx)` pops the next free worker. If none, the call blocks (respecting `ctx.Done`) until either a worker becomes free or `MaxConcurrent` allows an on-demand spawn — the on-demand spawn path pays cold-spawn latency but does not block above the ceiling.

There is no `Release`. After the broker hands a worker its SQL, that worker is *committed*: it is no longer in the pool, and after it exits the pool's bookkeeping decrements its live count without recycling the handle. A background refill goroutine watches the idle-pool depth and spawns replacements (subject to `SpawnConcurrency`) whenever depth drops below MinIdle.

**Spawn readiness.** Whether `clickhouse-local` with no `--query`/`--queries-file` actually preloads its executor before reading stdin is a load-bearing assumption (see §Status / open questions). If it preloads, MinIdle workers are *truly* warm and the cap saves the full ~60 ms cold-spawn cost per query. If it lazy-loads on first SQL byte, the warm pool buys nothing and O3's premise fails — that outcome forces a redesign (probably back toward O2 with reuse, or toward an O5 minimal server). M0 spike (§SD9) resolves this before any production code lands.

**Watchdog.** A goroutine sweeps live workers and SIGKILLs any whose age exceeds `WatchdogMaxLifetime`. This is belt-and-suspenders for the case where a caller forgets to `Close()` the reply (the buffer path frees the worker eagerly on stdout EOF, so this only matters for the streaming opt-in).

### SD4 — Result delivery: tier-pooled buffers via `valyala/bytebufferpool`

In the default (non-streaming) path, the broker runs a drain goroutine that copies the worker's stdout into a buffer acquired from a tiered `sync.Pool`. The worker exits as soon as it has written its output (milliseconds after the SQL completes), independent of how slowly the caller reads. This collapses two problems:

- Worker process / FDs / tmpdir do not stay alive while the caller reads; only the buffer is held.
- A forgotten `Close()` leaks at most a buffer (recyclable, bounded), not a process.

The tier discipline matters: a single `sync.Pool[*bytes.Buffer]` is the well-known footgun where a 50-MB query inflates a buffer and the pool then hands that buffer to a 4-KB query, pinning 50 MB indefinitely. We use [`github.com/valyala/bytebufferpool`](https://github.com/valyala/bytebufferpool) for its statistical bucket sizing, which adapts tier capacities to observed workload distribution and drops outliers on `Put` so a single large response does not poison the pool. The library is battle-tested (fasthttp dependency), zero-API-surface (`Get()` / `Put()`), and adds no transitive dependencies.

The reply shape:

```go
type ExecReply struct {
    io.ReadCloser           // Close() returns buffer to pool, fires audit
    ContentType string      // derived from requested format
    QueryId     string      // for audit join
    StartedAt   time.Time
    CacheHit    bool        // true iff served from §SD5 cache
    err         error       // exposed via Err() after EOF
}

func (r *ExecReply) Err() error // returns nil on clean exit, otherwise {exit_code, stderr_tail}
```

`Err()` is the trailing-error escape: stderr/exit-status only land after `cmd.Wait()` returns, which the drain goroutine handles before the buffer is handed to the caller. After the caller reads EOF on the embedded reader, `Err()` returns the worker's wait result. Idiom:

```go
defer res.Close()
io.Copy(dest, res)
if err := res.Err(); err != nil { ... }
```

**Per-request memory ceiling.** The drain goroutine watches the running buffer size; if it exceeds `Config.MaxInMemoryBytes` (default 64 MiB) the drain fails with a structured error suggesting `Streaming: true`. This is the explicit "you asked for an in-memory result that is too big" signal rather than implicit OOM.

### SD5 — Memoization: opt-in per-pool LRU

A result cache eliminates the worker entirely on a hit. Local-CH workloads are unusually well-suited to caching because most local data sources (`file()`, `url()`, `s3()`, scratch `engine=Memory` tables) are stable for the runtime's lifetime; server-side CH caching is hard precisely because tables mutate, but the local-scratch use case mostly dodges that.

Design:

- **Scope:** per-pool. The pool name is already the cap-grant unit, so cache scope inherits the same trust boundary. No cross-pool sharing.
- **Cache key:** `blake3(SQL || 0x00 || Format || 0x00 || canonicalised Settings)`. No SQL normalisation; whitespace/case variations are cache misses, which is acceptable.
- **Eligibility:** opt-in via the request's `Cacheable bool` field, default false. Independently, the broker refuses to cache anything whose SQL prefix is not in `{SELECT, SHOW, DESCRIBE, EXPLAIN, WITH}` regardless of the flag — cheap mutation guard.
- **Storage:** `hashicorp/golang-lru/v2`, keyed by digest, value `{ bytes []byte, contentType string, storedAt time.Time }`. Bytes are *cloned* out of the pool buffer (`bytes.Clone`) before the buffer is returned to the pool; cache entries must outlive pool buffers.
- **Eviction:** LRU + byte-budget bound (default 64 MiB per pool, configurable). Entries also TTL out (default 60s) — coarse staleness bound for sources we cannot track.
- **Hit path:** broker constructs `io.NopCloser(bytes.NewReader(entry.bytes))` and returns immediately with `CacheHit=true`. No worker touched. Latency: a hash lookup plus a reader wrap, sub-microsecond.
- **Caller responsibility for non-determinism.** The broker does not detect `now()`, `rand()`, network funcs, mutable dictionaries, etc. A caller that sets `Cacheable: true` on a non-deterministic query gets stale results; the flag is opt-in precisely because the caller knows whether its query is deterministic and we do not.

Invalidation hooks are deferred; v1 ships with TTL-only. A future `ch.local.admin.<pool>` subject can carry an explicit flush, and an `InvalidateOnInsert` hint per request can flush when the same caller publishes a write — neither is in v1.

### SD6 — Streaming opt-in (`Streaming bool`)

Setting `Streaming: true` bypasses §SD4's buffer pool entirely. The reply's embedded `io.ReadCloser` is the worker's stdout pipe directly; the worker stays alive until the caller drains the pipe (or `Close()`s, which propagates SIGTERM/SIGKILL). The caller pays for lifecycle discipline:

- `defer res.Close()` is mandatory; without it the worker leaks until the watchdog (§SD3 `WatchdogMaxLifetime`) reaps it.
- `Err()` blocks on `cmd.Wait` and is only meaningful after the caller reads EOF.
- Cancellation of the originating `ctx` triggers `Close()` automatically (the broker registers a goroutine on the request's `ctx.Done`), so callers do not need to thread cancellation by hand.

Streaming is the right path for: data-conversion outputs in the multi-MB-to-multi-GB range, progressive UIs that paint rows as they arrive, and any caller that wants to compose the result into an `io.Pipe` chain without materialising in memory. The buffered default is the right path for everything else; the size ceiling in §SD4 prevents the accidental "I forgot Streaming on a big query" case from silently using gigabytes of RAM.

### SD7 — Audit and capslock posture

**Audit.** Every request — buffered, streaming, or cache-hit — produces one row in `runtime.facts`. The broker emits one structured `zerolog.Event` per request (`Info` for success, `Warn` for failure); the log bridge ([ADR-0026 §SD6](./0026-app-runtime-and-capability-subjects.md) logs-as-facts) routes it into `runtime.facts` under `MembKindLog`, with each zerolog field landing as a typed `runtime.log.field` mixed-membership. The columnar query surface is preserved over the broker's audit corpus without extending `FactsStoreI` for one consumer — `fsbroker` and `persist` follow the same logs-as-facts pattern. If a richer broker-audit kind emerges later as a cross-broker need, it can be promoted from the log envelope to a first-class membership without changing the field set.

The event fields:

- `subject = "ch.local.exec.<pool>"`
- `sender` — bus client identity
- `sql_blake3` — fingerprint of the SQL (full text is too large for the audit table; the hash lets a debugger correlate without holding the query string)
- `format`, `cacheable`, `streaming`, `cache_hit` — request flags
- `latency_ns`, `bytes_out`, `exit_code`, `error`, `stderr_tail` — outcome

The cache-hit row is distinguished by `cache_hit=true` and a `latency_ns` typically under a microsecond; analytics that separate "true CH load" from "served from cache" filter on that flag.

**Capslock.** The `keelson/data/chlocalpool/` package trips `CAPABILITY_EXEC` (`os/exec`) and `CAPABILITY_FILES` (tmpdir creation, `/tmp/p2i-chl-*`). Per ADR-0026 §SD10 these are *hard fail* for app packages — but the package is runtime-internal, not in any `apps/` tree. Like `inprocbus`, `fsbroker`, and the FFFI2 bridge, it sits on the privileged side of the cap boundary.

Enforcement: the capslock-check library at [`public/keelson/security/capslock/`](../../public/keelson/security/capslock) (thin binary shim at `public/app/commands/capslock/`) holds a `trustBoundaryPackages` allowlist plus a `pathTraversesTrustBoundary` filter in `aggregateByPackage`. Capslock-reported capabilities whose call path traverses one of `keelson/data/chlocalbroker`, `keelson/data/chlocalpool`, `keelson/runtime/fsbroker`, `keelson/runtime/inprocbus`, or `keelson/runtime/persist` are absorbed by the broker — the importing app sees only the bus subject. Apps that reach `os/exec` (or any disallowed capability) **without** going through one of these packages are still flagged.

Apps remain clean: they `Publish` on `ch.local.exec.<pool>`, the bus client returns an `*ExecReply`, no app package imports `os/exec`. The capslock signal that would catch a regression — an app reaching `os/exec` directly — stays sharp.

### SD8 — Cancellation, error surface, and ctx discipline

**Ctx propagation via wire-encoded deadline.** The in-proc bus's `MsgHandlerFunc` signature is bytes-only (no ctx), so callers' deadlines travel on the payload: `wireRequest.DeadlineUnixNanos` carries the deadline if the caller's ctx has one. `ExecOnPool(ctx context.Context, bus, poolName, req)` encodes it; the broker derives its execution `context.WithDeadline` from `min(callerDeadline, brokerDefault)`. A caller's tight deadline propagates to `pool.Acquire`, to `cmd.Wait`, and ultimately to SIGTERM/SIGKILL on `Worker.Close`. A future `inprocbus` / NATS extension that adds per-call ctx in the handler signature would supersede this wire encoding.

- **Acquire cancellation.** `pool.Acquire(ctx)` respects `ctx.Done`; a cancelled wait returns `ctx.Err()` and does not consume a worker slot.
- **Mid-query cancellation.** A `ctx.Done` after the broker has handed a worker its SQL triggers SIGTERM to the worker; if it does not exit within a short grace (250 ms), SIGKILL. The reply's `Err()` returns `{exit_code, "killed by cancellation"}`. The pool's live count decrements normally.
- **Worker exit non-zero.** Stderr is captured (last 4 KiB) and surfaced via `Err()`. Common shapes: SQL parse error (CH writes a structured message to stderr), `--max_memory_usage` exceeded, schema not found. The broker writes the audit row with `exit_code != 0` regardless.
- **Spawn failure.** `Config.SpawnTimeout` bounds the refill spawn; on timeout the spawn is abandoned and logged at warn. If MinIdle stays under target across several spawn attempts, the pool emits a structured log + audit row tagged `runtime.kind.chlocalSpawnFailure` so operators can detect a configuration issue (binary missing, tmpdir not writable, capability denied by host).

### SD9 — Phasing and the M0 spike

**M0 — Spike (prerequisite; no production code).** Verify that `clickhouse-local --path <tmpdir>` invoked with stdin held open preloads its executor before reading the first byte from stdin. Method: `strace -e trace=read,write -p <pid>` on a freshly spawned worker, looking for syscalls indicative of completed initialisation before the first stdin read. Cross-check by measuring time-to-first-result for (a) a freshly spawned worker that has been sitting for 1s, (b) a freshly spawned worker that receives SQL immediately on stdin. If (a) is materially faster than (b) by close to the ~60 ms cold-spawn cost, the warm pool is real. If they are within a few ms, the design returns to the drawing board.

**Decision gate.** M0 either confirms O3 is viable or invalidates this ADR; the ADR is not implementable past M0 without that confirmation.

**M1 — `chlocalpool` standalone.** Implement `runtime/chlocalpool/` with full test coverage (spawn, refill-to-MinIdle, cancellation, watchdog reap, memory ceiling, stderr capture). No bus integration. The package is consumable directly by a Go caller in the same process for integration testing.

**M2 — `chlocalbroker` and first consumer.** Implement `runtime/chlocalbroker/` as a subject handler bound to `ch.local.exec.>` on the in-proc bus (ADR-0026 §SD5). Wire into the runtime boot site alongside `fsbroker` (ADR-0026 M2 Phase B precedent). First consumer: migrate `regex_explorer_chlocal.go` from its per-query subprocess to the cap subject. Includes the buffered (default) and streaming opt-in paths. No cache yet.

**M3 — Result cache.** Add the per-pool LRU per §SD5. Telemetry: cache-hit rate per pool exported via `runtime.kind.chlocalCacheTick` audit rows once per N seconds for monitoring.

**M4 — Admin subjects.** `ch.local.admin.<pool>` for `warmup` (force MinIdle), `flush` (drop cache), `inspect` (return pool stats as JSON). Optional Play app diagnostic panel.

**Forward-compat with NATS (post-ADR-0026 M4).** The cap surface (subject name, request schema) is wire-compatible with NATS. The internal `io.ReadCloser`-typed reply is *not* — readers do not serialise. When NATS lands, two options apply: (a) keep this broker in-proc (each app process hosts its own pool; subjects are still cap-grant gated but route locally), or (b) the broker moves to the runtime supervisor and the reply switches to a chunked-bytes-over-stream-subject or file-spill+`fs.handle.*` shape for cross-process delivery. The choice is deferred; both preserve the cap surface and the audit row.

### SD10 — Package layout and dependencies

```
public/keelson/data/
├── chlocalpool/                 # M1
│   ├── pool.go                  # Pool type, Acquire, refill, watchdog
│   ├── worker.go                # Worker spawn, stdin write, stdout drain, kill
│   ├── config.go                # Config + defaults
│   └── pool_test.go             # spawn, refill, cancel, kill, OOM, stderr capture
└── chlocalbroker/               # M2 + M3
    ├── service.go               # bus subject handler; structured zerolog audit (see SD7)
    ├── client.go                # ExecOnPool(ctx, bus, poolName, req)
    ├── payload.go               # ExecRequest, ExecReply, wireRequest (DeadlineUnixNanos)
    ├── cache.go                 # §SD5 per-pool LRU (M3)
    ├── service_test.go
    └── cache_test.go
```

**New dependencies.**

- `github.com/valyala/bytebufferpool` — tier-pooled buffer for stdout drain (§SD4). Pure Go, zero transitive deps, MIT.
- `github.com/hashicorp/golang-lru/v2` — result cache (§SD5, M3). Already in the dep tree if any existing package uses it; verify at M3 landing.

No CGO. No exec into anything other than the configured `BinaryPath`. No network listeners.

## Alternatives

- **O1 — Status quo per-query subprocess.** Rejected on C1: ~60 ms cold spawn is the dominant cost in the interactive path. Per-call-site capability discipline (re-implementing audit and capslock guard at each shell-out) also fails C3.
- **O2 — Server-mode reuse over HTTP/UDS.** Rejected on C4: reused workers expose engine=Memory leakage between consumers, need health-check machinery, need format-framing logic, and complicate cancellation. The one criterion where O2 wins (per-query CPU under sustained load) is irrelevant to the stated workload — interactive latency, not throughput.
- **O4 — Embed CH via cgo.** Rejected on the CGO-free invariant (ADR-0026 §Context). Even setting that aside, the build-time cost (linking a multi-million-line C++ corpus) is far out of proportion to the benefit.
- **O4b — Embed CH via purego (`libchdb.so` via `dlopen`).** Not considered at original decision time; the §Context language ("Rules out embedding CH as a linked library") conflated CGO with embedding. `chdb-io/chdb-go` ships an in-tree purego variant at [`chdb-go/chdb-purego/`](https://github.com/chdb-io/chdb-go/tree/main/chdb-purego) that loads `libchdb.so` at process start via `purego.Dlopen` + `RegisterLibFunc` — the Go toolchain never sees a C source file, so the CGO-free invariant is preserved. The option is not folded into the §QOC table for `ch.local.*` because the safety properties §SD2 / §SD4 / §SD7 / §SD8 rely on subprocess lifecycle and a capslock surface limited to `os/exec`; a purego pool changes both. Recorded as a candidate for a *sibling* cap family (`ch.embed.exec.*`) — see §Updates 2026-05-22 for the full trade-off table and decision-gate sketch.
- **O5 — Long-lived clickhouse-server.** Rejected on C5/C7: pulls in persistence, authentication, configuration, and process supervision surface for a workload that needs none of it. Worth revisiting only if a future requirement (concurrent multi-app shared state, persistent local materialised views) outgrows local scratch.
- **Stdin/stdout with REPL framing and worker reuse.** Considered as a middle ground between O2 and O3 — reuse workers, terminate each query's output with a unique sentinel SQL row, parse stdout up to that sentinel. Rejected: framing is fragile (a result row literally containing the sentinel string would desync the reader), stderr/stdout interleave, and the format-aware parsing collapses the C2 advantage.
- **Native CH protocol over TCP localhost.** Considered before settling on stdin/stdout. Rejected: native protocol always emits Native blocks, defeating the C2 (format-flexibility) goal. HTTP-over-UDS would have preserved C2 but introduces port/socket discovery and inherits the O2 reuse pathology if combined with pooling.
- **File-spill instead of in-memory buffer (universal).** Considered for the buffered default path. Rejected: adds an unnecessary `read()` round trip and tmpfile cleanup for the 99% case where results fit in memory. The streaming opt-in (§SD6) and the per-request memory ceiling (§SD4) cover the large-result case adequately.
- **Streaming as a separate subject (`ch.local.stream.<pool>`).** Considered for taxonomy symmetry with ADR-0026 §SD3's `ch.query.{db}` / `ch.stream.{db}` split. Rejected: that ADR's split distinguishes two materially different request semantics (single result set vs. row iterator); ours is a single request shape with two response delivery modes. A flag is the honest representation.

## Consequences

### Positive

- **Interactive SQL latency drops from ~60 ms to single-digit milliseconds.** The dominant cost in panels that emit a query per keystroke or per repaint becomes the SQL itself rather than the subprocess fork.
- **Format flexibility is intrinsic.** Pretty, JSONEachRow, CSV, Arrow, Markdown — any CH `FORMAT` works without broker changes. Data-conversion workloads (parquet→JSONEachRow, system table→Markdown) become a one-cap-grant away rather than a per-app shell-out.
- **Worker lifecycle is bounded by construction.** One-shot workers cannot pin on slow callers because the drain goroutine frees the worker as soon as stdout is consumed; cancellation propagates to SIGTERM/SIGKILL; the watchdog reaps the streaming-mode escape hatch.
- **Cap/audit/capslock discipline carries through unchanged from ADR-0026.** Apps stay clean of `os/exec`; every call produces a `runtime.facts` audit row; the manifest cross-check pins the surface.
- **Result memoization is opt-in and per-pool.** Deterministic queries against stable local sources can serve from cache sub-microsecond without the caller giving up the option to bypass.

### Negative

- **Two CH-shaped cap families coexist.** Both `ch.query.*` (ADR-0026 §SD3, server) and `ch.local.*` (this ADR) reach ClickHouse, with different trust/latency/format characteristics. Apps and operators must learn the distinction. Mitigation: subject names are self-documenting (`query` = server, `local` = subprocess), and the manifest's `Reason` field clarifies intent at grant time.
- **The warm-pool premise hinges on M0.** If `clickhouse-local` lazy-loads its executor on the first stdin byte rather than preloading at spawn, the warm-pool benefit collapses and the ADR returns to the drawing board. M0 is cheap (a 30-minute spike) but the risk is real.
- **Worker count under sustained load.** A bursty workload that exhausts MinIdle pays cold-spawn latency on every spillover request. Mitigation: tune MinIdle per workload; `MaxConcurrent` ceiling prevents process explosion. No throughput-optimisation work is in scope — operators wanting high QPS should be using `ch.query.*` against a server.
- **Caller bears non-determinism responsibility for the cache.** A caller that sets `Cacheable: true` on a query with `now()` / `rand()` / network funcs gets stale results. Mitigation: the flag is opt-in default-off; documentation in the package doc; operational guidance ("only set Cacheable on queries you know are deterministic for the cache TTL window").
- **Streaming mode is sharper-edged than buffered.** `defer res.Close()` discipline is on the caller; a forgotten Close leaks a worker process until the watchdog reaps it. Mitigation: watchdog default 60 s; lint guard could be added if abuse is observed.
- **One more third-party dependency (`bytebufferpool`).** Small, well-maintained, MIT, zero transitive deps — but a new go.mod line.

### Neutral

- **`ch.local.*` subject family becomes a public-stability surface.** Renaming requires a deprecation cycle. Pool names are also part of the surface for apps that name them in manifests; renaming a pool is an app-level migration.
- **The runtime grows another subprocess-supervising package.** First was the FFFI2 Rust client launcher; second is this. The ADR's package layout (§SD10) and the runtime EXEC exemption (§SD7) factor that out cleanly.
- **`runtime.kind.chlocalSpawnFailure` and `runtime.kind.chlocalCacheTick` enter the membership vocabulary.** Added to `factsmemberships.go` alongside ADR-0026's existing kinds. Once published, renaming requires a vocabulary-migration pass per ADR-0026 §Consequences.

## Status

Proposed — awaiting review by @spx. Implementation is gated on M0 (§SD9): `clickhouse-local`'s spawn-vs-first-byte preload semantics must be confirmed before any production code lands.

Open questions:

1. **M0 — preload semantics.** Does `clickhouse-local --path <tmpdir>` (no `--query`, no `--queries-file`) preload its executor before reading the first byte from stdin? The viability of the entire warm-pool design depends on the answer.

   > *Resolved by [Amendment 2026-05-14](#2026-05-14--m0-spike-preload-semantics-confirmed):* preload at spawn confirmed (B(p50)=7.8 ms vs A(p50)=41.3 ms on CH 26.3.9.8); warm-pool premise holds, M1 unblocked.
2. **Cache invalidation hooks.** Should the v1 cache support `InvalidateOnInsert` at all, or is TTL-only acceptable? Defer to operational experience; reopenable post-M3.
3. **Pool naming convention.** Should pools be free-form strings (current draft) or drawn from a controlled vocabulary registered in `app/factsmemberships.go`? Free-form is simpler; controlled would prevent typos splitting pools. Defer.
4. **NATS path for streaming.** When ADR-0026 M4 lands, do we keep the broker in-proc (per-app pool) or move it to a supervisor (cross-process subject, requires file-spill or chunked-bytes for the response)? Defer to the M4 design pass.
5. **Streaming opt-in path** (recorded in the M2 landing amendment). The bus is bytes-only, so streaming over the cap subject requires either chunked-reply-on-stream-inbox or a file-spill side channel. The M2 broker rejects `Streaming: true` with a structured error. Reopen when a consumer needs progressive consumption.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## Updates

### 2026-05-14 — M0 spike: preload semantics confirmed

Closes open question Q1 from §Status. The spike harness at `experiments/chlocal-preload/` measures three timing distributions for `clickhouse-local` invocations and concludes that the executor preloads at spawn, validating the warm-pool premise of §SD3.

**Method.** Three experiments, `n=25` measurements each, with 3 filesystem-cache warmup iterations discarded:

- **A. Cold spawn via `--query`.** `clickhouse-local --path <tmpdir> --query "SELECT 1 FORMAT TabSeparated"`. End-to-end; matches the current `regex_explorer_chlocal.go` pattern.
- **B. Warm worker via stdin, 1s dwell.** Spawn `clickhouse-local --path <tmpdir>`, sleep 1s, write `SELECT 1 FORMAT TabSeparated;` to stdin, close stdin. Measure only the post-write window — spawn and dwell are excluded.
- **C. Cold worker via stdin, no dwell.** Spawn `clickhouse-local --path <tmpdir>`, immediately write SQL to stdin. End-to-end. Controls for any code-path difference between `--query` and stdin.

Run on Linux 6.19.13-200.fc43.x86_64 against `/usr/bin/clickhouse-local` version 26.3.9.8 (Fedora 43 packaged build).

**Result.**

| Experiment           |    p50  |    p90  |    p95  |    min  |    max  |
|----------------------|--------:|--------:|--------:|--------:|--------:|
| A. cold `--query`    | 41.3 ms | 43.7 ms | 45.2 ms | 37.6 ms | 46.9 ms |
| B. warm 1s + stdin   |**7.8 ms**|  9.2 ms |  9.3 ms |  6.6 ms | 10.4 ms |
| C. cold + stdin      | 41.5 ms | 47.6 ms | 47.9 ms | 38.6 ms | 54.7 ms |

Three signals from the table:

1. **Preload happens at spawn.** B is 5.3× faster than A — ~33.5 ms recovered per query when a warm worker is available. The executor is fully initialised by the time the worker starts reading stdin, exactly as §SD3 requires.
2. **Stdin and `--query` are equivalent code paths.** A ≈ C at the p50 (41.3 ms vs 41.5 ms). Switching the worker protocol from `--query` to stdin/stdout costs nothing relative to the regex_explorer baseline; the format-flexibility argument of §SD2 carries through without a latency penalty.
3. **Trivial-query floor over the cap is ~8 ms.** B's p50 is the realistic minimum latency for `SELECT 1 FORMAT TabSeparated` end-to-end through the new subject — fork-free, parse + execute + exit. Real workloads add the actual query execution time on top.

The companion `strace.sh` was not invoked as part of this spike — the timing gap is wide enough to be definitive on its own. The harness remains available at `experiments/chlocal-preload/strace.sh` for future verification (e.g., if a `clickhouse-local` version bump alters init characteristics).

**What this enables.**

- **M1 is unblocked.** Implementation of `runtime/chlocalpool/` per §SD3 / §SD9 can proceed.
- **The MinIdle default of 2 (§SD3) is appropriate.** A single warm worker delivers the full 5× advantage; two is enough for a typical interactive burst without paying for unused process slots.
- **The spike harness ships as a regression test.** Re-running `go run ./experiments/chlocal-preload` after a CH version bump (or the next Fedora package update) re-validates the warm-pool premise. If a future result lands in the **INCONCLUSIVE** or **LAZY-LOAD SUSPECTED** band, the design assumption is broken and the cap implementation must be revisited.

**What this does not change.** Open questions Q2 (cache invalidation hooks beyond TTL), Q3 (pool-naming convention), and Q4 (NATS path for streaming) in §Status remain open. None gate M1; they are scheduled for M3, deferred, and post-ADR-0026-M4 respectively.

### 2026-05-14 — M2 landing: chlocalbroker + first consumer; Streaming opt-in deferred

Closes most of §SD9's M2: the bus subject handler, the buffered execution path, the lazy per-pool spawn, and the first consumer migration all landed. The Streaming opt-in (§SD6) is **deferred** out of M2 — see "what deferred and why" below.

**What landed.**

- [`runtime/chlocalbroker/`](../../public/keelson/data/chlocalbroker) — `Service` subscribes to `ch.local.exec.>`, lazy-creates a `chlocalpool.Pool` per subject suffix, drains the worker's stdout into a `valyala/bytebufferpool` buffer, copies out (so the pool buffer is safe to return) and publishes a JSON envelope `{Body, ContentType, ElapsedNs}` on the caller's reply inbox. Pre-spawn count, MaxConcurrent, and the rest of `chlocalpool.Config` are taken from the carousel's defaults.
- Client helper `chlocalbroker.ExecOnPool(bus, poolName, ExecRequest) → *ExecReply`. The returned `*ExecReply` embeds `io.ReadCloser` (a `bytes.NewReader` over the reply body for M2) and exposes `Err()` for the worker's exit status / stderr tail; `Close()` is a no-op in M2 (the bytes already live in the caller's address space).
- First consumer migrated: [`apps/regex_explorer`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer). Manifest declares `ch.local.exec.regex_explorer` (Pub, sticky); `AppInstance.Mount` captures `ctx.Bus()`; the new `executeArrowStreamViaBus` replaces `executeArrowStreamLocal` at the five production call sites in `regex_explorer_job.go` and `regex_explorer_tripwire.go`. The original `executeArrowStreamLocal` is retained for unit tests that bypass the bus.
- Carousel boot site (`imzero2_demo_cli.go`) now calls `chlocalbroker.NewService(bus, chlocalpool.Config{}, log.Logger)` next to `fsbroker` and `persist`. Stop is deferred on shutdown alongside the existing service teardowns. The bottom-panel `runtimestatus.Snapshot` gained a `ChLocalActive` field so the status indicator surfaces the new broker.

**Bug caught during M2 implementation.** The broker's bus client must declare `inprocbus.InboxPrefix + ">"` with `CapDirectionPub` in addition to `ch.local.exec.>` — otherwise every reply Publish to a caller's `_INBOX.*` fails the permission gate, the caller waits the full bus timeout, and every Request looks like a hang. `fsbroker` already had this pattern; the M2 broker now mirrors it.

**What deferred and why.** §SD9 scoped M2 to include the Streaming opt-in (§SD6). The in-proc bus's API is bytes-only — `Request(subject, payload []byte) → (reply []byte, error)` — and the eventual NATS replacement (ADR-0026 §SD4, M4) carries the same shape. Streaming a `clickhouse-local` worker's stdout to the caller as bytes-on-the-wire requires either:

- chunked replies on a per-call stream inbox (broker chunks bytes; caller subscribes to the chunk subject; reassembles; signals end with empty chunk or a `Last` flag),
- or a side-channel `io.Reader` / file-spill exchanged through the reply envelope (file path + `fs.handle.*` cap grant).

Neither was scoped alongside the broker / consumer / boot-wiring work in M2. The M2 broker accepts `Streaming: true` on the wire and rejects with a structured error pointing at this deferral; the field doc on `ExecRequest.Streaming` (in `client.go`) repeats the rationale. **The Streaming flag is now an open question Q5 in §Status**, deferred until a consumer actually needs progressive consumption of a large result set; the most likely such consumer is a future data-conversion utility (parquet → JSONEachRow over multi-MB scratch tables).

**What this enables.**

- **M3 (per-pool LRU cache) is unblocked.** The cache slots cleanly into the broker's handler ahead of `pool.Acquire`; the wire envelope already carries `ContentType` and gains a `CacheHit` flag.
- **Regex explorer is now cap-mediated.** Every regex query is auditable (when the audit sink is configured) under `ch.local.exec.regex_explorer`; the app declares no `os/exec` capability of its own and will be clean under the eventual capslock cross-check (§SD7).
- **A second consumer can land trivially.** Adding the cap to a manifest + calling `chlocalbroker.ExecOnPool` is the whole migration; the regex_explorer change is the template.

**What this doesn't change.** Open questions Q2, Q3, Q4 in §Status remain open. M3 is the next milestone.

### 2026-05-14 — M3 landing: per-pool LRU result cache

§SD9 M3 lands as designed in §SD5. The broker now consults a per-pool LRU before `pool.Acquire`; hits bypass the worker entirely and surface a sub-millisecond reply with `CacheHit=true`.

**What landed.**

- `runtime/chlocalbroker/cache.go` — `poolCache` wrapping `hashicorp/golang-lru/v2` with TTL and per-entry size cap. blake3-keyed via `computeCacheKey(sql, format, settings)`; settings keys are sorted so map iteration order does not perturb the digest.
- SQL eligibility gate `sqlIsCacheable` — strips leading whitespace + line / block comments, then checks the first keyword is in `{SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, WITH}` followed by a non-identifier rune. `INSERT` / `CREATE` / `DROP` / `SET` / `SELECTIVELY` are all rejected. Independent of the caller's `Cacheable` flag — the gate is the broker's mutation safety net.
- Wire envelope extended: `wireRequest.Cacheable` and `wireReply.CacheHit`. `ExecRequest.Cacheable` and `ExecReply.CacheHit` mirror them on the client side.
- `Service.SetCacheConfig(CacheConfig)` for runtime override of `MaxEntries` / `MaxEntrySize` / `TTL`; only affects pools whose caches have not been lazy-created yet. Tests dial TTL down for determinism.

**Defaults** (per §SD5):

- `DefaultCacheMaxEntries = 256`
- `DefaultCacheMaxEntrySize = 1 MiB` (results larger than this skip the cache rather than evict everything else)
- `DefaultCacheTTL = 60 s`

**Bytes-in-cache safety.** The body stored in the cache is the same fresh-allocated slice the broker copies out of the `bytebufferpool` drain — exclusively owned by the cache for the duration of its lifetime. The wire envelope's `Body []byte` is the same slice; JSON marshalling reads from it (base64-encodes inline) without retaining a reference.

**What this enables.**

- Interactive panels that re-issue identical SQL across frames (e.g. a regex_explorer query stable across two repaints) now serve from cache. The first call costs the cold-spawn or warm-pool latency; subsequent calls cost a hash lookup.
- Telemetry distinguishes "real CH load" from "served from cache" via the `CacheHit` flag. Cache-hit rate per pool is observable for free in the future audit row.

**What this doesn't change.** Open question Q2 (invalidation hooks beyond TTL) remains open and is deferred until operational experience suggests we need it. The flush primitive on `poolCache` is removed from M3 (was YAGNI) and can be reintroduced when `ch.local.admin.<pool>` lands.

### 2026-05-14 — regex_explorer fully ported off subprocess

The M2 landing amendment said "regex_explorer migrated" but kept `executeArrowStreamLocal` as a test-only fallback. This amendment finishes the port:

- `App.clBinary` field removed; the `REGEX_EXPLORER_CLICKHOUSE_LOCAL_BIN` environment variable is no longer consulted.
- `executeArrowStreamLocal`, `chLocalCloser`, `syncBuffer`, `truncateForErr` deleted — the regex_explorer package no longer imports `os/exec`.
- Integration tests rewritten to stand up an in-proc bus + `chlocalbroker.Service` via the new `setupTestBus` helper. The tests now exercise the production code path end-to-end (`executeArrowStreamViaBus`).

Combined with the capslock trust-boundary work above, the regex_explorer app's package is now clean under the cap discipline: its transitive reach to `os/exec` goes through `chlocalbroker → chlocalpool`, both exempt as runtime trust boundaries.

### 2026-05-15 — keelson namespace path migration (ADR-0035)

Runtime-tree path references in this ADR were swept from `public/thestack/runtime/...` to `public/keelson/runtime/...` as part of the keelson namespace introduction ([ADR-0035](./0035-keelson-namespace-introduction.md)), and then `chclient`, `chlocalbroker`, and `chlocalpool` were further lifted from `keelson/runtime/...` to `keelson/data/...` as siblings of `runtime/` (Step 4 of the migration). The decision recorded here (one-shot pre-spawned `clickhouse-local` workers, stdin/stdout, opt-in stream+cache) is unchanged; only path strings reflect the new location. `keelson/security/capslock/check.go`'s `trustBoundaryPackages` list was updated accordingly (chlocalbroker / chlocalpool entries reference `keelson/data/...`, the rest stay under `keelson/runtime/...`). `status` remains `proposed` — this update does not change it.

### 2026-05-22 — Embedded chDB via purego: option noted, not scoped

Records a design option that was not visible at original decision time. The §Context wording — *"CGO-free build (ADR-0026 invariant). Rules out embedding CH as a linked library."* — conflated CGO with embedding. `chdb-io/chdb-go` ships an in-tree purego variant at [`chdb-go/chdb-purego/`](https://github.com/chdb-io/chdb-go/tree/main/chdb-purego) whose `init()` calls `purego.Dlopen` on `libchdb.so` and binds the chDB C API via `RegisterLibFunc`. The Go toolchain never sees a C source file; the CGO-free invariant is preserved. An embedded chDB executor is therefore back on the table for some workloads — but not as a backend swap behind `ch.local.*`.

**This entry does not change the chosen design.** The `ch.local.exec.*` subject family's safety story rests on properties that only the subprocess model provides: SIGTERM/SIGKILL mid-query cancel (§SD8), `--max_memory_usage` worker-local cap (§SD2), 64 MiB drain ceiling (§SD4), and a capslock surface bounded to `os/exec` (§SD7). A purego pool quietly behind the same subject would silently break the contract callers reason about when granted that cap. The decision stands; this entry records that an embedded *sibling* cap is a candidate for a future ADR, not that O4 has been reopened.

**Trade-off summary (subprocess vs. embedded-via-purego).**

| Property | `ch.local.exec.*` (O3, chosen) | hypothetical `ch.embed.exec.*` (purego, O4b) |
|---|---|---|
| Cold-call floor | 7.8 ms p50 (warm-pool hit, M0 §SD9) | sub-millisecond (direct C call, no IPC) |
| Mid-query cancellation | SIGTERM → 250 ms grace → SIGKILL | cooperative only; no host-enforced kill |
| OOM containment | `--max_memory_usage` worker-local; child dies, host survives | chdb tracker; common failure mode aborts the host |
| In-memory ceiling | §SD4 drain enforces 64 MiB cap | none — chdb owns the allocation |
| Session state across queries | impossible by construction (worker exits on EOF) | natural (`chdb_connect` returns a persistent handle) |
| Streaming response | bytes-on-wire chunked-reply problem (§SD9 Q5, deferred) | native via `chdb_stream_query` / `chdb_stream_fetch_result` |
| Scheduler footprint | none (subprocess on its own kernel thread) | one Go P pinned for the query duration; long queries starve goroutines unless isolated |
| Capslock surface | `os/exec` + tmpdir (`keelson/data/chlocalpool` allowlisted) | `purego.Dlopen` + native-code mmap (new allowlist entry required) |
| Runtime artifact | `/usr/bin/clickhouse-local` (apt) | `libchdb.so` (curl-pipe install or vendored, ~200 MB) |
| Upstream cadence (at recording time) | `clickhouse-local` ships with CH; weekly v4.1.x releases | `chdb-go/chdb-purego/` shares chdb-go's cadence — 2 PRs merged in last 90 days, last release 2025-08-31 |

The pattern: the chosen O3 design wins on isolation, lifecycle bounding, and capslock cleanliness — properties that matter for the interactive-scratch workload §Context targets. A purego sibling would win on three axes ADR-0028 deliberately did *not* optimise for: cold-call latency below the 8 ms floor, persistent in-process session state, and natural streaming. They are different products serving different use cases.

**Sketch of a sibling cap (deferred; no implementation work in flight).** Subject family `ch.embed.exec.<pool>` and `ch.embed.stream.<pool>`; broker `keelson/data/chembedbroker/`; connection pool `keelson/data/chembedpool/` (one persistent `chdb_connection` per pool name, serialised or N-paralleled subject to a GOMAXPROCS budget so a long query cannot consume all P-slots). The `ExecRequest` / `ExecReply` envelopes from `chlocalbroker` are reusable verbatim — the wire shape is backend-agnostic. The §SD7 capslock allowlist would gain `chembedbroker` / `chembedpool` entries; `purego.Dlopen` + native-mmap join `os/exec` as runtime-internal-only capabilities. The streaming subject is genuinely useful here in a way it is not for §SD6 on the subprocess path — `chdb_stream_fetch_result` produces row chunks directly, sidestepping the chunked-bytes-over-stream-inbox and file-spill alternatives that gated Streaming on `ch.local.exec.*` (§SD9 Q5).

**Decision-gate analogue to M0.** ADR-0028 was gated on the M0 preload-semantics spike — does `clickhouse-local` initialise its executor at spawn? A sibling embedded-chDB cap is gated on an **embedded failure-mode spike**: does `libchdb.so` survive nasty inputs (malformed Parquet, oversized strings, deliberate `--max_memory_usage` overrun under purego control, mid-query ctx cancel via the cooperative path, repeated session-crash cycles) without taking the host process down? If common bad inputs SIGABRT the host, the cap is disqualified for any production manifest and exists only behind a developer-mode flag. Scope is similar to M0 — a day, not a sprint.

**Status.** Noted, not scoped. No ADR-0045 (or other subsequent number) drafted; no work scheduled. This entry exists to anchor the option so a future reader landing on a use case that ADR-0028's subprocess model cannot serve — sub-ms inner loops, persistent in-process session state, long-running streaming consumers — knows the option exists and what it costs to pursue. `status` of this ADR remains `proposed`; this update does not change it.

## References

- [ADR-0026 — App runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md) — parent framework; this ADR extends §SD3 (subject taxonomy) and §SD10 (capslock).
- [`public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_chlocal.go`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_chlocal.go) — current per-query subprocess pattern; first consumer to migrate in M2.
- [`public/keelson/runtime/heartbeat/heartbeat.go`](../../public/keelson/runtime/heartbeat/heartbeat.go) — long-lived goroutine + graceful Stop pattern reused by the pool refill / watchdog goroutines.
- [`public/keelson/runtime/inprocbus/`](../../public/keelson/runtime/inprocbus) — in-proc bus; broker subscribes here at M2.
- [`public/keelson/runtime/factsstore/chstore/`](../../public/keelson/runtime/factsstore/chstore) — audit/log write path used by §SD7.
- `public/app/commands/capslock/main.go` and `scripts/ci/capslock.sh` — capslock cross-check; allowlist extended in §SD7.
- [`github.com/valyala/bytebufferpool`](https://github.com/valyala/bytebufferpool) — tier-pooled byte buffer for §SD4.
- [`github.com/hashicorp/golang-lru/v2`](https://github.com/hashicorp/golang-lru) — LRU cache for §SD5.
- [`github.com/ebitengine/purego`](https://github.com/ebitengine/purego) — CGO-free FFI loader; powers `chdb-go/chdb-purego` (see §Updates 2026-05-22).
- [`github.com/chdb-io/chdb-go`](https://github.com/chdb-io/chdb-go) — Go bindings for chDB; ships an in-tree `chdb-purego/` variant referenced as O4b in §Alternatives and analysed in §Updates 2026-05-22.
