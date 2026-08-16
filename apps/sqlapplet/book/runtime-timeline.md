---
type: reference
audience: end-user
status: draft
title: Runtime event timeline
icon: "🧵"
endpoint: introspection
tabs: [timeline, table, detail]
topics: [runtime, observability]
keywords: [trail, history, audit, lifecycle, heartbeat, launch, workingset, persist, window, instance, facts]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Runtime event timeline

Everything this runtime has written about itself so far, on a time axis, one
lane per app window: process start, heartbeats, app lifecycle, launches,
workingset saves, capability grants, audited bus requests, logs, captured
query runs, column-width overrides, and app-state writes.

It reads `keelson('runtime_events')`
([ADR-0191](../../../doc/adr/0191-runtime-instance-attribution.md) §SD7), the
in-process view over the two tables the runtime persists to — `boxer.facts`
and `boxer.persiststate`. The view is scoped to **this run** by construction:
the process knows its own run id, so there is no run to pick and no window of
time to guess at.

**Lanes are app windows.** An event that carries the host-minted instance key
lands in `<app> #<key>`; two windows of the same app are two lanes, which is
the reason the key exists. An event that carries an app but no window lands in
`<app> (no window)`, and one that carries neither — process start, heartbeat —
lands in `(runtime)`. Lane order is order of first event, so `(runtime)` opens
the stack and each window joins where it started.

**Every event is a mark of the same width**, `mark_ms` wide. An app window's
lifetime is not drawn as a bar: its first and last marks are its `started` and
`stopped` lifecycle events, and reading the span off those keeps one grain on
screen instead of two. Marks are all the same colour for the same reason
[ADR-0174's timeline](../../../doc/adr/0174-play-sql-vocabulary-panel.md)
gives — a ramp sampled by packing order says nothing. The kind is a column;
read it in **Table**, or narrow to one kind with `kinds`.

**The knobs.** `kinds` is `all`, a comma list of kinds to keep, or a
`-`-prefixed comma list to drop (`-heartbeat` is the common one on a long
run). `mark_ms` is how wide an instant is drawn; the panel's own floor is one
pixel, so raising it is what makes a mark clickable on a long span. `lim` caps
the rows, keeping the most recent.

```md preamble
Lanes are windows, not apps: an event without an instance key lands in its
app's `(no window)` lane. Click a mark for its row in **Detail**.
```

```sql
SET param_kinds = 'all';
SET param_mark_ms = 1000;
SET param_lim = 20000;

WITH
  laned AS (
    SELECT
      ts_ms, kind, detail, app_id, instance_key, source, fact_id,
      -- A full app id is too long to read as a lane label, and its last
      -- segment is the name people use.
      if(app_id = '', '', arrayElement(splitByChar('/', app_id), -1)) AS app,
      multiIf(app_id = '',      '(runtime)',
              instance_key = 0, concat(app, ' (no window)'),
              concat(app, ' #', toString(instance_key))) AS lane
    FROM keelson('runtime_events')
    -- `kinds`: all, a list to keep, or a `-`-prefixed list to drop.
    WHERE {kinds:String} = 'all'
       OR if(startsWith({kinds:String}, '-'),
             NOT has(arrayMap(x -> trimBoth(x),
               splitByChar(',', substring({kinds:String}, 2))), kind),
             has(arrayMap(x -> trimBoth(x),
               splitByChar(',', {kinds:String})), kind))
  ),
  capped AS (
    SELECT * FROM laned ORDER BY ts_ms DESC LIMIT {lim:UInt32}
  )
SELECT
  fromUnixTimestamp64Milli(ts_ms, 'UTC') AS at,
  kind,
  lane,
  detail,
  app_id,
  instance_key,
  source,
  fact_id,
  -- The drawn triple, last: the panel finds these by name, and everything a
  -- reader wants first is above them.
  fromUnixTimestamp64Milli(ts_ms, 'UTC')                          AS _tl_time,
  fromUnixTimestamp64Milli(ts_ms + {mark_ms:UInt32}, 'UTC')       AS _tl_time_end,
  lane                                                            AS _tl_lane
FROM capped
ORDER BY _tl_time ASC, lane ASC, kind ASC
```

## Reading it honestly

- **"This run" is exact for a row that names one, and a time window for a row
  that does not.** Since ADR-0191 every kind names its run, so on a trail
  written by a current build the run id selects. Rows written *before* it —
  audit, grant, log, column-width and app-state rows — carry no run, and the
  view falls back to their timestamp landing after this process started, which
  also admits a *second* boxer process that overlapped it. Two runs on one box
  at once therefore blend in the historical part of the trail.
- **A `(no window)` lane is missing attribution, not a window-less event.** An
  audit row did come from some window; a row from before ADR-0191 simply does
  not record which, and nothing backfills it. Rows from a *service* — the
  applet store, the persist service — are correctly unattributed: a service
  has no window.
- **The instance key is unique within a run, not across runs.** It is a
  counter the host mints per process, so this table never has to disambiguate
  it — but a key copied out of here means nothing in another run.
- **There is no way to look at an earlier run.** The view is this process's
  own trail; an introspection table takes no arguments, so there is nothing to
  point at a different run. Reading an older run means SQL over `boxer.facts`
  on the default endpoint. ADR-0191 records that as deferred.
- **Heartbeats are most of a long run.** They land on a fixed cadence, so on a
  run of any length the `(runtime)` lane becomes a metronome and everything
  else is squeezed. `kinds = '-heartbeat'` is the usual first move.
- **A mark's width is a drawing choice, not a duration.** Every event is an
  instant; `mark_ms` only decides how many pixels it gets. Nothing here
  measures how long anything took — an audit row carries a latency, and it is
  in `detail`, not in the mark.
- **`detail` is rendered, not structured.** It is the row's own attribute
  values joined in written order, so it shows whatever the kind recorded —
  including fields no per-kind formatter was written for. It is meant to be
  read, not parsed; the underlying memberships are still in `boxer.facts` for
  a query that needs one of them exactly.
- **A persist deletion names no app.** The tombstone the store appends carries
  only the lifecycle flag, so a deleted key shows in `(runtime)` with the key
  in `detail` rather than in its app's lane. ADR-0191 records it as deferred.
- **`lim` drops the oldest rows, not the newest**, and the view itself stops
  at 20 000 rows. A run that overruns either loses its own beginning — the
  process-start mark included. The row count in the status bar is the check.
- **Absence of a kind is not absence of the activity.** Logs only reach the
  table where the log bridge is wired, query runs only where capture is on,
  and grants only where a broker decided something. An empty `grant` set means
  nothing was decided, or nothing was recorded.
- **The view is live and re-read per Run**, so it costs a ClickHouse round
  trip per press — but the extraction itself is compiled Go, not SQL. An
  earlier cut of this applet did the membership decoding in the buffer, and
  the resulting 7 KB statement cost seconds *per Run* in the client-side pass
  pipeline, which re-parses a statement once per pass — about thirty-four
  times for one Run, measured. That is what §SD7 moved into a provider.
