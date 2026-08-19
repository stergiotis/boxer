---
type: reference
audience: end-user
status: draft
title: Leaked goroutines
icon: "🫧"
endpoint: introspection
tabs: [table, detail]
datasets: [pprof_goroutineleak]
datasets_hint: "Capture one from imzrt → Profiles → Leaked goroutines."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Leaked goroutines

Every stack the runtime found blocked on a concurrency primitive it can no
longer be woken from, with how many goroutines are sitting on it. **An empty
result is the good outcome** — unlike the other pages in this book, nothing to
show is the answer you want.

The profile is `goroutineleak`, generally available since Go 1.27
([ADR-0199](../../../doc/adr/0199-adopt-go-1-27.md)). The runtime decides
"leaked" by reachability: a goroutine parked on a channel, mutex or WaitGroup
that nothing live can reach any more will never run again, so it is counted.

**What it cannot see.** A goroutine blocked on something still reachable
through a package-level variable, or through a live goroutine's own locals, is
indistinguishable from one that is merely waiting — so it is not reported. The
result is a lower bound: what is here is leaked, what is absent may still be.

**Needs a capture.** Open imzrt → Profiles → *Leaked goroutines*. The applet
binds the newest `pprof_goroutineleak` dataset at open, and if there is none it
says so and keeps looking, then binds and re-runs once one appears. Capturing
is not free of effect: the profile is computed by a garbage-collection pass, so
each capture forces a GC cycle and puts a visible step in imzrt's own heap and
GC plots.

**The knobs.** `fn_like` keeps stacks with a matching frame anywhere
(`ILIKE`, so `%runtime%` or `%.Watch` shapes work); `lim` caps the rows.

**Reading it.** `goroutines` is how many goroutines share that exact stack —
a leak in a loop shows as one row with a large count, not as many rows. `leaf`
is where they are parked, which is usually a channel receive or a lock; the
package that *created* them is what you want next, and that is what the Detail
tab's full stack carries.

```sql
SET param_fn_like = '%';
SET param_lim = 100;

SELECT
    value AS goroutines,
    leaf,
    pkg,
    stack
FROM keelson('pprof_goroutineleak')
WHERE arrayExists(f -> f ILIKE {fn_like:String}, stack)
ORDER BY goroutines DESC
LIMIT {lim:UInt64}
```
