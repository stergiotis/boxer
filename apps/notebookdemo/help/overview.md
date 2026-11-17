---
type: explanation
audience: end-user
status: draft
title: The embedded notebook demo
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The embedded notebook demo

This window is a host that draws a notebook's live reading in a region of
its own frame (ADR-0380). The row at the top is the host's own: it picks
which notebook is shown and shows what the host reads of the pane — the
identity its runs are attributed to, the reading on screen and how the last
run ended. Under the line is the pane: the same notebook reading the
notebook window and a notebooklet draw.

The two notebooks are embedded the two ways a host has:

- **The report sample, by its slug.** A notebook of a registered book,
  the one the launcher also lists as a notebooklet. It shows what it stores
  until **Run live**.
- **A notebook of the host's own.** A document the app wrote in its code,
  which no launcher lists. It stores nothing, so it runs when the window
  opens.

A run computes every record on the server the `CLICKHOUSE_*` variables
name, read-only, and stores nothing; both notebooks make their own rows, so
any ClickHouse answers them. The pane's line says which reading is on
screen, and a run that fails keeps the earlier one. The host's
capabilities are what a pane needs and nothing of collection.

## Operating the window as an agent

| Operation | Kind | What it does | Refused when |
| --- | --- | --- | --- |
| `get_panes` | query | each pane as the host reads it: its key, identity, reading and last run | never |
| `show_pane` | command, view | shows the pane named by `Key`, `report` or `squares` | the key is neither |
| `run_pane` | command, run | starts a run of the pane named by `Key`; `get_panes` says when it answered | the key is neither; a run is under way |
