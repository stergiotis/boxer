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
Distribution, Icicle, Files, Graph, Schema, and Detail alongside them). Drag a tab to
re-dock or split it; the layout holds for the session and starts fresh next
launch.

## Connecting to ClickHouse

The app speaks ClickHouse over HTTP and pulls results back as Arrow (`ArrowStream`),
so wide leeway-encoded tables arrive without a row-by-row decode. The endpoint
defaults to `http://localhost:8123/`; the top bar shows the active connection as
`<url>  as <user>`.

You never write a `FORMAT` clause — the app rewrites a read to end with
`FORMAT ArrowStream` before sending. An `INSERT INTO … SELECT` is the one
statement that both parses and *writes* (ADR-0181 §SD8): it ships without a
`FORMAT` clause, and Run refuses it until `BOXER_PLAY_ALLOW_WRITES=1` is set —
the refusal names the switch — after which the status line reports the rows
written instead of filling the result panes. Everything else that changes
state (`TRUNCATE` / `CREATE` / `ALTER` / DDL generally) still does **not**
round-trip through the playground; run it from a regular ClickHouse client.

The **Endpoint** menu pins where queries go, and carries one cache action.
Column handles and `section:*` expansion resolve against the endpoint's
`system.columns`, probed once per table and then remembered for the session.
Switching endpoint drops what was remembered, because it describes the server
you left. The same endpoint changing under you does not: play neither runs the
DDL nor hears about one, so a table that gained a column, or a view that was
dropped and recreated, keeps resolving to the shape it had at the first probe.
**Reload schema** re-probes every table the next query names. It does not touch
the pin or the Auto setting.

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
the placeholder is substituted server-side. Three type names are the exception
— they hold SQL rather than a value, and are substituted before the query is
sent; see [Parameters whose value is SQL](#parameters-whose-value-is-sql).

Each parameter sits in one of two tiers, and the buffer alone says which:

- **Live** — no `SET` line for the name. Typing in its widget writes the
  shared signal value (see **Signals** below), so panels that publish the
  same name keep working and the value is live for every query that reads
  it. This is the default for a new placeholder.
- **Pinned** — the buffer carries `SET param_<name> = <value>` at the top
  (for a SQL-valued knob, a `-- play: expr <name> = <sql>` line instead).
  The name is a **constant**: buffer-owned, part of the query text, and it
  shadows any signal of the same name.

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
- **SQL field** — a slot typed `Expr`, `ExprList` or `Identifier` gets a
  one-line SQL editor with syntax colour, because its value is SQL rather than
  a value. See [Parameters whose value is SQL](#parameters-whose-value-is-sql).
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

`-- play: enum`, `-- play: ungroup` and `-- play: expr` are three of four
comment-line directives the buffer carries with the SQL; the fourth,
`-- play: gloss`, binds a column rendering by rule and is described under
[Glosses](#glosses).

A widget whose name nothing fills yet is marked **needs a value**. That is the
same condition that blocks Run, so filling the widget clears both.

### Parameters whose value is SQL

`{name:Type}` normally names a ClickHouse type, and ClickHouse substitutes the
value server-side. Three names mean something else — the knob holds SQL:

| Slot | What it holds | Where it goes |
| --- | --- | --- |
| `{cond:Expr}` | one expression: a predicate, or a scalar | anywhere an expression may appear — `WHERE`, `HAVING`, `GROUP BY`, `ORDER BY`, a `SELECT` item |
| `{cols:ExprList}` | a comma-separated list, aliases and all | a `SELECT` or `WITH` list |
| `{col:Identifier}` | one database, table or column **name** | any identifier position |

`Identifier` is ClickHouse's own parameter type and behaves like every other
value: it rides the request URL, and its `SET param_<name>` line pins it. One
`Identifier` carries **one** name, not a dotted path — for `db.table` use two
slots, `{db:Identifier}.{tbl:Identifier}`, since a dotted value is quoted whole
and the server answers `Unknown table expression identifier`.

`Expr` and `ExprList` cannot ride that channel, because ClickHouse substitutes
values and an expression is not one. The playground substitutes them into the
query text itself, just before sending it. Three things follow:

- **They are declared in a comment, not a `SET`.** Filling the field writes
  `-- play: expr <name> = <sql>` into the buffer; everything after the first
  `=` is the expression, so a value full of `=` needs no escaping. Put the line
  **below** any `SET` prelude — a comment above it ends the prelude, and the
  query then runs without its parameters.
- **An `Expr` is parenthesised when substituted**, so `WHERE x AND {c:Expr}`
  with `a OR b` means what it reads as. An `ExprList` is spliced as written,
  since a list in parentheses would be a tuple.
- **The buffer no longer runs unaided.** A `{cond:Expr}` pasted into
  `clickhouse-client` fails on an unknown type — deliberately, rather than
  running something different. Use the **Preview** tab's *as sent* view for the
  text that actually executes.

Leave a SQL knob unpinned and it works like any other signal: a panel can
publish the predicate and the field follows it. **Pin** writes the declaration
into the buffer; **unpin** removes it and keeps the value live. The mechanism is
the one under [Signals](#signals-live-parameters), and the Snippets tab's
*Signals (unbound parameter)* entry demonstrates it with an ordinary value.
There is no pasteable example of the SQL case: a live value is, by definition,
not in the buffer — so a snippet cannot carry one.

If the substituted query does not parse, the error is underlined **in the
field** when it falls inside what you typed, and reported against the query
otherwise.

In an applet, a SQL knob cannot make the query do more than the applet
declared. The applet's security class — read, read-egress, mutating — is a
ceiling on what the substitution may produce, so an expression that reaches
outside the endpoint (a `url(…)`, a `remote(…)`) is refused before it runs,
naming the knob and what raised the class. The playground itself sets no
ceiling: its editor already accepts arbitrary SQL.

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

A column bound to a **gloss** (ADR-0186 — see [Glosses](#glosses)) shows the
gloss's one-line face in its cells instead of the plain value — `21.5 °C`,
`4111 •••• •••• 1111 ✓` in the success or error tone, `••••••`, `40 KiB`, a
`gloss/url` cell as a clickable link (it opens the URL; the row's other cells
still select the row) — and
its media type beside the type tag in the header; the header hover names how
the binding was made and shows the column's spec line. A **Raw cells** toggle
on the toolbar switches every gloss off for the session. Both grids honour the
glosses; the per-attribute grid applies them to a list column's items.

### Detail

A structured card for the row selected in the Table tab. The card picks its rendering
from the result's column names:

- **Leeway card** — when the columns are leeway-encoded (`id:…`, `tv:…`), the card
  groups them into the entity's plain `id` section, its tagged sections, and the
  membership chips on each attribute. A `SELECT *` from a leeway table takes this
  path.
- **Ad-hoc grouping** — for ordinary SQL results (aliased or aggregated columns),
  columns are grouped by name prefix into pinned / relations / data / meta sections.
  A glossed column ([Glosses](#glosses)) renders through its gloss here: a
  block face where the gloss has one — markdown, highlighted JSON / SQL / Go,
  a decoded image, a hyperlink, a tagged id split into tag and counter with
  Copy buttons — else its one-line face under a caption naming the column and
  the media type. A declaration the catalog cannot honour shows the plain
  first line and says why. On the leeway card, values pass through their
  column's gloss too, and a column whose gloss has a block face gets it there
  as well, stacked under the inline line.

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

### Files

A result carrying a `path` column browses as a tree of files. The tab interns
the paths — one row per entry — and shows them as a listing or as an outline,
with a breadcrumb, a quick filter, sortable name / size / modified columns, and
every *other* column of the result beside them. `is_dir`, `size`, `mtime`,
`link_target` and `is_symlink` are read by name when the query projects them;
nothing else is required.

`SELECT * FROM fs('<mount>')` is the case it was built for — a lading snapshot
browses without leaving play, hash and expiry riding along as columns — but any
result that names paths works.

The directories between the rows are synthesised, so they carry no size, no time
and no row of their own, and what a click publishes follows that split:
`selection_key` is always the path, `selection` — the row cursor **Detail** and
**Table** follow — only when a result row named the entry. Detail is the
preview: a row is metadata rather than bytes, so activating a file moves the
cursor rather than opening anything. The status line says what the interning
made of the result, including rows dropped at the cap or carrying no usable
path.

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
  installed them (`boxer leeway sqlsurface install`, one version marker
  `LW_SURFACE_VERSION` for all of it). The `LW_*` family is leeway's query
  vocabulary: `LW_CO_*` over positionally aligned lanes, `LW_RAGGED_*` over flat
  streams with a lengths lane, `LW_LU_*` / `LW_VALUE_BY_TAG_EQUAL` /
  `LW_LIST_BY_TAG_EQUAL` to read a tagged attribute, `LW_ASPECT_*` to decode a
  physical column name's aspects, and `LW_ID_*` over Fibonacci-tagged
  identifiers. Each row is marked `✓` or `MISSING` against what the endpoint
  carries — a missing one needs provisioning, not a different query. Functions
  the endpoint has that this build does not know about are listed as `extra`.
- **Client** — macros play rewrites into ordinary SQL before the statement leaves,
  so they work against any endpoint, including one carrying no UDFs at all:
  `descriptiveStatistics(...)`, `docsearch('...')`, `keelson('...')`, the leeway
  extraction and constructor families (`LW_GET*`, `LW_PLAIN` / `LW_TV*` —
  though what `LW_GET*` expands *into* calls the server-side read-back
  helpers, and the panel marks that dependency), `gloss(...)` (an alias
  declaring how a column renders — see [Glosses](#glosses)), and `LW_ID_*`
  (which is both — installable *and* expanded here).
- **play** — the `ts*` family, computed locally over the rows a sub-query returns.
  The server never sees the name.

Until the endpoint has answered, server rows show `?` rather than a verdict: an
unanswered probe is not the same as an empty server. Switching endpoints re-asks.

### Completion

The caret-side sibling of Vocabulary: what may stand **where the caret is**,
rather than everything this build can name. Inside `LW_COMPONENT('…')` it is the
registered component kinds; inside `tupleElement(LW_COMPONENT('SysMem'), '…')`
it is that kind's fields with their types; inside `keelson('…')` the
introspection tables and this session's bound datasets; at a `FROM` or a column
position, the endpoint's own tables and columns. It reads the enclosing call and
which argument the caret is in, so a position whose valid values depend on a
neighbouring argument — the field depends on the kind — gets the right list and
not a longer one.

Where nothing can answer exactly, the pane says so instead of showing a guess.
"waiting for the endpoint" means a catalogue listing has not come back yet;
"member access on a name needs the statement's scope" means the background parse
has not caught up; "no signature is declared for …" means the function does not
say what its arguments are. None of those are an empty list.

The pane is a table, not a popup: it takes no focus, a click on a matching row
inserts the rest of that name, and **Tab** types as far as the matching rows
agree — the whole name when only one matches, the common prefix when several do.
Tab means a tab character whenever there is nothing to complete.

The editor says the same thing on the token itself. A literal that resolves
against its position is underlined quietly; once the buffer settles, every
*other* literal in a position this build can check is underlined too — quietly
if it resolves, in the error tone if it does not, so a mistyped component kind is
visible before the run refuses it. A literal whose vocabulary needs an endpoint
answer that has not arrived is left alone rather than accused.

### Glosses

The result-side sibling of Vocabulary (ADR-0186). A **gloss** is a named way of
showing a value — a temperature with its unit, a Unix epoch as a moment, a span
as `1m 05s`, a card number masked with its Luhn verdict, a value masked to six
bullets, a URL as a link, a byte count in KiB, a fibonacci-tagged id split into
its tag and counter, and the ADR-0123 content types
(markdown, code, images) as one family. Every gloss
has a one-line face for the Table grids and, some, a block face for Detail —
in the ad-hoc pane and, stacked under the row's other values, in the leeway
card.

Three routes bind a column to a gloss, in precedence order:

- **an alias** — `` AS `label@gloss/temperature;unit=C` ``. The token after `@`
  is a media type: an IANA one for content (`text/markdown`) or play's private
  `gloss/…` for presentation. Parameters ride after `;` and are validated —
  an undeclared or misspelt parameter is refused with the reason, as an unknown
  type is; the ADR-0123 rule that a slash-less `@` (an email-like name,
  `dot_done@success`) is not a declaration still holds;
- **the `gloss(…)` macro** — `gloss(expr, 'gloss/length', 'unit', 'm')`
  expands, before the statement ships, into that alias: no backticks, parameter
  values as typed literals, `'label', 'name'` to name the alias, and an unknown
  gloss or parameter is a Diagnostics error carrying the call's position;
- **a rule** — `-- play: gloss <media type> <regex>` anywhere in the buffer,
  matched top to bottom against each column's **spec line**: `name:temperature
  section:sensor role:val ct:f64 sem:measured … arrow:float64` for a leeway
  column (the `LW_TV` token spelling — what you would type to mint it), just
  `name:temp_c arrow:float64` for a plain one. This is how a leeway column,
  whose physical name cannot be aliased without the result losing its leeway
  shape, gets a gloss. Some glosses bring an affinity rule along —
  `gloss/masked` for `sem:secret`, `gloss/url` for `sem:url`, `application/json`
  for `sem:json*` — and `gloss/raw` in a rule switches an affinity off for the
  columns it matches;
- **a rule set in code** — rules that should outlive a query are Go, checked
  in with the deployment: `gloss.Rules("acme").Rule("kelvin readings").When(gloss.Section("sensor"),
  gloss.NameMatches("^temp")).Show(gloss.MediaTypeTemperature, gloss.Unit("K"))`,
  registered on the `gloss.Repository` the host hands play at construction.
  They rank between the buffer's directives and the affinities, and the
  Glosses tab lists them under their set's name.

`gloss/taggedid` is worth calling out because its value is unreadable without
it. A fibonacci-tagged id (ADR-0106) packs a category and a per-category
counter into one `UInt64`, so a grid otherwise shows a 19-digit decimal that
says nothing: the gloss shows the two halves in hex instead, tag first —
`12393906174523605050` reads as `c:3a`. In Detail and on the leeway card it
spells the split out — the tag value with its fibonacci code width, the
counter with the room its tag leaves — and offers **Copy id** (the decimal, for
a `WHERE id =`) and **Copy hex**. A word that is not a tagged id, or a tag over
the reserved counter 0, shows plain in the warning tone rather than pretending
to split. It has no affinity: being a surrogate key does not make a column
fibonacci-tagged, so bind it by alias or by rule.

The tab shows the **catalog** (each gloss with its accepted value kinds,
parameters, a sample rendering, its affinities, and two Insert buttons —
*Insert rule* drops a `-- play: gloss` line at the caret, *Insert call* a
`gloss(expr, …)` projection item with the same token), the buffer's effective
**rules** (compiled, or refused with why), and the current result's
**columns** with their spec line, what each resolved to, and the rules that
matched but lost: a later directive behind an earlier one, an affinity behind
a directive, any rule behind an alias. **Raw cells** on the Table toolbar
bypasses every gloss for the session.

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
`BOXER_PLAY_ALLOW_WRITES` opts Run into executing an `INSERT … SELECT`
(see *Connecting to ClickHouse* above); it governs every play-engined host,
the applets included.

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
