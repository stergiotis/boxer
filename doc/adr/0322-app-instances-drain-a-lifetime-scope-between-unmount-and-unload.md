---
type: adr
status: proposed
date: 2026-10-11
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Implemented alongside this
> record; review decides whether it stands.

# ADR-0322: App instances drain a lifetime scope between Unmount and unload

## Context

The window host's closing edge
([ADR-0188](./0188-app-instance-effect-tracking.md)) is leave → Unmount →
unload: close the stop channel behind `MountContextI.Cancel()`, run the
app's `Unmount`, close the instance's bus client. Nothing ran between
`Unmount` returning and the client closing, so work that must finish after a
cancel had no bus to finish on.

The chat app's turn is the case that exposed it
([ADR-0303](./0303-a-conversation-outlives-its-process-a-run-journal-written-before-each-act.md),
[ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)).
A turn is one in-process call that can take minutes and writes a journal
entry before and after each act. When the person stops it, the model call
must die but the journal must still record the stop; when the conversation
is retired, the run must be closed, but only after the turn still writing has
left (ADR-0308 §SD9, one writer per run). Chat built this by hand, with a turn
count, a retired flag, a close-once channel and a wait for the late close.
That works inside a window. It fails at window close and at shutdown: the
close it schedules runs after `Unmount` returns, over the per-instance client
the host is about to close, so the run is left open and the next resume
settles it as unknown. Watchbill's worker carries a second hand-built copy of
the same shape (a stopping flag, a run map, settles under a background
context).

The analysis behind this record, including what the host contract did and
did not give, is
[host-lifecycle-hooks-problem](../adr-background-work/host-lifecycle-hooks-problem.md).
Reading the tree against it changed two of its premises:

- `bgjob` is not tied to nothing. Given a task API it runs under the task
  handle's context, which `task.ForApp` composes with the mount cancel. Chat
  passed no task API.
- `Manifest.BackgroundTickHz` names no missing hook. Windows are not
  collapsible, and the host runs every open window's `Frame` on every render
  pass, focused or not. Results already land. The render loop also has an
  idle heartbeat (`IDLE_REPAINT_INTERVAL` in the Rust host), so a result
  lands within it even when nothing asks for a repaint.

## Decision

**SD1 — A lifetime scope is the unit of work that is retired as one.**
Package `public/keelson/runtime/lifetime` provides `Scope`, which has:

- a work context, which ends at `Retire`;
- a tail context, which survives `Retire` and ends at `Abandon`;
- `Enter`/leave counting, which is refused after `Retire`;
- `OnDrained` hooks, which run once the scope is retired and empty;
- `Done`.

A child scope counts as one piece of its parent's work until it is done.
Retiring or abandoning a parent does the same to its children. Retire refuses
new work, but neither the tails of work already entered nor the hooks, so an
owner can retire from inside its own teardown and still have the closing
writes happen. A scope that nothing used is done when `Retire` returns.

**SD2 — The window host drains each instance's scope between Unmount and
unload.** The closing edge becomes:

1. **leave**: close the stop channel and retire the scope.
2. **unmount**: on the render goroutine, as before.
3. **drain**: wait until the scope is done, bounded by a deadline; past the
   deadline the scope is abandoned.
4. **unload**: close the bus client.

The scope reaches the app as the optional mount-context capability
`app.LifetimeI` (`app.LifetimeOf`), in the same optional shape as
`WindowFocusI`, so test fakes and other hosts owe nothing.

After a window close, the window leaves the desktop at once, and the drain
runs off the render goroutine with its own deadline (`DefaultCloseDrain`). At
shutdown, `ReapAll` unmounts every instance, then drains all of them in
parallel and waits within one budget. `hostboot` sets that budget to half the
shutdown grace, because the signal path forces an exit one grace after it
reaps. An instance whose scope is already done is released synchronously, as
before this record.

**SD3 — A task spawned through `task.ForApp` holds the scope until its
worker reports the end.** The hold is released by the worker's own `Done` or
`Error`, not by the handle turning terminal, which a mount cancel causes at
once while the worker may still be writing its tail. A spawn after the scope
retired is refused. `bgjob` runs without a task handle when a spawn fails, so
it is unaffected beyond losing the hold.

