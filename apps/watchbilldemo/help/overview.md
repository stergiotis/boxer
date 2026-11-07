---
type: explanation
audience: end-user
status: draft
title: The watchbill demo
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The watchbill demo

This window is the smallest client of the watchbill, the durable job queue:
it enqueues jobs of one kind of its own through the watchbill client, lists
the queue with a cancel and a retry on every row, and embeds the task
monitor so a running job's progress and its cancel button are the task's.
It demonstrates the three things an app owes to give the cell durable
work — a handler, a client on the mount context's bus, and the task
monitor beside the list — and nothing else. The management window,
Watchbill, is where jobs are inspected across apps.

## The handler, registered at init

The demo's kind is `demo.sleep`. Its handler is registered when the package
is linked, not when a window opens, so any worker in a binary that links
this package drains the kind whether or not the window is open. The
subject is the whole description of the work: a Go duration, and the word
`fail` after it to make the attempt end in error. `3s` sleeps three
seconds; `3s fail` sleeps three seconds and fails, so a retry policy has
something to do. The handler reports progress on the task every 50
milliseconds as it sleeps, counting to 100 items, and returns as soon as
its context ends, which is the cancel or the timeout. A subject it cannot
read fails the attempt, so a typo is visible on the row.

## The window

**Enqueue** sets the request: the duration, the attempts allowed (1 to 5),
the backoff between attempts (none, linear, exponential, from a one-second
base) and whether the attempt fails at its end. **Enqueue job** sends it;
the row that comes back is attributed to this window as its owner app.

**Jobs** is the queue as the worker lists it, at most 200 rows, every kind
and every state: id, kind, subject, state, attempt of attempts allowed,
worker run and the first line of the last error. **Cancel** applies to a
queued or running job and **Retry** to a finished one; a click on a row
whose state does not fit does nothing. The status badge shows the last
error, how many requests are in flight, or the last verb's note; the line
under it says how many jobs were listed and when.

The **task monitor** under the list shows the live runs: every job run is a
keelson task with the job's id, so its progress bar and its cancel button
are the same transition the row's Cancel asks for.

## How the list arrives

A poller refreshes off the frame: every two seconds, and sooner on
**Refresh** and on every `watchbill.changed` announcement a worker makes.
Each verb runs as one piece of work on the window's lifetime scope and
lands its outcome for the next frame. A paused window reads nothing until
it resumes. Jobs in flight are the worker's, not the window's: closing the
window does not stop them.

## The catalog

The window declares a small operations catalog, written out in
[Operations](operations.md): the controls as one resource, `get_view` and
`list_jobs` over the window's last list, `set_control` over the controls,
and `enqueue`, which is the Enqueue job button under another name. An
agent's `enqueue` carries the agent's context onto the job, so the row says
which task and call caused it and the worker runs it under the agentic
entry frame. Cancel and retry across every kind are the Watchbill window's
`cancel_job` and `retry_job`.

## When there is no worker or no bus

Every request is answered by a worker, which needs a live ClickHouse. When
none answers, the badge shows the timeout and the line "No worker answered:
the process's watchbill service needs a live ClickHouse (hostboot
Services.Watchbill)". Where the host minted no bus for the window, the
verbs fail into the same badge and the list stays empty; nothing hangs.

## Where to look next

The trail of one job, the workers alive on the cell and the verbs an agent
may call are the Watchbill window's. The `watchbill` applet book reads the
same tables in SQL. The how-to for writing a handler of your own is
`doc/howto/watchbill-jobs.md`.
