---
type: reference
audience: operator
status: draft
title: Phases
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Phases

The plan's phase is where it stands between the two servers: what the last
step that finished left, or the step running now. It is not the page on
screen; a page can be revisited without moving it. The window never drives
the phase. Each frame observes it from the jobs and the plan and mirrors
it, and the chip at the left of the bottom bar draws it with a one-line
summary; the chip opens the graph and the history.

## The fourteen phases

Running phases are drawn in the accent colour, outcomes by severity, the
rest muted.

| Phase | Meaning |
| --- | --- |
| `idle` | no servers discovered, no plan opened |
| `discovering` | reading both servers' system tables, no plan open |
| `discovered` | servers known, no plan yet |
| `opening` | a plan is being read from the data area, or imported |
| `planning` | judging the tables, or running their DDL on the target |
| `ddl pending` | a plan whose target needs DDL before a diff or a sync |
| `planned` | a plan whose target structure is in line |
| `comparing` | the content diff runs |
| `differs` | the latest comparison found differing tables |
| `identical` | the latest comparison found every compared table identical |
| `syncing` | the sync runs |
| `synced` | the latest sync verified every chunk it copied |
| `incomplete` | the latest sync left chunks unsynced, or stopped |
| `stale` | the servers moved under the plan |

A running job outranks what the plan holds; a plan the servers moved under
outranks its sections; between a diff and a sync the later one speaks.
Discovering again while a plan is open does not move the plan.

## What moves each edge

The gesture or event that causes each declared transition. A failed or
cancelled step returns to the phase it started from, so failure has no
phase of its own; the step's page says why.

- `idle` → `discovering`: **Discover the servers** on Connect.
- `idle` → `opening`: open a plan — a row of *Resume a plan*, **Import…**,
  or `BOXER_JACKSTAY_PLAN` at start.
- `discovering` → `discovered`: the inventories arrive.
- `discovered` → `planning`: **Plan the structure** on Databases;
  → `discovering`: discover again; → `opening`: open a plan.
- `opening` → `idle`: nothing opened; → `ddl pending`, `planned`,
  `differs`, `identical`, `synced` or `incomplete`: whatever the opened
  plan holds.
- `planning` → `ddl pending`: DDL needed; → `planned`: in line; → `stale`:
  the servers moved.
- `ddl pending` → `planning`: **Run on the target**, the confirmed DDL,
  which also judges the tables again; → `comparing` or `syncing`: the
  tables that need no DDL can be compared or synced meanwhile;
  → `opening`.
- `planned` → `comparing`: **Compare content**; → `syncing`: **Start**;
  → `planning`: plan again, or **Plan with this filter**; → `opening`.
- `comparing` → `differs`: differences; → `identical`: none; → `stale`.
- `differs` and `identical` → `syncing`: **Start**; → `comparing`: compare
  again; → `planning`; → `opening`.
- `syncing` → `synced`: every chunk verified; → `incomplete`: chunks left,
  or the sync stopped (cancelled from its progress row, the task monitor,
  or by closing the window); → `stale`; → `ddl pending`, `planned`,
  `differs` or `identical`: nothing to sync, so the plan stays where it
  was.
- `synced` → `comparing`: **Compare content again** on Run; → `syncing`,
  `planning`, `opening`.
- `incomplete` → `syncing`: **Start** again resumes from the journal
  (unless *begin a new run* is ticked); → `comparing`, `planning`,
  `opening`.
- `stale` → `planning`: **Plan the structure**, the only way on;
  → `opening`. Compare and Start stay disabled while stale.

A frame samples the jobs, so a step that starts and finishes between two
frames shows as an edge that skips its running phase; the window logs that
at debug. An observation no declared path reaches is logged as a warning.

## The operations catalog

The catalog carries the phase and nothing that moves it (ADR-0269). The
steps that write to a server stay behind the window's own confirmations,
on the **Syncing two ClickHouse servers** page. Two queries, both offered to
agents and both reading the mounted machine:

- `plan_state` — the current phase, the transitions allowed from it and
  the latest ones, up to sixteen.
- `plan_machine` — every phase and every allowed transition, as the table
  and list above.

Both read the resource `plan_state`, the phase's current value.
