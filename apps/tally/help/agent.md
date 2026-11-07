---
type: reference
audience: operator
status: draft
title: Operating tally as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating tally as an agent

tally declares no operations catalog yet: there is no `get_` or `set_`
operation to call on an open window, and `keelson('operations')` lists none
for it. What an agent can do is open the window in a chosen state through
a launch config, read the state it keeps through the workingset, and run
the same SQL the window runs through play, where the `lading` book carries
every question as a pasteable applet. This page is the contract of those
three routes.

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
row. An agent that needs the uncapped answer runs the same query in play,
where the `lad-find` and `lad-grep` applets carry it with the knobs as
parameters.

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

No catalog, so no agent can select a file, raise a tab or run a search on
an open window; the launch config is the way to put the window in a state,
and a new window is the way to change it. There is no write anywhere in the
app: no rename, delete, upload or snapshot. Snapshots are taken outside it
(`boxer fs snapshot`, `ladingingest.Snapshot`), and a tree an app computed
reaches it through `ladingadhoc.Publish`.
