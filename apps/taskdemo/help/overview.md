---
type: explanation
audience: end-user
status: draft
title: The task demo
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The task demo

The task demo spawns fake background work and shows it the way every app's
work is shown: as keelson tasks, watched by the task monitor. It is the
showcase for the task primitive (ADR-0038): one window, a **Spawn** section
with the controls, and the `taskmonitor` widget below it with the in-flight
and history panes. Nothing it does leaves the process. An agent drives the
controls, spawns a task and follows it through the window's operations
catalog ([Operations](./operations.md)); the tasks it spawns are visible to
every observer of the bus like any other app's, and, while in flight, in
`keelson('tasks')` on a host that runs the task supervisor.

## What a task is here

A task is one unit of long-running, cancellable, observable work. The
producer publishes it on the bus under `task.<id>.<verb>` with the verbs
`created`, `progress`, `cancel`, `done` and `error`; the owning app's id
rides in the payload, so one observer subscription on `task.>` sees every
producer's tasks. The task's id is a 21-character nanoid, not time-sortable.
The window declares the producer capability (`task.>` in both directions),
which covers publishing the task's events and subscribing to its cancel
inbox and to the monitor's watch.

A producer reports progress as often as it likes; the primitive's
humanized-change gate publishes only when the visible string — the
percentage, the bytes, the time left — would change. The demo's worker
calls `Report` every 20 ms and the progress bar still moves calmly; that
gate, not the worker, is what the window shows.

## The three spawn shapes

The three buttons start a task of one unit each, with the duration the
slider holds (1 to 60 s, 5 s at first). A button is the `spawn` operation
as the person's gesture, so an agent's spawn and a click run the same
handler:

| Button | Unit | Total | Reads as |
| --- | --- | --- | --- |
| **Start items task** | items | 1 000 | a count |
| **Start bytes task** | bytes | 3 GiB | sizes and a rate |
| **Start steps task** | steps | 5 | "step N of 5" |

Each task is spawned with the kind `demo.<unit>`, the title
`Fake <unit> task · <duration>s`, cancellable, and an estimate equal to
the duration. The worker is one goroutine per task in the window's lifetime
scope: it reports a fraction of the total every tick until the deadline,
then reports the total and finishes. Nothing starts once the window's
scope has retired.

**Simulate error at 80%** makes the next spawned tasks fail when they pass
80% of their duration, with the reason "simulated failure" and the error
"taskdemo: simulated failure at 80%"; the history pane shows the error's
details. The checkbox is read when a task starts, so it does not change a
task already running.

## The monitor and cancel

The monitor is the shared `taskmonitor` widget, open by default here. The
**In-flight (n)** pane has one row per running task with its progress bar
and, for a cancellable task, a cancel button; **History (n)** keeps the
finished ones, done or errored. A cancel publishes `task.<id>.cancel`; the
worker sees the handle cancelled on its next tick and ends the task with
`Done` and no result, and the monitor, which saw the cancel first, lists it
in the history as cancelled rather than done.

Closing the window retires its lifetime scope, which cancels every task
spawned under it and waits for the workers to leave before the window is
unloaded (ADR-0322); the terminals they publish after that land in no
window. On a host that gives the window no task API — a bare test
context — the window says so under the buttons and nothing can be spawned.

## The slow-frame toggle

**Slow Frame (12 ms), which every window waits for** makes every frame of
this window sleep 12 ms. It is deliberate and for one purpose: to trip the
window host's slow-window notice (ADR-0351 §SD6). The host reads each
window's frame-time ring once a second; a window whose p95 `Frame` stays at
or above 8 ms for ten seconds, having drawn at least 32 frames meanwhile,
is reported to the attention service as kind `window.slow`, one notice per
window, at the *highlight* level by default. The notice's **Open** opens
the app center's Running tab filtered to the window's title. It is not
reported again until the window has stayed below the line for ten seconds.

The wording of the toggle is the cost model's: the windows share one
render loop, so one slow `Frame` is paid by every window on the desktop,
not by this one alone.