**SD4 — Chat's coordinator is a child scope of its window's.**
- Retiring a conversation retires the child.
- A turn counts itself in, and writes under the child's tail context.
- The run's close is the child's drain hook.
- The wait a resume makes for a late close is the child's `Done`.

Because the child holds the window's scope, the host's drain ends only after
the close has landed. The turn runs as a keelson task, so the task monitor
shows it and can cancel it.

**SD5 — Results keep landing by polling, and `BackgroundTickHz` stays
unread.**
- Neither a host-run completion hook (`Post`) nor an off-thread wake into the
  render loop is added; see Alternatives and the deferrals under Status.
- The field stays declared, documented as read by no host. `keelson('apps')`
  carries it as a column, and the carousel's app list carries it in a column
  order that `--launch` predicates may address by position.

**SD6 — Hooks and tails run beside the owner, not on its thread.** A drain
hook runs on a goroutine of its own, and a tail runs on whichever goroutine
entered the work, so both run concurrently with the rest of the app and
after its `Unmount`. They make no imzero2 call and touch only state that is
safe to share. This is a written rule, not an enforced one: `lifetime` cannot
tell what a hook touches. A hook that panics takes the process down, as any
goroutine's panic does.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.AppI` closing-edge contract | drain step added between Unmount and unload | `AppI` doc, `app/EXPLANATION.md` |
| `app.LifetimeI`, `app.LifetimeOf`, `StaticMountContext.SetLifetime` | added (optional capability) | the window host sets it at Open |
| `task.ApiConfig.Lifetime` | added; `task.ForApp` fills it | spawned handles hold the scope |
| `windowhost.Inst.SetDrainDeadlines`, `DefaultCloseDrain`, `DefaultShutdownDrain` | added | `hostboot` derives the shutdown drain from `ShutdownGrace` |
| `Manifest.BackgroundTickHz` | documented as unread | ADR-0026 Update |
| chat manifest caps | `task.ProducerCaps` added | the turn is spawned as a task |

## Alternatives

- **An optional `DrainerI` on the app, called after Unmount.** This was the
  smallest host change, and it was considered as a first step. It was not
  taken because the host-owned scope gives the same drain step without a
  second interface, and it also makes tasks holds (SD3). With `DrainerI`,
  every app would still carry its own counters.
- **Wait for the drain synchronously in the reap, on the render goroutine.**
  Rejected: every window freezes for up to the deadline. This is the shape
  `ReapAll` does use, but only once the render loop has stopped.
- **Run Unmount off the render goroutine and let it block.** Rejected: it
  breaks [ADR-0261](./0261-one-render-goroutine-for-every-app.md), and apps
  touch render state in Unmount.
- **Release the hold when the task handle turns terminal.** Rejected: a mount
  cancel makes the handle terminal at once, before the worker has written
  its tail, which is the window this record exists to keep open.
- **Watchbill for the turn.** Rejected by ADR-0303 (option O5) for reasons
  that still hold: a turn waits on the person, returns a large value to the
  render thread, and must never re-send a call that may have left the process.
- **A `Post(func())` mailbox run by the host before `Frame`, and an
  off-thread wake into the Rust render loop.** Deferred (Status). The idle
  heartbeat bounds the landing delay. The wake needs an out-of-band channel
  into the Rust host and a matching one for the browser tab host. Chat, the
  app that would use both, repaints every frame while a turn runs anyway,
  because it shows the elapsed time.
- **Delete `BackgroundTickHz`.** Rejected for now: deleting it shifts a
  positional column in the carousel's list, and drops a column from
  `keelson('apps')`, to remove a field that costs nothing.
- **Cleanup triggered by reachability:** a finalizer, a `runtime.AddCleanup`,
  a guardian over the coordinator. Rejected: a window close is an event the
  host already sees, and reachability says nothing about when the run must
  be closed. See Prior art.

## Prior art

