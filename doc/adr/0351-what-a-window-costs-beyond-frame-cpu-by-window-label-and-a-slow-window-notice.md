---
type: adr
status: proposed
date: 2026-11-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0351: What a window costs beyond Frame — CPU by window label, and a slow-window notice

## Context

The Running tab
([ADR-0348](./0348-a-running-page-in-the-app-center-open-windows-what-they-cost-raise-and-close.md))
says what each window costs the render loop: the wall time of its `Frame`
call (ADR-0261 SD3). That is the cost every other window feels, but not the
whole of what a window costs. Its background tasks run on goroutines of
their own, and their CPU is attributed to no one, so a window that draws
cheaply and computes heavily looks idle. And the tab answers only when the
person thinks to open it.

The survey of other task managers
([task-manager-survey](../adr-background-work/task-manager-survey.md))
found both gaps filled elsewhere: in-process managers attribute CPU to the
unit that caused it (VS Code's extensions, JetBrains' plugins, Erlang's
processes), and browsers, Android and VS Code name a costly unit
unprompted and lead to the remedy. It found no in-process manager that
reports memory per unit, except Erlang, whose processes have separate heaps;
all boxer windows share one Go heap, and Go's heap profiles carry no labels.

What the tree has: `pprofarrow` turns a pprof profile into an Arrow table,
one row per stack, and drops sample labels. imzrt captures a CPU profile as
a task, publishes it as an ad-hoc dataset (ADR-0134) and opens play on it.
The attention service keeps host-side notices (ADR-0323 §SD7; the watchbill
worker's `NotifyJob` is the precedent). Nothing in the tree sets pprof
labels.

## Decision

We will label the goroutines a window's work runs on with the window's key,
measure CPU by that label on the person's request, show it as a column of
the Running tab, and have the host say when a window's `Frame` has been slow
for a while. Memory per window stays unmeasured.

### SD1 — Window labels

Every goroutine doing a window's work carries the pprof labels
`boxer.window` (the window key) and `boxer.app` (its app id).

- **On the render goroutine**, the window host sets the window's labels
  around its `Mount`, queued commands and `Frame`, and around its working
  set save and `Unmount` at the closing edge, with
  `pprof.SetGoroutineLabels` from a context built once per window; it
  clears them after. Setting them is a pointer store, so the render loop
  pays nothing measurable.
- **Goroutines started under them inherit them** — Go copies a goroutine's
  labels to every goroutine it starts. A `go` statement in an app's `Frame`
  or `Mount` is attributed without the app doing anything.
- **Bus requests** need nothing: the in-process bus delivers on the
  sender's goroutine, so a service answering a window's request works under
  the window's labels.
- **Tasks** carry them explicitly: `task.Run`, `task.RunToCompletion` and
  bgjob's runners label their goroutine with the owning instance's labels
  (`task.LabelGoroutine`), so a window's task is attributed even when it
  runs on a goroutine the window did not start. The goroutine keeps them
  afterwards, since Go offers no way to read the labels it had.

Attribution follows which goroutine started which, not causality. Two
consequences:

- **Unattributed:** what the host starts at boot — services' own loops,
  timers, scrapers — and tasks no window owns. Their CPU appears as *host*.
- **Misattributed:** a shared service that starts a long-lived goroutine
  lazily, inside a window's work, would carry that window's labels for the
  goroutine's whole life. Such a goroutine clears its labels where it
  starts. Of the `go` statements in the runtime's shared packages
  (reviewed 2026-11-01), one fit: `procpool`'s refill and watchdog loops,
  which `chlocalbroker` starts when a pool's first request arrives.
  Short-lived goroutines a service starts per request stay with the
  requesting window, which is where their cost belongs.

### SD2 — `pprofarrow` keeps labels

`pprofarrow` gains a `labels Map(String, String)` column, and rows merge by
stack and labels together. Readers that ignore the column see the same
totals over more rows.

### SD3 — Measured on demand, not continuously

The Running tab offers **Measure CPU**, which runs a CPU profile for five
seconds as a task of the app center and fills a **CPU** column: each
window's sampled CPU time over the measurement's length, as a share of one
core. Samples without a window label are summed into a *host* line under the
grid, and samples of windows that closed meanwhile into *closed windows*.
The column header says when the measurement was taken; the values stay until
the next one, so a stale figure looks stale rather than current.

A process has one CPU profiler. If another profile runs — imzrt's Profiles
tab, `--pprofCpuOutputFile` — the measurement fails and says so.

### SD4 — From the figure to the stacks

The measurement's profile is published as an ad-hoc dataset under its own
alias, `pprof_running_cpu`, so it does not replace imzrt's `pprof_cpu`. An
**Explore** button beside Measure CPU opens play on it, where
`labels['boxer.window']` narrows the stacks to one window. That is the
survey's "profile this unit" without a per-row button, which ADR-0348 §SD4
rules out for the tab.

### SD5 — Agents may measure

The catalog (ADR-0348 §SD5) gains a command `measure_cpu` with a duration
of one to thirty seconds, five by default, effect *view*, open to agents:
it changes no state the person owns. `list_running` gains a CPU column and
the measurement's time. One measurement runs at a time; a second request
while one runs is refused.

### SD6 — A slow-window notice

The window host watches the frame-time ring it already keeps. A window whose
p95 `Frame` stays at or above 8 ms — the threshold the Running tab already
warns at, moved to the window host as the one constant — for ten seconds is
reported to the attention service as kind `window.slow`, host-side as the
watchbill worker reports a job. The notice's source is the window; one is
kept per window, the latest. It goes when the window closes or the person
dismisses it. It is not reported again until the window has stayed below the
threshold for ten seconds, so a window hovering at the line does not repeat
itself.

The notice's **Open** opens the app center's Running tab filtered to that
window's title: `AppCenterLaunch` gains a `Filter` field, set into the
filter the tab already has.

**Default level: highlight.** Only a *highlight* reaches host chrome
(ADR-0323 §SD7), so `window.slow` defaults to it, and the person can lower
it in the levels grid. It is ADR-0323's first default at *highlight*, which
§SD6 otherwise reaches by override only; M3 records it there as an Update.
A *message* default, as other host-reported kinds have, was rejected: the
notice would stay unseen until the person found the kind in the grid and
raised it, which defeats a notice meant to arrive unasked.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| Window host | labels around `Mount` and `Frame`; the slow-window watcher; `slowFrameUs` moves here |
| `task`, `bgjob` | work runs under the owner's labels |
| `pprofarrow` | `labels` column |
| App center Running tab | Measure CPU, CPU column, host and closed-window lines, Explore |
| App center catalog | `measure_cpu`; CPU in `list_running` |
| `appcenter/launchcfg` | `AppCenterLaunch.Filter` (regenerated) |
| Attention | kind `window.slow`; a host-side notify for a window source |

## Alternatives

- **Continuous sampling.** A low-rate profile always running would keep the
  column live. Killed: it holds the process's one CPU profiler for good,
  which blocks imzrt's capture and the CLI flag, and its overhead is paid
  whether or not anyone looks.
- **Timing task work.** Wall time around each task's `work`. Killed: it
  measures waiting as much as computing — a task blocked on ClickHouse
  would look busy — and misses goroutines a window starts outside a task.
- **Per-goroutine CPU from the runtime.** Go exposes no per-goroutine CPU
  counter; `runtime/metrics` is process-wide.
- **Memory per window.** Not attributable from one shared heap without
  labelled heap profiles, which Go does not record. Not pursued; the survey
  found no in-process manager that does it.
- **A per-row Profile button.** Killed by ADR-0348 §SD4's no per-row
  buttons; the label filter in play reaches the same stacks.
- **Throttling slow windows' `Frame`** (the survey's "softer than ending").
  Deferred: an immediate-mode window not drawn this frame disappears, so it
  needs the host to retain and replay a window's last output.

