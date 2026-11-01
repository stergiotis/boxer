---
type: adr
status: proposed
date: 2026-11-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0354: Pausing a window — the host stops drawing it and asks the app to stop its work

## Context

Every open window's `Frame` runs on every pass of the one render goroutine
([ADR-0261](./0261-one-render-goroutine-for-every-app.md)); windows are not
collapsible, and the only way to stop a window costing anything is to close
it, which unmounts the app and loses what it held but its working set
([ADR-0148](./0148-app-workingsets.md)). The Running tab
([ADR-0348](./0348-a-running-page-in-the-app-center-open-windows-what-they-cost-raise-and-close.md))
and its CPU measurement and slow-window notice
([ADR-0351](./0351-what-a-window-costs-beyond-frame-cpu-by-window-label-and-a-slow-window-notice.md))
say which window costs what; closing is the one remedy they offer. The task
managers surveyed in
[task-manager-survey](../adr-background-work/task-manager-survey.md) offer
something softer than ending — throttling, napping, sleeping tabs.

ADR-0351 deferred throttling a window's `Frame` because an immediate-mode
window not drawn in a pass disappears. A window the host draws something
else into does not.

What constrains a pause:

- Go cannot suspend a goroutine. A window's tasks, pollers and timers run
  until the app stops them, and the in-process bus calls its handlers on the
  sender's goroutine whether or not the window is drawn.
- A window's queued commands apply at the start of its body, just before
  `Frame` (ADR-0269 §SD4); a window whose body is not run would leave an
  agent's command waiting until it timed out. Queries are answered off the
  render goroutine from the window's last snapshot.

## Decision

We will let the person pause a window. The host stops calling the app's
`Frame` and draws an empty state in its place; apps that implement an
optional interface are told, so they can stop their own work; commands to
a paused window are refused. Pausing keeps everything the app holds.

### SD1 — The host draws the pause

A paused window keeps its place, size and title. In place of the app's
body the host draws an empty state: that the window is paused, for how
long, whether its app stops its background work (SD2), and **Resume**. The
app's queued commands are not applied and its `Frame` is not called until
the person resumes. egui sizes a window's height to its content, so the
paused body claims the height the window had when paused, corrected each
frame by however short the last one came out. Pause and Resume are
requests, taken from any goroutine and applied at the start of the next
frame, so the app hears of them between its Frames and under its own
profiler labels. Pause is per window: two windows over one singleton
instance pause apart, and the instance hears of it (SD2) only when every
window over it is paused.

A paused window draws no frames, so the slow-window notice cannot fire for
it (ADR-0351 §SD6 needs frames drawn), and Measure CPU shows what its
background work still costs.

### SD2 — The app is asked, not frozen

An app may implement `app.PausableAppI`, whose one method the host calls on
the render goroutine at the pause and at the resume. The app stops and
restarts its own pollers, timers and subscriptions; what it keeps running
is its choice. An app that does not implement it only stops drawing; the
empty state says its background work continues. Tasks are not cancelled:
that is closing, not pausing.

### SD3 — Commands are refused, queries answered

While a window is paused its operations engine refuses every command with
the reason that the person paused the window, including those queued when
the pause came. Queries keep answering from the last snapshot. Pausing and
resuming are not operations: they are the person's, as closing is (ADR-0348
§SD4).

### SD4 — Where Pause is offered

- **The window's own row**, beside "SVG" in the row the host draws above
  every app's body.
- **The Running tab**: Pause and Resume for the windows the filter shows,
  beside Close all, with a "paused" state and its duration in the rows in
  place of frame times that are no longer current. The app center's own
  window is never among them.
- **The slow-window notice** (ADR-0351 §SD6) gains Pause beside Open and
  Dismiss.

Resume is on the empty state, and in the Running tab.

### SD5 — Audited, not restored

A pause and a resume are recorded three ways:

- **The app lifecycle facts** gain *paused* and *resumed* phases beside
  *started* and *stopped*; the row's reason names where the person did it
  (`window`, `running-tab`, `slow-window-notice`).
- **The trail** gets an audit event, domain `window`, action `pause` or
  `resume`, referring to the app and the window, with `by` the person,
  `from` where, and on a resume `paused-for`.
- **The host's log** gets a line.

`keelson('windows')` says since when a window is paused, in `paused_since`.
A restored session reopens its windows running: a pause does not outlive
the host.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| `app` | `PausableAppI` |
| Window host | pause state per window; the empty state; Pause in the window's row; the instance told when all its windows are paused |
| Operations engine | refuses commands while paused |
| `factsstore` | lifecycle phases *paused*, *resumed* |
| Host boot | the pause's audit event on the trail |
| `keelson('windows')` | `paused_since` |
| App center | Pause/Resume shown; "paused" in the rows |
| Attention chrome | Pause on a slow window's notice |

## Alternatives

- **Drawing only.** Pause skips `Frame` and nothing else. Killed: every
  poller and timer keeps its cost, and the apps that poll — the app center,
  imztop — are the ones a person would pause.
- **Cancel the window's tasks.** Frees the most. Killed: in-flight work is
  lost, which makes it closing under another name.
- **Apply commands without drawing.** Killed: an agent would change a
  window the person cannot see, and the person would find the changes on
  resume without having watched them happen.
- **Resume on an agent's command.** Killed: it undoes the person's pause
  without asking.
- **Unload: close keeping the working set, reopen on demand** (Chrome's
  discarded tab). Frees memory, which pause does not. Deferred: it can
  reuse SD1's empty state, but restoring through ADR-0148 rebuilds only
  what an app's working set holds.

## Consequences

### Positive

- A costly window can be quieted without losing its state, and resumed
  at once.
- Apps that poll can stop their polling for as long as nobody looks.

### Negative

- Memory is not freed.
- An app that does not implement `PausableAppI` keeps its background cost;
  the empty state says so, and Measure CPU shows it.
- An agent working through a paused window is refused until the person
  resumes it.

### Neutral

- A pause ends with the host.

## Verification plan — Tier 1

- Unit tests: a paused window's `Frame` is not called and the empty state
  is drawn; the instance hears of the pause once every window over it is
  paused, and of the resume at the first resume; the engine refuses a
  command submitted while paused and one queued before, and answers a
  query.
- A test runs the pause's and the resume's audit events through the
  trail's own bounds.
- The window host's `pause` scene pauses play from the window's row, reads
  the empty state, resumes from it, and finds play's body again; M2's
  scene finds "paused" in the Running tab.

## Milestones

- **M1 — Pause.** SD1 to SD3, SD5, the Pause in the window's row; the app
  center as the first `PausableAppI`: its poller reads nothing while
  paused, and reads at once on the resume.
- **M2 — Offered elsewhere.** SD4's Running tab and slow-window notice.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) — one render goroutine.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — commands apply before Frame; queries from the snapshot.
- [ADR-0148](./0148-app-workingsets.md) — working sets, which an unload would use.
- [ADR-0348](./0348-a-running-page-in-the-app-center-open-windows-what-they-cost-raise-and-close.md) — the Running tab; closing as the person's.
- [ADR-0351](./0351-what-a-window-costs-beyond-frame-cpu-by-window-label-and-a-slow-window-notice.md) — CPU per window and the slow-window notice.
- [task-manager-survey](../adr-background-work/task-manager-survey.md) — something softer than ending.