Cleanup that must happen after an owner lets go has a long record across
languages, and this decision takes most of its lessons from it.

**Finalizers**, code the collector runs when an object becomes unreachable,
were retreated from almost everywhere:

- Whether and when they run is unknowable. Java deprecated finalization for
  removal (JEP 421).
- They run on a thread the program does not choose, so they need the same
  synchronisation as any concurrent code (Boehm, *Destructors, Finalizers,
  and Synchronization*, POPL 2003).
- Ordering between finalizable objects that refer to each other has no good
  answer: topological order leaks cycles, and arbitrary order runs a
  finalizer on an object another one has already finalized.
- An object can be resurrected by its own finalizer.
- An object with a finalizer survives an extra collection.

What replaced them is cleanup bound to a scope the program can see (`using`,
try-with-resources, `with`, `defer`, `Drop`), with reachability-based hooks
kept only as a safety net (Java's `Cleaner`, Python's `weakref.finalize`, Go's
`runtime.AddCleanup`). Cleanup that needs I/O after a cancel is still open in
places: Rust has no async `Drop`.

**Structured concurrency** answers that case. A task group or nursery cannot
end before its children. Cleanup after a cancel runs under a cancellation
shield that has a deadline of its own: Trio's shielded cancel scope, Kotlin's
`NonCancellable`, Go's `context.WithoutCancel`.

**Chez Scheme's guardians** (Dybvig, Bruggeman and Eby, *Guardians in a
Generation-Based Garbage Collector*, PLDI 1993) keep reachability as the
trigger but run no code from the collector. An unreachable registered object
is preserved and queued, and the program polls the guardian and does the
cleanup itself, on its own thread, when it chooses. Detection belongs to the
collector, and execution to the program. Newer Chez versions add ordered
guardians, which withhold an object while another registered object can
still reach it.

How this record stands against those lessons:

