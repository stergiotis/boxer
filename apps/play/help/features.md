---
type: reference
audience: end-user
status: draft
title: Features
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Features

A reference for everything the SQL playground does. For a gentle introduction
see the **Overview**, for a verified query set see **Example queries**, and for
ready-to-run fragments see **Snippets**. This page describes each feature in turn.

The window is a rearrangeable, splittable dock of tabs (Editor, Preview, History,
Table, Projection, Timeline, Snippets, Detail, Map, World, Graph, Schema,
Diagnostics) between a pinned top bar (Run, Load, connection) and a status bar
(the query-state inspector). Drag a tab to re-dock or split it; the layout is
remembered.

## Connecting to ClickHouse

The app speaks ClickHouse over HTTP and pulls results back as Arrow (`ArrowStream`),
so wide leeway-encoded tables arrive without a row-by-row decode. The endpoint
defaults to `http://localhost:8123/`; the top bar shows the active connection as
`<url>  as <user>`.

You never write a `FORMAT` clause — the app rewrites the query to end with
`FORMAT ArrowStream` before sending. One consequence: DDL such as `TRUNCATE` /
`CREATE` / `ALTER` does **not** round-trip through the playground, because the
appended `FORMAT` clause is invalid on those statements. Run DDL from a regular
ClickHouse client instead.

See **Configuration** for the connection flags and environment variables.

## The editor

The **Editor** tab holds a multi-line, syntax-highlighted SQL buffer that grows to
fill the pane. The empty-buffer hint is `-- type SQL, press Run`. The buffer is
persisted across sessions (saved on Run and when the window closes) and restored on
the next launch.

There are no app-specific keyboard shortcuts: Run is a button (top bar), and the
editor supports the usual text-editing keys (select-all, copy, paste). Use the
**Snippets** tab to drop in fragments, and the **Preview** tab to see the parsed
canonical form.

## Query parameters

Write a `{name:Type}` placeholder in the query (e.g. `{event:String}`,
`{from:DateTime}`) and the playground surfaces an editing widget above the
editor. Filling the widget authors a `SET param_<name> = <value>` line at the
top of the buffer — the name is now a **constant**: buffer-owned, part of the
query text, reproducible by copy-paste. A placeholder you leave without a
`SET` is a live **signal** instead (see **Signals** below). On Run, the
`SET param_*` prelude is stripped from the body and shipped to ClickHouse on
the request URL, so the placeholder is substituted server-side either way.

The widget chosen for a slot depends on its shape:

- **Time-range picker** — an adjacent `{from:…}` + `{to:…}` pair becomes a single
  Grafana-style range control (two expression fields, presets, a timezone dropdown,
  an Apply button). Expressions like `now() - INTERVAL 1 HOUR` are resolved to exact
  bounds when the host has wired the time-range evaluator; the resolved values show
  beneath the control.
- **Date/time pair** — the same `from`/`to` pair falls back to two independent
  calendar pickers when the evaluator isn't available. Add a `-- play: ungroup`
  comment to force the pair into plain text fields instead.
- **Text field** — every other slot gets a single text input (hint
  `value for {<name> : <Type>}`) where you type the literal value or expression.

The **Hide prelude** checkbox (top bar, shown only when the query has parameters)
collapses the `SET param_*` lines: the prelude renders as a read-only label above
the editor and you edit it only through the widgets, while the editor binds to the
query body. Toggle it off to hand-edit the `SET` lines directly.

## Signals (live parameters)

A placeholder *without* a `SET` line is a **signal**: a live value shared by
name across every query and panel. Panels write them as you interact —
clicking a row (Table), a point (Projection), an event (Timeline), or a
country (World) writes `selection`; the Map's settled viewport writes the
`vp_*` set; the Timeline publishes the events extent as `tl_min`/`tl_max` —
and any query referencing the name picks the value up on its next run. The
**signals** section at the top of the Graph tab lists them — value, declared
type(s), and who last wrote it (a name read as different types by different
queries gets a conflict warning) — and is also where you set, add, or discard
one by hand. Adding a `SET` for the same name pins it into a constant that
shadows the signal until the `SET` is removed.

A referenced name that nothing fills blocks Run with a hint (instead of the
server's "substitution not set" error). The **Live** checkbox (top bar, shown
when the query has a signal input) re-runs the query automatically when a
referenced signal moves — edits to the SQL itself still wait for Run.

## Inline affordances

When the debounced parse recognises certain function calls, a small context tool
appears below the editor under an `AFFORDANCES` divider. Today this covers the
`multiMatch*` family: a regex tester that lists each pattern argument, compiles it,
and reports the match count against a shared **test input** field you type into —
so you can tune the patterns without leaving the panel.

## Running a query

- **Run** executes the editor SQL; while it runs the button becomes **Cancel** with
  a spinner. Execution is asynchronous, so the UI stays responsive.
