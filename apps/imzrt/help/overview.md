---
type: explanation
audience: operator
status: draft
title: Watching the Go runtime
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watching the Go runtime

imzrt is a dashboard over the Go runtime of the process it runs in: the heap,
the garbage collector and the scheduler, read through `runtime/metrics` and
drawn as sliding-window plots. It changes no runtime tunable (ADR-0061).
What it shows is this process's own telemetry, which the introspection
tables also carry for other consumers; its operations catalog, on the
**Operations** page, lets an agent read the latest sample and the view and
set the interval, the freeze, the smoothing and the tab through the same
handlers the buttons use. The one act it performs on purpose is on the
**Profiles** tab, which has its own page.

## One sampler for the process

There is one Go runtime per process, so there is one sampler: a goroutine
that reads the collector every interval and publishes a snapshot every
imzrt window draws. It keeps recording while every window is hidden, and
two windows show the same history. The default interval is one second, and
the history window is ten minutes; the **−** and **+** buttons move the
interval by 500 ms within 100 ms and 60 s. A shorter interval costs more of
what the plots show, because sampling runs inside the process it measures;
a longer one smooths the plots and misses short spikes.

**Freeze** stops the sampler for every imzrt window; the plots hold their
last sample, and **Go live** restarts it. While frozen the collector is not
read at all, so the dashboard's own cost drops to its rendering. Pause, the
host's act on one window, is a different thing and leaves the sampler
running.

The top bar also carries the Go version and platform, `GOMAXPROCS` against
the CPU count, `GOGC` (or *off*), the memory limit (or *off*), the live
goroutine count, live heap and GC cycle count, the trend-smoothing overlay
(which acts on the rate and latency plots only, draws the raw series faintly
underneath, and opens off), the time of the last sample, and *⊙
self-measured*: imzrt runs inside the process it measures, so its own
sampling and rendering are in these numbers. A count of *metric(s) n/a*
appears when the running Go version lacks one of the curated metrics.

## What is sampled

Every tick reads the collector once and derives from it:

- **Heap** — live bytes, heap objects, the GC goal, total mapped memory,
  released memory and the object count, as instants; the allocation rate
  and GC rate as differences against the previous tick; and when
  `GOMEMLIMIT` is set, a gauge of mapped memory against it. Two plots: the
  *GC sawtooth*, heap in use climbing toward the GC goal; and the *memory
  classes*, the mapped total stacked into objects, idle (free, unused and
  released), stacks (heap and OS), runtime metadata and other.
- **GC** — cycles, forced cycles and cycles per second; the pause
  distribution of the current interval as the difference of the cumulative
  pause histogram against the previous tick, read off as p50, p99 and max
  with the count of pauses in the window; allocation rate in bytes and
  objects per second. Plots of the pause percentiles, the GC rate (forced
  cycles stay raw under smoothing, being a spike train) and the allocation
  rate. On the first tick the delta is empty, so the percentiles read zero
  rather than spiking.
- **Scheduler** — goroutines, `GOMAXPROCS`, the p99 scheduling latency of
  the interval and, where the Go version exposes them, the stop-the-world
  counts for GC and other causes. A goroutine-count plot, a
  scheduling-latency spectrogram (one column per interval, latency on a log
  axis, colour the number of goroutines that waited that long, on a log
  scale, with the extreme always-empty bins clipped), and the p99 as a
  line.

Memory series are kept in MiB; the time axis is shared by every series.

## What is not here

No per-goroutine view, no runtime tunable, no persisted state, and no
history by operation: the series behind the plots stay in the window, and
`get_sample` reads only the latest sample's scalars. Captured profiles are
the only thing the window writes, and they go to the ad-hoc dataset store
rather than to disk; the **Profiles** page says how.
