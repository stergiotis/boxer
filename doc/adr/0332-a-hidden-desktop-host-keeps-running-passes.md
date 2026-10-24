---
type: adr
status: proposed
date: 2026-10-24
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0332: A hidden desktop host keeps running passes, and a window that stops drawing says so

## Context

[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD4 ties an agent's calls to the window's frame. A command is queued and
applies in the window's next frame; a query reads the snapshot that frame
took. While a task is attached, the host asks for a repaint at a bounded
interval, so a queued command waits at most that long. That bound holds only
while the client paints.

The desktop client is eframe over winit. On Wayland, winit withholds
`RedrawRequested` from a window while the frame callback it asked for at the
last present has not arrived, and a compositor sends none to a surface it
does not show. A window that is minimised, on another workspace, or behind a
locked screen therefore gets no redraw. eframe runs an egui pass only on a
redraw. The Go host waits inside its frame for the client's reply
(`StateManager.Sync`), so no window's frame runs at all, and every agent call
waits on the person bringing the window back.

What an agent saw of this, on a host whose window was not shown
(2026-10-24):

- a command answered `accepted`, the coordinator's wait ended, and it stayed
  queued for minutes. The agent could not tell this apart from a call the
  host had dropped, and it reported a working publish as broken;
- a window that had drawn once, but never with a task attached, refused every
  query with "the window has not drawn yet". The window list said `ready`;
- a window that had answered earlier kept answering from a snapshot minutes
  old, with nothing marking its age;
- each of those refusals of a call made without `args` also carried the
  coordinator's hint that arguments go under `args`. That hint is irrelevant
  to an operation that takes none.

A measurement on GNOME on Wayland (2026-10-24, Mesa 26.0 radv, eframe 0.36.2,
winit 0.30.13, wgpu 30) used a minimal eframe app that minimises itself and
asks for a repaint every 100 ms. Over the 6.5 s after it was hidden, stock
winit ran 1 pass. With winit's wait for the frame callback bounded at 250 ms
it ran 26, at most 0.25 s apart. The present did not block on the hidden
surface. A running host was seen with its render goroutine waiting on the
client for four minutes while its window was not shown.

## Design space (QOC)

**Question.** How does an agent's call make progress while the desktop does
not show the host, and how does the agent learn that it is waiting?

**Options.**

- **O1** — bound winit's wait for a Wayland frame callback, in a vendored
  copy. Past the bound a pending redraw is delivered, so the client keeps
  passing.
- **O2** — apply queued commands and take snapshots outside a drawn frame,
  while the render goroutine waits for the client.
- **O3** — extend eframe's direct-paint path for invisible windows, which
  exists for Windows hosts, to a Wayland window whose frame callback is
  overdue, in a vendored copy of eframe.
- **O4** — run the desktop client on X11 (Xwayland), which does not throttle
  redraws to frame callbacks.
- **O5** — leave frames to the desktop, and tell the agent when its window
  has stopped drawing.

**Criteria.**

- **C1** — a call makes progress without the person.
- **C2** — the person's input keeps priority over a command (ADR-0269 §SD4's
  order: write-back first, then commands).
- **C3** — one render goroutine owns app state
  ([ADR-0261](./0261-one-render-goroutine-for-every-app.md)).
- **C4** — the cost of carrying the change across dependency bumps.
- **C5** — a shown window behaves as before.

| | C1 | C2 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- |
| O1 | + | + | + | − | + |
| O2 | + | − | − | + | + |
| O3 | + | + | + | −− | + |
| O4 | + | + | + | + | − |
| O5 | − | + | + | + | + |

O2 fails C2. A command applied between a frame's body and its write-back can
be overwritten by that write-back, which is the race §SD4's order exists to
prevent. It also strains C3: the render goroutine is blocked in a read from
the client, so serving commands there needs that read made interruptible. O3
vendors eframe, which is a member of the egui ring that moves on every egui
release. O4 gives up Wayland-native scaling and input for every person in
order to serve agents. O5 alone leaves C1 unmet. It is kept as the floor for
the stalls O1 does not remove: a client that hangs, a suspended machine.

## Decision

O1, with O5 beside it.

### SD1 — The desktop client keeps running passes while its window is not shown

