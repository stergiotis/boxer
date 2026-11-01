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
does not carry left out. Under it, the row's exceptions, its mark and the
mark buttons (below). The tabs:

- **fields** — every column of the row; for an audit event its outcome,
  principal, entity, the `from` claim, its attributes and references.
- **previous** — for an audit event naming an entity, the previous event of
  the same domain and entity within `BOXER_TRAILVIEWER_LOOKBACK` (30 days by
  default), as a table of field, before, after and the change; for an agent
  grant, the previous grant of the same task.
- **context** — the rows just before and after this one in the same writer
  run, whatever the filter hides. A click selects one.
- **compare** — this row against a second one (below).

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

Beside the tabs, **Compare with…** picks a second row: press it and click a
row, or Ctrl- or Cmd-click a row in the table. The **compare** tab then
diffs the two over the timeline's columns and, when both are audit
events, their own columns and attributes. The line above the table says
which row is which; the older row is always on the left.

For an **entity** case, the line above the table and the top of the
**fields** tab give the entity's state as of the window's end: in each
domain the entity has events in, the last event of the entity at or before
that instant, within the look-back, and when it was.

## Exceptions and rules

The pane beside the table switches between **Facets**, **Exceptions** and
**Rules**. The rules are the viewer's, shipped as data; each raises rows
worth a look, with a one-line reason:

- **outcome** — a denied, failed, timeout or error outcome.
- **model-refused** — a model call the provider refused.
- **grant-widened** — a task's grant widened.
- **trail-row** — the trail's own rows: a gap, an invalid row, the
  recorder's configuration, a ditch.
- **unsealed** — a row of a writer run whose evidence line reads broken,
  fork or gap. The line names no rows, so every row of the run is listed.
- **novel-action** — a domain.action the trail did not record in the
  look-back before the window.
- **claim-differs** — a `from` claim the previous-state read finds
  differs. The read runs for at most sixteen new rows per refill.

The rules run over the rows the window holds: its case, range and filter,
up to `BOXER_TRAILVIEWER_ROWS`. **Rules** shows each rule's predicate,
written over the marts' column names, and how many open exceptions it
raised. **Exceptions** lists them newest first. Click one to select its
row. The table's second column shows a row's rules, or its mark once it
has one. The rules make a reading list. They decide nothing and do
nothing.

## Review marks

The detail pane's **reviewed**, **flagged** and **dismissed** buttons mark
the selected row, with the reason typed beside them. A dismissal needs a
reason. The mark is written to the trail itself as an audit event in the
`review` domain: one reference `trail-row:<id>`, the reason, and the
rules it clears. Its principal is a pseudonymous reference derived from
the OS user and the host, not a name. A marked row's exceptions leave the
list, and the mark shows in the table and the detail pane.
`dm_trail_reviews` lists every mark for SQL. Each window shows a row's
latest mark.

## The tree, the matrix and the dotted chart

The bar above the table switches the central area between **Table** and
the analytic views; the detail pane's tabs return to the table. Each view
reads the case, the range and the filter the table last read, again on
every refresh, and states in one line what it shows and whether anything
was cut.

- **Tree** — for a **task** or **conversation** case only: the rows as an
  outline, each hanging off the row its `parent-ref` names (a model call
  under the action that asked for it, an action under its model call),
  oldest first, with elapsed-ms, status and kind beside the headline. A row
  whose parent is not among the rows held is a root, and the line says how
  many. A tree of up to 200 nodes opens fully expanded. A click on a node
  selects its row, and selecting a row elsewhere selects its node.
- **Actor × object** — pick the actor (principal, app or window) and the
  object (subject, entity or operation); the grid has one row per actor,
  one column per object, and the number of rows of each pair in the cell,
  with the pair's last outcome on hover. A principal, a data subject and an
  entity are an audit event's own columns, so a matrix over one of them
  counts the case's audit events only, and says so. The grid shows the
  fifty busiest actors and objects; the line says when there were more.
- **Dotted chart** — one row per run, window, conversation or entity, time
  across, one mark per bucket of the strip's size, larger for more rows (in
  four sizes, the legend gives each size's counts). The forty busiest rows
  are drawn, the busiest on top.

## Saved views

The **Views** pane beside the table keeps the case, the range (as the
picker shows it), the filter and the tab under a name: type it and press
**Save**; saving under a name already there replaces it. **Open** brings a
view back and **Delete** removes it. The list lives in the host's persist
store under the viewer's app id, shared by every viewer window; it is read
when the window opens.

## Send to play

**Send to play** publishes the table as it stands — the rows held, in the
columns shown — as an ad-hoc dataset named `trail_rows`, and opens a play
window reading it, where it can be sorted, searched and queried. There is
no file export. The publish is itself recorded on the trail.

## Opening the viewer on a case

Another app can open the viewer on a case through a launch config: the
case and its value, optionally a range, a tab and a saved view's name.
Chat's Analytics tab has **Open in trail viewer**, which opens it on the
window's conversation over the last week. A config the viewer cannot read
is ignored: the window opens on its defaults and says why under the
controls.

## For an agent

The viewer declares an operations catalog (ADR-0345). `list_rows` lists a
case's rows over a range and filter given as its own arguments, from the
rows the window holds, and says when its arguments reach outside what the
window read; `get_row` reads one row, `get_previous` the selected row's
previous state, `get_verdict` the evidence lines, `get_view` what the
window shows and the saved views' names, `get_exceptions` the open
exceptions of a case over a range with every rule's predicate, and
`get_tree` a task's or conversation's tree in depth-first order.
`set_case`, `set_range`, `set_filter`, `select_row` and `set_tab` (one of
fields, previous, context, compare, tree, matrix, dotted) change what the
window shows and nothing else.
`mark_row` writes to the trail and is not offered to agents: review is the
person's act. Row text reaches a model marked untrusted.
