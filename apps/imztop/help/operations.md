---
type: reference
audience: operator
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The catalog an agent drives, and the person's controls route through the
same handlers (ADR-0329). Every query reads the latest published sample,
in live or replay mode, and returns a bounded result; every command sets a
value and never toggles it, so a repeat is `applied`. All of them are
offered to agents.

## Resources

- `proc_view` — the process table's sort key, direction, filter and tree
  toggle. A command to it while the person is typing in the filter box is
  a conflict.
- `procmap_metric` — what a Proc Map box's area encodes.
- `topology_dim` — what the Topology tint encodes.
- `interface` — the network interface the Network panel shows.
- `panel` — the panel `show_panel` last brought to the front. The person's
  own tab clicks are not tracked, because the dock layout is not state the
  app holds.
- `live` — live data frozen or flowing. Process-wide.
- `replay` — the replay session, its window, pause and speed, but not the
  playback position, which moves on its own. Process-wide.

- `replay_state` — the replay session's phase as the chip in the top bar
  draws it: `off`, `opening`, `on` or `failed`. This window's mirror of
  the process-wide session; the **Replay** page says what moves it.

The two process-wide resources carry their real value only in the
controlling window; every other window reports a fixed value.

## The controller

The first imztop window to mount or draw claims the controller role; it is
released when that window unmounts and claimed by the next one that draws.
Only the controller may write `live` and `replay`. Elsewhere the six
process-wide commands are unavailable, and the refusal says which case it
is:

- *another imztop window controls freeze and replay; share that window
  instead*
- *no imztop window controls freeze and replay yet; it is claimed when a
  window mounts or draws*

`get_view` reports `Controller` so an agent can tell before calling.

## Queries

- `get_overview` — the latest sample: mode (live or replay), whether live
  data is frozen, when the sample was taken and the observed interval,
  total CPU and logical CPUs, the three load averages, memory and swap,
  disk and network rates summed over devices, PSI averages when present,
  GPUs, batteries, the process count, the collectors that failed, and the
  names `get_history` accepts: single series and keyed families with the
  first 32 keys of each.
- `list_processes` — processes from the latest sample, at most 50 rows.
  `SortBy` is one of `cpu` (smoothed, the default), `cpu_raw`, `mem`,
  `pid`, `user`, `name`, `io_read`, `io_write`; `Order` is `asc`, `desc`
  or empty for the key's own direction (descending for measurements,
  ascending for identifiers). `Filter` is the same regex battery as the
  table's box: space-separated, case-insensitive RE2 tokens that must all
  match the name or the command line, a token that does not compile
  matching literally, empty keeping all. `Limit` is 1 to 50; 0 means 20.
  The result carries `Matched`, the count that passed the filter, beside
  the rows returned. Command lines are truncated at 256 characters. A bad
  sort key or order is refused with the list of accepted values.
- `get_process` — one process by PID with its parent's name and its direct
  children by smoothed CPU, at most 50 of them, with the full child count.
  Refused when no sample has arrived yet, or when the PID is not in the
  latest sample.
- `get_history` — one series over the in-memory window: samples, first and
  last time, min, mean, p95, max, last, and up to 120 bucket means, oldest
  first (`Points` 1 to 120; 0 means 60). The unit is percent or MiB/s. A
  name the sample does not carry is refused with the names it does, and
  the keyed families as `prefix:<key>`; `get_overview` lists the keys. In
  replay the window is the replayed one. Older history is SQL over
  `boxer.facts`, not this operation.
- `get_hardware` — CPU model, logical CPUs, packages, cores and NUMA nodes
  from the topology, the CPUs this cgroup may run on, per-core MHz, the
  temperature sensors and the container engine. Sensors are not recorded
  by the tee, so they are empty in replay.
- `get_view` — what this window shows: mode, whether it is the controller,
  frozen, the process table's sort, order, filter and tree, the Proc Map
  metric, the Topology dimension, the interface, and the replay session:
  its state (`off`, `opening`, `on`, `failed`), the error when it failed,
  host, endpoint, whether the window held no recorded samples, its bounds,
  position, pause, speed and whether playback reached the end.
- `get_coverage` — where recorded history exists, as the availability
  strip loaded it: the range it was read over and the contiguous stretches
  with their row counts, the latest 200, with `Truncated` when older ones
  were left out, and the coverage query's error when it failed. Available
  only while replay is on: *coverage is loaded while replay is on; call
  enter_replay first*.

- `replay_state` — the replay phase, the transitions allowed from it and
  the latest ones, up to sixteen; `replay_machine` — every phase and every
  allowed transition, as the chip's graph draws them. The two operations
  every app's mounted machine offers (ADR-0269).

The process rows, the overview and the view are marked untrusted, since a
command line is text someone else typed.

## View commands

Effect `view`; each writes one resource and is the same handler as the
control named beside it.

- `set_proc_view` — sort key, order, filter and tree, with the same
  vocabulary as `list_processes`. The column headers, the filter box and
  the tree button.
- `set_procmap_metric` — `rss`, `cpu` or `io`. The Proc Map area combo.
- `set_topology_dim` — `load` or `freq`. The Topology tint combo.
- `select_interface` — an interface name from the latest sample; refused
  when it is not there. The Network interface combo.
- `show_panel` — one of `cpu`, `topology`, `proc_map`, `pressure`,
  `memory`, `battery`, `sensors`, `disk`, `network`, `gpu`, `processes`.
  Clicking a dock tab.

## Process-wide commands

Effect `view`, controller only, each the same handler as the control named
beside it.

- `set_frozen` — freeze live data on the last sample or let it flow; every
  imztop window follows. Unavailable while replay is on: *replay is on;
  use set_replay_paused*. The Freeze / Go live button.
- `enter_replay` — open a replay over a window given as `FromUnixMs` and
  `ToUnixMs`; both zero opens the last ten minutes. The command is
  asynchronous (ADR-0358): the open dials ClickHouse on its own goroutine,
  the call stays *running* meanwhile, and it settles with the host, the
  endpoint and the window it opened, fails with the reason the open
  failed for, or settles as `off` when a leave landed first. `replay_state`
  and `get_view` report `opening`, then `on` or `failed`, while it runs. A
  session already opening or open over the window asked for is the value
  already set; one open over another window is refused (*use
  set_replay_window*), and one still opening is refused the same way
  (*read get_view, then use set_replay_window*). An agent's call needs the
  ClickHouse server holding `boxer.facts` in its grant (ADR-0329 §SD4):
  without it the call is refused for the destination `clickhouse:<host>`.
  The Replay button.
- `leave_replay` — end replay and return every window to live data. Safe
  while opening, which then discards itself. The Go live, Cancel or
  Dismiss button.
- `set_replay_paused` — pause or resume playback. The Play / Pause button.
- `set_replay_speed` — one of 0.5, 1, 2, 5, 10, 60; anything else is
  refused. The speed combo.
- `set_replay_window` — move to another window and play it from the
  start. Both bounds are required (*give from_unix_ms and to_unix_ms*),
  `from_unix_ms` must be positive and before `to_unix_ms`, and the window
  already playing is not restarted. The earlier / later buttons.

The last three are unavailable while replay is not on. Stepping a paused
replay by one bundle is the person's Step button and not an operation; the
position in `get_view` and `set_replay_window` cover the same ground.