- Run is **refused** — with an actionable hint in the status bar — when the query
  references a placeholder that neither a `SET` nor a signal fills (see
  **Signals**); nothing is sent, since the server could only reject it.
- **Load .sql…** (shown when the host wired the file capability) opens a file picker
  and replaces the buffer with the chosen file's contents.
- Results land in the **Table** tab and feed the **Detail**, **Projection**, and
  **Timeline** views; the **status bar** names the outcome.

## Query state (the status bar)

The status bar is a query-result inspector: a severity-coloured state badge plus a
one-line summary, with an arrow-square-out toggle that pops out a tethered inspector
window (the state graph, the transition history, and the provenance of the reading).
It tells the **input** (the editor SQL) and the **output** (the displayed results)
apart, so an empty result and a stale result are distinct, named states:

- **idle** — neutral badge, `type SQL and press Run`. No query has run yet.
- **running** — accent badge, `executing…`. A query is in flight.
- **rows** — green badge, `N rows · 12ms · 4 kB read · 8s ago`.
- **empty** — amber badge, `0 rows · ran 8s ago`. The query ran and matched nothing.
- **failed** — red badge, `errored: <message>`.
- **rows (stale)** / **empty (stale)** / **failed (stale)** — muted badge,
  `… · inputs changed`.

The **stale** variants appear when the run's inputs have diverged from what
produced the results on screen: a buffer edit, a parameter change, a snippet
insert — or a referenced **signal** that moved since the run (a Table click
that changed `selection`, a Map pan that moved `vp_*`, …). The table below is
showing output for inputs you've since changed; press Run to refresh, or check
**Live** to re-run on signal moves automatically (buffer edits always wait for
Run).

## Result views

Results render across several dock tabs. Pagination applies to the Table tab only;
the Projection and Timeline views work over the whole result set.

### Table

The result grid, in a leading-`#` selectable form. Click anywhere on a row to select
it — the selection is absolute (it survives paging), is published as the
`selection` signal, and drives the **Detail** view.
Above the grid, the pager pages through large results and lets you pick the page size
(50 to 10000 rows); a `rows A–B of N` label shows the current window. Column widths
are sized from a sample of the first rows and are drag-resizable.

Empty/loading states are explicit: a spinner with *Executing query…* while running,
*Run a query to see results.* before the first query, and *0 rows — the query ran but
matched nothing.* for an empty result.

### Detail

A structured card for the row selected in the Table tab. The card picks its rendering
from the result's column names:

- **Leeway card** — when the columns are leeway-encoded (`id:…`, `tv:…`), the card
  groups them into the entity's plain `id` section, its tagged sections, and the
  membership chips on each attribute. A `SELECT *` from a leeway table takes this
  path.
- **Ad-hoc grouping** — for ordinary SQL results (aliased or aggregated columns),
  columns are grouped by name prefix into pinned / relations / data / meta sections.

Before a query it reads *Run a query, then select a row to see its detail.* When a
result lands the first row is selected automatically, so the card populates straight
away; click another row in **Table** (or a point in **Projection** / an event in
**Timeline**) to retarget it.

### Projection

A 2-D UMAP scatter of the result's feature columns. Click **Compute projection** to
run it (needs at least three rows); the button becomes **Cancel** while it works, and
an fsmview chip shows the projector's lifecycle (extracting → running → done, or
failed / cancelled). When done you get the scatter plus a **colour by** picker
(monochrome or any feature, binned with a legend) and the UMAP parameters. Pan and
zoom with the mouse; click a point to select that row (it drives the Detail tab).
Very large results are sampled (10000-row cap) so UMAP stays interactive.

### Timeline

Plots time-shaped results on a horizontal time axis, when the result matches the
timeline column contract — return one of these shapes:

- **Points** — `_tl_time`
- **Intervals** — `_tl_time` + `_tl_time_end` (plus optional `_tl_lane`, `_tl_intensity`)
- **Annotations** — `_tl_time` + `_tl_label`

Timestamps must be `DateTime64`. When the contract isn't met the panel shows the
expected shapes instead of a plot, so you can fix the `SELECT`. A **Now line**
checkbox draws a marker at the current time. An optional **Background bands** editor
overlays shaded ranges: write a small `SELECT` returning `_tl_band_from` /
`_tl_band_to` / `_tl_band_color` / `_tl_band_label`, optionally reading the
`{tl_min:…}` / `{tl_max:…}` parameters — the Timeline publishes the events' time
extent under those names as signals after each render.

### World

A schematic world choropleth (ADR-0114) over the active result: it claims a result
whose string column resolves to countries (ISO 3166 alpha-2/alpha-3 codes or
country names), fills each country by the value column picked in the toolbar
(**auto** = first numeric; no numeric column falls back to presence-only fill), and
counts unmatched and duplicate rows in its status line (duplicates: last row wins —
the pane never aggregates for you). Hover reads `name · value`; clicking a country
selects its row, driving the Detail tab. The **Snippets** library carries a
ready-to-run example ("World choropleth (countries)").

