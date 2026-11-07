---
type: reference
audience: operator
status: draft
title: Panels and their methods
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Panels and their methods

What each dock tab shows, and where a number is derived rather than read,
how. A collector that failed in the latest sample is listed by
`get_overview` under `Errors`, and the panel says *collector unavailable*.

## CPU

The model name and core count, the 1-, 5- and 15-minute load averages, the
total busy percentage, and the package power in watts where the collector
reports it. Below, two distribution summaries, each a t-digest with an
inspector behind its chip: *cores*, the spread of per-core CPU% at the
current instant, which answers whether some cores are hot while others
idle; and *history*, the spread of total CPU% over the in-memory window.
Then a per-core heatmap, one band of rows per logical CPU and one column
per sample, 600 columns wide (ten minutes at the default one-second
cadence), and a grid of per-core sparklines.

## Topology

The CPU as nested boxes, package → cache → core → thread, from the sampled
topology; drag a box to drill in, hover for details. The tint encodes
`load` or `freq`, chosen by the combo (`set_topology_dim`).

## Proc Map

The process tree as a treemap: processes nested parent → child, each box
sized by `rss`, `cpu` or `io` as the combo chooses (`set_procmap_metric`),
and tinted by CPU load.

## Pressure

Linux PSI (Pressure Stall Information): the share of wall time tasks spent
stalled on CPU, memory or IO, averaged over 10, 60 and 300 s windows. *some*
is at least one task stalled; *full* is every non-idle task stalled, a
strong saturation signal. CPU has no meaningful full line, so only its some
row is shown. When the kernel was built without `CONFIG_PSI` or booted with
`psi=0` the panel says so and `get_overview` reports `PSIAvailable` false.

## Memory

RAM used as a bar in the accent colour and swap used in the warning colour,
because any non-trivial swap activity is a sign of RAM pressure; history of
memory used below.

## Battery

Each battery as a bar of its charge, tinted low below 20%, with its name and
state (charging, discharging, full); *AC* when there is none. The charge
history is the `battery` series of `get_history`, not a plot here.

## Sensors

Temperature sensors with their critical thresholds where known. Empty in
replay with a note, because the tee does not store this kind.

## Disk and Network

Disk: each mount's used space as a bar over its total, then read and write
rates in MiB/s. Network: the interface combo (`select_interface`) picks
which interface's counters the readout shows. Both panels end in the same
kind of history plot: one thin line per device or interface and the two
summed series as thick lines, with the axis ticks scaled to the sums. The
keyed history families `disk_read:`, `disk_write:`, `net_rx:` and `net_tx:`
are what `get_history` reads per device or interface, beside the summed
`disk_read`, `disk_write`, `net_rx` and `net_tx`.

## GPU

Per device: name, busy percentage, memory used and total, with busy history
as the `gpu_busy:` family.

## Processes

The table on the **Watching a host** page. Two of its columns are
derived:

- `CPU%` is the process's CPU over the last sampler interval, 100 being one
  core.
- `CPU%~` is that value smoothed by an exponentially weighted moving
  average whose time constant is 1.5 s. The per-sample weight is
  `α = 1 − exp(−Δt / τ)` with Δt the observed cadence, so a step from 0 to
  100% reaches about 63% of its new value 1.5 s later at any sampling rate.
  The smoothing state is keyed by PID and process start time, so a new
  process that lands on a reused PID does not inherit the dead one's value;
  it is rebuilt from the current sample each tick, so a process that
  disappears falls out of it. `CPU%~` is the default sort key, chosen
  because it keeps the row order stable across brief spikes; `cpu_raw` is
  there when the spike is the point.

The I/O columns come from `/proc/[pid]/io`; a process whose rate could not
be read sorts below every measured one and carries `IOKnown` false in the
catalog.

## The availability strip

Drawn in replay under the transport row; its method is on the **Replay**
page. It is a timeline, not a plot: the bands are the stretches the
coverage query returned, the rug's intensity is mean CPU busy per bin
divided by 100, and nothing is interpolated between bins.

## What is smoothed and what is not

The trend-smoothing overlay in the top bar acts on the history plots only,
draws the raw series faintly underneath, and opens off. The EWMA on `CPU%~`
is always on and is the only smoothing the process table applies. PSI's
averages are the kernel's own.
