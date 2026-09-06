---
type: adr
status: accepted
date: 2026-09-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-06
---

# ADR-0222: tally as a composition target — launch arguments, path-set queries, ad-hoc trees

## Context

Any app can hand play a query. `windowhost.open` carries a
[`playLaunch`](../../apps/play/launchcfg/launchcfg.go) config — buffer, tab,
auto-run, endpoint — and [ADR-0134](./0134-adhoc-datasets.md) lets the caller
first publish its own in-memory data, so the query it passes may name rows
that exist nowhere durable. mdedit, writingstylescope, imzrt and the regex
explorer all reach play this way; writingstylescope uses both halves at once.

tally ([ADR-0200](./0200-tally-lading-browser.md)) is on the wrong end of
that. It *sends* to play and nothing sends to it. Its launch config exists,
but it was designed for the workingset ([ADR-0148](./0148-app-workingsets.md)):
two pane locations, a sync flag, a target pane. A caller can say which
snapshot to open and cannot say which file to select, which tab to raise, or
anything at all about *what to look at* — and there is no analogue of an
ad-hoc dataset, so nothing a caller computed can be browsed.

The obvious transcription of ad-hoc datasets does not work. A statement
naming both `keelson('<handle>')` and a lading macro cannot be answered by
any endpoint — the datasets live on this process's introspection plane and
the lading tables on the ClickHouse server — and play's dispatch policy
refuses that combination rather than picking a side. tally's entire surface
is server-side SQL, so its ad-hoc payload has to be server-side too.

## Decision

**SD1 — three seams, mirroring play, and one of them deferred.** play is
composable through a launch contract, a payload the contract can name, and an
embedder API for a hosted instance. tally takes the first two. The embedder
API is deferred: nothing hosts a tally instance, and an API with no caller is
a guess about what the caller wants. Trigger: a first embedder.

**SD2 — the launch contract says what to look at, not only where.** Five
members appended to the `tallyLaunch` cohort (ordinals 150–154, the
append-only rule): `tallyLaunchSelA` / `tallyLaunchSelB` (the selected file
per pane), `tallyLaunchTab` (which dock tab to raise), `tallyLaunchSql` and
`tallyLaunchSqlLabel` (SD3). Existing configs decode unchanged, and the
workingset composes the new fields so a restored window comes back on the
same file and tab.

There is no `AutoRun` flag opposite play's. play seeds an *editor*, where an
unrun buffer is a legitimate state a reader may want to inspect first; tally
has no editor, and a query in a launch config with nothing to run it is not
a state anyone asked for. A config carrying SQL runs it.

**SD3 — a passed query is a path set, and it is checked, not trusted.** The
config's `Sql` is a read over the lading surface yielding a `path` column.
Three more are honoured where the query selected them: `mount` and `snap`, so
an `fs('*')`-shaped query lands its rows on the right snapshots — rows without
them are anchored to pane A's location, and counted and reported when there is
no such location either — and `is_dir`, so a directory row is a directory in
the tree rather than a leaf that pretends to be a file. It expands through
`ladingsql.Expand` under the app's own visibility, like every other query
tally runs.

Two guards, because a launch config is an argument from another app and
tally runs it with the operator's ClickHouse credentials. The statement must
classify read-only (`analysis.ClassifyStatementKind`) or the pane refuses and
says why — ADR-0200 §SD5 makes "reads, never writes" a property of the app,
and a property that survives arbitrary caller input has to be checked. And
the result is capped at a row count the pane names, independently of whatever
`LIMIT` the caller wrote.

**SD4 — the result is a tree, not a table.** The Results pane renders the
rows as a virtual `fs.FS` through the same `fsbrowser` widget as panes A and
B, so Preview, Info, History and the toolbar work on a query result exactly
as they work on a snapshot. Rows spanning several locations get a
`<mount>@<snapshot>/` first segment; a single-location result keeps bare
paths. The pane remembers each path's `(mount, snapshot)`, which is what Info
and History need to resolve a selection.

This is the point of the whole slice: play answers a query with a table, and
if tally answered with one too there would be no reason to pass it here.
Rejected alternatives — overlaying the path set on pane A, which would make
pane A lie about being a real snapshot at a real directory (and the widget's
filter is an RE2 pattern, not a membership set); and seeding the Find tab,
whose shape is pattern/extension/size, not an arbitrary read.

**SD5 — an ad-hoc tree is an ephemeral mount, not a new transport.**
`public/fs/lading/ladingadhoc` publishes any `fs.FS` as a short-retention
lading mount: it mints a mount id under a tag value it claims for the purpose
(ADR-0198 §SD3 makes the tag the application's degree of freedom, and
`ladingsql.VisibleUnderTag` already reads that grouping), records the policy
under the publisher's name, and snapshots through `ladingingest`. Everything
tally does then works on the caller's tree — find, du, diff, history,
preview, the SQL surface, the SFTP head.

