---
type: reference
audience: end-user
status: draft
title: ClickHouse flamegraph
summary: "Draw the server's sampled stacks as a flamegraph"
icon: "🔥"
endpoint: default
tabs: [icicle, treemap, table]
keywords: [clickhouse, system tables, introspection, server, system.trace_log, flamegraph, icicle, profiler, cpu, memory, stack, symbol]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ClickHouse flamegraph

The ClickHouse server's own sampling profiler, `system.trace_log`, drawn as a
flamegraph: one row per stack depth, each frame's **width** the samples (or
bytes) that flowed through it. The first frame of every stack is what the
samples were taken for — a query id, `(merges)` for background merges, or
`[ThreadName]` for the server's own threads — so clicking a query id in the
Icicle tab re-roots the plot on that query.

```md preamble
Widths are **samples**, not time, unless the profiler period is known. A
server at the default period takes one sample per second per thread.
```

**The knobs.** `trace_type` is one of `CPU`, `Real` (wall clock, including
waits), `Memory` (allocations above the sampling threshold, weighted by bytes)
or `MemoryPeak`; the memory types count allocated bytes and leave frees out.
`minutes` is how far back to read. `query_id` narrows to one query (`LIKE`, so
`%` keeps all of them).

**Needs the profiler and symbols.** The server fills `trace_log` only for the
periods `query_profiler_cpu_time_period_ns` and
`query_profiler_real_time_period_ns` set, and the defaults sample once per
second: a short query leaves no samples at all. Lower the period in the user's
settings profile to profile one. Frames are symbolised by the server with
`addressToSymbol`, which needs `allow_introspection_functions` — the buffer
sets it per query in a `SETTINGS` clause, so a user whose profile forbids
changing it (or a connection under `readonly = 1`) gets an error instead of a
plot.

**Reading it honestly.** Width is cumulative: a frame is as wide as everything
below it. Horizontal order is a sort, not a timeline. `Real` samples include
threads that were waiting, so a wide frame there can be idle time rather than
work. A frame the server cannot symbolise (JIT-compiled code, a library
without symbols) shows as its hex address. Symbolisation happens after
aggregation, so each unique stack is
resolved once, but a window with many unique stacks is still the slowest
query in this book.

```sql
SET param_trace_type = 'CPU';
SET param_minutes = 60;
SET param_query_id = '%';
WITH s AS (
    SELECT multiIf(query_id = '', concat('[', thread_name, ']'),
                   position(query_id, '::') > 0, '(merges)', query_id) AS root,
           trace,
           if(trace_type IN ('Memory', 'MemoryPeak'),
              toFloat64(sumIf(size, size > 0)), toFloat64(count())) AS w
    FROM system.trace_log
    WHERE trace_type = {trace_type:String}
      AND event_time >= now() - toIntervalMinute({minutes:UInt32})
      AND query_id LIKE {query_id:String}
    GROUP BY root, trace, trace_type)
SELECT
    arrayConcat([root], arrayMap((sym, a) -> if(sym = '', concat('0x', lower(hex(a))), demangle(sym)),
                                 arrayMap(a -> addressToSymbol(a), reverse(trace)),
                                 reverse(trace))) AS stack,
    w AS value
FROM s
SETTINGS allow_introspection_functions = 1
```
