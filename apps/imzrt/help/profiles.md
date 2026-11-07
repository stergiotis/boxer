---
type: explanation
audience: operator
status: draft
title: Profiles
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Profiles

The **Profiles** tab captures a pprof profile of this process, converts it
to rows and publishes it as an ad-hoc dataset (ADR-0134), then opens play
on it (ADR-0135). It is the one deliberate act the otherwise observe-only
dashboard performs, and it is always a click.

## The captures

One row per kind, in this order:

- **CPU (10 s)** — samples the CPU for a fixed ten seconds, with progress.
  It fails when another CPU profile is already running: a second window's
  capture, or a process started with a CPU-profile output file. Cancelling
  discards the partial profile.
- **Heap**, **Allocs**, **Goroutines** — instantaneous snapshots of the
  named runtime profiles.
- **Leaked goroutines** — the `goroutineleak` profile. It needs no tunable,
  but it is the one capture here that is not free of effect on what the
  dashboard shows: it is computed by a GC-assisted reachability pass, so a
  capture forces a garbage collection. Measured against a 205 MB live
  heap it took 4.5–7.3 ms against 200–300 µs for the goroutine profile,
  one extra GC cycle, about 0.1 ms of it stop-the-world. The latency lands
  off the render thread like every other capture; the forced cycle does
  not, and puts a step in the heap and GC series the other tabs are
  plotting.

Block and mutex profiles are omitted on purpose: they stay empty unless
`runtime.SetBlockProfileRate` or `SetMutexProfileFraction` is set, and
imzrt does not mutate runtime tunables.

Every capture runs off the render thread as a job on the window's task
API, with the standard progress row and a Cancel; a click while a capture
of that kind is running is dropped. A failed capture says so under its row.

## Where a capture lands

Capture state is process-wide, like the sampler: one Go runtime means one
current dataset per kind, shared by every imzrt window. The dataset's alias
is `pprof_<kind>` (`pprof_cpu`, `pprof_heap`, `pprof_allocs`,
`pprof_goroutine`, `pprof_goroutineleak`), and the catalog table
`keelson('adhoc')` lists it. A re-capture republishes onto the same handle
with a new revision, so a seeded play buffer or saved SQL keeps working
across captures; the row shows the handle, the revision and the row count.
Handles are not retracted when the window closes: a profile stays
explorable after the dashboard is gone, and the store bounds the cost at
one dataset per kind.

Each row carries a root-first `stack` array and that stack's own `value`,
which is the folded shape play's icicle panel reads directly.

## Open in play

**Open in play** appears once a kind has a dataset. It opens a play window
bound to the introspection endpoint, where ad-hoc datasets resolve,
seeded with one statement that projects the capture into the icicle panel's
contract and rescales the value into a readable unit: CPU in milliseconds,
heap and allocs in MiB, goroutines as counts. The window follows the alias
rather than one handle, so a later re-capture reaches it. The request is a
task too; the button reads *Opening…* while it is in flight, and the
outcome is reported under the row: *opened in play — N rows as pprof_kind,
rev R (window W)*, or the refusal with its reason.

On a host without a bus — a test, the tour — neither button is drawn, and
the tab says why: *No bus on this host — capturing and opening in play need
the app runtime.*
