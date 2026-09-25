---
type: reference
audience: end-user
status: draft
title: App sessions
summary: "Every app window over the last month, one lane per app"
icon: "🪟"
endpoint: introspection
tabs: [timeline, table, detail]
topics: [runtime, observability]
keywords: [session, window, app, history, lifecycle, runs, processes, timeline, concurrent]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# App sessions

When each app had a window open, across every process the trail holds
over the last 30 days, drawn as bars with one lane per app. It reads
`keelson('app_runs')`
([ADR-0260](../../../doc/adr/0260-app-center-one-page-per-app.md) §SD5), the
cross-run counterpart to the current-run trail in *Runtime event timeline*.
The app center's Runs section opens the same drawing for one app.

**A bar is one window's session.** It starts at the window's `started`
lifecycle row and ends at its `stopped` row. A session with no close was cut
short, by a crash or a kill, or is still open. Its bar ends at the last
heartbeat of its process, the latest moment the trail says it was alive.
With no heartbeat after the start, it is a one-second mark. A session whose
start is older than the look-back is a one-second mark at its close. The
`ending` column says which case a bar is.

**The knob.** `apps` is `all`, a comma list of app names to keep, or a
`-`-prefixed comma list to drop. Names are the last segment of the app id
(`play`, `launcher`), as the lanes show them.

```md preamble
One lane per app. A bar without a close ends at its process's last heartbeat
and says so in `ending`; click a bar for its row in **Detail**.
```

```sql
SET param_apps = 'all';

SELECT
  app,
  fromUnixTimestamp64Milli(start_ms, 'UTC') AS started,
  fromUnixTimestamp64Milli(end_ms, 'UTC')   AS ended,
  intDiv(end_ms - start_ms, 1000)           AS seconds,
  ending,
  run_id,
  instance_key,
  app_id,
  -- The drawn triple, last: the panel finds these by name.
  fromUnixTimestamp64Milli(start_ms, 'UTC') AS _tl_time,
  fromUnixTimestamp64Milli(end_ms, 'UTC')   AS _tl_time_end,
  app                                       AS _tl_lane
FROM (
  SELECT *,
    arrayElement(splitByChar('/', app_id), -1) AS app,
    -- A session whose start is not in the look-back is a mark at its close,
    -- not a bar from the look-back's edge.
    if(started_ms > 0, started_ms, stopped_ms - 1000) AS start_ms,
    multiIf(stopped_ms > 0, stopped_ms,
            run_seen_ms > start_ms, run_seen_ms,
            start_ms + 1000) AS end_ms,
    multiIf(started_ms = 0, concat('no start in the look-back; closed: ', stop_reason),
            stopped_ms > 0, concat('closed: ', stop_reason),
            run_seen_ms > start_ms, 'no close: process last seen',
            'no close: no later sign of the process') AS ending
  FROM keelson('app_runs')
)
-- `apps`: all, a list to keep, or a `-`-prefixed list to drop.
WHERE {apps:String} = 'all'
   OR if(startsWith({apps:String}, '-'),
         NOT has(arrayMap(x -> trimBoth(x),
           splitByChar(',', substring({apps:String}, 2))), app),
         has(arrayMap(x -> trimBoth(x),
           splitByChar(',', {apps:String})), app))
ORDER BY _tl_time ASC, app ASC
```

## Reading it honestly

- **The table is bounded.** It reads the last 30 days and at most 5 000
  sessions, newest first. On a trail that overruns the cap, the oldest
  sessions are the ones missing.
- **Most sessions on a development machine are automated.** Headless scene
  runs and screenshot tours each start a process and open one window. Nothing
  on the trail marks a process as headless, so they cannot be filtered out
  here. Their bars are short and many.
- **Sessions from before runs were recorded are left out.** Lifecycle rows
  written before ADR-0191 name no run, and the instance key restarts in every
  process, so they cannot be paired into sessions.
- **Two windows of one app share a lane.** The lane is the app. Two sessions
  of the same app that overlap are drawn over each other. The app center's
  one-app drawing has no lanes and stacks them instead.
- **A bar that ends at a heartbeat is a lower bound.** The process was alive
  then; the window may have closed earlier without a record, or later without
  one being written.
- **Overlap is not use.** Two bars that overlap were open at the same time.
  No focus or input is recorded, so the trail cannot say either was being
  used.
- **The instance key is unique within a process, not across processes.**
  Group by `run_id` and `instance_key` together.
