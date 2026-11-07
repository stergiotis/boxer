---
type: explanation
audience: operator
status: draft
title: Replay
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Replay

Replay plays stored history back through the same panels (ADR-0197). The
bundles come from the ClickHouse server holding `boxer.facts`, where
`sysmetricsd --tee` wrote them; they are folded by the same code the live
sampler uses, so the plots, the per-process smoothing and the published
sample are the same things over a different source. The session is
process-wide, like Freeze: one session serves every imztop window, and all
of them are on the same historical cursor.

## The four states

The session is in one of four states, which `get_view` reports as
`Replay.State`:

- **off** — live data; no session, no connection.
- **opening** — a session is being opened on its own goroutine. Opening
  dials the server and verifies the schema, so it is never instant and
  never on the render thread. The live panels stay on screen meanwhile, and
  the transport row shows a spinner, *Opening replay…* and **Cancel**.
- **on** — the session is playing, paused, or parked at the end of its
  window. The banner reads *REPLAY — stored history for `host`, not live*.
- **failed** — opening did not succeed; the status carries why. The banner
  reads *REPLAY UNAVAILABLE — showing live*, the transport row shows the
  reason and **Dismiss**, and the panels keep drawing live data.

What moves them: **Replay** in the top bar, or `enter_replay`, takes off
(or failed) to opening. The open completing takes opening to on; the open
failing takes it to failed. **Go live**, **Cancel**, **Dismiss** or
`leave_replay` take any state to off, and an open still in flight then
discards itself rather than installing. A window going away while its open
is in flight gives the claim back and leaves the session off. Entering
while a session is opening or on is a no-op from the button; the command
says why on the **Operations** page.

## "Not recorded" and "waiting"

An empty frame has three causes, and they call for different things, so
the window does not say the same thing for each:

- *Imztop: waiting for first sample…* — live, and no bundle has arrived
  from the scraper yet.
- *Replaying — waiting for the first stored sample…* — the session is on
  and playback has not reached the first bundle in the window yet.
- *No stored history for `host` in this window.* — the session is on and
  the transport reached the end of the window without folding a single
  bundle. `get_view` reports this as `Replay.Empty`, and the banner reads
  *REPLAY — nothing recorded for `host`*. It is not an error: a host the
  tee never ran for is a host with no history. Jog to an earlier window, or
  leave replay.

A panel can be empty in replay for a reason about the recording rather than
the machine: the tee stores no sensors kind, so the Sensors tab in replay
says *Temperatures are not recorded — replay shows only what the tee
stored* instead of reading as a host with no temperatures. `get_hardware`
returns no sensors in replay for the same reason.

## Two clocks

The bundles carry the times they were recorded at, and the plot axis and
the cadence readout report that run, not this playback. Wall-clock pacing
is therefore free to mean speed. A gap between two stored bundles is
compressed to at most two seconds, so a break in the recording reads as a
pause in the plots rather than a hung window.

## The transport row

While the session is on, a row under the top bar holds, from the left:
**Go live** (first, so it is never clipped off the right edge of a narrow
window), **Pause** / **Play**, **Step** (one bundle forward, only while
paused), the **speed** combo (0.5×, 1×, 2×, 5×, 10×, 60×), **earlier** and
**later** (jog the window one span back or forward; later is clamped so it
cannot run past now), the window being replayed and *at HH:MM:SS*, where
playback has got to. Freeze in the top bar acts on this transport while
replay is on.

## The availability strip

Below the transport is a timeline showing where stored history is, so a
window is picked against a picture of the data rather than named blind. It
opens on the last six hours; pan and zoom are this window's own. The shaded
bands are contiguous stretches with recorded rows, the rug under the axis is
the mean CPU busy per bin (so the strip answers "when was something
happening", not only "when was anything recorded"), and the caret is where
playback is. Dragging the strip under the axis brushes a range and seeks
the session to it; a window set by the jog buttons is mirrored onto the
brush. The coverage behind the strip is loaded off the render thread for a
range padded around the view, refreshed at most every 400 ms as the view
moves, and dropped when replay ends so a new session cannot briefly draw the
previous host's coverage. Its legend says *availability unknown — the
coverage query failed* when the query failed, and *no stored history for
`host` in this span* when it found none. `get_coverage` returns the same
runs.