The imzero2 desktop client carries winit with a bounded wait for a Wayland
frame callback (`FRAME_CALLBACK_BOUND`, 250 ms). After the bound, a pending
redraw is delivered and the event loop wakes for it. A window the compositor
shows gets its callback within a refresh interval, far inside the bound, so
nothing changes for it. A hidden window passes at the cadence the Go side
asks for, but no faster than once per bound. With the reactive idle heartbeat
that is one pass a second. While a task is attached, ADR-0269's repaint
interval gives about four a second.

The copy and its deltas live under `rust/imzero2/vendor/winit`, whose
`VENDORING.md` is the re-sync procedure.

### SD2 — The engine knows when its window last drew, and says so

The operations engine records when each frame begins. Once a window has gone
`StallAfter` (5 s) without a frame, what the engine answers says so:

- the status of a command still queued names the wait: queued for the
  window's next frame, which has not come for the given time;
- a query answered from an older snapshot carries the same note in its
  reason;
- a query that no frame answers is refused with it. "The window has not
  drawn yet" stays for a window that never drew;
- the host's window list carries the time without a frame as `StalledSecs`.

### SD3 — The coordinator words a wait that ended without an outcome

A call still `accepted` when the coordinator's wait ends says it is still
queued and applies at the window's next frame. The hint that arguments go
under `args` follows only a refusal of an operation that takes arguments: one
whose schema declares a property, or whose refusal names the schema.

### SD4 — Deferred

- **A signal to the person** — for example a desktop notification — when a
  task's call waits on a stalled host. Reopened if stalls that SD1 does not
  remove turn out to be common.
- **The browser-tab host.** Browsers stop animation frames in a background
  tab, which stalls the same way. SD2 reports it; nothing keeps it passing.
- **Offering the bound upstream** to winit, and dropping the vendored copy
  once a release carries it or something equivalent.

## Milestones

- **M1 — The stall is reported (SD2, SD3).** ✓
- **M2 — The desktop client passes while hidden (SD1).** ✓
- **M3 — Checked on a desktop host with an agent's task.** Minimise the host
  while a task is attached: a command applies within about a second, and no
  answer carries a stall note.

## Surfaces — Tier 1

| Surface | Change | Notes |
| --- | --- | --- |
| `opengine.Engine` | added `SinceDrawn`, `Stalled`, `StallAfter`; stall notes on status, query and refusal | SD2 |
| `opwire.InstanceInfo` | added `Stalled` | SD2 |
| `agent.Instance`, the list wire | added `StalledSecs` | SD2 |
| chat coordinator | the still-queued wording; the args hint gated on the operation's schema; `list_windows` describes `StalledSecs` | SD3 |
| `rust/imzero2` | `[patch.crates-io] winit` to `vendor/winit` | SD1 |

## Alternatives

O2–O4, above, with their kill reasons.

## Consequences

### Positive

- An agent's work in a window that is not shown proceeds instead of waiting
  for the person.
- An agent can tell a slow window from a dropped call.

### Negative

- A 2.7 MB third-party crate is carried, and has to be re-applied when eframe
  moves to a new winit minor.
- A hidden host renders: about once a second when idle in reactive cadence,
  about four times a second in continuous cadence or while a task is
  attached, where it used to render nothing.

### Neutral

- A shown window behaves as before.
- The browser host does not link winit; its digest is unaffected.

## Migration — Tier 1

None. The new wire field is omitted when zero, and readers that do not know
it ignore it.

## Verification plan — Tier 1

- `opengine`: a stalled window's queued status, its query from an old
  snapshot, and its recovery on the next frame
  (`TestAStalledWindowSaysHowLongItHasNotDrawn`).
- `apps/chat`: the args hint follows only an operation that takes arguments
  (`TestTheArgsHintFollowsOnlyAnOperationThatTakesArgs`). The preservation
  goldens carry the `list_windows` description.
- The hidden-window check in `vendor/winit/VENDORING.md`, on a Wayland
  desktop, when the vendored copy is re-synced.
- M3, by hand.

## Status

Proposed 2026-10-24.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  §SD4 — the frame that commands and queries are tied to.
- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) — the render
  goroutine rule O2 strains.
- [`rust/imzero2/vendor/winit/VENDORING.md`](../../rust/imzero2/vendor/winit/VENDORING.md)
  — the deltas and the re-sync procedure.
- [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) §1.10 — the licence
  record.
