---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# keelson background task primitive

This file explains *why* `keelson/runtime/task` is shaped the way it is —
the contract, the cancellation semantics, the emission gate, and what
intentionally lives outside the package. The decision record is
[ADR-0038](../../../../doc/adr/0038-keelson-background-task-primitive.md);
this document captures the engineering reasoning that ADR readers will
look for when they come back six months from now.

## Where this sits

Long-running work — ClickHouse exports, filesystem scans, Kafka
catch-up reads, imztop sampling, the M4 cross-process workers that NATS
will eventually unlock — cannot live inside `AppI.Frame()`. The frame
loop is a 16ms budget; tasks routinely run for seconds to hours.

[ADR-0026](../../../../doc/adr/0026-app-runtime-and-capability-subjects.md)
treats the bus as the universal transport. [ADR-0036](../../../../doc/adr/0036-runtime-buscodec.md)
anchors every payload on a canonical-CBOR wire. The task package is the
third leg: a *bus-protocol primitive* for the off-frame world. Apps own
the goroutine that does the work; this package owns the subject
taxonomy, the wire schema, the producer handle, the observer helper, and
the emission discipline.

The same primitive carries in-proc tasks today (over `inprocbus`) and
cross-process tasks tomorrow (over NATS at M4) with no API change. That
forward-compat is the single most important design constraint.

## Producer vs runtime ownership

Two ways to model a task primitive presented themselves:

1. **Runtime-managed pool.** Keelson owns worker goroutines; apps
   submit a `TaskSpec` and receive a future.
2. **Bus-protocol contract.** The app owns its goroutine; keelson owns
   the protocol the goroutine speaks.

The bus-protocol model wins on every dimension that matters here. A
pool couples to in-proc execution (the M4 swap stops working
transparently), forces an early commitment on scheduling policy (none
of the candidate consumers is asking for one), and overlaps with
`chlocalpool`'s domain-specific worker model without a clear ownership
story. Layering a pool *on top of* the bus contract later is one-day
work; the inverse migration is the rewrite this package exists to
avoid.

So the contract is: you write the worker loop, you respect `Ctx().Done()`,
you call `h.Report(...)` and `h.Done(...)`; we shape the wire and
handle observation.

## Subject layout: flat, not scoped

Subjects are `task.<id>.<verb>` with no app-id prefix. Two reasons:

1. **Observer simplicity.** A single `task.>` subscription fans out
   across every producer in the process (or cluster). Scoped subjects
   (`app.<owner>.task.<id>.<verb>`) would force observers to subscribe
   `app.*.task.>` and demux an extra wildcard hop.
2. **Decoupling from session lifetime.** A task may outlive the owner
   app's window session — a user kicks off a 10-minute export then
   closes the launcher tile. The export keeps running; observers (the
   M3 supervisor, a future tray-area progress bar) keep watching. If
   the subject carried the app id, the supervisor would need to know
   it; with origin in the payload (`OwnerAppId`), the subject stays
   stable across the app lifecycle.

The trade-off is that capslock can't enforce per-app subject discipline
purely from the subject pattern — it has to look at the payload
`OwnerAppId`. That is a capslock concern, not a wire concern, and the
ADR explicitly punts it.

## Cancellation: three sources, one context

A task can be cancelled in three ways:

1. **The parent context cancels.** The caller's `context.Context`
   passed to `Spawn` propagates: when the parent fires, the handle's
   internal context fires too (it is a `context.WithCancel(parent)`).
2. **A `task.<id>.cancel` message arrives.** During `Spawn`, the
   handle subscribes to its own cancel subject; the subscription's
   handler calls the internal `cancel()` function.
3. **The handle reaches a terminal state.** Calling `Done` or
   `Error` fires the cancel — this is what allows worker code to
   safely run `if h.Cancelled() { return }` as the loop condition,
   regardless of whether the task ended in success, failure, or
   external interruption.

The worker is responsible for *checking* the context. The package
cannot pre-empt a foreign goroutine. A worker that never checks
`Ctx().Done()` (or its convenience wrapper `Cancelled()`) will not
honour a bus-cancel, by design.

### Subscription ordering on Spawn

The cancel subscription is set up *before* `TaskCreated` is
published. This matters because the in-proc bus dispatches synchronously
inside `Publish` — if an observer subscribed to `task.>` reacts to
`TaskCreated` by immediately publishing a cancel, the handler runs
before `Spawn` returns. With the subscription pre-installed, the cancel
correctly fires the handle's context; without it, the cancel would
match no subscribers and silently disappear.

