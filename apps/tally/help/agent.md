---
type: reference
audience: operator
status: draft
title: Operating tally as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating tally as an agent

tally declares an operations catalog (ADR-0269, keelson-app A1): an agent
reads an open window's panes and listings, searches the store with the
Find tab's own statements, and moves the view — a pane's location and
selection, the raised tab, the Results query and its run. The resources are
`panes` and `query`; `keelson('operations')` lists the operations with their
arguments, and this page says how to use them together with the two other
routes: opening the window in a chosen state through a launch config, and
running the same SQL in play, where the `lading` book carries every question
as a pasteable applet. Nothing in the catalog writes to the store.

## The catalog

Queries answer from the frame's snapshot and take their own arguments; the
person's quick filter and target pane are not an argument's default.

| Operation | What it does |
| --- | --- |
| `get_view` | the panes (location, selection, listing size), the store's mounts with their ids, the tab names, the Results query and where its run stands, whether the window is connected |
| `list_entries` | one pane's directory as the browser last drew it (`Pane` A, B or R; `Limit` default 50, at most 200); `Truncated` says the listing holds more |
| `get_selection` | one pane's selected file with the mount and snapshot it is read from |
| `find` | the Find tab's name search as an external read: a regex battery over the path (space-separated, case-insensitive RE2 tokens that must all match; a token that does not compile matches literally and `Literal` says so), an extension, a minimum size and a scope — `dir`, `snapshot` or `all` — scoped to a browse pane's location; largest first, at most 500 rows, `Truncated` past that, and the statement in `SQL` to run uncapped in play |
| `grep` | the Find tab's content search: an RE2 pattern over each line of the files the store marked text, with line numbers, at most 1000 rows |
| `set_location` | point a browse pane at a mount (an id from `get_view`), a snapshot (RFC 3339 with nanoseconds, or `latest`) and a directory; a part left empty keeps the pane's; the pane lists in a later frame |
| `select_entry` | select a path from the pane's listing; a path the listing does not carry is refused; an empty path clears |
| `show_tab` | raise a dock tab by name: `paneA`, `paneB`, `results`, `preview`, `info`, `history`, `diff`, `find`, `du`, `problems`; `results` only while the window carries a query |
| `set_query` | set the Results SQL; a statement that does not classify read-only is refused with its classification named, as the launch config's `Sql` is |
| `run_query` | run the Results SQL; asynchronous — the call stays running until the lane lands or fails and answers with the result's state; unavailable while a run is in flight or without a query |

The refusals name their remedy: an unknown pane, a mount not in the store's
list (`get_view` names them), a pane with no location yet (`set_location`),
a path not in the listing (`list_entries`), a tab the window has not got,
a query that is not read-only, a search without a store connection. `find`
and `grep` run against the operator's own ClickHouse credentials with the
caps above and a one-minute bound; the uncapped answer is play's.

The person's clicks on the Mounts list and its snapshots go through
`set_location`, so the command log sees them as theirs and an agent's call
and a click cannot drift (A3); the browser's own selection is a widget
write-back.

## Opening tally on a launch config

A launch request names the app id
`github.com/stergiotis/boxer/apps/tally` and the config kind
`tallyLaunch` (ADR-0135, ADR-0200 §SD9). The config's fields, from
`apps/tally/launchcfg`:

| Field | Meaning |
| --- | --- |
| `MountA`, `MountB` | a mount id in hex, the spelling the SFTP directory uses (`0x` prefix optional) |
| `SnapA`, `SnapB` | a snapshot as RFC 3339 with nanoseconds; empty follows the newest complete one |
| `DirA`, `DirB` | the directory each pane opens, as an io/fs path (`.` is the root) |
| `SelA`, `SelB` | the file each pane selects, under its snapshot; empty selects nothing |
| `Sync` | synchronized browsing across the two panes |
| `Target` | `A` or `B`, the pane the Mounts clicks address |
| `Tab` | the dock tab to raise: `paneA`, `paneB`, `results`, `preview`, `info`, `history`, `diff`, `find`, `du`, `problems`; empty leaves the dock as it opens |
| `Sql` | a path-set query over the lading surface (ADR-0222 §SD3); its rows become the Results pane |
| `SqlLabel` | what to call the result set in the Results pane, not the query |
| `Database` | the ClickHouse database the store's tables live in; empty is the default store |

A field that does not parse is left at its default and the window still
opens on what it could read; `Database` is the exception, because the
connection opens over it and a name with no store behind it fails there. A
selected file implies its directory, so `SelA` alone is enough; a selection
the listing does not carry leaves the pane unselected. `results` as a tab
resolves only while the config carries a `Sql`.

A `Sql` runs once, when the window opens, and only if it is provably a
read: a statement that does not classify read-only is refused with its
classification named, the Results pane shows the message and nothing runs.
The query yields a `path` column, with `mount` and `snap` optional; rows
that name no snapshot are anchored to the target pane's, and rows that can
be placed nowhere are counted and dropped. The pane holds at most 5000
entries whatever `LIMIT` the statement wrote, and says so past the cap.

## Reading the window back: the workingset

tally composes a workingset (ADR-0148): the same `tallyLaunch` record,
written when the window closes, with the two panes' locations and
selections, the sync flag, the target, the last tab the window raised and
the query it was opened with. `keelson('workingsets')` carries it, and the
`appstate` book reads it as SQL. A pane that follows latest records no
snapshot, so a restore follows latest too. The dock's own focus is not
readable from the Go side, so the tab reported is the last one the window
raised, not necessarily the one in front.

## Find and content search as SQL

The Find tab builds one of two statements and runs it through the store's
`fs()` and `fsdata()` relations, the same functions the `lading` book
exposes in play. Both patterns are RE2, evaluated by ClickHouse `match()`:

- **Find**, when the content field is empty: files under the scope whose
  path matches the pattern, with the extension and minimum size as further
  predicates, largest first, capped at 500 rows.
- **Content search**, when the content field is set: only files the store
  marked text take part; the pattern is matched against each file's data
  and then against each line, which gives exact line numbers, ordered by
  mount, path and line, capped at 1000 rows.

The scope is the target pane's directory, its whole snapshot, or every
visible mount's newest snapshot (`fs('*')`). Search is an order, not a
state: the same knobs run again on the button, and a search that failed is
re-run rather than remembered. The lane is cancellable from the progress
row. The catalog's `find` and `grep` run the same statements with the same
caps; an agent that needs the uncapped answer runs the query in play, where
the `lad-find` and `lad-grep` applets carry it with the knobs as parameters.

## Disk usage

The du tab runs two queries over the target pane's directory: every
directory's recursive size under the scope, largest first, capped at 300
rows for the table; and the largest files under the scope, capped at 4000,
for the treemap. Below the cap the treemap is exact; above it, it is the
top-N picture, and the table's `bytes` column is the number to quote.

## Problems and the audit

The Problems tab lists the entries the walker could not read, capped at
1000. **Audit every block** is a run the reader orders on purpose: it
recomputes every stored block's BLAKE3 against the digest the walker wrote
and reports the count of blocks and the count that differ. It reads the
whole snapshot's data and costs what the snapshot weighs, so it is not run
on open and is not repeated without a click.

## What is not here

There is no write anywhere in the app, in the catalog or out of it: no
rename, delete, upload or snapshot. Snapshots are taken outside it
(`boxer fs snapshot`, `ladingingest.Snapshot`), and a tree an app computed
reaches it through `ladingadhoc.Publish`.
