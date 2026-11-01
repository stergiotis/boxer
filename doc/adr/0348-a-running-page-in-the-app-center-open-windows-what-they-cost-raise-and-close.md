---
type: adr
status: accepted
date: 2026-10-29
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-01
---

# ADR-0348: A Running page in the app center — the open windows, what they cost, and closing them

## Context

A host draws every open window from one render goroutine
([ADR-0261](./0261-one-render-goroutine-for-every-app.md)). With many windows
open, nothing lists what is open, which window makes the loop slow, or which
window still has work in flight. The data exists, in three live introspection
tables keyed by the same window key:

- `keelson('windows')` — one row per open window: app, title, kind, how and
  why it was launched, its geometry and whether it is active.
- `keelson('frame_times')` — each window's `Frame` cost and FFFI messages per
  frame, with quantiles, and one row for the loop's period (ADR-0261 SD3).
- `keelson('tasks')` — every task in flight, attributed to the window that
  spawned it by `owner_instance_key`
  ([ADR-0188](./0188-app-instance-effect-tracking.md) SD4). Apps' background
  work reaches this table through bgjob runners given a task API and through
  `task.Run` / `task.RunToCompletion`, which also hold the window's lifetime
  until the work ends
  ([ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md)).

The app center ([ADR-0260](./0260-app-center-one-page-per-app.md)) already
reads `frame_times` and `tasks`, one app at a time: it is organised by
registered app, and its list is the registry, so it says what could run rather
than what is running.

Constraints the design inherits:

- A `keelson.query` statement names only the table its grant covers
  ([ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) SD3), so rows from several tables
  are joined in the reader. An introspection provider sees no predicate.
- The window host's `windowhost.raise` refuses to raise another app's window,
  so that an app cannot pull the person away to someone else's; there is no
  bus subject that closes a window. The host itself can do both
  (`windowhost.Inst.Raise`, `Close`).
- A window list in the Window menu is deferred by
  [ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) and
  again by [ADR-0346](./0346-imzero2-tiled-desktop-dynamic-layouts-over-floating-windows.md),
  which is reshaping the window host and the `windows` columns.
- Some costs cannot be attributed to a window. Apps share one Go process: heap
  is shared, and a goroutine is not labelled with the window that started it.
  Render cost per window is measured (frame time); background CPU and memory
  per window are not.

## Decision

We will add a **Running** tab to the app center: one row per open window of
the host, assembled in the reader from `keelson('windows')`,
`keelson('frame_times')` and `keelson('tasks')`, filtered by a substring, with
one action — Close all, over the windows the filter shows — that the host
hands the app center at boot. The first cut covers this host's windows only.

### SD1 — A tab of the app center, beside the app pages

