---
type: explanation
audience: end-user
status: draft
title: Job states and the trail
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Job states and the trail

A job is one row of the durable store, and its state is a column of that
row. Every change of state is one conditional update of the row followed by
one event row, so the trail is the row's history and the row is its latest
line. The states, in the store's order:

| state | meaning |
| --- | --- |
| `queued` | waiting for a worker; *run after* says from when |
| `running` | claimed; *worker run* names the run that holds it |
| `succeeded` | finished well |
| `failed` | an attempt ended in error and attempts remain; the row carries the next *run after* |
| `discarded` | the last attempt failed |
| `cancel` | a cancel was requested and the worker has not yet acknowledged it |
| `cancelled` | the worker acknowledged the cancel |
| `abandoned` | the run that held the job went silent |

`succeeded`, `discarded`, `cancelled` and `abandoned` are the **final**
states: the job has left the queue, and **Retry** applies. **Cancel**
applies to `queued` and `running`. A cancel of a queued job moves it to
`cancelled` at once; a cancel of a running job moves it to `cancel`, and
the worker's acknowledgement to `cancelled`.

## The machine the chip draws

The state chip in the status row, and the `job_state` and `job_machine`
operations, draw the states as a graph. The window mirrors the selected
row's state into it and never drives a transition, so the edges are
documentation of what the store and the worker do:

- `queued` → `running` (a worker claimed it), or → `cancelled`;
- `running` → `succeeded`, `failed`, `discarded`, `cancel` or `abandoned`;
- `failed` → `queued`, labelled *policy*: the worker re-queues under the
  job's backoff;
- `cancel` → `cancelled`;
- `abandoned` → `queued`, labelled *sweep*: a live worker rescued what a
  dead run held;
- `succeeded`, `discarded`, `cancelled` → `queued`, labelled *retry*: a
  person or an agent asked.

The chip tones the states: queued informative, running primary, succeeded
success, failed and cancel warning, discarded and abandoned error.

## The trail

The **Trail** section of the detail pane is `keelson('watchbill_event')`
for the selected job, one line per transition, newest last as the table
returns them: *at*, *state*, *attempt*, *worker run*, *note* and *error*.
The note is what the requester or the worker said; a cancel or retry from
this window reads "asked by *app id*: from the watchbill window". The error
column shows the attempt's first line, and a click on the row unfolds it.
"No transitions recorded yet" means the table has no row for the job;
"Reading the trail…" that the poller has not landed it.

## The workers

The bottom line is `keelson('watchbill_worker')`: one row per worker run
on the cell with its host, kinds, queues, limit per kind, whether it is
alive, whether it is this process, what it is running, its last heartbeat
and poll, and whether it serves and sweeps. A job of a kind no live worker
links stays `queued`; the line is where to look.
