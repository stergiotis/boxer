---
type: reference
audience: agent operating the window, and the person reading what it did
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The catalog an agent drives under a task grant. Queries read the window's
last read of `keelson('app_state')`, the poller's snapshot, never a
statement of their own; the one command over the view selects an app, and
`refresh` asks the poller to read again. Clearing an entry or forgetting an
app is not here: it is the person's act at the window.

## Resources

- `selection` — the app whose entries the right pane lists; empty for
  none. It is restorable: the host may put a selection back.
- `read` — the window's last read of the table: when it landed and what it
  reported. The poller writes between frames, so the window latches the
  value at the top of each frame and a read that lands is attributed to
  the app, not to the person.

## Queries

### `list_entries`

Lists what apps keep, from the last read, filtered by the arguments.

- `App` — a case-insensitive substring of the app id; empty keeps every
  app.
- `Kind` — the entry kind, exactly: `persist`, `workingset` or
  `column_width`; empty keeps every kind.
- `Limit` — entries to return, in table order (app, kind, key); 0 is 50, at
  most 200. A negative limit is refused: "limit is not negative".

The result carries `Matched`, how many entries of the last read match the
arguments whatever the limit, `Entries`, the first `Limit` matches, each
with kind, app, key, entity id, payload bytes, the table's detail cut at
512 characters, when it was written, the run and the window that wrote
it, and `ReadAt`, when the window last read the table; zero before the
first read. The output is marked untrusted: keys and details are what apps
wrote.

### `get_view`

Reads the selection and the state of the window's read: `Selected`, how
many `Entries` and `Apps` the last read held, `ReadAt`, whether the window
`Reads` at all (false where the host minted no bus for it), the last read's
or verb's `ReadError`, the last verb's `Note`, and the verbs `Inflight`.

## Commands

### `select_app`

Selects the app whose entries the right pane lists; a view effect. An
empty id clears the selection. An id no app kept state under at the last
read is refused: "no app of that id kept any state at the last read;
list_entries names the apps that did". The person's gesture is a row of
the app list.

### `refresh`

Asks the window to read the table again; a view effect on the `read`
resource. The read lands in a later frame, and `get_view`'s `ReadAt` says
when. The person's gesture is the Refresh button.

## Not offered: delete and forget

Clearing an entry and forgetting an app write the persist store, outside
the window; they would be consequential, and no task has a reason to do
either that the person cannot do faster. So they stay the person's own
clicks, with forget behind an armed confirmation that names the app and
the count. An agent that finds state worth clearing reports it and asks.
