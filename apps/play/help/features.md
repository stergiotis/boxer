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

The window is a rearrangeable, splittable dock of tabs between a pinned top bar
(Run, Load, connection) and a status bar (the query-state inspector). They fall
into three groups: the **editor** (Editor, History), the **tool panes** beside
it (Docs, Preview, Flow, Passes, Diagnostics, Snippets, Experiments — each reads
the buffer, or something derived from it, while you type), and the **result
panes** below (Table, Projection, Timeline, Map, World, Kanban, Network, Sankey,
Distribution, Icicle, Graph, Schema, and Detail alongside them). Drag a tab to
re-dock or split it; the layout holds for the session and starts fresh next
launch.

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

Two keyboard shortcuts reach the Run button without leaving the editor
(`Cmd` in place of `Ctrl` on macOS):

- **Ctrl+Enter** — run, exactly as the button does.
- **Ctrl+Shift+Enter** — run just the query the caret is in. See *Running one
  query* below.

Beyond those the editor supports the usual text-editing keys (select-all, copy,
paste). Use the **Snippets** tab to drop in fragments, and the **Preview** tab
to see the parsed canonical form.

A line-number gutter runs down the left edge, with a marks lane beside the
numbers: `!` on a line carrying an error, `|` on the lines of the query
Ctrl+Shift+Enter would run, `>` on the lines of the statement the caret is in.
A line qualifying for more than one shows the first of those that applies.
Lines are not wrapped — long lines scroll sideways, and the gutter stays put
while they do.

The editor annotates as you pause typing:

- **Syntax errors** get an error-toned underline on the token the parser tripped
  on; the syntax colours stay up underneath, and the underline clears when the
  buffer parses.
- **Unfilled placeholders** get a warning-toned underline, agreeing with the
  "needs a value" mark in the parameters pane. A Run is blocked only by the
  unfilled names in what it ships: one in a sibling statement marks the buffer
  but does not stop running this one.
- **Multi-statement buffers** — statements separated by `;` — tint the statement
  the caret is in, and Run ships just that statement, with any leading `SET`
  prelude riding along. The **Preview** tab's "As sent to server" view names it
  ("statement 2 of 4") and shows exactly what would be sent. A syntax error in
  one statement does not stop another from running. Parameter values ride the
  request only for the statement that ships, and a signal moving elsewhere in
  the buffer does not mark its result stale; the saved history still records
  the whole buffer, so restoring a run brings back everything you had.
A single-statement buffer — the common case — is unchanged by all of this: no
statement tint, and Run sends the whole buffer as before.

### Running one query

**Ctrl+Shift+Enter** runs the innermost query the caret is in, rather than the
whole statement. That covers a subquery in a `FROM` or a `WHERE`, a CTE body,
one branch of a `UNION` / `EXCEPT` / `INTERSECT` chain, and — since the
statement split runs first — one statement of a `;`-separated buffer.

For a chain, the caret inside a branch runs just that branch, parenthesised or
not; the caret on the connective itself belongs to no branch and runs the
whole chain.

The gutter marks it: `|` on every line of the query that would run. That mark
is always on, so the shortcut is never invisible.

The WITH items in scope are carried along: run a subquery that reads a CTE
defined further out and the definition ships with it, flattened into one `WITH`
list, outermost first. Sibling definitions travel too, wherever they sit in
the clause — ClickHouse binds the names of one `WITH` list regardless of order
— and a branch of a chain also carries the `WITH` clauses of the branches
before it, which ClickHouse scopes forward across the chain. The one item that
can never travel is the definition you are inside: it would be defined in
terms of the very text being run.

Two cases fall back to an ordinary Run rather than refusing: a caret already at
statement level, which has nothing narrower to resolve to, and a statement that
does not currently parse, which has no structure to narrow within. The status
line says which happened — *subquery only*, or *no subquery at the caret — ran
the whole query* — so a run that did not narrow never looks like one that did.

