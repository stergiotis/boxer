---
type: reference
audience: end-user
status: draft
title: Heap in use and churn
icon: "🧠"
endpoint: introspection
tabs: [table, detail]
datasets: [pprof_heap]
datasets_hint: "Capture one from imzrt → Profiles → Capture Heap."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Heap in use and churn

Allocation sites of the most recent heap capture. A heap profile carries
four measures per stack, and this table splits them into the two questions
they answer: **in_use** (bytes live at the last GC — who holds memory) and
**churn** (bytes allocated over the process lifetime and freed again — who
makes garbage). A site with modest `in_use` and huge `churn` is a
GC-pressure source even though it never shows up in a memory-leak hunt.

**Needs a capture.** Open imzrt → Profiles → *Capture Heap*, before or after
opening this applet: with no `pprof_heap` dataset yet it says so and keeps
looking, then binds and re-runs on its own once one appears. Re-captures
republish onto the same dataset — Run refreshes an open window.

**The knobs.** `fn_like` filters allocation sites (`ILIKE`); `lim` caps the
rows. Rows sort by live bytes; click the churn column to hunt allocators
instead.

**Reading it honestly.** The heap profile is sampled (one sample per
~512 KiB allocated, scaled back up), so small sites are estimates.
`allocated_total` counts since process start, not per window — two captures
minutes apart differ in churn by exactly the allocation between them.

```sql
SET param_fn_like = '%';
SET param_lim = 100;

SELECT
    leaf AS fn,
    pkg,
    formatReadableSize(sum(value)) AS in_use,
    sum(inuse_objects) AS live_objects,
    formatReadableSize(sum(alloc_space) - sum(value)) AS churn,
    formatReadableSize(sum(alloc_space)) AS allocated_total,
    sum(alloc_objects) AS allocated_objects
FROM keelson('pprof_heap')
WHERE fn ILIKE {fn_like:String}
GROUP BY fn, pkg
ORDER BY sum(value) DESC
LIMIT {lim:UInt64}
```
