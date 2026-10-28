---
type: explanation
audience: operator
status: draft
title: Reading the log viewer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the log viewer

The viewer reads keelson's process-wide `logbridge.Sink` — a retain-N
tail ring fed by zerolog. Every panel below works against the same
tail snapshot, so what you see updates atomically per frame.

## Top strip — counters

Six soft-tone badges:

- **Tail** — rows currently retained in the ring vs ring capacity.
- **Decoded / Written** — totals since process start.
- **Dropped** — rows the ring evicted because the consumer fell
  behind. Non-zero is a yellow warning; chronically non-zero means
  the tail is too small or the producer too fast.
- **ParseErrs** — events the sink couldn't decode. Always red when
  non-zero; the detail pane shows the offending payload.

## Filter row

Three controls, ANDed together:

- **Level dropdown** — minimum severity. `trace` keeps everything;
  `warn` drops debug/info; `error` drops warn and below.
- **App id substring** — case-insensitive match against the row's
  `AppId`. Useful when one app is noisy.
- **Message substring** — case-insensitive match against the formatted
  log message.

## Table

A virtualised tail table — only visible rows are rendered, so the
panel stays responsive past 10⁵ retained rows. Click any row to open
the **detail pane** below; the panel resizes to whatever you drag,
and stays open until you click the row again.

## Detail pane

For the selected row:

- Structured fields rendered hierarchically via the `fieldview`
  widget — bytes columns truncate gracefully, nested error contexts
  expand on demand.
- The wrapped-error chain (when the row carries one) renders via the
  `errorview` widget so each `eh.Errorf` wrap appears with its
  stream-of-facts.

## For an agent

The viewer declares an operations catalog (ADR-0345). A task the window
is shared with reads the tail with `list_rows`, filtered by its own
level floor and substrings rather than by the filter row, and one row in
full with `get_row`; `get_counters` returns the strip above and
`get_view` the filter row, Follow and the selection. `set_filter`,
`set_follow` and `select_row` change what the window shows and nothing
else. Rows are text the process wrote and reach a model marked
untrusted. A row's key is valid while the row is in the ring; a row that
rolled out may still be in `boxer.facts` when `BOXER_LOG_FACTS` is set.
