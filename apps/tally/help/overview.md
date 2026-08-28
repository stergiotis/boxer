---
type: explanation
audience: operator
status: draft
title: Browsing lading snapshots
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Browsing lading snapshots

tally is the browser for the lading store (ADR-0198): the mounts it holds,
each mount's complete snapshots, and one snapshot at a time as a file tree.
It reads; it never writes. There is no rename, delete or upload anywhere in
it, because a snapshot is written once and never changes — the only
mutations a store knows are taking another snapshot and letting retention
expire one.

**Mounts** (left) lists every mount the store holds, by the name its policy
record declared or by its id, with its newest complete snapshot. Pick a mount
to browse it; pick a snapshot underneath to pin the pane to that instant, or
leave *follow latest* on to stay on the newest one.

**Pane A** is the browser: a directory as a sortable list, or the tree below
it as an outline. Double-click or Enter enters a directory, Backspace goes
up, the arrows move the cursor, ctrl-click adds to the selection. Selecting a
file shows it in **Preview** (text with highlighting, markdown, JSON, images,
a waveform player for a recording, a hex dump for anything else) and its
recorded attributes in **Info** — size, times, mode, the BLAKE3 hash, whether
the content was stored inline, referenced or not at all, and the day the
snapshot expires.

A recording — `.wav`, `.flac`, `.mp3`, `.opus` and the rest — previews as a
waveform with a transport: Space or *Play* starts it, clicking the waveform
seeks, dragging pans, the wheel scrolls and ctrl-wheel zooms. Playing needs the
whole recording, so it is read out of the store once, up to a limit the app
names when a file is over it; the copy it works from is encrypted or held in
memory that has no name, and it is released the moment you select something
else. There is no sound if the host has no audio device — the playhead still
moves, and the reason is under the waveform.

Snapshots are taken outside the app: `boxer fs snapshot --mount <id> <dir>`,
or `ladingingest.Snapshot` from Go. The `lading` sqlapplet book carries the
same questions as SQL — ledger, find, content search, history, diff, du,
problems, audit — for anything this browser does not show.
