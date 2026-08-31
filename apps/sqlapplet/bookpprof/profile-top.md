---
type: reference
audience: end-user
status: draft
title: CPU top functions
summary: "Rank functions by self and cumulative CPU cost"
icon: "🔥"
endpoint: introspection
tabs: [table, detail]
datasets: [pprof_cpu]
datasets_hint: "Capture one from imzrt → Profiles → Capture CPU."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# CPU top functions

The classic `pprof top`, as a table: every function seen in the most recent
CPU capture, with its **self** cost (samples whose stack ends in the
function) and its **cumulative** cost (samples whose stack contains it
anywhere). Rows sort by self cost; click a column header to re-sort, and a
row's Detail shows both numbers with the package split out.

**Needs a capture.** Open imzrt → Profiles → *Capture CPU*. Order does not
matter: the applet binds the newest `pprof_cpu` dataset at open, and if
there is none it says so and keeps looking, then binds and re-runs on its
own once one appears. A later re-capture republishes onto the same dataset,
so an already-open window just needs Run to see the fresh profile.

**The knobs.** `fn_like` filters function names (`ILIKE`, so
`%runtime%` or `%.Render` shapes work); `lim` caps the rows.

**Reading it honestly.** Self and cumulative come from the same stacks, so
a leaf-heavy function scores high on both. A function that recurses is
counted once per stack for the cumulative figure (`arrayDistinct`), not
once per frame. Deep stacks arrive truncated by the runtime, so a rarely
sampled root can under-count.

```sql
SET param_fn_like = '%';
SET param_lim = 100;

WITH
  self AS (
    SELECT leaf AS fn, any(pkg) AS pkg, sum(value) AS self_ns
    FROM keelson('pprof_cpu')
    GROUP BY fn),
  cum AS (
    SELECT arrayJoin(arrayDistinct(stack)) AS fn, sum(value) AS cum_ns
    FROM keelson('pprof_cpu')
    GROUP BY fn)
SELECT
    fn,
    pkg,
    round(self_ns / 1e6, 1) AS self_ms,
    round(cum_ns / 1e6, 1) AS cum_ms
FROM self
FULL JOIN cum USING (fn)
WHERE fn ILIKE {fn_like:String}
ORDER BY self_ms DESC
LIMIT {lim:UInt64}
```
