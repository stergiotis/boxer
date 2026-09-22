---
type: adr
status: accepted
date: 2026-09-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-15
---

# ADR-0236: a management window for the watchbill

## Context

[ADR-0223](./0223-watchbill-durable-work-on-facts.md) §SD7 made the applet
book the first surface over the job table, on the [ADR-0132](./0132-sqlapplet-sql-defined-applets.md)
rule that a book comes before an app. [ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md)
gave apps the verbs the book cannot offer — enqueue, cancel, retry, get,
list — and its first client, `apps/watchbilldemo`, exercises them for one
kind. What is missing is the thing River ships as River UI and every queue
grows sooner or later: one place to see the queue across kinds, pick a job,
read its trail, and act on it.

Two facts shape the design. The client protocol carries the job row and
nothing else: the event trail and the worker's presence are introspection
tables (`keelson('watchbill_event')`, `keelson('watchbill_worker')`), and a
verb to fetch events would be a verb River does not have, which ADR-0234
§SD7 keeps out. And every run is a keelson task, so live progress and the
cancel button already exist in the task monitor.

## Design space (QOC)

**Question.** Where does an operator manage the queue, and through which
reads and writes?

**Options.**

- **O1 — Extend the applet book with actions.** Rejected in ADR-0234 for
  bending the applet model.
- **O2 — Grow the demo app.** One kind, one window, a demo's shape.
- **O3 — A management app** (chosen): the protocol for the row and the
  verbs, the introspection endpoint for the trail and the workers, the
  task monitor for live runs, a launch config for a deep link.
- **O4 — A board (kanban) as the primary view.** Columns per state, cards
  per job, drag as the verb.

**Criteria.** C1 every write goes through the audited protocol; C2 the
trail and the workers are visible without a verb River lacks; C3 a job can
be reached from another app by id; C4 nothing runs on the frame goroutine
that waits on a store; C5 the first cut stays small enough to ship.

O3. O4's drag maps only two of its moves onto verbs and must snap the
rest back; it is a follow-up view over the same state, not the first cut.

## Decision

Build `apps/watchbill`, a windowed app on the runtime topic.

### Subsidiary design decisions

- **SD1 — Reads.** The job list comes from the list verb, filtered by
  state on the worker and by kind substring in the window, bounded. The
  selected job's trail and the process's worker row come from the
  introspection endpoint (`introspect.LocalQueryEndpoint()`) as two SQL
  reads over the `keelson()` tables; where the endpoint is absent the
  window says so and shows the row alone.
- **SD2 — Writes.** Cancel and retry through `watchbill.Client`, with the
  window named in the note. A running job's cancel is also the task's.
- **SD3 — Nothing waits on the frame.** A poller refreshes the list, the
  trail and the worker row on a tick and on every `watchbill.changed`;
  each verb runs in its own goroutine. The frame renders the last
  snapshot.
- **SD4 — A launch config for deep links.** `watchbillLaunch` (kind, one
  job id, one state), on the ADR-0135 path, so any app can open the
  window on a job it enqueued. No workingset: a filter is not worth
  restoring.
- **SD5 — Layout.** A top bar of state chips and a kind filter, with one
  Clear that drops both; a left
  table of jobs where a click selects; a detail pane with the state
  machine chip, the policy and identity fields, the last error, the
  event trail, and the task monitor; a bottom line for the worker. The
  table is play's Table pane in shape: a row-number gutter, the density's
  tight inset, monospace cells truncated to the column and selectable
  anywhere, the selection painted by egui_table, header buttons that
  cycle a sort, and column widths the user drags kept through the
  ADR-0151 resolver on the host's facts store, so a drag survives the
  window and the process. The split between the list and the detail is
  the window's own state rather than egui's: a drag moves it, a window
  resize does not (the drawn width is clamped while the window is narrow
  and the kept width returns when it grows), and it persists under a
  declared key.
- **SD6 — Out.** The board view (O4); presence of workers in other
  processes (ADR-0234 §SD5's deferral); enqueue from this window — an
  operator retries what exists, a consumer enqueues.

## Alternatives

- **Actions in the applet book (O1).** Rejected in ADR-0234 for the
  reason that still holds: an applet is a document over read-only SQL.
- **Grow the demo (O2).** A demo enqueues its own kind and stays small on
  purpose; a manager never enqueues and must see every kind.
- **A board as the first view (O4).** Drag maps onto two verbs (to
  cancelled, to queued) and would have to refuse every other move; it is
  a second view over the same snapshot, once the table exists.
- **An events verb on the protocol.** River has none, and ADR-0234 §SD7
  admits a verb only where River has it; the trail is an introspection
  read.
- **A workingset for the filters.** A filter is set in a second and
  restoring one surprises more than it saves.

## Consequences

### Positive

- Every write from the window is a request on the bus, attributed and
  audited like any other; the window holds no store.
- The trail and the worker row cost no new surface: they are the tables
  ADR-0223 and ADR-0234 already publish.
- Another app can open the window on a job by id.

### Negative

- Without the introspection endpoint the window shows rows and verbs but
  no trail and no worker; it says so rather than degrade silently.
- The list is bounded and polled: a queue past the bound, or a change
  between ticks, is a stale window until the next read.
- The window sees the worker of its own process only, the deferral
  ADR-0234 §SD5 records.

### Neutral

- The demo app and this window share their read and write plumbing in
  shape but not in code; a third client would be the moment to lift it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| vdd vocabulary registry | `wbLaunchJobId`, `wbLaunchKind`, `wbLaunchState` (192–194) | the assignments golden |
| Codec kinds | `watchbillLaunch` | kindcheck; the launchcfg golden |
| App registry | `github.com/stergiotis/boxer/apps/watchbill` | the carousel import list; the capslock app set |

## Verification plan — Tier 1

- **Lane: default `go test`.** Over an in-proc bus and a worker on the
  memory store: the list reflects the state filter; a selection asks for
  its trail; retry and cancel from the window land on the row with the
  window in the note; a launch config selects and filters; the endpoint
  reads parse a JSONEachRow reply from a stand-in HTTP server; without an
  endpoint the window reports it and nothing errors; a header click
  cycles the sort as a stable permutation; a dragged width observed past
  the settle window is flushed to the store and resolved by a fresh
  resolver over it.
- **Live.** The window on a host with a ClickHouse: enqueue from the demo
  app, find the job here, read its trail, retry it; drag a column, restart
  the host, find the column where it was left.

## Status

Accepted 2026-09-15.

## Updates

### 2026-09-22 — the trail and the workers are read over the bus (ADR-0253)

SD1's two reads no longer go to the local query endpoint. The window
declares one sticky grant per table, `keelson.query.watchbill_event` and
`keelson.query.watchbill_worker`, and reads each as a request served by the
host over the ADR-0094 §SD4 engine
([ADR-0253](0253-introspection-table-reads-as-a-bus-capability.md),
proposed). The rows and the statements are as they were; the bus audits
the read with the window as sender, and the capslock finding for
`apps/watchbill` is gone. The Verification plan's "stand-in HTTP server" is
now a stub on the bus that answers `keelson.query.*`.

## References

- [ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md) — the protocol this window drives; §SD7 for what stays out.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) §SD7 — the book, the first surface.
- [ADR-0135](./0135-app-launch-requests.md) — the launch config path.
- [ADR-0094](./0094-keelson-introspection-tables.md) §SD6 — the local query endpoint.
