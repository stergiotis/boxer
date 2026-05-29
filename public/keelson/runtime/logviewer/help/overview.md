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
