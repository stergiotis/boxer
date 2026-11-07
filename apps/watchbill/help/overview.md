---
type: explanation
audience: end-user
status: draft
title: The Watchbill window
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The Watchbill window

Watchbill manages the durable jobs of a cell: the queue across every kind,
one job's fields and its trail, the workers alive on the cell, and the two
verbs a person may apply to a job, cancel and retry. The window holds no
jobs of its own. The list comes from the worker's list verb over the bus;
the trail and the worker line are two introspection tables,
`keelson('watchbill_event')` and `keelson('watchbill_worker')`, read through
the `keelson.query` grant the manifest declares for each; cancel and retry
are requests the durable store answers. The same rows are in
`keelson('watchbill')` for SQL, and in the `watchbill` applet book.

## What is on screen

The **filters row** holds one pill per job state, in the store's order, the
**Kind** field, **Clear**, **Refresh** and **Auto-refresh**. The pills are
the worker's filter: the list is read under the states that are on, every
state when none is. The Kind field is the window's own filter, a regex
battery: space-separated, case-insensitive patterns, all of which must
match the kind. A token that is not a valid regular expression matches
literally, and a line under the row says so. Clear drops every pill and the
kind text at once.

The **status row** shows the selected job's state chip, the last outcome
and when the list was read. The chip is tethered: a click opens the state
graph, the transition table and the history, and the inspector names where
the value came from. The outcome is one badge: the last error, "N
request(s) in flight" while a verb waits for the store, or the last verb's
note. The count reads "X of Y job(s) shown": Y is what the worker listed
under the state filter, X what the kind battery kept.

The **list** is a grid of the visible jobs: id, kind, subject, queue, state,
attempt, run after, worker run, last error. A header click sorts, text by
value and attempt numerically; a third click returns to the listed order.
Dragged column widths are kept across runs. A row click selects the job.

The **detail** pane shows the selected job: its id, the fields the store
holds (kind, subject, queue, priority, attempt of attempts allowed, backoff
policy, timeout, run after, finished at, worker run, owner app, requester
run, args kind), its last error in full, the **Trail** and the **Live
runs** of the task monitor. **Cancel** is drawn while the job is queued or
running; **Retry** while it is in a final state. A job that has left the
listed rows says so and asks you to widen the filter or wait for the list.

The **workers line** at the bottom counts the worker runs alive on the cell
and lists each: its run, host, kinds, queues and the limit per kind. This
process's own run adds how many jobs it is running and its poll, and
carries badges: "this process", "not serving" when it takes no jobs, and
"not sweeping" when it reads no heartbeats and so rescues nothing a dead
run left.

## How the rows arrive

A poller reads off the frame: every two seconds while Auto-refresh is on,
and at once after Refresh, a filter change, a verb, or a
`watchbill.changed` announcement from a worker. One read lists at most
500 jobs under the state filter, then the selected job's trail and the
worker rows. A failed list keeps the rows you had and shows the error; a
timeout adds "No worker answered: the process's watchbill service needs a
live ClickHouse." A paused window reads nothing until it resumes.

Where the host minted no bus for the window, the trail and the workers
line say they have nothing to read through.

## Opening it on a job

Another app opens the window with a launch config naming a job id, a kind
and a state: the job is selected, the kind text set and the state's pill
turned on, before the first read.

## For agents

The window's operations are on the **Operations** page; the states a job
moves through and what the trail records are on **Job states and the
trail**.
