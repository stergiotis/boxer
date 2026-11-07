---
type: reference
audience: operator
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The catalog an agent drives, and the person's controls route through the
same handlers (ADR-0269). Every query reads the sampler's latest published
sample and the window's view and returns a bounded result; every command
sets a value and never toggles it, so a repeat is `applied`. All of them
are offered to agents. The sampler is one per process, so what is set on
it moves every imzrt window; smoothing and the tab are this window's.

## Resources

- `view` — the sampling interval and whether the sampler is frozen
  (process-wide), this window's smoothing overlay and its half-width, and
  the tab `show_tab` last brought to the front. The person's own tab
  clicks are not tracked, because the dock layout is not state the app
  holds.
- `profiles` — per profile kind, the dataset's handle, revision and row
  count, and whether a capture or an open in play is in flight.
  Process-wide, like the captures.

## Queries

- `get_view` — the interval in milliseconds, frozen, smoothing on or off
  and its half-width, the shown tab, whether the sampler runs, and when
  the latest sample was taken.
- `get_sample` — the latest sample's scalars: goroutines and
  `GOMAXPROCS`; live heap, heap objects and their bytes, the GC goal,
  mapped and released memory; GC cycles and forced cycles, `GOGC` (0 when
  off) and the memory limit (0 when unset); the allocation rate in bytes
  and objects per second and GC cycles per second over the last interval;
  the GC pause p50, p99 and max in milliseconds and the pause count; the
  scheduling-latency p99; the stop-the-world counts where the Go version
  has them; and the count of curated metrics it lacks. The history behind
  the plots stays in the window and is not an operation. Refused before
  the first sample: *no sample has arrived yet; the sampler publishes one
  every interval, and get_view reports the interval*.
- `list_profiles` — the profile kinds in the tab's order, each with its
  label, the alias `pprof_<kind>`, the dataset's handle (empty before the
  first capture), revision and rows, whether a capture or an open is in
  flight, and the row's last note and last error. `Limit` bounds the kinds
  returned, 0 for all five; `Truncated` says when it left some out. The
  notes are marked untrusted, since a refusal from the host is text the
  window did not write.

## View commands

Effect `view`; each writes `view` and is the same handler as the control
named beside it.

- `set_interval` — the sampling interval in milliseconds, 100 to 60000.
  Out of range is refused naming the clamp: *interval_ms is between 100
  and 60000; the − and + buttons move it by 500 ms within the same
  clamp*. The − and + buttons.
- `set_smoothing` — the trend-smoothing overlay on or off, and its
  half-width in samples; 0 keeps the current one, and a half-width outside
  the kernel's range is refused naming the range. The smooth check box and
  its ± stepper.
- `set_frozen` — stop the sampler and hold the last sample, or let it run
  again; every imzrt window follows. The Freeze / Go live button.
- `show_tab` — one of `heap`, `gc`, `scheduler`, `profiles`; anything
  else is refused with that list. Clicking a dock tab.

## Profile commands

The one act the dashboard performs on purpose, with its two halves.
Both are consequential, since they publish a dataset or open a window
outside this one, so the host confirms an agent's call every time; and
both are asynchronous (ADR-0358): the call stays *running* and settles
with the kind's row as `list_profiles` lists it, or fails with the
capture's or the host's error. Both are unavailable on a host without a
bus: *no bus on this host: capturing and opening in play need the app
runtime*.

- `capture_profile` — capture a pprof profile of this process (`Kind` is
  `cpu`, `heap`, `allocs`, `goroutine` or `goroutineleak`) and publish it
  as the ad-hoc dataset `pprof_<kind>`; a re-capture raises the revision.
  The CPU capture samples for ten seconds. Refused while a capture of that
  kind runs: *a cpu capture is running; list_profiles reports when it
  lands*. `goroutineleak` forces a garbage collection, which steps the heap
  and GC series the other tabs plot; the **Profiles** page has the
  measurement. The Capture button of the kind's row.
- `open_in_play` — open a play window on the kind's dataset, bound to the
  introspection endpoint and focused on the icicle panel. Refused before
  the kind has a dataset (*heap has no dataset yet; capture_profile
  publishes one*) and while an open of that kind is in flight. The
  settled row's `LastNote` names the window the host opened. The Open in
  play button of the kind's row.
