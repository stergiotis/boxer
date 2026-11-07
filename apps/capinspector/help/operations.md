---
type: reference
audience: operator
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The inspector's catalog (ADR-0345) carries only what no introspection table
does: the window's selection, the selected capability's static spec with
its effective backend, and the in-process activity counters the sparkline
strip draws from. The declared subjects per app are the table
`keelson('client_caps')`, and the audit records themselves are
`keelson('app_audit')`; a question about either is SQL over that table in
play, not an operation here. All three operations are offered to agents.

## The capability ids

Every operation names a capability by its id, one of `run`, `facts`, `bus`,
`fs`, `persist`, `task`, `watchbill`, `appstate`, `keelsonquery`, `adhoc`,
`llm`, `http`, `agent`, in picker order. `get_view` lists them with their
labels. An id outside that list is refused with the list: *cap is one of
run, facts, …*.

## Resource

- `selection` — the capability the page shows; empty for the picker alone.
  Restored across a rebuild when the id is still known.

## Queries

- `get_view` — the picker and the selected capability's page: the selected
  id and its label, the subject family the capability serves (or what
  stands in for one), the package and types that implement it, the shipped
  backends with the one the runtime selected at boot marked effective,
  `Degraded` when no backend was recorded as effective (the capability's
  service is unbound), the `database.table` the backend persists into when
  it owns one, and every capability the picker offers. With nothing
  selected only the picker list is filled.
- `get_activity` — the audit activity per capability as the strip draws
  it: the record count since the process started (for the bus, every
  record), the records per bucket over the last minute oldest first with
  the seconds one bucket covers, the effective backend id, and `Degraded`.
  `Cap` narrows it to one capability; empty returns every one. An unknown
  id is refused as above.

## Command

- `select_cap` — select the capability the page shows; an empty id shows
  the picker alone. Effect `view`, writes `selection`, never toggles, so
  selecting what is already selected is applied without change. An unknown
  id is refused as above. The person's gesture is a chip in the picker or a
  card in the schematic, routed through the same handler.