This is also why `Spawn`'s parent-cancel goroutine cleans up the
subscription on parent cancellation: without it, the subscription
leaks until process exit.

## Emission gate: humanized-change

A naive task primitive throttles progress emissions at a fixed
frequency (10 Hz, 30 Hz). Two problems:

- **Producers must think about it.** A worker calling `Report` in a
  tight inner loop floods the bus unless the frequency is right; the
  frequency depends on the task duration, which the producer often
  does not know up front.
- **Observer perception is non-linear.** A 30s task ticking at 10 Hz
  emits 300 progress frames; a human sees 30 distinct visible states
  at most. A 30-minute task ticking at 10 Hz emits 18,000 frames; a
  human sees ~1800 distinct minute-level states at most.

The gate this package implements: **publish only when the humanized
form of the progress would change.** The estimator computes the
visible string ("47% · 2m12s left" or "1.2 GiB / 3.2 GiB · 18 MiB/s ·
1m50s left") via `dustin/go-humanize`; the handle compares it to the
last-published string and emits a `TaskProgress` only on change.

Three consequences fall out:

1. **Bus traffic auto-tunes.** A 100ms task emits a handful of
   frames; a 30-minute task ticks roughly once per visible
   second-resolution change. Producer code stays a tight loop with no
   awareness of cadence.
2. **Indeterminate tasks need a heartbeat.** When `Total = 0`, the
   visible form may not change for long stretches even though work is
   happening — the spinner shouldn't freeze. The handle emits at
   `DefaultIndeterminateHeartbeatMs` (1 Hz) regardless of the
   humanized-change gate.
3. **The final state always emits.** When `Done` or `Error` runs, any
   pending sample held back by the gate is flushed first — so the
   last progress observers see matches the final state, not a stale
   one from two ticks earlier.

The estimator is its own subpackage so its sliding-window throughput
+ humanizer can be unit-tested without the bus, and so a second
consumer (a future REPL widget, an export status line) can reuse the
exact same humanization logic.

## What is intentionally NOT in M1

- **No supervisor.** Audit-to-`runtime.facts` lives in a separate
  `task/supervisor` package at M3. M1 apps take no `factsstore`
  dependency.
- **No retry / chaining / dependencies.** Compose at the producer.
  A task that wants to run B after A's `Done` just calls `Spawn` for
  B inside A's terminal block — no contract needed.
- **No cross-process cancellation.** The bus-cancel mechanism works
  across NATS naturally at M4; M1 only exercises it in-proc.
- **No typed `Result` registry.** `Result []byte` is opaque;
  observers dispatch on `Kind`. A registry can be added later
  without wire change if a real consumer needs it.
- **No `BackgroundTickHz` integration.** The frame-loop background
  tick is for frame-shaped work; task is for arbitrary work. Apps
  that mix the two run both in parallel.

## When to use this vs `chlocalpool`

`chlocalpool` (ADR-0028) is a domain-specific *execution* pool for
clickhouse-local subprocess workers. The task primitive is a *coordination
contract* for arbitrary work. They compose: a chlocal worker can
report its progress on `task.>` while running inside the pool. The
pool tracks subprocess lifecycle; the task tracks user-visible work.

If you find yourself wanting "a goroutine pool with progress and
cancel," you want this primitive — start your goroutine, hand it the
handle. If you find yourself wanting "a clickhouse-local subprocess
with pooling and warm-start," you want chlocalpool — and you can
publish task lifecycle events on the side.

## Forward-compat checklist (M4 NATS swap)

The package will be exercised in M4 against a NATS-backed `BusI`.
The properties the design relies on:

- **Subject patterns are NATS-shaped.** `task.<id>.<verb>` and the
  `>` / `*` wildcards in `PatternAll` / `PatternCancelAll`
  translate verbatim.
- **Synchronous in-handler publish is not assumed.** The cancel
  subscription's handler closes a channel via `cancel()` — that is
  safe whether the handler runs inline (in-proc) or on a separate
  goroutine (NATS).
- **The estimator has no clock dependency beyond `nowFn`.** Tests
  inject a fixed clock; production uses `time.Now`. NATS network
  round-trips affect `AtMs` precision, not correctness.
- **`buscodec` is the wire.** CBOR canonical bytes survive
  serialisation across the transport boundary unchanged.

No M1 code paths assume in-proc semantics; the swap will be a
host-level concern, not a task-package concern.