| Lesson | Here |
| --- | --- |
| Trigger cleanup by an event the program sees, not by reachability | Window close and shutdown trigger the drain (SD2). |
| Cleanup after a cancel runs shielded, under its own deadline | The tail context, ended by `Abandon` (SD1). |
| Do not rely on cleanup for correctness | An abandoned drain leaves the run open, and resume settles it as unknown (ADR-0308). |
| Order cleanup between dependents | Scopes form a tree, so children always finish first and no cycle can form. |
| No resurrection | `Enter` is refused after `Retire`, and `OnDrained` once the drain began. |
| Do not kill the worker (Java's `Thread.stop`) | `Abandon` is cooperative; past the deadline the host closes the resource, the bus, rather than the goroutine. |
| Run cleanup on the program's own thread (guardians) | Not met: hooks run on a goroutine of their own, the finalizer-thread model. SD6 states the rule that makes this safe. |
| Count every piece of async work | Not met: a goroutine started without `Enter` is invisible to the drain, as un-awaited work is to an async `Drop`. The deadline and the journal are the backstop. |

The guardian's polling is also the shape the deferred `Post` should take if
it is built: a queue the host empties on the render goroutine before `Frame`,
not callbacks run from the goroutine that finished the work.

## Consequences

### Positive

- A conversation retired at window close or shutdown has its run closed
  before the bus closes, within the deadline. `TestTheWindowDrainWaitsForTheRunsClose`
  fails when the coordinator's scope is not a child of the window's.
- Chat's turn count, retired flag and close-once channel are gone; the
  closing order is the scope's, tested once in `lifetime`.
- A task's done or error message is published before its instance's bus
  closes, rather than being lost to a closed client.

### Negative

- A closed window's resources (its bus client, subscriptions and grants)
  live up to `DefaultCloseDrain` longer when its instance has work left.
  Nothing on screen shows such an instance.
- A singleton-registered app reopened while its previous instance drains
  is mounted again on the same value while the old drain still runs. No
  singleton holds its scope today.
- The drain outcome is logged, not recorded: the stopped lifecycle row does
  not say whether the instance drained or was abandoned.
- Hooks run concurrently with the app (SD6), so they carry the
  synchronisation burden finalizers carry. Work an app starts without
  counting it in is not waited for.

### Neutral

- Apps that never touch their scope see no change. Their scope is done when
  it retires, and their bus closes synchronously at reap, as before.
- Two windows resuming one run are still serialised only within a window.
  The host's drain does not order a resume in a new window against a close
  still draining in the old one. The writer lease of ADR-0319 §SD7 does not
  either: it is held by the host process, which both windows share. What
  keeps the record whole is the journal service's checks (ADR-0308 §SD9):
  - A close made over a run row another writer has moved since is refused,
    and the stale writer's `Close` takes the newer row as the run's state.
  - An entry that does not follow the run's last entry is refused.

  Checked against the service on 2026-10-11:
  - If the resume reads the run before the close lands, the draining close
    does nothing, and its late tail is refused.
  - If the tail lands after the resume read the run, the resuming window's
    first write is refused, the close then lands, and that window must
    resume again.
  In neither order is an entry lost or reordered.

## Migration — Tier 1

- **Breaks.** Nothing compiles differently for an app. The closing edge
  waits longer for an instance whose scope has work.
- **Path.** An app with work that must finish after a cancel counts it into
  `lifetime.New(app.LifetimeOf(ctx))`, or spawns it through `task.ForApp`,
  and writes its tail under `Scope.Tail`.
- **Regeneration.** None.
- **Old shape.** Hand-built drains, such as watchbill's worker, keep working.
  Moving them onto a scope is optional.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `lifetime`'s tests, `task`'s
  `TestForAppTasksHoldTheLifetimeUntilTheWorkerEnds`, the window host's
  `TestDrain_*` and closing-edge tests, and chat's
  `TestTheWindowDrainWaitsForTheRunsClose`.
- **What would fail.**
  - A bus that closes before the tail: `TestDrain_CloseWaitsForTheTailBeforeTheBusCloses`.
  - A drain the deadline does not bound: `TestDrain_TheDeadlineAbandonsTheScope`
    and `TestDrain_ReapAllWaitsWithinTheShutdownBudget`.
  - A task hold released at mount cancel: the task test.
  - A coordinator detached from its window: the chat test.
- **Gap.** No test drives the signal path end to end with an app holding
  its scope. The relation between the shutdown drain and the forced exit
  rests on `hostboot` setting the drain to half the grace.

## Status

Proposed — awaiting review by the code owner.

Deferred, each with its trigger:

- **Recording the drain outcome** on the stopped lifecycle row. Trigger:
  resume on boot (ADR-0303 §SD9) needs to list instances that were
  abandoned.
- **Showing draining instances** in the app center, and letting a person
  abandon one. Trigger: a drain observed to outlast a close by long enough
  for someone to notice.
- **Ordering a resume in one window against a close still draining in
  another** (Neutral, above). The cost is a refused write, after which the
  person resumes again; a lease per window rather than per process would
  remove it. Trigger: a person meeting that refusal.
- **`Post` and an off-thread wake** (SD5), `Post` as a queue polled on the
  render goroutine (Prior art). Trigger: an app whose waiting
  state does not repaint by itself needs a result to land faster than the
  idle heartbeat, or the per-frame repaint during jobs shows up as a
  measured cost.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the app contract and `Manifest`
- [ADR-0038](./0038-keelson-background-task-primitive.md) — the task primitive
- [ADR-0188](./0188-app-instance-effect-tracking.md) — the closing edge this record extends
- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) — one render goroutine
- [ADR-0303](./0303-a-conversation-outlives-its-process-a-run-journal-written-before-each-act.md) §SD9, O5
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD9, §SD11
- R. K. Dybvig, C. Bruggeman, D. Eby, *Guardians in a Generation-Based Garbage Collector*, PLDI 1993
- H.-J. Boehm, *Destructors, Finalizers, and Synchronization*, POPL 2003
- [JEP 421: Deprecate Finalization for Removal](https://openjdk.org/jeps/421)
- [host-lifecycle-hooks-problem](../adr-background-work/host-lifecycle-hooks-problem.md) — the problem statement
