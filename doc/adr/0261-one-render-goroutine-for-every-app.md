---
type: adr
status: proposed
date: 2026-09-25
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0261: One render goroutine for every app — stated, checked and measured

## Context

A keelson host draws every open window from one loop. `application.Run`
calls the render handlers in turn on the goroutine that called it, and the
window host's `Frame` calls each open app's `AppI.Frame` inline, one after
another. The chrome, the menus and the file dialog draw on the same loop.
The frame leaves as one ordered stream of FFFI messages over one pipe to one
egui `Context`, which the client interprets on a single thread in another
process.

This has been true since the window host existed. It was never written down,
and nothing enforced it. Several things depend on it:

- **The wire carries order.** Id-stack scopes, deferred-block captures
  (`BeginCapture` / `EndCapture`) and the frame-end `Sync` only mean
  something in the order they were sent. The FFFI channel has no lock. A
  call from a second goroutine interleaves into the frame the loop is
  writing, and the client reads a stream neither side wrote.
- **The frame state is process-global.** The current FFFI channel
  (`typed.SetCurrentFffiVar`), `c.CurrentApplicationState`, the style
  tokens and the density preset are globals. They are not handed to each
  call.
- **App code relies on it without saying so.** An app's `Frame` reads its
  own fields without a lock, because nothing else draws while it runs. The
  window host releases its own mutex before calling `Frame`, so an app can
  re-enter the host (open another window) on the same goroutine.

Apps already move slow work off the frame. Queries, file walks and fetches
run on bgjob and keelson tasks, and their results land in the next frame.
The app center's poller and the clipboard broker's drained queue are
examples. The fetcher opcodes are held to frame end for the same reason
(the `imzero2-fetchers` skill).

What was missing is visibility. When one app's `Frame` is slow, every
window waits for it, and nothing said which app that was.

## Decision

We will keep one render goroutine for every app in a host, and make the rule
part of the app contract:

- **SD1 — The contract.** `AppI.Mount`, `AppI.Frame`, `AppI.Unmount` and
  every imzero2 call run on the render goroutine, the one running
  `application.Run`.
  Work that blocks or takes long runs elsewhere and hands its result to a
  later frame. `AppI`'s doc comment states the rule.
- **SD2 — The check.** The FFFI channel can be bound to the render
  goroutine (`Fffi2.BindToCurrentGoroutine`). Once bound, a send, a capture
  scope or a receive from any other goroutine panics on the offending
  goroutine, so its stack names the caller. The check is opt-in through
  `IMZERO2_RENDER_GOROUTINE_CHECK`, because Go exposes no goroutine id.
  Reading one means formatting the stack header, about a microsecond per
  message. The scene runner and the integration lane leave it off; it is
  switched on by hand when a violation is suspected.
- **SD3 — The measurement.** The window host times each window's `Mount`
  and `Frame` on the render goroutine, counts the FFFI messages each
  `Frame` produced, and times the loop's period. It publishes these as the
  live introspection table `keelson('frame_times')`: one `loop` row and one
  `window` row per open window, with quantiles over recent frames.
  The app center reads the table as its "Frame time" section, one app's
  windows against the loop's median period.
- **SD4 — Shutdown reaps on the render goroutine.** SIGINT and SIGTERM ask
  the loop to stop at its next frame boundary, and the windows are reaped
  by the runtime's `Close` after the loop returns, on the same goroutine.
  The signal handler reaps from its own goroutine only when the loop has
  not returned within the shutdown grace period. A loop that does not
  return is wedged, most likely blocked on a client that no longer answers,
  so the loop is waiting rather than drawing. Reaping there still saves the apps' state
  before the forced exit.

## Alternatives

- **Run app frames in parallel, each into its own buffer, and join the
  buffers in window order.** Rejected. It rebuilds the ordering the single
  goroutine gives for free, and every app and every shared global would
  have to become safe for concurrent use. It would not help against a slow
  or hung app either, because the frame still waits for every app to finish.
  If Go-side frame cost ever dominates with every app keeping heavy work off
  the frame, per-window encoding in parallel can be revisited. SD3's table
  is how that condition would be seen.
- **Isolate apps from each other with goroutines.** Rejected. A goroutine
  shares the process's globals and cannot be preempted by the host, so it
  isolates neither faults nor stalls. Isolation that holds needs a process
  boundary, which the multi-tenant display and in-browser worker notes
  under `doc/adr-background-work/` explore.
- **Lock the FFFI channel.** Rejected. A mutex makes interleaved messages
  atomic one at a time, but a frame is a sequence: a second goroutine's
  message would still land inside another app's scope.
- **Always-on check, or on by default in scenes and the integration lane.**
  Rejected. The cost per message is small but paid on every message of
  every frame, and the owner chose to keep the check a tool that is
  switched on by hand rather than a default.
- **`runtime.LockOSThread` on the render goroutine.** Not needed. The
  constraint is one goroutine, not one OS thread: the Go side makes no
  thread-affine call, since egui runs in the client process.

## Consequences

### Positive

- The rule an app has always been under is written where an app author
  reads it, and a scene or test run with the check on turns a violation
  into a panic at the caller.
- A slow app is visible by name, in the app center and in SQL over
  `keelson('frame_times')`, without a profiler.

### Negative

- The check is opt-in, so a violation on a path no checked run exercises
  still goes unseen.
- The timings are Go-side wall time: building the app's messages, and
  writing any that go straight to the pipe. The client's interpretation and
  paint are not in them, and neither is the chrome. The loop row's period
  includes the paint, the wait for the client's reply and any idle time the
  render cadence leaves, so it is a budget, not a sum of the window rows.
- Messages captured into a deferred block count toward the app that
  produced them. The parent message the block is later spliced into counts
  as one more, toward whoever sends it.

- Two shutdown paths still reap without the render goroutine. The first is
  the wedged-loop fallback of SD4. The second is the flight recorder's
  signal flag: armed for SIGINT or SIGTERM, it writes its trace and exits
  from its own goroutine. That handler always raced the old reap-first
  ordering. With the reap moved after the loop, it now exits before any
  reaping, so a run with that flag saves no app state on the signal.

### Neutral

- A one-app host that draws through a renderer adapter rather than the
  window host (the screenshot tour) has no window rows. The table is
  registered only beside a window host, like `keelson('windows')`.

## Status

Proposed 2026-09-25. SD1–SD4 were built the same day. The app center's page
drew its "Frame time" section headless, with the check switched on and no
panic. The scene runner's SIGTERM ended the host through SD4's normal path.
The owner's answers:

- **Q1.** Should the check be on by default in the scene runner and the
  integration lane? **No** — it stays opt-in (SD2).
- **Q2.** Should shutdown reaping move onto the render goroutine, so
  `Unmount` can join the contract? **Yes** (SD1, SD4).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.