Note that the caret must have been **placed** in the editor at least once. A
buffer restored from the last session, or one seeded by `BOXER_PLAY_SQL`, has
its caret at offset 0 — the head of the buffer, which resolves to the whole
query.

### The Subquery toggle

The **Subquery** checkbox in the top bar, off by default, turns on the editor's
full account of that gesture, and adds a **Run subquery** button beside Run. It
changes nothing about what Run or Ctrl+Enter do.

Each button is exactly its keystroke, in both directions and in both states of
the toggle: Run is Ctrl+Enter, Run subquery is Ctrl+Shift+Enter. Neither
keystroke ever changes meaning — the toggle changes which buttons are on the
bar, not what anything does.

Run subquery is never greyed out. With the caret at statement level it does
what the keystroke does: runs the whole query, and says so in the status line.

Everything it draws describes one thing: **the query the caret is in.**

- The **query itself** is tinted — its extent, marked off from whatever
  surrounds it. A query that *is* the whole statement is not tinted: there is
  nothing to distinguish it from, and a full-width wash would say nothing.
- Its **environment** — the WITH items in scope, and the `SET` prelude — is
  underlined in the info tone. These are lines elsewhere in the buffer that the
  query depends on, and that travel with it when it runs alone.
- References that **will not resolve** are underlined in the error tone. This
  is the *correlated* subquery — one referring to a table alias belonging to
  the query around it — and its cousin, a recursive CTE body naming itself,
  whose own definition is the one thing that cannot travel with it. Nothing
  makes either runnable on its own — carrying the WITH items does not help —
  and ClickHouse rejects the run with the name it could not find. The mark is
  there so you see it before you run rather than after.

A qualified name that resolves nowhere at all is left unmarked, since
`tuple.field` on a Tuple column looks the same to the parser.

All of this is drawn for the statement's own query as well, not only for nested
ones — a `WITH` clause running to most of the buffer, with the query it feeds
at the bottom, is exactly where seeing the query and its closure helps.

What the toggle does **not** say is what Ctrl+Shift+Enter would run — the
gutter's `|` says that, and it appears only where running the query alone
would differ from Run. So a tinted region with no `|` beside it reads as
"this is the query, and it is already what runs".

## Query parameters

Write a `{name:Type}` placeholder in the query (e.g. `{event:String}`,
`{from:DateTime}`) and the playground surfaces an editing widget above the
editor. On Run, the values are shipped to ClickHouse on the request URL and
the placeholder is substituted server-side.

Each parameter sits in one of two tiers, and the buffer alone says which:

- **Live** — no `SET` line for the name. Typing in its widget writes the
  shared signal value (see **Signals** below), so panels that publish the
  same name keep working and the value is live for every query that reads
  it. This is the default for a new placeholder.
- **Pinned** — the buffer carries `SET param_<name> = <value>` at the top.
  The name is a **constant**: buffer-owned, part of the query text,
  reproducible by copy-paste, and it shadows any signal of the same name.

The **pin** button beside each widget moves a value between the tiers: *pin*
writes the current value into the buffer as a `SET`, *unpin* removes that line
and keeps the value as a live signal. Deleting the `SET` by hand does the same
thing as unpinning. A folded range migrates as a unit, so a picker never ends
up writing one bound to the buffer and the other to the store.

Pin when you want the query to carry its own values — sharing a snippet,
keeping a reproducible artifact. Leave it live when a panel should be able to
drive the value, or when the **Live** checkbox should re-run the query as the
value moves.

The widget chosen for a slot depends on its shape:

- **Time-range picker** — two DateTime parameters naming the bounds of one range
  become a single Grafana-style range control (two expression fields, presets, a
  timezone dropdown, an Apply button). Expressions like `now() - INTERVAL 1 HOUR`
  are resolved to exact bounds when the host has wired the time-range evaluator;
  the resolved values show beneath the control.
