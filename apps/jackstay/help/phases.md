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

The catalog carries the phase and the plan, and nothing that moves either
(ADR-0269). There is no command: every step that writes to a server — the
DDL on Structure, the sync on Sync — stays behind the window's own
confirmation, which is the person's act (ADR-0259), and the summaries say
so. Six queries, all offered to agents. Two read the mounted machine:

- `plan_state` — the current phase, the transitions allowed from it and
  the latest ones, up to sixteen.
- `plan_machine` — every phase and every allowed transition, as the table
  and list above.

Both read the resource `plan_state`, the phase's current value. Four read
the plan the window holds, bounded as one inline result; each is refused
before a plan is open, naming how one is opened:

- `get_plan` — the plan's file name, servers and phase, and its tables in
  the plan's order: source and target, engine, rows and bytes when
  planned, the verdict, whether the target needs DDL, the filter, and
  whether a comparison or a sync left a section. `Limit` is 1 to 200, 0
  for 50; `Matched` counts every table and `Truncated` says when the limit
  cut the list. The stale notes and the status line's note and error come
  with it.
- `get_table` — one table by its source database and name: the verdict's
  reasons, the planner's notes, the DDL the target needs (up to sixteen
  statements, as text — the window runs them, not the catalog), the keys
  and engines, and the table's last comparison and sync report. A name
  not in the plan is refused: *no table of that name in the plan; get_plan
  lists them as database and name*.
- `get_compare` — the last content comparison: per compared table, when
  it finished, whether the chunks were identical, final or possibly
  spurious, the rows on each side, the chunks and how many differed, and
  the rows missing, extra or changed where resolved. Refused before any
  table was compared: *Compare content on the Differences page runs the
  comparison*.
- `get_sync` — the last sync: when it started and ended, whether it
  stopped without a result, the rows and bytes it moved, the chunks left
  failed or stale, and every table's report with its problems. Refused
  before a sync ran: *Start on the Sync page runs one, behind the window's
  confirmation*.

Table names, notes and problems are text a server or a plan file carried,
so the four are marked untrusted.
