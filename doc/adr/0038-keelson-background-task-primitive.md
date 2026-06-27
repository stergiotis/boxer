---
type: adr
status: accepted
date: 2026-05-16
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0038: Background task primitive in keelson/runtime/task

## Context

[ADR-0026](./0026-app-runtime-and-capability-subjects.md) defines the app runtime, the in-proc bus (M1/M2; NATS at M4), and the cap-as-subject taxonomy. [ADR-0036](./0036-runtime-buscodec.md) anchors every payload on a canonical-CBOR wire. Apps render frame-by-frame inside `AppI.Frame(ctx)`; long-running work cannot live there.

Several workloads on the near roadmap need a place to live *outside* the frame loop, with shared semantics:

- ClickHouse exports / bulk fetches via `chlocalbroker` (ADR-0028) — minutes for large result sets.
- Filesystem scans and imports through `fsbroker` — proportional to directory size.
- Kafka catch-up reads (ADR-0005) — bounded by topic lag, not by frame budget.
- Snapshot rendering for the screenshot tour and the imztop sampling loop (ADR-0020).
- Future cross-process work offloaded over NATS once M4 lands.

Every one of these wants the same four things: progress feedback to the UI, cancellation from a button click or window close, an audit trail of who started what, and a forward-compat path to the M4 swap. There is currently no shared primitive. Each candidate consumer is poised to invent its own: a private goroutine, an ad-hoc progress struct guarded by a mutex, a one-off bus message for "I'm 30% done," a private cancel channel. `chlocalpool` (ADR-0028) is a domain-specific worker pool, not a general task surface. `MountContext.Cancel()` is app-level only — it cannot scope to a single in-flight operation. `fsbroker.Service.Cancel(reqId)` is a one-off cancel for file dialogs.

Constraints inherited from the rest of the stack:

- **Bus-first.** ADR-0026 treats subjects as the universal transport. A task primitive that uses anything else (channels, callbacks, shared memory) will not survive the M4 NATS swap unchanged.
- **buscodec is the wire.** All payloads route through `runtime/buscodec` (ADR-0036). No `encoding/json` direct calls.
- **CGO-free** (ADR-0026 invariant).
- **No mandatory IDL step.** Payload types stay as plain Go structs with `cbor:`/`json:` tags.
- **Observer-friendly.** A status panel must watch every in-flight task without owning any of them. This is the harness for the future `runtime.facts` audit and for any UI that wants a "current activity" list.
- **Producer ergonomics.** The worker goroutine is hot-path code (a tight `for` loop over rows, files, frames). Reporting progress must not require the producer to think about bus rate-limiting, throttling, or marshalling.

Dependencies already present in `go.mod` (no new modules required):

- `github.com/matoous/go-nanoid/v2 v2.1.0` — task id generation.
- `github.com/dustin/go-humanize v1.0.1` — byte/duration/throughput humanization for the emission gate.

## Design space (QOC)

**Question.** How should keelson expose long-running, cancellable, observable work to apps so that (a) one contract covers ClickHouse exports, FS scans, Kafka reads, imztop polling, and future cross-process tasks; (b) producers stay simple while bus traffic stays bounded; (c) any consumer can observe and cancel without owning the work; (d) the M4 NATS swap requires no API change?

**Options.**

- **O1 — Status quo.** Each consumer rolls its own goroutine, progress struct, and cancel channel. No central package.
- **O2 — Runtime-managed task pool.** A keelson-side scheduler with worker goroutines; apps submit `TaskSpec` and receive futures. Pool owns execution.
- **O3 — Bus-protocol primitive (chosen).** Apps own the goroutine; keelson owns the subject taxonomy, payload schema, producer handle, observer helpers, and (M3) supervisor. Execution is app-side; coordination is bus-side.
- **O4 — Frame-loop extension via `BackgroundTickHz`.** Treat background work as another frame surface; producers report progress via the existing frame contract.
- **O5 — One pseudo-app per task.** Each task is a short-lived `AppI` instance with its own Mount/Frame/Unmount lifecycle.

**Criteria.**