- **Date/time pair** — the same pair falls back to two independent calendar
  pickers when the evaluator isn't available. The control says so when it is
  standing in.
- **Dropdown** — a slot whose values are a known set, declared by a comment
  line: `-- play: enum size_by bytes,count`. Each option is `value` or
  `value=Label`, and an empty value is allowed for the "no filter" entry —
  `-- play: enum catalog =All catalogs,boxer`. The buffer keeps whatever value
  is picked, so a dropdown is a spelling aid, not a constraint: a value the
  list does not carry still shows, marked *not in the list*. A hint naming a
  placeholder the query does not have is reported in the line beneath the
  widgets, since its only other symptom is a text field.
- **Text field** — every other slot gets a single text input (hint
  `value for {<name> : <Type>}`) where you type the literal value or expression.

A **Reset** appears beside the PARAMETERS caption as soon as a knob is off the
value the buffer was loaded with, and puts them all back. Only parameters the
buffer `SET`s have a default to return to — a live value belongs to whichever
panel publishes it.

Bounds pair by **stem**: strip a `from`/`to`, `min`/`max`, `start`/`end`,
`lo`/`hi` or `since`/`until` suffix, and two placeholders left with the same stem
are one range — `{from:…}` + `{to:…}` (the empty stem), `{tl_min:…}` +
`{tl_max:…}` (stem `tl`), `{a_start:…}` + `{a_end:…}` (stem `a`). Order and
distance don't matter: the bounds can sit anywhere in the query, in either order,
with anything between them. Both halves must be DateTime or DateTime64.

When two DateTime parameters *don't* fold, the pane says why in one line beneath
the widgets — usually that they share no stem, that one half isn't DateTime, or
that a hand-written `SET` pinned one half and left the other live (one picker
cannot write two tiers; pin or unpin the other half and it folds). A fold that
did happen is labelled with the two names it claimed, so you can always see what
the editor inferred. Add a **`-- play: ungroup`** comment line anywhere in the
query to refuse every fold and get one plain text field per parameter.

A widget whose name nothing fills yet is marked **needs a value**. That is the
same condition that blocks Run, so filling the widget clears both.

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
parameter widgets above the editor write the same values, so a signal has a
typed control as well as a raw one.

