---
type: explanation
audience: operator
status: draft
title: Reading the log viewer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the log viewer

The viewer reads the host's log lines from `boxer.facts`. They reach the
table only when the host runs with `BOXER_LOG_FACTS=1` and ClickHouse
answers; otherwise the window shows one line saying logs are not
persisted and why, and nothing else. Whether ClickHouse answers at all is
also on the status line, as "audit trail not durable".

Each window keeps the newest rows that match its filter — at most
`BOXER_LOGVIEWER_ROWS` of them — and re-reads them in the background every
`BOXER_LOGVIEWER_REFRESH`, when you press **Refresh**, and shortly after
you change the filter. Drawing the window never queries ClickHouse.

## Top strip

- **Read hh:mm:ss** — when the window last read its rows, and how long
  ago. Log lines reach the table in the bridge's batches, so the newest
  can trail the console by a flush. It turns amber when the last read
  failed; the reason is written under the strip, and the rows shown are
  from the read before.
- **Refresh** — read now.
- **Rows** — rows the window holds vs its cap.
- **Decoded / Written** — totals since process start.
- **Dropped** — rows the bridge's flush ring evicted because the store
  fell behind. Non-zero is a yellow warning.
- **ParseErrs** — events the bridge couldn't decode. Always red when
  non-zero.

## Filter row

Controls, ANDed together, all applied by the read:

- **Level dropdown** — minimum severity. `trace` keeps everything;
  `warn` drops debug/info; `error` drops warn and below.
- **App id substring** — case-insensitive match against the row's
  `AppId`. Useful when one app is noisy.
- **Message substring** — case-insensitive match against the log
  message.
- **This run / All runs** — this process's lines, or every run in the
  last seven days. The few lines written before the host finished
  starting carry no run and show only under All runs.

A narrower filter narrows the rows already shown at once; a wider one
shows its rows when the read lands.

## Table

A virtualised table, oldest at the top — only visible rows are rendered.
**Follow** keeps the newest row in view. Click any row to open the
**detail pane** below; the panel resizes to whatever you drag, and stays
open until you click the row again.

## Detail pane

For the selected row:

- Structured fields rendered via the `fieldview` widget — bytes columns
  truncate gracefully.
- The error text and stack. A wrapped-error chain is stored in
  `boxer.facts` as its flat summary only, so a row read back shows the
  summary, not the chain's tree.

## For an agent

The viewer declares an operations catalog (ADR-0345). A task the window
is shared with reads the window's rows with `list_rows`, filtered by its
own level floor and substrings rather than by the filter row, and one row
in full with `get_row`; `get_counters` returns the strip above and
`get_view` the filter row, Follow and the selection. Every answer comes
from the window's rows as last read, says when that was, and never waits
on ClickHouse; `list_rows` says when its arguments are wider than the
filter the rows were read under. `set_filter`, `set_follow` and
`select_row` change what the window shows and nothing else. Rows are text
the process wrote and reach a model marked untrusted. A row's key is
valid while the window holds the row. Without `BOXER_LOG_FACTS`,
`list_rows` answers an empty set and says why.
