---
type: reference
audience: agent operating the window, and the person reading what it did
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The catalog an agent drives under a task grant. The queries read the
window's last list of the queue, the poller's snapshot, never a request
of their own; `set_control` sets what the Enqueue button sends; `enqueue`
sends it. Cancel and retry are not here: the Watchbill window's
`cancel_job` and `retry_job` apply to a row of this demo's kind like any
other.

## Resources

- `controls` — what Enqueue sends: the duration in seconds, how the
  attempt ends, the attempts allowed and the backoff. It is restorable:
  the host may put a set of controls back.

## Queries

### `get_view`

Reads the controls and the state of the window's list: `Controls`
(`DurationSec`, `End` as `fail` or `succeed`, `Attempts`, `Backoff`),
`Jobs`, how many rows the last list held across every kind, `OwnJobs`, how
many of them are `demo.sleep`, `ListedAt`, when the poller last listed the
queue (zero before the first list), the last list's or verb's `LastError`,
the last verb's `Note`, and the verbs `Inflight`.

### `list_jobs`

Lists the demo's own jobs, kind `demo.sleep`, from the last list.

- `States` — the states to list; empty lists every state. A word that is
  not a state is refused with the states.
- `Limit` — rows to return, newest first as the window lists them; 0 is
  50, at most 200. A negative limit is refused.

The result carries `Jobs`, each with id, subject, state, attempt of
attempts allowed, the worker run, the first line of the last error and
`AgentCaused`, true when an agent's call enqueued it; `Matched`, how many
of the demo's jobs match whatever the limit; `Truncated`; and `ListedAt`.
The full queue across kinds is `keelson('watchbill')`. The output is
marked untrusted: a subject is what a requester wrote.

## Commands

### `set_control`

Sets one or more controls; a document effect on `controls`. A field left
empty keeps its control, and a repeat is applied again rather than
toggled.

- `DurationSec` — 0.5 to 60; out of range is refused: "the duration is
  between 0.5 and 60 seconds".
- `End` — `fail` or `succeed`; any other word is refused.
- `Attempts` — 1 to 5; out of range is refused.
- `Backoff` — `none`, `linear` or `exponential`; any other word is refused
  with the choices.

The person's gesture is the duration and attempts sliders, the backoff
radio buttons and the fail check box.

### `enqueue`

Enqueues one `demo.sleep` job with the controls' policy. It writes a row
to the durable store, outside the window, so it is consequential: the
host asks the person to confirm an agent's call every time. It is
asynchronous: the handler asks the worker off the frame, and the call
settles when the worker accepted the job into the store, with the job's
`Id`, `State`, `Subject` and `AgentCaused`. While a request is in flight
the command is unavailable: "a request is in flight; wait for it to
settle". Without a bus it is refused with where to open the window.

An agent's call carries the agent's context, and the handler copies it
onto the watchbill request. The worker attests the context with the
dispatcher before writing the row; a context the dispatcher does not know
is refused and nothing is written. The row then carries the task and the
call, `keelson('watchbill')` shows them, and the worker runs the job under
the agentic entry frame. The person's own click, the Enqueue job button,
goes through the same command and carries none.
