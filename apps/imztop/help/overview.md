---
type: explanation
audience: operator
status: draft
title: Watching a host
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watching a host

imztop is a resource monitor in the shape of `top`: a dock of panels over
one host's CPU, memory, disk, network, GPU, sensors, battery and processes,
with a process table underneath. It reads nothing from `/proc` itself. It
subscribes to the system-metrics plane and folds each published bundle into
about ten minutes of in-memory history; the scraper that produces the
bundles runs elsewhere (the host, or an external `sysmetricsd`), so the
cadence shown in the top bar is observed, not chosen. There is no write
side: no kill, nice or signal.

Two things an agent should know before anything else. The window has an
operations catalog, listed by name on the **Operations** page, and what
those operations read is the published sample the frame drew, never this
window's view. And two pieces of state are process-wide rather than
per-window: live data frozen or flowing, and the replay session. Both are
written through one controlling window; the **Operations** page says which
refusal names that.

## The top bar

From the left: the host the bundles come from; **Freeze** / **Go live**,
which holds the last sample on screen for every imztop window at once by
dropping the frames that arrive while frozen, and while replay is on acts on
the replay transport instead; the observed cadence; the trend-smoothing
overlay, which acts on the history plots only and opens with smoothing off;
and **Replay**, which opens a session over the last ten minutes of stored
history. The **Replay** page covers that mode, its transport row and the
availability strip.

## The panels

The dock holds eleven tabs: CPU, Topology, Proc Map, Pressure, Memory,
Battery, Sensors, Disk, Network, GPU and Processes. Each is a tab you can
rearrange; `show_panel` brings one to the front once, after which your next
click wins. What each panel shows and how its numbers are made is on the
**Panels** page.

## The process table

The table lists the latest sample's processes, sorted by one column and
filtered by one box. The default order is the smoothed CPU column
(`CPU%~`), which is steady across brief spikes; the raw column (`CPU%`) is
the last interval's value. The other sort keys are memory, PID, user, name
and the two I/O rates; a header click on the current key flips the
direction. The tree button nests children under parents.

The filter is a regex battery: space-separated, case-insensitive RE2 tokens
that must all match the process name or its command line. A token that
does not compile matches literally, and a line under the box says so, so a
row is not read as a regex result when it is not one. The box commits on
every keystroke, so a `set_proc_view` command while someone is typing in it
is a conflict rather than a race.

## Where the history goes

The in-memory window is what the plots and `get_history` cover. Stored
history exists only where `sysmetricsd --tee` has run; it lands in
`boxer.facts`, and reading more than the window is SQL over that table in
play, not an operation here. Replay reads it back from the same table.