### Map

An in-database-rendered geo raster over a pannable map (ADR-0096), for tables with
`mercator_x` / `mercator_y` columns (e.g. the ADS-B demo loader's
`planes_mercator`): the visible viewport is rendered to pixels by a ClickHouse
query on each pan/zoom settle. Table, sampling, colour mode and opacity are panel
controls — this tab queries on its own, independent of the editor's result. The
settled viewport is published as the `vp_*` signals (packed-mercator bounds plus
output dimensions), so any query can reference `{vp_min_x:UInt32}` … to
cross-filter against the visible extent.

### Graph

The reactive query-graph view (ADR-0097): each top-level CTE of the last-run
buffer is a node, with the final `SELECT` as the sink the panels observe. Observe
an intermediate node to point every result tab at that node's rows instead. The
**signals** section at the top lists the live parameter values (see "Signals"
above) and lets you set, add, or discard one.

### Schema

A leeway `TableDesc` inspector over the active result's Arrow schema — column
types and inferred structure in a master-detail view (ad-hoc results show plain
opaque columns; tagged sections aren't recoverable from an arbitrary result).

### Preview

The editor's SQL re-rendered in its canonical, syntax-highlighted form (comments
stripped, keywords/whitespace normalised). It's a parse aid — not a second query — so
you can see the structure even when your own formatting is irregular. When boxer's
grammar can't parse the buffer, the pane points at **Diagnostics** instead of a
canonical form; Run still sends the buffer verbatim.

An **As sent to server** checkbox flips the pane to the wire form: the exact
statement that will be POSTed (pre-execute rewrites applied, `FORMAT`
appended), with captions naming what rides the URL instead of the body —
`params on URL: …` for the `SET`-bound constants, and `signals on URL:
name=value, …` for the signal values the store would supply at Run. Unlike the
canonical view this renders even for SQL boxer's grammar can't parse, because
it is what would actually be sent.

### Diagnostics

The single home of the playground's error texts — the other tabs only point here.
Three sections: **Statement** is the parse status of the (debounced) editor buffer;
when boxer's built-in grammar rejects it, an `EXPLAIN AST` probe against the live
endpoint tells you whether that is just a boxer grammar gap (ClickHouse parses it —
the statement will run, with the canonical preview, parameter widgets, query-graph
split and pre-execute rewrites unavailable) or genuinely broken SQL (ClickHouse's
own diagnostic is shown, with positions matching the editor). **Query graph** is
the split status of the last Run. **Last run** carries the full execution error —
the status bar shows only its first line — or the usual result summary.

### History

Previously-run queries, newest first. Each row reads `HH:MM:SS  <N>r <elapsed>` (or
`ERR`) followed by the query text; click one to reload that SQL into the editor.
The signal values the run shipped are re-seeded into the store alongside the
buffer, so re-running reproduces the same inputs (signals do not otherwise
persist across sessions; constants persist via the buffer).

### Snippets

A small library of ready-to-run fragments (play's own `snippets` help doc). Each
fenced SQL block carries two buttons: **Insert** splices the snippet at the editor's
cursor (good for a clause or the parameter prelude), and **Replace** swaps the whole
buffer (good for a whole-query starting point). Keep the editor visible while you
click so Insert lands at the caret.

## Configuration

Command-line flags (all optional):

- `--clickHouseUrl` — ClickHouse HTTP endpoint (default `http://localhost:8123/`).
- `--clickHouseUser` — account (default `default`; or set `CLICKHOUSE_USER`).
- `--clickHousePassword` — password (or set `CLICKHOUSE_PASSWORD`).
- `--initialSqlPath` — a `.sql` file preloaded into the editor.

The editor buffer (`lastSql`) and the timeline bands SQL persist across sessions.
`BOXER_PLAY_SQL` overrides the restored buffer (useful for scripted runs), and
the automation variables `BOXER_PLAY_AUTORUN` (run the initial SQL on launch),
`BOXER_PLAY_SCREENSHOT` (capture to a path), and `BOXER_PLAY_EXIT_ON_SHOT`
(quit after the screenshot) drive headless captures.

## The demo data

The table you query depends on your deployment — a boxer deployment typically exposes
`spinnaker.facts`. For local exploration there is a self-contained demo table,
`anchor.facts`, populated by an integration test (it skips silently without a local
ClickHouse):

```bash
go test -tags="$(cat ./tags)" -run TestLeewayClickHouse \
  ./public/semistructured/leeway/anchor/
```

That loads ~60 entities across three scenarios (drone deliveries, cyber incidents,
alpine sensor readings). The **Example queries** and **Snippets** pages target it.
Leeway physical column names differ per schema, so a query written for `anchor.facts`
transfers to `spinnaker.facts` by swapping the table name and adjusting column names.
