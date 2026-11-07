---
type: explanation
audience: end-user, and an agent reading the bundle through the catalog
status: draft
title: The ad-hoc dataset demo
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The ad-hoc dataset demo

This window computes a small series and hands it to the rest of boxer in
the two shapes an app can hand data without storing it: as a **bundle**, rows
with the applet document that reads them, shown in place and queried in
play; and as a **tree**, a snapshot in the lading store that tally browses.
It is the in-tree example of both, and nothing in it is specific to the
series: the same two calls carry any Arrow table and any file tree.

## What a bundle is

A bundle is one applet document and the ad-hoc datasets it reads,
published, republished and retracted as one unit, with its provenance
recorded as data. The datasets are Arrow tables held by the runtime's
ad-hoc data service, sealed and owned by the window that published them;
the document is SQL in the applet dialect that reads them by local name on
the introspection endpoint. Querying, transforming and charting a dataset
happen in play; any other app reads a dataset whole. Here the document is
`SELECT * FROM keelson('items') ORDER BY x` over one dataset, `items`, with
a table tab.

An alias has one owner. A window that may be opened twice publishes under
an alias of its own, so this window's bundle is `adhocdemo_items_w` followed
by its instance key, and a second window publishes a second bundle. The
alias and the revision are in the bar: "bundle *alias* · rev N".

## The window

The first publish starts when the window opens, as a task owned by the
window; until its result lands the pane says "Preparing — waiting on the
bundle publish" with the task's progress row, or the failure with a
**Retry**. When it lands, the pane becomes a **bundle view**: an embedded
play built from the document, with the datasets bound and the query run
once, following the bundle. **Regenerate** computes the next generation of
the series and republishes the bundle under the same alias; the view learns
of the new revision from the service's event, like any other receiver
would, and a click while a publish runs is dropped. Each generation shifts
the sine the series carries, so a regeneration is visible in the table.

The view's toolbar has play's two escape hatches: **Copy** in the
Definition drawer takes the document to the clipboard, and **Open in
Playground** opens the bundle in a play window of its own. Both are
capabilities of this window, declared in its manifest beside the publish
and the view's resolve and event subjects.

## The tree, and the handover to tally

**Publish tree & browse in tally** walks a small in-memory tree — a
README, a note, a CSV of the same series and a nested leaf, each changing
with the generation — into the lading snapshot store as a short-lived
mount named "adhoc demo tree", then asks the host to open tally on it:
pane A on the mount at that snapshot, and the Results pane on a query over
the markdown files in it, largest first. Pressing again writes another
snapshot of the same mount rather than minting a second one, so tally can
diff the two. The bar shows "browsing N entries as mount … · expires
*date*" when it lands.

This half needs a ClickHouse server the `CLICKHOUSE_*` variables name, and
the app runtime to open tally through; without a bus it fails with "no bus
wired — opening tally needs the app runtime", and without the server with
"ClickHouse not reachable". The whole gesture is one task bounded at 30
seconds.

## For agents: the window's own operations

The catalog has two resources of the window's own, `generation`, the
series generation asked for and the one whose publish landed, and `tree`,
the same for the tree with its mount, and three operations over them:

- `get_view` — the bundle's alias, the revision the view shows, the
  generation asked for and the one published, whether a publish runs and
  its last error; the tree's generation, mount, note and error.
- `regenerate` — the Regenerate button: the next generation, republished
  under the same alias as a task. It is asynchronous and settles when the
  republish lands, with the generation, the alias and the revision the
  view shows at that moment; the view follows the service's event, so the
  revision may still be the previous one for a frame. It is unavailable
  before the first publish landed ("the first publish has not landed;
  get_view says what it waits on") and while one runs ("a publish is in
  flight; wait for it to settle"). A failed publish settles the call with
  the failure.
- `publish_tree` — the Publish tree button. It writes a snapshot into the
  lading store and opens a tally window, both outside this window, so it is
  consequential and the host asks the person every time; it is
  asynchronous and settles when the snapshot landed, with the generation,
  the mount, the entries and the expiry. Unavailable while one runs.
  Without a server or a bus it settles with the failure the bar shows.

The two buttons go through the same two commands, so the command log
records a click under the operation an agent would call.

## For agents: the delegated bundle-view operations

The view is operable, so the window's catalog offers a subset of play's
operations over it, each prefixed `bundle_` and served by the play inside
the view, under play's own availability and agent limits. The view is
named `items`; every `bundle_` operation takes a `bundle_view` argument
that may be left out while the window has one view.

- `bundle_list_views` — the window's operable views, the bundle each shows,
  its revision, its panes, and what it waits on.
- Reading: `bundle_get_state`, `bundle_describe_result`,
  `bundle_sample_rows`, `bundle_list_history`, `bundle_list_panes`, and
  for the one pane the document shows, `bundle_get_table` and
  `bundle_set_table_options`.
- Running: `bundle_run`, `bundle_cancel_run`, `bundle_set_param`,
  `bundle_show_pane`.

Not offered, by design: changing the SQL, binding datasets and publishing
— the document is the publisher's, and a changed document is a new bundle
made in play — and the authoring tools. Play's operations for panes the
document does not show are declared and reported unavailable with the
reason. Before the first publish lands there is no view, and every
`bundle_` call is refused with what the view waits on; the window's own
state is in its progress row and this page.

## What is kept and what is not

The bundle is not retracted when the window closes: the runtime retracts
what the window published when the host closes its bus client. The tree
snapshot expires on the date the bar shows; the mount is the store's.