- **C1 — Forward-compat with M4 NATS.** Will the API survive transparent migration to cross-process workers?
- **C2 — Migration cost from today's ad-hoc patterns.** How much code per consumer to adopt?
- **C3 — Observability.** Can multiple consumers (UI panel, supervisor, debug log) watch the same task without coordination, and can audit be added later without touching producers?
- **C4 — Cancellation propagation.** Can a click in the UI cancel a task started by another app, or a task whose owner-app window has closed?
- **C5 — Producer ergonomics.** Hot-path worker code stays a tight loop with one call per step.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 status quo | O2 runtime pool | O3 bus protocol | O4 frame-loop ext | O5 pseudo-app |
|----|:--:|:--:|:--:|:--:|:--:|
| C1 | −  | −− | ++ | −  | +  |
| C2 | ++ | −  | −  | +  | −− |
| C3 | −− | +  | ++ | −  | +  |
| C4 | −  | +  | ++ | −  | +  |
| C5 | +  | +  | +  | −  | −− |

## Decision

We introduce `public/keelson/runtime/task/` as a bus-protocol primitive for long-running, cancellable, observable work. The package owns the subject taxonomy and wire schema; apps own the goroutines that do the work.

### Subject taxonomy

Flat, per-task: `task.<id>.<verb>` with verbs `created`, `progress`, `cancel`, `done`, `error`. The owner-app id rides in the payload (`OwnerAppId`), not in the subject — tasks may outlive a window session, and a single observer subscription on `task.>` fans out across all producers.

### Identity

`TaskIdT` is a 21-character nanoid using the default URL-safe alphabet (`A–Z a–z 0–9 _ -`), all NATS-token-safe. Ids are *not* time-sortable; the M3 supervisor orders task history by payload `AtMs`. Empty `Id` in `SpawnOpts` triggers auto-generation via `gonanoid.New()`.

### Wire payloads (buscodec-encoded)

```go
type TaskCreated struct {
    Id           TaskIdT
    Kind, Title  string
    OwnerAppId   app.AppIdT
    CancellableB bool
    EstimatedMs  int64
    AtMs         int64
}
type TaskProgress struct {
    Id               TaskIdT
    Current, Total   uint64   // Total=0 ⇒ indeterminate
    Unit             string   // "items" | "bytes" | "steps"
    ThroughputPerSec float64  // sliding-window estimate, 0 until stable
    EtaMs            int64    // -1 if unknown
    Note             string
    AtMs             int64
}
type TaskDone   struct { Id TaskIdT; AtMs int64; Result []byte /* opaque, decoded by Kind */ }
type TaskError  struct { Id TaskIdT; AtMs int64; Reason string; Error []byte /* eh.MarshalError chain */ }
type TaskCancel struct { Id TaskIdT; AtMs int64; Reason string }
```

`TaskError.Error` carries the boxer error-chain shape (`{streams:[{<name>:[facts]}]}`) so that `errorview` renders failed tasks directly.

### Producer surface

```go
type HandleI interface {
    Id() TaskIdT
    Ctx() context.Context        // cancels on bus-cancel, parent-ctx-cancel, or terminal
    Report(p ProgressReport)     // throttled by humanized-change gate
    Note(note string)            // text-only update; same gate
    Done(result []byte) (err error)
    Error(err error, reason string) (rerr error)
    Cancelled() bool
}

func Spawn(parent context.Context, bus app.BusI, opts SpawnOpts) (h HandleI, err error)
```

