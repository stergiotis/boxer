---
type: reference
audience: agent operating the window, and the person reading what it did
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The catalog an agent drives under a task grant. Every operation is offered
to agents; the person's pills, row clicks and buttons go through the same
handlers, so what an agent did and what a person clicked read alike in the
command log.

## Resources

- `filters` — the state filter and the kind text, as `queued,running|mail`:
  the states that are on, joined by commas, then the kind text. It reports
  editing while the Kind field has focus, so a `set_filters` during typing
  is a conflict rather than a lost keystroke.
- `selection` — the selected job's id; empty for none.
- `job_state` — the selected job's current state, as the state chip shows
  it; empty until a frame has mirrored the selected job.

## Queries

### `list_jobs`

Lists jobs among the rows the window holds. It is a function of its
arguments and the snapshot: the person's filter row is not read.

- `States` — states to list; each must be one of the store's states
  (`queued`, `running`, `succeeded`, `failed`, `discarded`, `cancel`,
  `cancelled`, `abandoned`) or the call is refused with the list of them.
  Empty lists every state among the held rows.
- `Kind` — a regex battery over the kind: space-separated case-insensitive
  patterns, all of which must match. Empty matches every kind.
- `Limit` — rows to return, at most 200; 0 returns 200.

The result carries `Jobs` (id, kind, subject, queue, state, attempt, run
after, finished at, last error), `Truncated` when more matched than were
returned, and two fields about the window's own bound: `Held` is how many
rows the window holds, and `HeldFor` the state filter they were read under.
The window reads at most 500 rows under its state filter, so a job outside
that filter is not among the held rows whatever `States` asks; `set_filters`
with empty `States` widens the next read. The full table is
`keelson('watchbill')`. The output is marked untrusted: subjects and
errors are what requesters and handlers wrote.

### `job_state` and `job_machine`

The selected job's state machine, as the state chip draws it. `job_state`
returns the current state and the edges that lead out of it; `job_machine`
returns the whole graph. The window mirrors the selected row's state into
the machine and never drives a transition itself, so the machine reports
no history, and until a frame has mirrored the selected job it reports no
state. The phases and their edges are on **Job states and the trail**.

## Commands

### `set_filters`

Sets the state filter and the kind text; a view effect. `States` is
validated as for `list_jobs`. The list is read again under the new filter.
The person's gesture is the state pills, the Kind field and Clear.

### `select_job`

Selects a job by id; a view effect. The poller fetches its trail. An id
that is not among the listed rows is accepted: the detail pane says the
job is not listed and asks to widen the filter or wait. The person's
gesture is a row click.

### `cancel_job`

Asks the durable store to cancel a job; consequential, so the host confirms
it with the person every time, and asynchronous. The window refuses before
the round trip when it holds the job and the job is not queued or running:
"job … is *state*; cancel applies to queued or running jobs". Otherwise the
request runs off the frame and the call settles once the store answered,
with `Id`, `Applied` and `Note`. `Applied` is false when the store did not
apply the verb, because the job was in a state it does not apply to or
does not exist; a transport failure fails the call. The person's gesture is
the Cancel button.

### `retry_job`

Asks the store to put a finished job back in the queue with its attempts
reset; consequential and asynchronous like `cancel_job`. The window refuses
a job it holds that is not finished: "retry applies to a finished job:
succeeded, discarded, cancelled, abandoned". It settles the same way. The
person's gesture is the Retry button.

Both verbs are unavailable while one is in flight, with the reason "a
cancel or retry is in flight; wait for it to settle". Settling a job no
verb was asked for is refused. Each verb records a note on the job's trail:
"asked by *the window's app id*: from the watchbill window".
