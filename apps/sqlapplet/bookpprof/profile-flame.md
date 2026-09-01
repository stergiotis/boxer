---
type: reference
audience: end-user
status: draft
title: CPU flamegraph
summary: "Draw the CPU capture as a flamegraph"
icon: "🌋"
endpoint: introspection
tabs: [icicle, table, detail]
datasets: [pprof_cpu]
datasets_hint: "Capture one from imzrt → Profiles → Capture CPU."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# CPU flamegraph

The most recent CPU capture as a flamegraph: one row per stack depth, each
frame's **width** the CPU time that flowed through it, children abutting
under their caller. It is the conventional way to read a stack profile, and
it is the whole point of the capture's shape — a converted profile is one
row per unique stack with that stack's own samples, which is exactly the
Icicle tab's folded contract, so the query is a projection and nothing more.

The Icicle tab draws it; its own controls flip between flamegraph and
icicle orientation, re-root on a clicked frame, and sort siblings. The
Table tab is the same rows unfolded — a stack array and its cost — and a
click's Detail shows one path in full.

**Needs a capture.** Open imzrt → Profiles → *Capture CPU*, before or after
opening this applet: with no `pprof_cpu` dataset yet it says so and keeps
looking, then binds and re-runs on its own once one appears. Re-captures
republish onto the same dataset — Run refreshes an open window.

**The knob.** `focus` keeps the stacks with a frame matching it (`ILIKE`,
so `%runtime%` or `%.Render` shapes work); the default `%` keeps all of
them. Matching keeps each stack **whole**, root to leaf, so a focused view
still shows how the frame was reached rather than a subtree floating free
of its callers. To re-root instead — the same picture with the ancestors
folded away — click the frame in the plot.

**Reading it honestly.** Width is cumulative time, so a frame is as wide as
everything below it; a leaf's own cost is its width minus its children's.
Horizontal order carries no meaning — it is a sort, not a timeline, and the
adjacent frames were never adjacent in time. Deep stacks arrive truncated
by the runtime, so a path can be missing the roots that led to it. Frames
too narrow to draw are dropped rather than shown as slivers, and the plot's
status line reports how much value that cost.

```sql
SET param_focus = '%';

WITH s AS (
    SELECT stack, value AS ns
    FROM keelson('pprof_cpu')
    WHERE arrayExists(f -> f ILIKE {focus:String}, stack))
SELECT
    stack,
    ns / 1e6 AS value,
    'ms' AS unit
FROM s
```