`Spawn` publishes `task.<id>.created`, subscribes `task.<id>.cancel` (cancels the handle's internal context), and returns the handle. `Done` and `Error` are idempotent and unsubscribe cleanly. The handle is safe for concurrent use — a worker goroutine calling `Report` and a UI goroutine calling `Note` is the canonical pattern.

### Emission discipline

A `task/estimator` subpackage tracks raw `(Current, AtMs)` samples in a sliding window, computes throughput via `go-humanize`'s `BigBytes`/`RelTime` for the visible string, and gates emission on **humanized-change**: `Report` triggers a `task.<id>.progress` publish only when the visible string (`"47% · 2m12s left"` or `"1.2 GB / 3.4 GB · 18 MB/s · 2m left"`) would change since the last emission. Indeterminate-mode tasks emit at most 1 Hz on note change or as a heartbeat. The final pending sample is always flushed before `Done`/`Error` publishes.

Producers call `Report` in their hot loop without rate-limiting concerns; bus traffic auto-tunes to perceived precision (fast tasks emit a few times total; slow tasks tick once per visible second).

### Observer surface

```go
func WatchAll(bus app.BusI, obs ObserverI) (unsubscribe func(), err error)
func RequestCancel(bus app.BusI, id TaskIdT, reason string) (err error)
```

`WatchAll` performs one `task.>` subscription and demuxes by verb suffix. `RequestCancel` is a thin publish helper for UI cancel buttons.

### Capability declarations

```go
func ProducerCaps() []app.SubjectFilter // task.> Both
func ObserverCaps() []app.SubjectFilter // task.> Sub
func CancelerCaps() []app.SubjectFilter // task.*.cancel Pub
```

Apps that publish *and* observe the world (producer + UI panel in one binary) declare both. `capslock` enforces accuracy at build time per ADR-0026 §SD10.

### Milestones

> **Landed as of 2026-06-08:** all milestones — `keelson/runtime/task`, the demo, the supervisor, and the `taskmonitor` widget.

- **M1.** `keelson/runtime/task/` + `task/estimator/` subpackage; producer, observer helper, `RequestCancel`, cap helpers; unit tests over `inprocbus`; REFERENCE.md + EXPLANATION.md.
- **M2.** Demo app (`apps/taskdemo/`) wired through `c.PanelCentral()` with a cancel button.
- **M3.** Opt-in `keelson/runtime/task/supervisor/` — subscribes `task.>`, persists Created/Done/Error/Cancel into `runtime.facts` via `factsstore`, marks tasks abandoned when no emission within N seconds, exposes `task.list.inflight` request/reply.
- **M4.** Reusable `leewaywidgets/taskmonitor` widget — in-flight + recent rows, per-row cancel button, `errorview` for failures.

Implementation order: M1 is a precondition for any consumer; M2-M4 are independent and can land in any order driven by demand.

## Alternatives

- **O1 status quo.** Rejected — five candidate consumers each reinventing the same wheel produces five subtly-different cancel semantics, no central audit, no observer story, and a fleet rewrite the day M4 NATS lands. The cost of leaving this gap grows linearly with the consumer count.
- **O2 runtime-managed task pool.** Rejected — couples to in-proc execution, overlaps with `chlocalpool`'s domain-specific pool without a clear ownership story, and forces an early commitment on scheduling policy (priority? fairness? backpressure?) that no consumer is yet asking for. A pool can be layered *on top* of O3 later as a convenience for in-process workers; the inverse migration is the rewrite this ADR is trying to avoid.
- **O4 frame-loop extension via `BackgroundTickHz`.** Rejected — `BackgroundTickHz` exists for off-frame *frame-shaped* work (think: a heartbeat publisher), not for arbitrary long-running operations whose duration is data-dependent. Pumping a 30-minute ClickHouse export through a 10 Hz frame loop is the wrong granularity in both directions.
- **O5 one pseudo-app per task.** Rejected — `AppI` carries window-host hooks, persisted keys, surface hints, manifest registration. Wrapping a single export in that scaffolding is two orders of magnitude more code than the task itself, and the lifecycle mismatch (registry permanence vs task ephemerality) creates new edges (do tasks show up in app pickers? what happens to in-flight tasks across runtime restarts?). Real apps that *contain* tasks remain the right granularity.

## Consequences

### Positive

- Single subject contract spans ClickHouse exports, FS scans, Kafka catch-up, imztop polling, and any future M4 cross-process worker. The M4 swap requires zero API change.
- Bus traffic auto-tunes via the humanized-change gate — producer code stays a tight loop, observer-perceived precision is consistent across task durations.
- Multiple observers (UI panel, debug log, supervisor) attach to the same task with one `task.>` subscription each, no producer coordination.
- Cancellation is location-agnostic — a cancel button in a status panel, a `Cancel()` from the runtime on app shutdown, and a peer task that wants to abort a sibling all use the same mechanism.
- Error wire shape reuses `eh.MarshalError`, so the existing `errorview` widget renders failed tasks directly with no new wiring.
- M3 supervisor lands as a separate package — apps using the primitive in M1/M2 take no dependency on `factsstore`, keeping the surface unit-testable in isolation.
- Both new deps (`nanoid`, `humanize`) already in `go.mod`; no module churn.

### Negative

- Producers must respect `Ctx().Done()` in their worker loop. A worker that never checks the context will not cancel on bus-cancel — by design (we cannot pre-empt foreign goroutines), but worth flagging. The contract is documented in EXPLANATION.md and exercised by an integration test that fails-fast on uncancellable loops.
- Two new capability patterns enter the cap catalog (`task.>` Both/Sub, `task.*.cancel` Pub). Apps that produce tasks must declare them in `Manifest.Caps`; `capslock` enforces accuracy.
- Task ids are not creation-time sortable — the M3 supervisor and any persisted history must order by payload `AtMs`, not by id. Acceptable trade-off for the 21-char wire compactness.
- The `Result []byte` field is opaque on the wire; consumers must dispatch by `Kind`. We do not introduce a typed-result registry until a real consumer asks for one.
- Cross-process cancellation in M4 will require the cancel subscription to reach the producer node — straightforward with NATS, but a deferred concern, not a M1 problem.

### Neutral

- `Spawn` requires a `context.Context` so handles compose with the standard Go cancellation idiom. Apps without a parent context can pass `context.Background()`; the handle still cancels on bus-cancel and terminal-state.
- Task kinds (`"ch.export"`, `"fs.scan"`, …) are conventional strings, not a registered enum. We do not enforce a registry; consumers that want to display kind-specific UI dispatch on the string. A registry can be added later without wire change.
- Supervisor heartbeat threshold (N seconds with no emission ⇒ abandoned) is an M3 policy knob; default proposed at 30 s, finalised in the M3 PR.
- The estimator subpackage is keelson-private (`keelson/runtime/task/estimator/`); we do not surface it as a reusable humanizer until a second consumer asks. Boxer's `go-humanize` import covers the format primitives; the sliding-window throughput is the only original part.

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). All milestones have shipped — `keelson/runtime/task`, the task demo, the supervisor, and the `taskmonitor` widget.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-29 — First GUI consumer: ECDF confidence-band warm-up (cached, deduplicated compute job)