The app center has two tabs in its top bar: **Running**, the table across the
window's full width, and **Apps**, the app list and the selected app's page
as before. Switching keeps the selection. A window opened with no launch
config opens on Running; one opened on an app (the launcher's Inspect) opens
on Apps. A click on a row opens that window's app's page under Apps, so the
overview leads to the existing detail. The Window menu's **Running windows**
opens a new app center window on the Running tab; host boot hands the chrome
that action where the app center can be launched.

The windows, and the tasks no open window owns, are `recordtable` grids:
sortable headers, column widths the person drags and keeps, and no per-row
controls. Above them a filter box narrows both: a case-insensitive substring
of a window's app id, app name, title or one of its tasks' titles, and of an
unowned task's title, kind or owner — the app list's filter, extended to what
the tab shows. The count above the table says how many of the open windows
the filter shows.

A first cut showed Running whenever no app was selected, behind an entry
above the app list. Reaching it meant pressing an entry that also cleared the
selection, and its table sat beside a list it did not use; a tab says what it
is and leaves the table the window's width.

A filter in SQL or as a regular expression was considered. SQL would see one
table only — `keelson.query` admits one per statement, and the rows are three
tables joined in Go — so it could not filter on frame time or task counts
without publishing the joined rows as a dataset on every read; play already
queries `keelson('windows')` for anyone who wants SQL. A regular expression
adds an error state while the person types, and no other filter in the app
center or imztop uses one.

### SD2 — Rows, columns and order

One row per row of `keelson('windows')`. Its columns: the app and the window
title; the window's frame time at p50 and p95 against the loop's median period,
as the per-app "Frame time" lens shows it; messages per frame; the number of
tasks it owns, with the oldest one's title and age; its agent tasks; whether it
is active. Rows start sorted by p95 frame time, slowest first: the question the page
answers first is which window holds the loop up. A header click sorts by its
column, numbers descending first.

Tasks whose `owner_instance_key` names no open window are listed under the
rows as **not owned by an open window**: a closed window's work still draining,
and runtime services' tasks, which carry no window key. They are the work a
person might otherwise believe ended with its window.

### SD3 — What the page does not measure, and says so

The page shows no memory column per window and states why in its header:
the apps share one heap, and imztop shows the process's figures. CPU per
window is measured on request, from goroutine labels
([ADR-0351](./0351-what-a-window-costs-beyond-frame-cpu-by-window-label-and-a-slow-window-notice.md)).
The task count is a lower bound and is labelled as the tasks the window
registered: a goroutine started without the task API is not counted, the gap
ADR-0322 records for its drain. Jobs that re-run on a timer or on typing are
left untracked by their apps, because every task writes audit rows; frame time
is what shows a window that is busy that way.

### SD4 — Close all, from the host, not the bus

The tab has one action: **Close all**, a button labelled with how many
windows it closes — every window the filter shows that is not already
closing, the app center's own among them when it is shown. Its own window
closes last, after the others' closes are asked for. Narrowing the filter to
one window closes that one; with no filter, Close all closes every window,
the app center included. Host boot hands the app center a small host interface with
`Close(key, reason)`, as it hands the launcher its host; `*windowhost.Inst`
satisfies it structurally, and neither package imports the other's types. No
bus subject is added: no manifest can ask for an authority to close windows.
A host that wires none (a narrower registry, an embed) shows no Close all.
Each close goes through the window's ordinary closing edge — workingset saved,
Unmount, drain — and its reason, "closed from the app center", lands in the
stopped lifecycle row; the row reads "closing" until a read no longer lists
the window.

Close all asks first, and the confirmation names the windows by title, the
first three and a count. It holds the window keys and the filter they were
shown under: a read that adds a window does not add it to the confirmation,
a window that closed meanwhile drops out of it, and typing in the filter
drops the confirmation.

A first cut had Raise and Close on every row. In a `recordtable` grid, whose
cells are text, per-row controls would need the grid to draw host widgets in
a cell; raising a window by title is the Window menu's to offer
(Alternatives), and one button over a filter covers closing one window and
closing many.

### SD5 — Agents read, the person acts

The app center's catalog
([ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md))
gains a query, `list_running`, which returns the rows of the tab's last read,
bounded and unfiltered; a `view` resource and a `set_view` command, which
switch the tab the way the person's click does; a `running_filter` resource
and a `set_running_filter` command, which set the filter as typing does; and
`show_app` now also switches to Apps. Close all is not an operation: closing a
window ends its work and drains it, which is what ADR-0345 SD4 keeps the
person's.

### SD6 — Reads

Three statements, one per table, run by the app center's poller off the frame,
on its existing tick and only while the page is shown, joined in Go on the
window key. `frame_times` and `tasks` are already among the app center's
grants; `windows` is added.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| App center manifest (`appcenter`) | `keelson.query` grant for `windows` added | the manifest test that pins its tables |
| App center exported API | a host interface and its setter added | host boot wires it beside the launcher's host |
| App center operations catalog | `list_running` query, `view` and `running_filter` resources with `set_view` and `set_running_filter` commands added; `show_app` also writes `view` | `appcenter_ops_test.go`; the catalog's help text |
| Host chrome config (`imzero2/host.ChromeConfig`) | `ShowRunning` added | host boot sets it beside the launcher's Inspect |

## Alternatives

- **A separate system app.** It would duplicate the app center's poller,
  lenses and grants, and add a manifest and a catalog, to show rows that link
  back to app center pages anyway.
- **A list in the Window menu.** A menu has no room for cost columns, the item
  is deferred twice, and the window host is under change for ADR-0346; it
  remains the right home for a plain raise-by-title list later.
- **Extending imztop.** imztop is OS-level and read-only
  ([ADR-0020](./0020-imzero2-imztop-resource-monitor.md)); the per-window data
  is in keelson tables, and mixing the host's windows with the machine's
  processes conflates two scales.
- **A bus subject to raise or close any window.** It would make closing
  another app's window an authority any manifest can request, for a single
  consumer that host boot already wires in process.
- **A memory column per window.** Not measurable from one shared Go heap;
  numbers shaped like a task manager's that are not would mislead. CPU per
  window, sampled from goroutine labels, is ADR-0351's.
- **A stall column.** A stall is the host not drawing at all — the desktop
  not showing it ([ADR-0332](./0332-a-hidden-desktop-host-keeps-running-passes.md))
  — which stops every window's frame, the page's own included, so the column
  would be blank whenever a person can read it; agents already read a
  window's stall from the agent window list.
- **Tracking timer- and keystroke-driven jobs as tasks without audit rows.**
  Deferred: frame time already shows such a window, and the option changes the
  task supervisor for every app.

## Consequences

### Positive

- One place answers "what is open, what holds the loop up, what is still
  working", and leads to the app's page for the rest.
- Work that outlives its window — a draining close, a write run to completion
  — becomes visible as unowned tasks.
- No new bus authority.

### Negative

- Three reads every tick while the page is shown, joined in Go.
- The task count is only as complete as the apps' use of the task API.
- Close all is as final as each window's close button, at one remove from
  the windows, and over as many as the filter shows, the app center's own
  window included; the confirmation naming them is the only guard.
- The tab offers no way to bring a window to the front.

### Neutral

- Other processes (watchbill workers, pooled workers, agent-job processes)
  are out of scope; imztop shows them as OS processes without saying which is
  which.

## Migration — Tier 1

Nothing to migrate: the change is additive. Hosts that do not wire the host
interface keep an app center without actions.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the join from three column sets, including a
  task owned by no open window and a window with no frame samples; the
  `list_running` operation against a fixed read; every statement through the
  `keelson.query` gate and engine over a bus. A headless scene under the app
  center's scenes: play opened beside it with a query in flight shows a row
  for play; the filter set to its query run's title shows play's window
  alone, and Close all on it removes play's window from the tree. A second scene opens the page from the
  Window menu with play alone and finds both windows listed.
- **What would fail.** A row missing or misattributed, an unowned task
  dropped, or Close not reaching the window host.
- **Gap.** Per-window memory, by SD3; CPU is ADR-0351's.

## Milestones

- **M1 — The page, read-only.** SD1–SD3, SD6, `list_running`; unit tests
  and the headless scene's read half.
- **M2 — Close all.** SD4, the filter and the host wiring; the scene's close
  half.
- **M3 — The Window menu.** Running windows (SD1); its own scene.

Deferred: other processes; tracking timer-driven jobs.

## Status

Accepted 2026-11-01, with M1 to M3 implemented. CPU per window is
[ADR-0351](./0351-what-a-window-costs-beyond-frame-cpu-by-window-label-and-a-slow-window-notice.md)'s.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0188](./0188-app-instance-effect-tracking.md) — instance-scoped effects and the live effect tables.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — one table per `keelson.query` statement.
- [ADR-0260](./0260-app-center-one-page-per-app.md) — the app center.
- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) — one render goroutine; `frame_times`.
- [ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md), [ADR-0346](./0346-imzero2-tiled-desktop-dynamic-layouts-over-floating-windows.md) — the window host; the deferred window list.
- [ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md) — the drain that tasks hold.
- [ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md) — system apps' catalogs; what stays the person's.
