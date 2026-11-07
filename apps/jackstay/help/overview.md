---
type: explanation
audience: operator
status: draft
title: Syncing two ClickHouse servers
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Syncing two ClickHouse servers

jackstay is the guided form of the `boxer jackstay` command (ADR-0259): a
copy of tables from one ClickHouse server to another, one step per page,
over the same plan document and the same workflow functions the CLI uses.
It is built for someone who uses it rarely. Every page says where it sits in
the sequence, what the earlier steps produced and what the next click will
do to which server; the brief beside each page restates it, and each page
ends with *The same step from a shell*, the CLI line that does what the page
does, for the run someone automates afterwards.

The source is only read. The target is written only by two steps, both of
them behind a confirmation that names it: the DDL on Structure, and the copy
on Sync.

## The six pages, in procedure order

The breadcrumb across the top is the trail; a done step is ticked, the
current one emphasised, a locked one dimmed with its reason on hover. Back
and Next sit in the bottom bar; the page's own action, the one that reads or
writes a server, sits at the foot of the page, apart from them.

1. **Connect** — *Which two servers?* Name both as an HTTP URL or
   `host:port` with a user; passwords come from the environment only
   (`CLICKHOUSE_PASSWORD` for the source, `BOXER_JACKSTAY_TARGET_PASSWORD`
   for the target) and are never written to the plan. **Discover the
   servers** reads both servers' system tables. With no target configured
   the target is the source server, and both being one server means the
   target databases will be renamed copies. Above the form, *Resume a plan*
   lists the plans this window used before; opening one lands on the step it
   was left at.
2. **Databases** — *Which databases, and where do they land?* Tick the
   source databases and name where each lands on the target; the row says
   whether that database is new there. *only tables that classify as
   leeway* narrows the plan through the data catalog. **Plan the structure**
   judges every table of the chosen databases against the target and writes
   the plan file. It changes nothing on either server. Locked until the
   servers are discovered.
3. **Structure** — *What is on each side?* Every table has a verdict:
   `identical` (same columns and key), `narrower` (the target has fewer
   columns; the shared ones sync), `create` (absent on the target; the DDL
   creates it), `extend` (the target lacks columns; the DDL adds them),
   `incompatible` (a different key or column types; it cannot sync),
   `unsupported` (views, dictionaries and non-table engines are not synced).
   Selecting a table shows its reasons, notes, the columns copied and
   hashed, its row filter and the DDL that would run. **Apply the DDL…** is
   the first write to the target; see the confirmation below. Tables with a
   create or extend verdict cannot be compared or synced before it runs.
   Locked until a plan exists.
4. **Differences** — *How do the tables' contents differ?* **Compare
   content** scans each side once for leaf digests and compares small
   differing leaves row by row; no row moves. Per table: identical, or
   rows missing on the target, extra on the target and changed, with the
   chunks that differ. *read merge engines with FINAL* reads
   ReplacingMergeTree and its relatives as if merged, at a cost; without it
   a difference can be spurious and the row says so.
5. **Sync** — *Copy what, and how?* The mode is proposed from the plan's
   state, repair after a comparison that found differences and full
   otherwise, with the reason stated, until you pick one; sample copies a
   fraction of the keys. For a target table that already holds rows, the
   existing-rows policy applies. Compression on the wire and *begin a new
   run instead of resuming* sit under Advanced. Whenever the choices rest
   for half a second the page runs a pre-flight that rediscovers both
   servers and shows what would move and whether the target's disks have
   room. **Start the sync…** is the second write; see below. The sync
   refuses an unsaved plan, because its journal lives beside the file.
6. **Run** — *How is the sync going?* The sync in flight with its progress
   row, or the last run's outcome: rows and bytes per table, every chunk
   verified by digest after landing, the chunks not synced, the target's
   disks and the footprint on them. A stopped sync resumes from its journal
   when started again. **Compare content again** goes back to Differences
   and runs it.

Where the plan stands between the servers is a separate thing from the page
on screen, and it is the subject of the **Phases** page.

## The two-step confirmation

Applying DDL and starting a sync are the person's own consequential clicks,
and ADR-0259 keeps them off the operations catalog: no agent can start
either, and the window confirms them itself rather than through the host's
queue, because the decision is the person's at the window, not one someone
elsewhere owes. The first click arms the action and the page restates what
it will do — *Run N statements on target?* or *Start the full sync of N rows
to target?* — beside a **Run on the target** or **Start** button in the
danger role and a **Cancel**. The sync's arming is withdrawn if any choice
it was armed for changes, so the second click confirms what the first one
saw. Applying DDL first restates the plan: it discovers both servers again
and judges every table afresh, and refuses if a verdict or statement has
moved; only the statements still pending run.

## The row filter

On Structure, a syncable table has a *Row filter*: a SQL `WHERE` fragment
without the keyword, such as `tenant = 'a' AND ts >= toDateTime('2026-01-01',
'UTC')`. Only the rows it selects are compared and synced, on both servers;
target rows outside it are left alone, and under a filter the sync never
drops a partition or truncates a table. Empty means every row. A filter
changes which rows every later step reads, so it takes effect by planning
the structure again — **Plan with this filter** — which also checks it
(ADR-0271); the filter is then recorded in the plan beside the table.

## What is persisted, and where plans live

- **The plan file.** Written when the structure is planned and kept up to
  date after every step; its name and the time it was saved are in the
  line under the breadcrumb. Plans and their sync journals live in the
  window's data area, which the runtime's fs broker owns: the window names
  files and never holds a path. A plan comes in from elsewhere with
  **Import…** and goes out with **Export…**, each through a file dialog you
  answer. `BOXER_JACKSTAY_PLAN` names a plan in the data area to open at
  start and save to, which is how a headless scene reaches the later steps.
  A plan whose source is a pack is read by the CLI only.
- **The recent-plans list.** The window's one persisted key: the plans it
  used, newest first, at most eight, with their servers and the step they
  reached, listed on Connect.
- **Nothing else.** The choices on a page are the window's and are lost
  with it, except where a step wrote them into the plan. Every step runs as
  a keelson task owned by the window, so `keelson('tasks')` lists it and
  closing the window cancels it; a sync stopped that way resumes from its
  journal.

Without a bus — a test, the tour — the servers can still be discovered,
planned and compared, but the plan cannot be saved, opened or exported, and
the Connect page says so.
