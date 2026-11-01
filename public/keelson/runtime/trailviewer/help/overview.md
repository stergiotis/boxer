---
type: explanation
audience: operator
status: draft
title: Reading the trail viewer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the trail viewer

The viewer reads the audit trail on `boxer.facts` through the trail
marts (`dm_trail_timeline` and the per-kind views beside it). It needs
ClickHouse: on a host whose trail is not persisted the window shows one
line saying so, and why, and nothing else. The views must be installed on
the server — the host's SQL surface install does that; a server without
them answers each read with an error under the controls.

The window re-reads in the background every `BOXER_TRAILVIEWER_REFRESH`,
when you press **Refresh**, and shortly after you change the case, the
range or the filter. Drawing the window never queries ClickHouse. Its own
reads are stamped with the viewer's identity, so they appear in the trail
as query runs too.

## A case

A case is one key and one value, read over one time window. The kinds:

- **run** — every row one writer run wrote (the window opens on this
  process's run).
- **window** — `run:instance`, every row of one window.
- **app** — every row one app wrote, across runs.
- **conversation** — the agentic rows of one conversation, and the audit
  events that reference it.
- **task** — the rows of one agent task.
- **subject**, **principal**, **entity** — audit events about one data
  subject, caused by one principal, or about one entity (`type:value`, the
  event's first reference).
- **destination** — egress fetches to one host.
- **all** — every row in the window, for accounting.

Pick the kind, type the value and press **Apply**. A read is always bounded
by the time window. Ranges of the form "last N units" move with the clock
on every refresh; a range such as "Yesterday" is evaluated once into fixed
times. Times are shown in the picker's zone.

## The strip

Bars count rows per time bucket, stacked by kind or by status; the legend
names every series. The line beside the button says how many rows, how
many buckets of what size, and which bucket was busiest. A click on a bar
narrows the window to that bucket.

## Table and facets

The table holds the newest rows of the case — at most
`BOXER_TRAILVIEWER_ROWS` at a time; scrolling to the end reads the next as
many. **Group by parent** folds rows under the model call that asked for
them. The facets pane lists, for kind, status, app, domain.action and
principal, the values in the case and window with their counts. A click
keeps only rows with the value, a right-click or a click with Ctrl, Cmd or
Alt removes them, and a click on a value already in the filter takes it
out. The filter is always written at the top of the pane.

## The evidence line

Above the table, one line per writer run among the rows (the first eight)
says what the run's sealed record shows:

- **holds** — the seals chain and name every row.
- **broken at ordinal N: reason** — the seal stream breaks at seal N.
- **gap: K rows no seal names** — rows are on the store that no seal
  covers.
- **fork: two seals follow one** — the stream forks.
- **unanchored since ordinal N** — the anchored heads break from seal N on.
- **unverified** — nothing could check the run: the trail is not durable,
  or the check failed; the reason follows.

This process's run is read from `keelson('evidence_checks')`, other runs
from the recorder's seal verification. The table's first column carries
the same mark for every row; a run that does not hold marks its rows and
never hides them.

## The detail pane

Click a row to open it. The line under the header is the identity chain:
person › window › task › model call › tool call, with the links the row
does not carry left out. Three tabs:

- **fields** — every column of the row; for an audit event its outcome,
  principal, entity, the `from` claim, its attributes and references.
- **previous** — for an audit event naming an entity, the previous event of
  the same domain and entity within `BOXER_TRAILVIEWER_LOOKBACK` (30 days by
  default), as a table of field, before, after and the change; for an agent
  grant, the previous grant of the same task.
- **context** — the rows just before and after this one in the same writer
  run, whatever the filter hides. A click selects one.

The previous tab's verdict is on the row's own `from` claim:

- **matches the previous event** — the claim is the value the previous
  event held for a field this event changed.
- **differs from the previous event** — it is not; the previous values are
  named.
- **no previous event within the look-back** — nothing to compare against;
  where the row says its state before came from a default, that is shown.
- **a gap row lies between** — the recorder dropped rows of this run in
  between, so the previous event may be among them.

A row with a previous event and no `from` attribute makes no claim, and the
table is shown without a verdict.

## For an agent

The viewer declares an operations catalog (ADR-0345). `list_rows` lists a
case's rows over a range and filter given as its own arguments, from the
rows the window holds, and says when its arguments reach outside what the
window read; `get_row` reads one row, `get_previous` the selected row's
previous state, `get_verdict` the evidence lines and `get_view` what the
window shows. `set_case`, `set_range`, `set_filter`, `select_row` and
`set_tab` change what the window shows and nothing else. Row text reaches a
model marked untrusted.