The model is ADR-0134's, transposed onto a store that is append-only:

| ad-hoc dataset | ad-hoc tree |
| --- | --- |
| handle | mount id |
| revision | snapshot instant |
| alias | policy name |
| retract | retention class |

Republishing passes the held mount id back and writes another snapshot of the
same mount, so a caller pressing its button twice does not leave two mounts
behind — the handle-reuse rule of ADR-0134 §SD2. There is no retract verb:
lading snapshots are written once and expire, the only mutations ADR-0198
admits, and a delete verb would be a second lifetime model for one table.

The publisher writes with its own ClickHouse credentials, as mdedit's upload
does ([ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md) §SD5), so this
needs no new capability subject. Trigger for one: a publisher that should not
hold those credentials.

Publishing a tree is a publish, not a handoff: the rows are in the store for
the retention class whether or not anyone opens the window.

**SD6 — the reverse affordance, where the condition is checkable.** play
gains "Open in tally", offered when the buffer names a lading macro
(`ladingsql.References` — the same test play's dispatch policy already
applies to route such a statement). It is tally's "Open in play" mirrored,
and it is what makes the pair compositional rather than one-directional. No
launch helper ships beside it: play's own callers hand-roll encode and
`windowhost.RequestOpen`, and tally's leaf config package stays free of the
app runtime for the same reason play's does (ADR-0017 §SD4).

**SD7 — dogfood in the app that already dogfoods the other half.**
`adhocdemo` (ADR-0134's demo) grows a tree half: it builds a small in-memory
tree, publishes it as an ad-hoc mount, and opens tally on a path-set query
over it. One app then demonstrates both ad-hoc shapes side by side.

## Alternatives

- **Answer a passed query with a table.** Rejected: play already does that,
  and if tally answered the same way there would be no reason to pass a query
  here rather than there. The tree is the whole point of the destination.
- **Overlay the path set on a browse pane instead of a Results pane.**
  Rejected: it would make pane A lie about being a real snapshot at a real
  directory, and the browser's filter is an RE2 pattern rather than a
  membership set, so the overlay is not a shape the widget has.
- **Seed the Find tab with the passed query.** Cheapest, and rejected: Find's
  shape is pattern / extension / minimum size, not an arbitrary read, so the
  passed query would be a second-class citizen of a tab built for something
  else.
- **Carry ad-hoc trees as an `adhocdata` dataset of paths.** Rejected because
  it cannot work: a statement naming both `keelson('<handle>')` and a lading
  macro has no endpoint that can answer it, which play's own dispatch policy
  already refuses rather than guesses at.
- **A new bus service carrying a file tree, beside `adhoc.publish`.**
  Rejected as a parallel universe: the tree would reach the browser and
  nothing else — no find, no du, no diff, no SQL, no SFTP — where an
  ephemeral mount reaches all of them for the price of rows that expire.
- **A retract verb for published trees.** Rejected: lading snapshots are
  written once and expire (ADR-0198), and a delete verb would be a second
  lifetime model for one set of tables. Republishing into the held mount
  covers the case that motivated it — a button pressed twice.
- **An `AutoRun` flag mirroring play's.** Rejected: play seeds an editor,
  where an unrun buffer is a state a reader may want; tally has no editor, so
  a query in a config with nothing to run it is a state nobody asked for.

## Consequences

- tally is reachable as a viewer by any app holding a bus, on a location, a
  file, a tab, or a query — the position play has held since ADR-0135.
- The lading store gains a second class of writer. Ad-hoc mounts share the
  tables with recorded ones and are told apart by tag and by the policy
  record, not by a separate table.
- Trees published by a demo or a scratch gesture occupy the store for at
  least their retention class, and appear in tally's Mounts pane meanwhile.
  That is the cost of reusing the store rather than inventing a transport.
- A caller can hand tally SQL that tally executes. The read-only check is the
  whole of the defence; it is a real boundary and is tested as one.
- Deferred, with triggers: an embedder API (a first embedder); a capability
  subject for tree publishing (a sandboxed publisher); result panes that
  span locations more richly than the prefix segment; a retraction verb (a
  caller that cannot wait for retention).

## Status

Accepted 2026-09-06.

## Verification

Vocabulary assignments golden and the launchcfg codec golden pin the new
members. Pure tests cover the launch round-trip through the new fields, the
read-only refusal and its message, the path-set row reader (single- and
multi-location, the cap), and the virtual `fs.FS` — directory synthesis,
`ReadDir` ordering, and `Open` delegating to the right snapshot. `ladingadhoc`
has a unit test over id minting and its argument checks, and an
integration-lane test for publish → browse → republish-into-the-same-mount. A
second integration test runs the whole path-set path against a real store —
publish a tree, query it as a launch config would, read a file back through
the tree the pane browses — which is everything but the render call. That last
step needs a window and stays manual: open adhocdemo, publish the tree, browse
it in tally.
