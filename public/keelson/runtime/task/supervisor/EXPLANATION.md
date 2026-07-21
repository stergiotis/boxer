---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# keelson task supervisor

This file explains *why* `keelson/runtime/task/supervisor` is shaped the
way it is — what it persists, what it does not, how the heartbeat
watchdog decides "abandoned," and the choices behind serving the
in-flight snapshot over a bus request/reply rather than a Go API call.
The decision record is [ADR-0038](../../../../../doc/adr/0038-keelson-background-task-primitive.md);
this document captures the engineering rationale that ADR readers will
look for when the contract needs to evolve.

## Where this sits

The M1 producer package handed apps a `task.HandleI`. The M2 demo
showed an in-process consumer doing its own bookkeeping. The
supervisor is the same observer pattern hoisted to the runtime: a
host-side service that subscribes to `task.>`, persists every
terminal-grade verb (created, done, error, cancel, abandoned) into
`boxer.facts`, and exposes the current in-flight set on a
request/reply subject so any consumer — UI panel, M4 NATS bridge, the
M2 supervisor of supervisors — can query it without rebuilding the
same map.

Crucially the supervisor is **opt-in**. M1 / M2 apps work with or
without one. A host that doesn't wire the supervisor sacrifices the
audit trail and the abandoned-detection signal; everything else
behaves identically.

## Persistence routes through WriteLog, not a typed row

The factsstore surface today has typed rows for grants, audit, state,
log, runtime-start, heartbeat, app-lifecycle. There is no
`TaskEventRow`. The supervisor uses `FactsStoreI.WriteLog` with
`Service: LogService` ("runtime.task.supervisor") and structured
fields for `task_id`, `task_kind`, `duration_ms`, etc.

Why not a dedicated `WriteTaskEvent` + `TaskEventRow`?

- **Schema churn.** Adding a typed row touches `factsstore.go`,
  `chstore.go` (CH+leeway write path), `factsschema/memberships.go`
  (new MembTaskId / MembTaskKind / MembTaskState symbols), and the
  generated DML in `factsschema/dml/`. Five files vs zero.
- **No real consumer yet.** Typed rows pay off when queries scan
  millions of rows on dedicated columns. The audit trail is a few
  rows per task and is read by humans through the logviewer. Service
  = "runtime.task.supervisor" is exactly the projection the
  existing logviewer uses to scope a view.
- **Forward-compat is one-line.** If a real columnar consumer
  appears, add `WriteTaskEvent` + `TaskEventRow` and switch the
  supervisor's `writeAudit` to it. The wire surface (subjects,
  payloads, snapshot reply) does not change.

The structured fields preserve the high-cardinality data
(`task_id` as string, `error_chain` as the raw `eh.MarshalError` bytes
on `task.error` rows) so a later projection pass can hydrate a typed
row without re-running tasks.

The `error_chain` field stays as raw bytes deliberately — the
typed `LogErrorContext` projection lives in `logbridge` (private
`decodeErrorContext`). The supervisor does not import logbridge to
avoid pulling its zerolog hooks into runtime services that do not
otherwise use them; downstream readers that want the structured
chain can decode the bytes themselves.

## Why progress is never persisted

Progress emissions are bounded by the humanized-change gate in the
producer's handle — fast tasks tick a handful of times, slow tasks
tick every visible second. Even so, a 30-minute task can produce
~1800 progress rows. The audit trail's reader cares about *what
happened* (started, finished, failed, abandoned), not about every
visible % step.

Skipping progress also matters because the supervisor must not impede
the bus dispatch path. The in-proc bus dispatches synchronously
inside `Publish`: a producer calling `h.Report(...)` runs the
supervisor's `OnProgress` on the producer's own goroutine. A
no-op `OnProgress` keeps the bus hot path clean; an
`OnProgress` that writes a CH row at every tick turns the producer's
hot loop into a serialised IO bottleneck.

The supervisor still uses progress events to update the in-flight
map's `lastEmitMs` and recover entries from `InflightStateAbandoned`
back to `InflightStateRunning`. That is in-memory work, no IO.

## The cancellation lifecycle has two distinct events