The **signals** section at the top of the Graph tab lists them — value,
declared type(s), and who last wrote it (`param-widget` for the parameter
pane, `signals-editor` for this section itself, a panel's name for a panel) —
and is also where you set, add, or discard a name by hand, including names no
placeholder in the buffer mentions. A name read as different types by
different queries gets a conflict warning. `selection_id` is marked when it
lags the cursor: it follows the last row that carried a leeway id, so clicking
a row without one leaves it pointing at the previous match.

A referenced name that nothing fills blocks Run with a hint (instead of the
server's "substitution not set" error); the widget for that name carries the
matching **needs a value** mark. The **Live** checkbox (top bar, shown when
the query has a signal input) re-runs the query automatically when a
referenced signal moves — edits to the SQL itself still wait for Run.

If a query keeps re-running because it feeds its own input — its result moves
a signal it reads, which triggers another run, which moves it again — Live
switches itself off after a few rounds and the status bar names the signal
that was cycling. Re-check Live to resume. Values you type never count towards
that: a person driving a value fast is not a loop.

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
- **running** — accent badge, `executing…`, followed by the live counters when
  the server sends them: `1.9B / 2.5B rows (77%) · 14.5 GB read · 1.2M rows/s ·
  ETA 1m20s · mem 1.1 MB · 24.5s`.
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

### Live progress

While a query runs, ClickHouse streams counters back — rows read, bytes read,
memory, elapsed. They appear in three places:

- a bar beside **Cancel** in the top bar, with the percentage and the ETA;
- a slim bar above a result pane whose contents are being replaced, so a re-run
  is visible where you are reading rather than only in the chrome;
- the status line, and the centred *Executing query…* state when there is no
  previous result to show.

The rate and the ETA are estimated from the tick stream (a smoothed rate, and an
ETA that is allowed to fall freely but resists small rises, so it does not
oscillate). Both need a couple of ticks before they appear. The percentage and
the ETA need the server to report a **total** to divide by; it cannot always —
an unbounded `system.numbers` scan, for instance, has no total — and the bar is
then indeterminate, with rows and rate but no ETA.

Endpoints that do not stream progress at all (the in-process query engines, and
anything not plain `http://`) show the spinner alone.

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

Above either card, when the selected row carries one or more **datetime attributes**,
a compact **timeline** plots them on a shared UTC axis. Each attribute is one legend
entry — a coloured swatch and its identity. A flag from a **tagged section** is
labelled with that section's memberships (primary · secondary) and every
co-attribute value, mirroring the card row below it; a backbone or ad-hoc flag
shows its name and value. On the axis:

- a scalar timestamp, or each item of a datetime array, is a numbered flag (all of
  one attribute's items share its number and colour; hover a flag for its value);
- a begin/end datetime pair in one section — such as a leeway `timeRange`'s
  `beginIncl` + `endExcl` — is drawn as interval bars on a lane labelled with the
  attribute. Two unrelated datetimes in a section are shown as separate flags, not
  as a fabricated interval.

It recognises datetime **value** columns by their type — `DateTime64` / `Date`,
arrays and dictionary-encoded forms, and leeway datetime attributes (including the
whole-second-integer form and the entity timestamp). The structural support
columns (`len`, `card`) that accompany a leeway datetime attribute are not
plotted. A row with no datetime attribute shows no timeline.

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

The reactive query-graph view (ADR-0097). It opens with the **system graph** —
a live drawing of the whole reactive surface: constants and signals feed the
buffer's query nodes, nodes feed the panel tabs, and panel writes loop back to
the signals they set (accent edges). Unfilled signals tint amber; drag pans,
ctrl+scroll zooms; clicking a query node observes it in the result panels.
Below it, the **signals** section lists the live parameter values (see
"Signals" above) and lets you set, add, or discard one; then each node of the
last-run buffer follows as a collapsible entry — the final `SELECT` is the
sink the panels observe, with per-node buttons to observe it in all panels or
bind a single tab to it.

### Flow

A dataflow graph of the **active node's** SQL — the statement the result
panels observe (the last Run's final `SELECT`, or the node observed from the
Graph tab). Where the Graph tab draws the surface *between* queries, Flow
draws the inside of one: sources — tables, sibling CTEs, subqueries, table
functions — feed the join tree, and its output passes through the clause
stages in evaluation order (`PREWHERE → WHERE → GROUP BY → HAVING → SELECT →
QUALIFY → DISTINCT → ORDER BY → LIMIT`) into the result. Drag pans,
ctrl+scroll zooms; the **layout** toggle flips left-right/top-down.

Clicking a node highlights it and shows its clause text under the canvas; on
the statement lens the same click also tints that clause's bytes in the
editor, for as long as the buffer still contains the statement that ran.

The **lens** selector picks what the graph is derived from:

- **statement** — the SQL itself, parsed locally. Works offline and is the
  only lens with editor highlighting.
- **ast**, **plan**, **pipeline** — the server's `EXPLAIN AST`,
  `EXPLAIN PLAN` and `EXPLAIN PIPELINE` of the same statement. A lens query
  follows the statement's own endpoint routing — index structure and schema
  are endpoint-local, so the EXPLAIN interrogates the endpoint the query
  would actually run on. An endpoint whose SQL surface has no `EXPLAIN`
  (the in-process introspection plane, today) says so in plain language
  instead of relaying its parser error.
- **estimate** — `EXPLAIN ESTIMATE`: one node per MergeTree table the
  statement reads, carrying the parts, rows and marks the server expects to
  touch. A statement reading no MergeTree tables estimates empty, and the
  pane says so.
- **indexes** — `EXPLAIN PLAN indexes = 1`: the plan with each read step's
  index usage folded into its detail — selected vs initial parts and
  granules per index, so a filter that fails to prune shows up as `n/n`.
  Click the read node (or flip to the text view) for the figures.
- **lineage** — column-level provenance of the SELECT list, derived locally
  like the statement lens: each output column is fed by the source columns
  its expression references, resolved the way ClickHouse resolves them — a
  select-list alias shadows a column, so `SELECT a AS b, b+1 AS c` draws
  `c ← b ← t.a`. A bare column over several joined sources is flagged
  ambiguous rather than guessed; `*` stays a star per source (the panel
  cannot know the column set offline); scalar subqueries are marked, not
  traced. Clicking a column highlights its expression (or the identifier)
  in the editor. Columns a `WHERE` or `GROUP BY` consumes without
  projecting are not drawn — this lens answers "where does each output
  column come from", not "what does the query read".

For a remote lens, the **view** toggle switches between the parsed graph and
the **raw EXPLAIN text** exactly as the server returned it, indentation
intact — the graph is a reading of that text; the text is the full detail,
and what a bug report should carry. Lens results refresh on Run and when the
statement's parameters move; a clicked node's step text appears under the
canvas the same way clause text does.

The **source** toggle picks what the tab derives from. **run** (the
default) is the last Run's statement, with the observe gesture choosing the
node. **caret** follows the editor instead: the statement under the caret
in the current buffer, re-derived as you edit — and within it, the caret
picks the node, so placing it inside a CTE body shows that CTE's flow. The
node badge reads `· live` in this mode. Local lenses update immediately;
remote lenses ask the server only once the buffer has settled, so a
half-typed statement keeps the last answer instead of streaming errors.

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

### Vocabulary

The functions a buffer can name, filtered the same way as Snippets, each with an
**Insert** button that drops a call template at the caret. Three sections, split by
where a name is actually evaluated — which is what decides how it fails:

- **Server** — SQL user-defined functions, which exist only where somebody
  installed them. The `LW_*` family is leeway's query vocabulary: `LW_CO_*` over
  positionally aligned lanes, `LW_RAGGED_*` over flat streams with a lengths lane,
  `LW_VALUE_BY_TAG_EQUAL` / `LW_LIST_BY_TAG_EQUAL` to read a tagged attribute, and
  `LW_ID_*` over Fibonacci-tagged identifiers. Each row is marked `✓` or `MISSING`
  against what the endpoint carries — a missing one needs provisioning, not a
  different query. Functions the endpoint has that this build does not know about
  are listed as `extra`.
- **Client** — macros play rewrites into ordinary SQL before the statement leaves,
  so they work against any endpoint, including one carrying no UDFs at all:
  `descriptiveStatistics(...)`, `docsearch('...')`, `keelson('...')`, and `LW_ID_*`
  (which is both — installable *and* expanded here).
- **play** — the `ts*` family, computed locally over the rows a sub-query returns.
  The server never sees the name.

Until the endpoint has answered, server rows show `?` rather than a verdict: an
unanswered probe is not the same as an empty server. Switching endpoints re-asks.

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
`boxer.facts`. For local exploration there is a self-contained demo table,
`anchor.facts`, populated by an integration test (it skips silently without a local
ClickHouse):

```bash
go test -tags="$(cat ./tags)" -run TestLeewayClickHouse \
  ./public/semistructured/leeway/anchor/
```

That loads ~60 entities across three scenarios (drone deliveries, cyber incidents,
alpine sensor readings). The **Example queries** and **Snippets** pages target it.
Leeway physical column names differ per schema, so a query written for `anchor.facts`
transfers to `boxer.facts` by swapping the table name and adjusting column names.
