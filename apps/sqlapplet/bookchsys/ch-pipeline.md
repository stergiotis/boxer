---
type: reference
audience: end-user
status: draft
title: Query pipeline
summary: "Draw one query's processor pipeline with its timings"
icon: "🧵"
endpoint: default
tabs: [network, graphview, table]
keywords: [clickhouse, system tables, introspection, server, system.processors_profile_log, graph, pipeline, processors, explain, query plan, performance]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Query pipeline

The processors one query ran, from `system.processors_profile_log`, as a
directed graph from source to output. A pipeline runs most steps as several
identical processors side by side, one per stream, so the graph folds them:
one vertex per processor **kind** within a query-plan step, labelled with how
many instances ran (`×8`) and the time they spent working between them, and
one edge per connection between kinds, labelled with the rows that crossed
it. Vertices are grouped by plan step.

The Network tab lays it out in layers, source to sink; the Graphview tab lets
it settle by force. The Table tab lists the same kinds, slowest first.

**The knob.** `qid` is the query id to draw. Empty picks the slowest query of
the last hour that the log recorded, skipping queries that read the log
itself — this applet's own runs among them. Paste an id from the flamegraph
or from `system.query_log` to draw a particular one.

**Needs the log.** Processors are logged when the query ran with
`log_processors_profiles = 1` (the default in recent releases), and the log is
flushed on an interval, so a query run a few seconds ago may not be there yet.

**Reading the sizes.** `elapsed_us` is time spent working, not waiting; the
Table carries the input and output waits beside it. A folded vertex sums the
time of instances that ran in parallel, so it can exceed the query's wall
time, and the times along a path do not add up to it either.

```sql
SET param_qid = '';
WITH
  pick AS (
    SELECT query_id FROM system.query_log
    WHERE event_time >= now() - toIntervalHour(1)
      AND type = 'QueryFinish'
      AND NOT has(tables, 'system.processors_profile_log')
      AND query_id IN (SELECT query_id FROM system.processors_profile_log
                       WHERE event_time >= now() - toIntervalHour(1))
    ORDER BY query_duration_ms DESC
    LIMIT 1),
  p AS (
    SELECT id, parent_ids, name, plan_step_name, elapsed_us, input_wait_elapsed_us,
           output_wait_elapsed_us, input_rows, output_rows, query_id,
           concat(toString(plan_step), ':', name) AS kind
    FROM system.processors_profile_log
    WHERE query_id = if({qid:String} = '', (SELECT query_id FROM pick), {qid:String})),
  kinds AS (
    SELECT kind, any(name) AS name, any(plan_step_name) AS step, count() AS instances,
           sum(elapsed_us) AS elapsed_us, sum(input_wait_elapsed_us) AS input_wait_us,
           sum(output_wait_elapsed_us) AS output_wait_us,
           sum(input_rows) AS input_rows, sum(output_rows) AS output_rows, any(query_id) AS query_id
    FROM p
    GROUP BY kind),
  outs AS (
    SELECT kind, output_rows, arrayJoin(parent_ids) AS pid FROM p),
  links AS (
    SELECT o.kind AS src, d.kind AS dst, sum(o.output_rows) AS rows
    FROM outs AS o
    INNER JOIN p AS d ON d.id = o.pid
    GROUP BY src, dst),
  edges AS (
    SELECT src AS source, dst AS target, formatReadableQuantity(rows) AS label
    FROM links
    WHERE src != dst),
  vertices AS (
    SELECT kind AS id,
           concat(name, if(instances > 1, concat(' ×', toString(instances)), ''), '\n',
                  toString(round(elapsed_us / 1000, 2)), ' ms') AS label,
           step AS group,
           elapsed_us AS weight
    FROM kinds)
SELECT name, step, instances, elapsed_us, input_wait_us, output_wait_us, input_rows, output_rows, query_id
FROM kinds
ORDER BY elapsed_us DESC
```