The bus carries both `task.<id>.cancel` (the request) and
`task.<id>.done` or `task.<id>.error` (the producer's terminal). The
M3 supervisor writes a row for the cancel *and* a row for the
subsequent terminal. There is no relabeling of "done after cancel was
requested" into "cancelled-as-terminal" — that promotion lives in the
M2 demo's UI projection, not in the audit trail.

The rationale: the audit trail records *bus events*. A reader
reconstructing a timeline sees the user click cancel (one row) and
the producer ack the cancel by terminating (another row). Combining
them into a single "cancelled" verb would lose the temporal gap
between intent and effect — that gap is exactly what reveals
producers that misbehave (cancel requested at T+1s, terminal at
T+15s ⇒ "this worker was slow to honour cancellation").

## Heartbeat watchdog: who emitted last

`InflightStateAbandoned` is the supervisor's verdict that *the
producer has gone silent past the threshold*. Each in-flight entry's
`lastEmitMs` is updated on every observed event (`OnCreated`,
`OnProgress`, `OnCancel`). The watchdog goroutine ticks at
`HeartbeatTickMs` and promotes any entry whose
`now - lastEmitMs > HeartbeatThresholdMs`.

Three deliberate choices in this design:

1. **No synthetic bus event.** Abandoned-detection writes a
   `task.abandoned` *audit row* but does not publish a `task.<id>.error`
   or similar on the bus. The producer may yet recover — and indeed
   `OnProgress` clears the abandoned label back to running. Publishing
   a fake terminal would race the real terminal that might still
   arrive.
2. **Threshold and tick are decoupled.** A 30s threshold with a
   5s tick means a producer that goes silent at T=0 is marked
   abandoned somewhere in `[T+30s, T+35s]`. A coupled value (threshold
   = tick) would either over-trigger on short stalls or accumulate
   latency proportional to the period.
3. **The watchdog never deletes inflight entries.** Abandoned is a
   *state*, not a removal. The entry stays in the snapshot so a UI
   can show "this task is suspected dead," and `OnProgress` can
   resurrect it. Only the producer's real terminal (or, eventually,
   an explicit supervisor-side reaper not in M3) deletes.

## Why the snapshot is a bus request/reply

A UI consuming the supervisor could read the in-flight map via a Go
API call (`supervisor.InflightSnapshot()`). The supervisor exposes
that for in-process consumers, but **also** offers the same data over
a bus request/reply (`task.SubjectListInflight = "task.list.inflight"`
— the constant lives on the `task` package so consumers do not need to
import `supervisor`). Why both?

- **In-process consumers** (the host's status bar, an embedded
  taskmonitor widget) get the cheap path. No bus round-trip, no
  marshalling.
- **Out-of-process consumers** (M4 cross-cluster bridges, a CLI
  diagnostic, a future remote-runtime debugger) get a wire-stable
  contract. The reply shape is `task.InflightSnapshotReply` —
  buscodec-encoded, deterministic — and works identically across
  in-proc and NATS transports. Both the reply struct and the
  `task.SubjectListInflight` subject live on the `task` package so
  the supervisor stays an implementation detail; M4 bridges depend
  only on the contract, not on the audit machinery.

The Go method and the bus reply share the same internal snapshot
builder; they cannot drift. The Go API exposes the full payload
fields (`Created`, `Progress` as `task.*` structs); the bus reply
exposes projected flat fields (`Current`, `Total`, `Unit`, `EtaMs`,
`State` as string) for transport compactness and locale-independent
re-humanization on the consumer side.

## What is intentionally NOT in M3

- **No supervisor-side cancel-on-abandoned.** The watchdog only
  classifies; it does not publish a cancel for the producer to
  observe.
- **No retention policy.** Audit rows accumulate; pruning is a
  factsstore-layer concern.
- **No reaper for stuck cancelling-state entries.** A task whose
  cancel request was acknowledged but never terminates stays in
  `InflightStateCancelling` until the producer terminates or the
  heartbeat threshold fires (which then promotes to
  `InflightStateAbandoned`).
- **No structured `LogErrorContext` projection.** The raw
  `eh.MarshalError` bytes ride on the `error_chain` field. Decoding
  to the typed projection requires `logbridge.decodeErrorContext`,
  which is currently package-private; promoting it is a one-line
  change deferred until a consumer asks.
- **No multi-supervisor coordination.** Two supervisors observing
  the same bus would double-write audit rows and double-promote
  abandoned entries. M3 assumes a single supervisor per bus; the
  host wires accordingly.

## Forward-compat with NATS (M4)

The supervisor uses only `app.BusI` (Publish / Subscribe / Request).
Subject patterns are NATS-shaped. Replies travel on
`_INBOX.*` (the inprocbus `InboxPrefix`); NATS uses the same
convention with its own inbox prefix and the supervisor's
`Caps()` is structured so a NATS-backed host can substitute the
inbox-prefix cap entry without touching the supervisor code.

The Go API (`InflightSnapshot`, `Start`, `Stop`, `PersistedCount`)
remains in-process and is the right surface for hosts that run the
supervisor co-located with the bus. The bus surface is the right
surface for everyone else.
