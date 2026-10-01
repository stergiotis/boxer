---
type: adr
status: proposed
date: 2026-10-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0270: Play's operations catalog and its agent limits

## Context

[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
(proposed) gives apps a catalog of commands and queries that agents call
under a task grant, and leaves play's catalog to a decision of its own (its
M4). Play is the participant the contract was drawn for and the one where an
agent's work leaves the process: a run sends SQL to a database.

What the catalog has to account for, read from the tree on 2026-10-01:

- **The buffer is Go's.** The editor re-sends the SQL every frame and the
  frontend writes it back only when the person types, so an assignment made
  before the window draws shows in that frame without an override.
  `consumePickedSql` already loads text this way.
- **Parameters have two homes.** A pinned value is a `SET param_…` line in
  the buffer; a live value is in the signal store. The widget writes a draft
  and a sync after the panels draw routes it by tier. Slots are re-derived
  from the buffer on a 300 ms debounce.
- **A run's settings are per client.** `readonly = 2` is sent when
  `BOXER_PLAY_ALLOW_WRITES` is off; nothing marks one run as different from
  another. Client-side macros are rewritten off the render goroutine, and
  `keelson('…')` references survive into the residual.
- **The label of a result is not kept with it.** The dispatch decision that
  classifies a run as confined (ADR-0145) lives on the run's goroutine and as
  the client's last decision across all lanes.
- **Two writes skip the run path's gate.** Result pinning and Series
  adjudication write through the client's raw query path to the base
  endpoint, without the dispatch seam or `BOXER_PLAY_ALLOW_WRITES`.

## Decision

We will give play a first catalog of queries and commands over its buffer,
parameters, signals, runs, results and panes; run every agent-caused
statement under play's agent limits; and keep pinning, adjudication, endpoint
changes and turning Live on away from agents.

### SD1 — Resources and the first operations

| Resource | Value compared across the write-back |
|---|---|
| `sql` | the buffer |
| `params` | the parameter drafts, by name |
| `signals` | the signal store's revision |
| `result` | the main result's id |
| `panes` | the raised tab and the tab bindings |

| Operation | Class · effect | Does |
|---|---|---|
| `get_state` | query | the buffer, parameters with tier and value, signals with writer, Live, the raised tab, bindings, the main result's phase |
| `describe_result` | query | the main result's id, columns and types, row count, error |
| `sample_rows` | query | up to 50 rows of the main result as cell text, at most 8 KiB, marked untrusted |
| `list_panes` | query | each pane, whether it can draw the result and why not, the signals it writes |
| `set_sql` | command · document | replace the buffer |
| `set_param` | command · document | set one parameter, through its draft, in its tier |
| `set_signal` | command · document | set one signal, with the task as writer |
| `run` | command · run | run the buffer, under the agent limits |
| `show_pane` | command · view | raise a pane |
| `bind_pane` | command · document | bind a pane to a split node |

`get_state` and `sample_rows` are marked untrusted: buffers and cells come
from wherever the person or a dataset got them. A command to `sql` while the
editor holds keyboard focus, or to `params` while a parameter field does, is a
conflict (ADR-0269 §SD4).

### SD2 — Agent limits on runs

A run is agent-caused when an agent's `run` starts it, or when Live reruns it
because of an input a task wrote (SD3). The run carries the call's
on-behalf-of context to the client, which checks, on the statement it is about
to send and before sending:

- the statement classifies as a read that names nothing outside the endpoint
  (`ClassifyQuerySecurity`'s read class); mutating, egress-reading and
  unclassifiable statements fail with "agent limit";
- every `keelson('…')` table it names is a destination of the grant, as
  `keelson:<table>`;
- the endpoint is a destination of the grant, as `clickhouse:<host>`; a
  statement Auto routes to the host's introspection engine needs only its
  tables;
- `readonly = 2` is sent whatever `BOXER_PLAY_ALLOW_WRITES` says.

The limits fail a run; they do not change it. Views, dictionaries and table
engines can still reach beyond the endpoint without the statement naming
them, which ADR-0269 §SD6 records with the restricted database user that
closes it.

### SD3 — Live reruns after an agent's write

A `set_sql`, `set_param`, `set_signal` or `run` marks the window as
agent-driven by its task. While it is, a Live rerun carries that task's
context and runs under SD2. The person's next edit of the buffer or of a
parameter clears the mark. A signal the task writes carries the task as
writer, so the Live breaker counts it as a machine write; turning Live on, or
resuming it, stays the person's (ADR-0269 §SD8).

### SD4 — The label of a result

The main lane keeps the sensitivity of the dispatch decision that produced
its result, and the window's label is confined while that result is. Queries
and runs carry it (ADR-0269 §SD7).

### SD5 — Not exposed

- **Pinning and Series adjudication**, until they write through the endpoint
  seam and its gate; then as consequential commands.
- **Endpoint changes**: they move what every later run reaches.
- **Turning Live on**: it is the person's (SD3).
- **The person's gestures**: play's own widgets keep their direct paths;
  their changes reach the revisions through the write-back and the end of
  the frame. Routing them through the catalog is deferred.

## Alternatives

- **Range edits of the buffer** (`edit_buffer` with offsets) in place of
  `set_sql`. Deferred: a replace with an expected revision is enough to
  detect a stale write, and offsets into text the model reads as JSON are
  fragile.
- **A side lane for an agent's queries**, leaving the person's result in
  place. Deferred: the run path, its history and its panes are what the
  person sees; a lane of its own would hide database activity in the window.
- **Classifying on the render goroutine** with the diagnostics driver's
  memoised class. Rejected: it classifies the authored buffer before the
  client-side rewrites, and the residual is what the server receives.

## Consequences

### Positive

- An agent drives play's query loop — edit, parameterise, run, read — and
  every step shows in the window as the person's own would.
- What an agent's run reaches is bounded by the grant, checked where the
  statement is final.

### Negative

- A statement the classifier cannot parse cannot be run by an agent.
- Parameters set right after `set_sql` are refused until the slot debounce
  has seen the new buffer.

### Neutral

- The person's gestures in play stay outside the command log.

## Migration — Tier 1

- **Breaks.** Nothing: the catalog is additive.
- **Path.** Play's Model tab keeps its private tool loop.

## Verification plan — Tier 1

- **Lane: default `go test`.** The catalog registers; each command changes
  what its resource reads; the agent-limit check refuses a mutating, an
  egress-reading and an unclassifiable statement, a `keelson()` table and an
  endpoint outside the grant, and passes a read the grant covers; an
  agent-caused run sends `readonly = 2` with writes allowed.
- **Lane: headless scene.** The agent console opens play under a test grant,
  replaces the buffer with a read of `keelson('apps')`, runs it, and reads
  the result; a run naming a table outside the grant fails with the agent
  limit.

## Milestones

- **M1.** SD1's queries and `set_sql`, `set_signal`, `show_pane`; SD4.
- **M2.** `run` and `set_param` under SD2 and SD3.
- **M3.** `list_panes`, `bind_pane`.

## Status

Proposed 2026-10-01 — awaiting review by the code owner. Built on ADR-0269,
itself proposed.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the contract this catalog serves.
- [ADR-0097](./0097-play-reactive-query-graph.md) — play's query graph and signal store.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the dispatch seam the run limits sit behind.
- [ADR-0145](./0145-sealed-app-data.md) — the confined label.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — `readonly = 2` on play's read path.
