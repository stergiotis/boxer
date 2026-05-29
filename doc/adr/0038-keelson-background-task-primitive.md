---
type: adr
status: proposed
date: 2026-05-16
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

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

We introduce `src/go/public/keelson/runtime/task/` as a bus-protocol primitive for long-running, cancellable, observable work. The package owns the subject taxonomy and wire schema; apps own the goroutines that do the work.

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

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-29 — First GUI consumer: ECDF confidence-band warm-up (cached, deduplicated compute job)

The imzero2 ECDF / `distsummary` widget became the first in-tree consumer of this primitive, and it is exactly the workload the Context envisioned: the simultaneous confidence band needs an O(n²) Berk-Jones/Moscovich critical-value inversion that runs ≈minutes at n=10⁴. It had been computed synchronously inside `AppI.Frame` (`widgets/ecdf` → `analytics/stats/ecdfbands.BandsForGrid`), so opening a distsummary inspector over a 10⁴-sample digest froze the UI — "long-running work cannot live there," verbatim.

The primitive needed **no protocol change**. The worker uses it as specified: `Spawn` → per-eval `Report(UnitSteps)` → `Done(nil)`. Three design calls in this ADR were validated rather than revisited:

- **`Result` stays opaque and unused here.** The band result travels through a *domain* cache (`ecdfbands` keyed by `{n, α, method}`), not the `Done` payload — so `Done(nil)` is called purely for the side effect (a warmed cache), exactly the case the Producer surface's "pass nil result … the outcome is the side effect itself" anticipates. No typed-result registry was needed (Consequences §Neutral holds).
- **Caching + dedup is a thin app-side layer, not pool machinery.** A non-blocking probe (`ecdfbands.BandReady`) lets the render thread pick draw-vs-schedule each frame; an in-flight registry keyed by the same `{n, α, method}` makes every widget and frame at that key attach to one solve. This is precisely the "pool layered on top of O3" that the O2 rejection predicted — coordination stays bus-side, the dedup/caching stays domain-side.
- **The producer honours `Ctx().Done()`** (Consequences §Negative): the inversion checks the context once per eval, so a bus cancel and the host's mount-cancel (window close) both abort a long solve within one eval. Progress is reported per bisection eval (~62 of equal cost), giving the estimator a near-linear signal and a stable ETA.

Host wiring: imztop takes the API via `task.ForApp(ctx)` at Mount; the demo gallery, whose demos are bus-only (no `MountContextI`), builds one with `task.NewBusApi(ApiConfig{Bus})` from its `BusInit`-captured bus. The widget degrades to an **in-process-only** job (off-thread solve, in-process progress, no supervisor audit) when no task API is supplied, so library and test callers are unaffected. A small stateless `widgets/jobprogress` widget renders the progress + ETA inline below the plot (consuming the in-process snapshot, not a per-widget `task.>` subscription — `taskmonitor` (M4) remains the global, bus-driven view).

A complementary, fidelity-preserving speedup landed alongside: `ecdfbands.logFactorial` is now memoised into a lock-free table, cutting the inversion's dominant `math.Lgamma` cost (~1.3–1.45× at n=512–1024, more at larger n) without changing any band value.

Source: [`../../public/analytics/stats/ecdfbands/`](../../public/analytics/stats/ecdfbands/) (`WarmBand` / `BandReady` / `ProgressFunc`, tabulated `logFactorial`); [`../../public/thestack/imzero2/egui2/widgets/ecdf/bandjob.go`](../../public/thestack/imzero2/egui2/widgets/ecdf/bandjob.go) (registry + `Spawn`); [`../../public/thestack/imzero2/egui2/widgets/jobprogress/`](../../public/thestack/imzero2/egui2/widgets/jobprogress/); [`../../public/thestack/imzero2/egui2/widgets/distsummary/distsummary.go`](../../public/thestack/imzero2/egui2/widgets/distsummary/distsummary.go) (render-thread orchestration). `status` and `reviewed-date` are deliberately not re-stamped — the decision is unchanged, only confirmed by a consumer.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — app runtime, in-proc bus, cap-as-subject taxonomy. Defines `AppI`, `Manifest.Caps`, `BusI`, `MountContext.Cancel()`.
- [ADR-0036](./0036-runtime-buscodec.md) — canonical CBOR wire via `buscodec`; every task payload routes through `Encode[T]/Decode[T]`.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — `chlocalpool` worker pattern (sibling, domain-specific); a future consumer of the task primitive for export-style queries.
- [ADR-0005](./0015-streaming-persisted-kafka-from-connect.md) — Kafka catch-up reads, a candidate consumer.
- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — imztop sampling loop, a candidate consumer.
- `src/go/public/keelson/runtime/task/` — package source (to land at M1).
- `github.com/matoous/go-nanoid/v2` — `gonanoid.New()` for task ids.
- `github.com/dustin/go-humanize` — `BigBytes`, `RelTime`, `Comma` for the emission gate's visible-string computation.