The imzero2 ECDF / `distsummary` widget became the first in-tree consumer of this primitive, and it is exactly the workload the Context envisioned: the simultaneous confidence band needs an O(n²) Berk-Jones/Moscovich critical-value inversion that runs ≈minutes at n=10⁴. It had been computed synchronously inside `AppI.Frame` (`widgets/ecdf` → `analytics/stats/ecdfbands.BandsForGrid`), so opening a distsummary inspector over a 10⁴-sample digest froze the UI — "long-running work cannot live there," verbatim.

The primitive needed **no protocol change**. The worker uses it as specified: `Spawn` → per-eval `Report(UnitSteps)` → `Done(nil)`. Three design calls in this ADR were validated rather than revisited:

- **`Result` stays opaque and unused here.** The band result travels through a *domain* cache (`ecdfbands` keyed by `{n, α, method}`), not the `Done` payload — so `Done(nil)` is called purely for the side effect (a warmed cache), exactly the case the Producer surface's "pass nil result … the outcome is the side effect itself" anticipates. No typed-result registry was needed (Consequences §Neutral holds).
- **Caching + dedup is a thin app-side layer, not pool machinery.** A non-blocking probe (`ecdfbands.BandReady`) lets the render thread pick draw-vs-schedule each frame; an in-flight registry keyed **per inspector instance** (the widget's per-call scope) makes every frame from one open inspector attach to its single solve. Cross-instance dedup rides on the domain cache itself rather than on this registry — a second inspector on the same `{n, α, method}` finds `BandReady` true, or, if it began warming concurrently, runs a redundant-but-harmless solve the domain cache absorbs on store. Per-instance keying is the deliberate trade that lets a closing inspector cancel exactly its own solve without disturbing another window still waiting on the same parameters. This is precisely the "pool layered on top of O3" that the O2 rejection predicted — coordination stays bus-side, the dedup/caching stays domain-side.
- **The producer honours `Ctx().Done()`** (Consequences §Negative): the inversion checks the context once per eval, so cancellation lands within one eval. Three paths feed it — a bus cancel, the host's app-level mount-cancel, and the per-instance addition `ecdf.CancelBandJob(scope)`, which the `distsummary` inspector fires when its window is closed (title-bar X) or retracted (anchor handle) so a long solve never outlives the window that asked for it. The job owns a cancellable context as the cancel root and the keelson task is `Spawn`ed parented on it, so all three converge — and the widget-driven cancel works even on the in-process-only (nil task API) path. Progress is reported per bisection eval (~62 of equal cost), giving the estimator a near-linear signal and a stable ETA.

Host wiring: imztop takes the API via `task.ForApp(ctx)` at Mount; the demo gallery, whose demos are bus-only (no `MountContextI`), builds one with `task.NewBusApi(ApiConfig{Bus})` from its `BusInit`-captured bus. The widget degrades to an **in-process-only** job (off-thread solve, in-process progress, no supervisor audit) when no task API is supplied, so library and test callers are unaffected. A small stateless `widgets/jobprogress` widget renders the progress + ETA inline below the plot (consuming the in-process snapshot, not a per-widget `task.>` subscription — `taskmonitor` (M4) remains the global, bus-driven view).

A complementary, fidelity-preserving speedup landed alongside: `ecdfbands.logFactorial` is now memoised into a lock-free table, cutting the inversion's dominant `math.Lgamma` cost (~1.3–1.45× at n=512–1024, more at larger n) without changing any band value.

Source: [`../../public/analytics/stats/ecdfbands/`](../../public/analytics/stats/ecdfbands/) (`WarmBand` / `BandReady` / `ProgressFunc`, tabulated `logFactorial`); [`../../public/thestack/imzero2/egui2/widgets/ecdf/bandjob.go`](../../public/thestack/imzero2/egui2/widgets/ecdf/bandjob.go) (per-instance registry + `Spawn` + `cancelBandJob`); [`../../public/thestack/imzero2/egui2/widgets/jobprogress/`](../../public/thestack/imzero2/egui2/widgets/jobprogress/); [`../../public/thestack/imzero2/egui2/widgets/distsummary/distsummary.go`](../../public/thestack/imzero2/egui2/widgets/distsummary/distsummary.go) (render-thread orchestration). `status` and `reviewed-date` are deliberately not re-stamped — the decision is unchanged, only confirmed by a consumer.

### 2026-06-22 — Optional durable execution backend: River (open-core) on embedded SQLite

This ADR deliberately stopped at *coordination*: O2 (runtime-managed pool) was rejected, execution stays app-side, and the M3 supervisor is **audit-only** — a task that stops heartbeating is promoted to `InflightStateAbandoned` and *recorded*, never resumed. O5's rejection named the exact edge left open: "what happens to in-flight tasks across runtime restarts?" Today the answer is — they die, and the supervisor notes the death. For the ephemeral, UI-coupled consumers (imztop sampling, snapshot rendering, the ECDF warm-up whose result is a cache side-effect) that is correct: they are frame-shaped and worthless after a restart. But three of the Context's named workloads are *restart-worthy* — a multi-minute `chlocalbroker` export, an `fsbroker` import proportional to tree size, a Kafka catch-up bounded by lag. Losing one to a window crash or a deploy is a real cost the `abandoned` state only *names*.

[River](https://github.com/riverqueue/river) (open-core, MPL-2.0) on its embedded **SQLite** driver is the "pool layered *on top* of O3" that the O2 rejection — and the 2026-05-29 ECDF update — both predicted. It slots *beneath* `HandleI` as an optional durable executor for the heavy subset. It is **not** a protocol change and does not touch the bus contract: coordination stays bus-side, durable execution moves DB-side.

**What River open-core on SQLite provides** (the half keelson would otherwise have to build and harden itself):

- **Durable jobs that survive restart.** Work is rows in a single SQLite file (`river_job`); a crash mid-export resumes instead of abandoning. At-least-once via `SKIP LOCKED`.
- **Automatic retries with backoff** (`retry_policy`, overridable per worker) and a **rescuer** that requeues jobs whose worker died without reporting — the durability the supervisor's `abandoned` state only *observes*.
- **Durable scheduling** — scheduled-at jobs plus in-memory cron/periodic (single-process keelson needs no more; the DB-persisted *durable periodic* variant is Pro and out of scope).
- **Concurrency + backpressure** per queue (`MaxWorkers`) — one principled mechanism in place of N hand-rolled goroutine pools the Context warned about.
- **A richer state machine + history** (`available/running/retryable/scheduled/completed/cancelled/discarded`, attempt counts, persisted error chain) — a strict superset of `InflightStateE`.
- **CGO-free, server-free.** The `riversqlite` driver ships no SQLite library of its own; paired with pure-Go `modernc.org/sqlite` it honours the ADR-0026 CGO-free invariant. One file, no daemon, embeddable in the same binary.
- **River UI** — an embeddable, CGO-free `http.Handler`, SQLite-capable — as an *ops-side* durable dashboard (an adjunct to, not a replacement for, the in-app `taskmonitor`).

**What keelson must still own itself** (River knows nothing about any of this):

- **The entire bus protocol** — the `task.<id>.{created,progress,cancel,done,error}` taxonomy and the buscodec/CBOR wire (ADR-0036). River persists opaque JSON args; the CBOR payload rides as opaque bytes, consistent with the existing `Result []byte` decode-by-`Kind` rule.
- **Progress + the estimator.** River has no progress concept. The worker still calls keelson `Report()`; `task/estimator` and the humanized-change gate remain the sole authority over what reaches the bus.
- **The live observer/UI surface** — `WatchAll`, `taskmonitor` (M4), `jobprogress`. The in-app "current activity" panel stays keelson's; River UI is at most an external adjunct.
- **Cancellation bridging.** `task.<id>.cancel` → River job cancellation (ctx cause). The producer-honours-`Ctx().Done()` contract (Consequences §Negative) is unchanged and is exactly what a River worker already expects.
- **Capability declarations** (`task.>` Both/Sub, `task.*.cancel` Pub) and `OwnerAppId`/manifest identity — River has no notion of apps or caps.
- **The adapter shim and the routing policy.** A `HandleI` backed by a River `Worker[T]`, plus the per-consumer decision of which `Kind`s are durable (export / import / catch-up / scheduled rollup) vs which stay in-process (everything frame-shaped). That choice is the app-side judgement this ADR has always kept app-side.
- **Idempotent worker bodies** (at-least-once ⇒ a resumed export must tolerate re-run), schema migration (`rivermigrate`) inside the app binary, and the SQLite connection discipline (`SetMaxOpenConns(1)`, `_journal_mode=WAL`, `_busy_timeout`).

**Integration shape.** `Spawn` gains an optional durable path: for a durable `Kind` it enqueues a River job and returns a `HandleI` whose `Report/Note/Done/Error/Cancelled` are wired to the same bus verbs. The River worker runs the body, calls the handle's `Report` (→ `task.<id>.progress`), and `Done`/`Error` close the task exactly as today. To an observer the task is indistinguishable from an in-process one — it simply survives a restart and retries on failure. This keeps the M4 split clean: **River = durable cross-process work distribution; NATS = ephemeral progress/cancel fan-out.**

**Boundaries (open-core + SQLite).** SQLite serialises writers, so the pool is pinned at `SetMaxOpenConns(1)` and throughput is modest — right for minutes-long exports, wrong for a 10 Hz sampler (the granularity rule that sank O4, now applied in the other direction). Listen/notify is emulated (a `river_notification` outbox polled ~50 ms) and inserts/completions run one row at a time, so SQLite River is a single-node *durability* layer, not a throughput play; the M4 server tier swaps the driver to `riverpgxv5` (reusing the `pgx` dependency already in-tree at `apps/godepview`) with no job-code change. The driver is self-described as *early testing*. And we stay strictly **open-core**: workflows, sequences, durable periodic jobs, partitioned concurrency limits, batching, dead-letter queues and encrypted jobs are River **Pro** and explicitly out of scope — a DAG-shaped need is a separate ADR, not a backend toggle.

`status` and `reviewed-date` are deliberately not re-stamped: O3 is unchanged. This records an *optional, additive* durable backend beneath `HandleI`, not a revision of the decision.

Source / further reading: [River](https://github.com/riverqueue/river) (MPL-2.0); `riverdriver/riversqlite` (embedded, CGO-free with `modernc.org/sqlite`); [River UI](https://github.com/riverqueue/riverui) (embeddable `http.Handler`). Pairs with `runtime/task` as the "pool on top of O3" foreshadowed in the O2 rejection.

### 2026-06-27 — Retiring a standalone streaming processor; per-entity streaming belongs in a worker body

`public/analytics/processor` was a generic "entity-lifecycle over discontinuous batches" engine: a source yields batches (the canonical one a paginated ClickHouse `SELECT … ORDER BY entity_id`), the processor partitions rows by entity ID and drains each entity's row stream through a stateful consumer running in a dedicated, per-entity goroutine that is joined before the next entity starts. Extracted from an application's `StatefulProcessor` pattern, it carried its own panic-recovery, a `MetricsCollectorI` (modelled on `caching.MetricsCollectorI`), an opt-in `Prefetcher`, and a `sync.Pool` for the cross-goroutine chunk handoff. It is now deleted — it had no in-tree consumer (the analyzer that drove the extraction is gone). The retirement is noted here because the package sat on this ADR's ground: its goroutine ownership, ctx-honour-and-join cancellation, and local progress hooks are a single-process, bus-unaware, audit-unaware re-implementation of the very "private goroutine + ad-hoc progress struct + private cancel channel" the Context names as the O1 anti-pattern — built for ClickHouse bulk fetch, this ADR's first named workload — while lacking the durability the 2026-06-22 River update routes that restart-worthy workload to. Its copy/pool machinery (and a latent buffer-aliasing data race in `Prefetcher`) existed only to feed the per-entity goroutine, which — because entities run strictly serially — bought only intra-entity pipelining, the least useful of the three overlaps (cross-entity, source-I/O, intra-entity); source-I/O overlap is already the separate `Prefetcher`'s job.

The reusable lesson, if a consumer reappears: the per-entity streaming is the only novel part, and it is a ~25-line synchronous `iter.Pull` group-by over sorted input — no goroutine, no channel, no pool, memory-safe by construction. It belongs as the *body* of a `task` worker (River-durable when the work is restart-worthy, e.g. a multi-minute export), reporting via `Report()`, so this primitive keeps the goroutine/cancel/progress/audit envelope and the worker owns only the row loop. The complementary scatter/gather case (amortise random-access key I/O via caching + suspend/replay) stays `caching`'s, not this body's. The package's design survey — why not RxGo / Proto.Actor / Benthos / Goka — lives in its `EXPLANATION.md` in git history.

`status` and `reviewed-date` are deliberately not re-stamped: this records a retirement consistent with O3, not a revision of the decision.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — app runtime, in-proc bus, cap-as-subject taxonomy. Defines `AppI`, `Manifest.Caps`, `BusI`, `MountContext.Cancel()`.
- [ADR-0036](./0036-runtime-buscodec.md) — canonical CBOR wire via `buscodec`; every task payload routes through `Encode[T]/Decode[T]`.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — `chlocalpool` worker pattern (sibling, domain-specific); a future consumer of the task primitive for export-style queries.
- [ADR-0005](0005-streaming-persisted-kafka-from-connect.md) — Kafka catch-up reads, a candidate consumer.
- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — imztop sampling loop, a candidate consumer.
- `public/keelson/runtime/task/` — package source (to land at M1).
- `github.com/matoous/go-nanoid/v2` — `gonanoid.New()` for task ids.
- `github.com/dustin/go-humanize` — `BigBytes`, `RelTime`, `Comma` for the emission gate's visible-string computation.