## Consequences

### Positive

- A window's CPU, background work included, is one click away and sortable
  beside its `Frame` time.
- The stacks behind a figure are one more click, in the tool that already
  reads profiles.
- A slow window is named without the person looking for it.

### Negative

- Attribution is by goroutine ancestry. Services' own loops show as
  *host*, and a service that starts a shared goroutine lazily, added after
  this review, is misattributed until it clears its labels.
- Sampling is statistical: at the default 100 Hz, five seconds gives
  roughly five hundred samples per busy core, so a window below about one
  percent of a core reads as noise.
- The measurement competes with imzrt's capture for the one profiler.
- Memory per window remains unanswered.

### Neutral

- Labels cost nothing until a profile runs; every profile taken in the
  process, imzrt's included, now carries them.

## Verification plan — Tier 1

- A unit test spins a goroutine started under a window's labels and one
  started without, profiles for a second, and finds the first's samples
  under `boxer.window` and the second's under none.
- `pprofarrow`'s tests: a labelled profile yields the `labels` column, and
  two samples of one stack under different labels stay two rows.
- A scene presses Measure CPU, waits for the measurement, and reads the CPU
  column's header and the host line.
- The slow-window watcher is tested on a fake clock: reported after ten
  seconds above the threshold, not again until ten seconds below.

## Milestones

- **M1 — Labels.** SD1 and SD2: window labels, task and bgjob labels,
  `procpool`'s loops unlabelled, `pprofarrow`'s `labels` column.
- **M2 — Measure CPU.** SD3 to SD5: Measure CPU, the column, Explore,
  `measure_cpu`.
- **M3 — Slow-window notice.** SD6: the watcher, `window.slow` at
  *highlight*, `AppCenterLaunch.Filter`, the Update to ADR-0323.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0348](./0348-a-running-page-in-the-app-center-open-windows-what-they-cost-raise-and-close.md) — the Running tab, its catalog, no per-row buttons.
- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) — one render goroutine; `frame_times`.
- [ADR-0134](./0134-adhoc-datasets.md) — ad-hoc datasets.
- [ADR-0061](./0061-imzero2-imzrt-go-runtime-dashboard.md) — imzrt, whose Profiles tab shares the profiler.
- [ADR-0323](./0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md) — attention levels; host-side notices.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — operations and their effects.
- [task-manager-survey](../adr-background-work/task-manager-survey.md) — the survey this follows from.
- [pprof-profiles-as-data](../adr-background-work/pprof-profiles-as-data.md) — `pprofarrow`'s shape.
