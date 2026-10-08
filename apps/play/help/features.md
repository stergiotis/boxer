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
it (Docs, Preview, Flow, Passes, Diagnostics, Snippets, Vocabulary,
Completion, Glosses — each reads the buffer, or something derived
from it, while you type), and the **result panes** below (Table, Projection,
Timeline, Map, Vector field, World, Kanban, Chat, Cards, Network, Graphview,
Sankey, Distribution, Icicle, Series, Treemap, Chart, Files, Graph, Schema, and
Detail alongside them). What each result pane needs from the SQL is under
[How a pane reads the query](#how-a-pane-reads-the-query). Drag a tab to
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
name across every query and panel. Panels write them as you interact, and
any query referencing the name picks the value up on its next run. The
parameter widgets above the editor write the same values, so a signal has a
typed control as well as a raw one.

The names the panes write are reserved, with fixed types:

| Name | Type | Written by |
| --- | --- | --- |
| `selection` | `Int64` | a click on a row: Table, Projection, Timeline, World, Kanban, Chat, Cards, Distribution, Series, Chart, Files |
| `selection_node` | `String` | stamped with every `selection`: the query node the row belongs to |
| `selection_id` | `UInt64` | stamped with `selection` when the result has a leeway id column |
| `selection_key` | `String` | stamped with `selection` when the result has a column named `key`; written directly by Network, Graphview and Sankey (a vertex id), Icicle (a frame label), Treemap (a leaf name) and Files (a path) |
| `selection_country` | `String` | World: the clicked country |
| `vp_min_x`, `vp_max_x`, `vp_min_y`, `vp_max_y`, `vp_w`, `vp_h` | `UInt32` | Map: the settled viewport in packed mercator, and its pixel size |
| `tl_min`, `tl_max` | `DateTime64(3, 'UTC')` | Timeline: the extent of the events drawn |
| `tl_from`, `tl_to` | `DateTime64(3, 'UTC')` | Timeline: the brushed window |
| `vf_t`, `vf_min_lat` … `vf_max_lon` | `DateTime64(3, 'UTC')`, `Float64` | Vector field: display time and view |
| `gv_*` | various | Graphview: selection, hover, camera and gestures (see [Graphview](#graphview)) |

Some of these **block** until a pane writes them, because no stand-in value is
honest: `selection`, `selection_id`, the `vp_*` viewport and `tl_min` / `tl_max`.
The rest are **seeded**, so a query reading them runs from the start: the
string names empty, `tl_from` / `tl_to` an unbounded window that keeps every
row, `vf_*` a time and box that select nothing, and the `gv_*` names empty or
zero. A query written against a seeded name should treat the seed as "nothing
chosen yet", as in `if({gv_focus:String} = '', 'default', {gv_focus:String})`.

The **signals** section at the top of the Graph tab lists them — value,
declared type(s), and who last wrote it (`param-widget` for the parameter
pane, `signals-editor` for this section itself, a panel's name for a panel) —
and is also where you set, add, or discard a name by hand, including names no
placeholder in the buffer mentions. A name read as different types by
different queries gets a conflict warning. `selection_id` is marked when it
lags the cursor: it follows the last row that carried a leeway id, so clicking
a row without one leaves it pointing at the previous match.

A referenced name that nothing fills (a name of your own, or a reserved name
that blocks) stops Run with a hint (instead of the server's "substitution not
set" error); the widget for that name carries the
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
- Results land in the **Table** tab and feed every result pane whose contract
  they meet (see [How a pane reads the query](#how-a-pane-reads-the-query)); the
  **status bar** names the outcome.
- **Conditions** (top bar, off by default; ADR-0121) rewrites the statement so
  that each OR-free part of its `WHERE` comes back as a named result column,
  so every row says which part of the query admitted it. It applies to
  retrieval queries over a known schema and is offered only where the
  endpoint's schema probe makes the rewrite possible.

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

### How a pane reads the query

Every result pane has a **contract**: what it needs from the SQL before it will
draw. The contracts come in three kinds, and a pane's section below says which
it uses.

- **Named columns of the active result.** Kanban needs `lane` and `title`,
  Chart reads `x` / `y` / `z` / `series`, Distribution reads `series`, `n`,
  `ps` and `qs`. Matching is on the column name, so alias your columns. A
  column that declares a gloss in its name matches on its label, so
  `body@text/markdown` is still Chat's `body` and `value@gloss/bytes` is still
  Icicle's `value`. Two exceptions match the bare name only: the Timeline's
  reserved `_tl_*` slots, and Kanban's `dot_<label>@<tone>` tallies, whose
  `@` names a colour.
- **Typed columns of the active result.** Series takes the first time-typed
  column as its axis and every numeric column as a lane, whatever they are
  called.
- **Named CTEs.** Network and Graphview read a CTE called `edges`, Sankey
  one called `flows`, Vector field one called `vector_field`, and several
  panes take optional decoration CTEs (`lanes`, `vertices`, `nodes`,
  `graph_opts`, `scores`, `spans`, `reactions`, `participants`). These panes
  ignore the final `SELECT` altogether. The CTE is found by name in the last
  Run, including one that nothing in the statement references. Each CTE runs on
  its own query with its dependencies folded in. It re-runs on its own when a
  signal it reads moves, without **Live**, which governs the final `SELECT`
  only. Editing the CTE's SQL still needs a Run.

The **active result** is the final `SELECT`, the node observed from the
**Graph** tab, or, for a tab bound to a node there, that node. Binding is how
two panes show two CTEs of one query side by side.

A pane whose required input does not match shows its reason in place of the
picture, naming the columns it wanted and usually a `SELECT` that would satisfy
it. An **optional** CTE that ran but does not match, such as a `vertices` CTE
without an `id`, is left out and the pane draws without it, with one line above
the picture naming the CTE and what it lacked.

The tab titles carry one mark each: `-` when the pane cannot draw what the
last Run would hand it, `*` when it writes a signal the buffer reads, and `!`
when it writes a name the buffer reads that nothing has filled yet (or, on a
tool pane, when it has something to report). A pane is never marked merely
because nothing has run.

Clicks publish signals (see [Signals](#signals-live-parameters)). The panes
whose marks are result rows (Table, Projection, Timeline, World, Kanban, Chat,
Cards, Distribution, Series, Chart, and Files for a file a row names) write
`selection`, the row cursor. Every
`selection` write also stamps `selection_node` (the node that row belongs to),
`selection_id` (when the result has a leeway id column) and `selection_key`
(when the result has a column named `key`). The panes whose marks are not rows
write a **value** to `selection_key` instead: Network, Graphview and Sankey a
vertex id, Icicle a frame label, Treemap a leaf name, Files a path.
Unselecting writes the empty string.

### Table

The result grid, in a leading-`#` selectable form. Click anywhere on a row to select
it — the selection is absolute (it survives paging), is published as the
`selection` signal, and drives the **Detail** view. A result with a column named
`key` also publishes that row's `key` as `selection_key`, and one with a leeway id
column publishes the id as `selection_id`, so a second query can follow the click
by value rather than by row number.
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

For a leeway-shaped result the **Leeway display** bar has a **Canonical
hashes** toggle (ADR-0219). On, the per-DB-row grid appends two columns after
the data: every row's **canonform** digest — its content identity, the same
for two rows whose content is the same whatever their column widths, aspects,
section names or attribute order — and its **canonwire** fingerprint — its
wire identity, the same only for two rows that are the same record. Both are
computed for the whole result by a background job; cells read `…` until it
lands, show the first sixteen hex characters, and carry the full value on
hover. Two rows with equal canonform digests and different canonwire
fingerprints differ only in something the content identity erases. The Detail
pane shows the same two values for the selected row, with a copy button and
the CBOR behind them.

### Detail

A structured card for the row selected in the Table tab. The card picks its rendering
from the result's column names:

- **Leeway card** — when the columns are leeway-encoded (`id:…`, `tv:…`), the card
  reads the row as attributes (ADR-0289): one row per attribute in its section,
  the **attribute** column naming it by its first membership — a ref by the name
  the session's registries give it — or by its plain column, the **labels**
  column holding its further memberships, and the **values** column its values,
  bytes as text when printable and as hex otherwise. Columns marked
  machine-readable only are left out. A `SELECT *` from a leeway table takes
  this path.
- **Ad-hoc grouping** — for ordinary SQL results (aliased or aggregated columns),
  columns are grouped by name prefix into pinned / relations / data / meta sections.
  A glossed column ([Glosses](#glosses)) renders through its gloss here: a
  block face where the gloss has one — markdown, highlighted JSON / SQL / Go,
  CBOR diagnostic notation, a decoded image, a hyperlink, a tagged id split
  into tag and counter with
  Copy buttons — else its one-line face under a caption naming the column and
  the media type. A declaration the catalog cannot honour shows the plain
  first line and says why. On the leeway card, values pass through their
  column's gloss too, and a column whose gloss has a block face gets it there
  as well, stacked under the inline line.

On the leeway card, an **identity** strip above the card shows the row's two
canonical identities (ADR-0219): the **canonform** digest (ADR-0201, keyed
BLAKE3 over the content form — hover it for the pin naming the classifier,
plains mask and digester it is a function of) and the **canonwire**
fingerprint (keyed BLAKE3 over the lossless wire item of ADR-0210, with the
item's byte length and whether the table-free checker accepts it). Each has a
**copy** button. A **CBOR** disclosure, closed by default, renders the items
both were computed from in RFC 8949 diagnostic notation — the canonform
attribute items and entity item as a sequence, and the canonwire entity item —
with the positions labelled (`/ version /`, `/ plains /`, `/ tagged /`, the
plain item types, `/ memberships /`) and a compact / expanded toggle. The same
notation is what `boxer.sh cbor diagnostics --pretty` prints on a terminal.

**For an agent.** `get_detail` reads the row as attributes: each named by its
first membership — a ref by the name the session's registries give it — or by
its plain column, with its values (bytes as text when printable, else hex; a
list's items; a set's in value order), its further memberships as labels, and
the `LW_GET` expression that reads it in SQL; columns the card hides are
counted, not listed. `get_canonical` reads the strip: both digests, the canonform pin and
the checker's verdict, and the items in the same notation — the canonwire item
is the row's whole content, losslessly — cut at a line boundary under the
operations' byte bound.

Above either card, when the selected row carries one or more **datetime attributes**,
a compact **timeline** plots them on a shared UTC axis. Each attribute is one legend
entry — a coloured swatch and its identity. A flag from a **tagged section** is
labelled with its attribute's name and labels and every
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
away; click another row in **Table**, or a mark in any pane that writes
`selection` (a Projection node, a Timeline event, a Kanban card, a Chart point,
…), to retarget it. The card follows the node the click came from, so a pane
bound to a CTE drives Detail with that CTE's rows.

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
made of the result, including rows dropped at the 100,000-row cap or carrying
no usable path.

### Projection

A neighbour embedding of the result's entities, drawn as a live graph. The
features are read off each row's leeway card, so the result must be
**leeway-shaped** — the rows of a leeway table such as `boxer.facts`, its columns as
stored (`SELECT * FROM boxer.facts LIMIT 3000`, or a column subset that keeps the
table's shape); an aggregate, a join or hand-picked columns are not, and the tab
says so instead of offering Compute.
Click **Compute projection** to run it (needs at least three rows): the rows' features
become a k-nearest-neighbour graph, HDBSCAN clusters it, and the graph is laid out
under the neighbour-embedding force model. The button becomes **Cancel** while it
works, and an fsmview chip shows the projector's lifecycle (extracting → running →
done, or failed / cancelled). **Neighbours** and **min cluster** apply on the next
Compute, as does **features**: *structure* (the default) builds the graph over a
hashed vector of which sections and attributes each record has, under cosine, so
records of one kind sit together whatever their values or sizes, *shape* over the
sixteen size-and-skew features under Euclidean distance, and *components* over
which registered component kinds each record carries, one column per kind, for a
facts-shaped result; **exaggeration** applies live and moves the same graph along the
attraction–repulsion spectrum — about 1 draws t-SNE, 4 UMAP, 30 ForceAtlas2 — after
an annealing schedule that starts high. **Colour by** fills nodes by any feature,
binned; **auras by cluster** draws a blob per HDBSCAN cluster with a legend, leaving
low-probability members and noise out; **edges** shows the neighbour edges the layout
runs on, off by default because they cover the picture. Drag pans and moves a node, ctrl+scroll zooms,
**fit** reframes, **re-lay-out** restarts the schedule, **settle** runs it ahead.
Click a node to select that row (it drives the Detail tab). Very large results are
sampled (10000-row cap) so the exact k-NN stays interactive. HDBSCAN reads each
row's density at its (min cluster − 1)-th neighbour rather than at the graph's
last, so a record kind smaller than the neighbour count is still found; the
defaults are 15 neighbours and a min cluster of 5.

A run belongs to the result it was computed over. The next result the tab is
fed — any run in the window, or a re-run of the same query — drops it, whether
the tab is in view or not, and the tab says so until the next Compute.
**Publish as dataset** (below) is how a run outlives the next query.

**Show** switches what is drawn under the status line, over the same run and its
cluster numbers. *graph* is the neighbour graph above. *archetypes* reads each
cluster as one line of what its rows typically hold — a number's median, a
label and its share — then the rows holding its lowest and highest values, then
only the rows that break the pattern, most telling first: an attribute the
cluster nearly always has and the row lacks (`−num·disk`), one it rarely has
(`+num·cpu`), a rare label with how many of the cluster's rows hold it
(`state failed (1 of 12 rows)`), an extreme number with its direction. *rows*
draws every row in its cluster's band; *row* draws the selected row against its
cluster's typical values and its nearest peers. Membership ids are named through
the session's registries. The **structure ↔ values** slider moves the cells from
presence to the values themselves, and **each row on its own ↔ one frame** from
each row listing its own attributes to every row drawn against one set of
columns. A row that shares under half of its cluster's attributes is read with
the unclustered rows, though the graph keeps it in its cluster.

**Why these clusters** (a collapsible section under the status line, present when
HDBSCAN found any) reads the clusters back off the feature set the run used: the
shape features as thresholds, the component kinds or structural items as
predicates. Each cluster's row
carries a rule as a SQL predicate over the feature columns, with the rule's
precision (how much of what it catches is the cluster) and recall (how much of the
cluster it catches), and a **copy SQL** button. With **one tree per cluster** on
(the default) the rule comes from a tree fitted to that cluster against everything
else, noise included — the answer to "what is in this cluster?", with each cluster
getting its own best split. Off, the rules come from one tree fitted to all the
labels at once — a single partition of the feature space whose leaves do not
overlap, whose summary line says how many clustered rows it reproduces, and in
which a small cluster can lose its leaf to the larger ones' splits. A leaf holds
at least 0.5 % of the clustered rows (and at least three), and its rule goes to
the cluster most of its rows are in, so in the partition a cluster under half
that size has no leaf at any depth — the row says so. A cluster's own tree
allows leaves as small as the cluster, so one tree per cluster reads even the
smallest. **Rule depth** cuts the same trees shallower or deeper, live: fewer
terms read easier, more terms fit closer. Thresholds are the shortest decimals
between the two adjacent values, so a copied predicate partitions the rows
exactly as the tree did. Beside the rule, the features that set the cluster apart: each is ranked by
the chance a member's value exceeds a non-member's (an AUC of 0.5 is no
separation), listed with the members' median against everyone else's, and only
when the separation is clear. Which rules run where depends on the feature set:
under *structure* and *components* a rule is a predicate over column handles —
`` has(`geoPoint:lv`, 'home') ``, `LW_COMPONENT_FILTER('Kind')` — which
runs against the result as it is; under *shape* it is thresholds over the sixteen
shape features, which are not columns of the result, so it runs against the
published rows dataset (below), where they are. Handles are spelled as the
physical columns spell them, as the Table's headers do; play folds both spellings
to one column. Either way the fit is stated, so a rule can be read with the
trust it earned.

**By attributes** switches the same table to what the clusters' entities *are*:
each entity becomes a set of items — its tagged sections and co-groups, short
values of its columns, its low-cardinality memberships — and per cluster the row
shows the best small conjunction of items held or lacked, as SQL over column
handles — `` length(`section:column`) > 0 `` for a section, `` has(`section:column`, v) ``
for a value, `` has(`section:lv`, 'name') `` for a membership,
`LW_COMPONENT_FILTER('Kind')` for a registered component the row carries —
which play resolves before the statement ships,
with its precision and recall, beside the items most over- or under-represented
in the cluster, each with its share in the cluster against the rest and its lift.
Only items that survive a Fisher exact test corrected for the number of items
and clusters are listed, and the summary line states the vocabulary and the
number of tests. Unlike the feature rules these run against the result as it
is; a rule whose item has no column in the result says so.

**Publish as dataset** (in the toolbar once a run is done, when the session has
capabilities) writes the run as two ad-hoc datasets, each on a stable handle of
its own that a query names as `keelson('<handle>')` — the scaffold that lands at
the caret spells both, and the summary beside the button shows them with their
revision. The rows dataset holds one row per projected entity — the result's row
index and its plain identity columns, the sixteen features under the names the
rules use, `cluster` (numbered as the tab shows, noise at −1), `probability`, the
layout `x` and `y`, `feature_set`, and `items`, the entity's item names as an
array — and the rules dataset one row per cluster and reading with the rule as
SQL, its precision, recall and coverage. A copied feature rule runs as written
against the rows dataset, and the attribute contrasts are an `arrayJoin(items)`
with a `GROUP BY cluster`. Publishing again republishes onto the same handles;
another play window reads them by the same names; they live for the session only.

**For an agent.** `compute_projection` runs the tab with optional neighbours,
min cluster and feature set, and raises it — the layout moves only while the tab is
drawn. `get_projection` reports the run: its status and error, the clusters with
their sizes and the noise, the status line, whether the layout has settled,
whether a new result dropped the last run, and, when asked for, a page of points
with their row, cluster, probability and position. The next result drops the
run: call `publish_projection` first when the run should outlive it, and read the
clusters back as `keelson('projection')` and `keelson('projection_rules')`.
`explain_clusters` returns "why these clusters", by features (at a rule depth, one
tree per cluster or the one partition) or by attributes: per cluster the SQL rule,
its fit and what sets the cluster apart — the same text the section shows.
`get_archetypes` returns the archetypes view as data: per cluster its rule, the
attributes every row holds with one value, what it typically holds, its
extremes, and the exception rows with their departures and result rows.
`list_panes` says up front when a result is not leeway-shaped.

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

Clicking an event writes `selection`. Dragging on the strip under the axis
brushes a window, published as `tl_from` / `tl_to`
(`DateTime64(3, 'UTC')`). With nothing brushed they hold an unbounded window, so
a query filtering on `t BETWEEN {tl_from:DateTime64(3, 'UTC')} AND
{tl_to:DateTime64(3, 'UTC')}` runs, and keeps every row, before any brush. The
**Map** reads the same window.

### World

A schematic world choropleth (ADR-0114) over the active result: it claims a result
whose string column resolves to countries (ISO 3166 alpha-2/alpha-3 codes or
country names), fills each country by the value column picked in the toolbar
(**auto** = first numeric; no numeric column falls back to presence-only fill), and
counts unmatched and duplicate rows in its status line (duplicates: last row wins —
the pane never aggregates for you). The country column is the first string column,
preferring one whose name hints at a country, in which at least half of the
sampled distinct values resolve. Hover reads `name · value`; clicking a country
selects its row, driving the Detail tab, and publishes the country as
`selection_country`. The **Snippets** library carries a
ready-to-run example ("World choropleth (countries)").

### Chat

A message transcript (ADR-0239) over a result naming `ts`, `sender` and `body`
columns, matched on the gloss label so `body@text/markdown` still claims `body`.
Optional `id`, `reply_to`, `system`, `deleted`, `edited_at`, `status` and
`conversation` columns add quote strips, centred system lines, deleted
placeholders, edited marks, delivery checks and a conversation picker; a
`reactions` CTE (`id`, `key`, `sender`) draws reaction pills under the bubble
with the reactors on hover, and a `participants` CTE (`sender`, `name`,
`color`) names and colours the senders. Every column the contract does not
claim renders inside the bubble through its gloss — an image column is an
attachment, a JSON column a payload — and, unglossed, as a caption line. The
**viewer** picker in the options row decides whose bubbles sit on the right:
with two speakers and a viewer among them the pane draws the SMS-style
dialogue, otherwise the group layout with names and initials discs. The
newest messages are drawn (300 by default; **older** above the first widens
the window), the view follows the tail until you scroll up, and **newest**
pins it again. Clicking a bubble selects its row, driving Detail and Table;
clicking a quote strip jumps to the quoted message. The **Snippets** library
carries a ready-to-run example ("Chat transcript").

### Cards

A paged grid of uniform cards (ADR-0245) over a result naming a `card_title`
or a `card_hero` column — for results whose rows are items with a face:
pictures, recordings, documents. The slots are `card_hero`, `card_overline`,
`card_title`, `card_subtitle`, `card_body`, `card_tags` (an array, or one
string), `card_tone` (`success`, `warning`, `error`, `info`, `accent`,
`neutral`, `disabled` — an accent edge) and `card_footer`, each optional and
each matched on the gloss label, so `card_body@text/markdown` still claims
`card_body`. `card_` is a reserved namespace: a misspelt slot (`card_titel`)
is refused by name rather than shown as something else.

**Every other column is a fact** — a `label  value` line on the card through
the column's gloss, inline face only, NULLs skipped, and `+k more` where they
do not all fit; Detail is where the rest is read, and clicking a card selects
its row there.

**Each row can say what it is.** A text column named `<label>_gloss` holds the
media type of that row's value in `<label>` — `content AS card_hero, mime AS
card_hero_gloss` — so one result can carry a picture on one card and a
recording on the next (see *Glosses* below: the row value is a gloss route of
its own, and the Table, Detail and Chat read it too). A value that does not
bind — `image/pgn`, `png` — is refused on that card, with the reason, not
rendered as something plausible.

An image hero is contained in a fixed-aspect box (16:9, 4:3 or 1:1 from the
toolbar) — never cropped, stretched or scaled past its own size — and only a
thumbnail the size of its box is kept, so a page costs thumbnails rather than
originals. An
`audio/wav` hero is the recording's waveform with play and pause; click the
waveform to seek, and starting one recording stops the other. Every card on a
page has the same height whatever it carries: long titles are cut at two lines
and bodies at the density's budget (**S / M / L**), with the whole text on
hover. The pager (12 / 24 / 48 / 96 cards) bounds what is decoded at once; a
page fills in over a few frames rather than in one long one, cards scrolled
out of view waiting until they are scrolled to. Arrow keys move the selection
once a card is clicked, turning the page at its edges; **PageUp** /
**PageDown** turn it outright, and **Space** plays or pauses the selected
recording. With nothing to draw the pane offers **publish sample
cards**: an ordinary ad-hoc dataset, `keelson('fixture_cards')`, of images and
recordings chosen to be awkward, and the query that reads it.

### Kanban

The result as cards in lanes (ADR-0122). One row is one card. The board needs a
`lane` and a `title` column, of any type. An optional `subtitle` adds a second
line. Up to three `dot_<label>` integer columns add tallies drawn as coloured
dots, and `countIf(…)` produces one. A dot column named `` `dot_open@warning` ``
picks its colour from the tone vocabulary (`success`, `warning`, `error`,
`info`, `accent`, `neutral`, `disabled`). Without a tone, the dots take
success, warning and grey in column order. A tally of zero draws no dot. The
board refuses a fourth dot column, a non-integer tally (`Float64`, `Decimal`)
and an unknown tone by name.

Lanes appear in the order rows first name them, so `ORDER BY` sets it. An
optional `lanes` CTE with a `lane` column declares them instead, in its own
row order, empty lanes included. A lane the rows use but the CTE leaves out is
added at the end rather than dropped. An empty lane value is titled `(none)`.

The board is read-only and caps at 2000 cards. The status line counts what it
left out. Clicking a card writes `selection`, driving Detail and Table. The
**Snippets** library has two boards: "Kanban board" and "ADR board".

### Network

A node-link graph laid out in ranks by Graphviz (ADR-0129), top-down or
left-right. It reads **named CTEs, not the final `SELECT`**:

- `edges` (required) has `source` and `target` of any type, and optionally
  `label`, `tone` and a numeric `weight`. Rows with an empty endpoint are
  dropped, and parallel edges between the same pair collapse to the first.
- `vertices` (optional, decoration) has an `id`, and optionally `label`,
  `group`, `shape` (`ellipse` / `oval`, `circle`, otherwise a box), `tone`,
  `weight`, `opacity` and `selected`. An endpoint with no `vertices` row is
  drawn anyway, undecorated. Tone wins over group colour. The tone vocabulary
  is Kanban's without `disabled`.

The final `SELECT` is free for whatever the Table should show, often
`SELECT * FROM edges`. The pane caps at 400 nodes and 1000 edges and says so.
Clicking a node writes its id to `selection_key`, and clicking it again
clears it. It does not move the row cursor, because a node is not a row of the
result. The **Snippets** library shows the contract on two corpora ("Tables
that share a schema", "Decision graph").

### Graphview

The same `edges` / `vertices` contract laid out live (ADR-0227, widened by
ADR-0231): a force simulation, a tree, or rings around chosen centres, which
you can drag, zoom and pin. It honours more of the contract than Network:

- **Size, colour, opacity, aura.** `weight` sizes nodes and thickens edges, and
  `radius` overrides it. `group` is drawn as a translucent aura once
  **auras by group** is ticked; a `groups` string-array column gives a node
  several. `donut`, `donut_total` and `donut_tones` draw a ring. `shape` is
  ignored, since every node is a circle.
- **Placement.** `pin_x` / `pin_y` fix a node. `start_x` / `start_y` seed one
  that is new to the picture. `pull_x` / `pull_y` / `pull_strength` draw it
  towards a point. `center` marks the centres of the radial layout. `lat` /
  `lon` place the graph on a map, over a basemap or offline country outlines,
  and a pin on the same row wins over them.
- **Interaction.** `pick` and `selected` preselect a node. `fit` names the nodes
  the camera frames. `label_always` keeps a label drawn past the 48-node
  label threshold. On edges, `id` distinguishes parallel edges, and `opacity`,
  `length` and `strength` tune the spring.

An optional one-row **`graph_opts`** CTE carries settings: `layout` (`force`,
`force_gravity`, `hierarchical`, `radial`, `random`), `orientation`,
`force_model`, the spacings (`ring_dist`, `row_dist`, `col_dist`, `k_scale`,
`gravity`, `exaggeration`), `hide_edges`, `undirected`, `pin_on_drag`, and
the encodings `size_by`, `tone_by`, `opacity_by` and `aura_by`. An encoding
names a column of `vertices` or a graph metric: `degree`, `in_degree`,
`out_degree`, `pagerank`, `betweenness`, `kcore`, `triangles`, `clustering`,
`clique`, `component`, `scc`, `component_size`, `distance`, `distance_in`,
`distance_out`, `relevance`. A leading `-` inverts the scale. `component` and
`scc` are categories, so they can colour or group but not size. The distance
metrics measure from the selection, or from the hover with
`distance_from = 'hover'`. A setting the pane cannot use is named in the status
line rather than guessed at. A control you move in the toolbar outranks
`graph_opts`, which outranks the default.

The pane publishes more than any other, all under `gv_*`, and all seeded so a
query reading them runs from the start. Selection and hover are **state**:
`gv_selection` (`Array(String)`), `selection_key`, `gv_hover` (after a short
dwell) and `gv_aura_hidden`. The camera is also state, written when a pan or
zoom settles: `gv_min_x` … `gv_max_y`, `gv_zoom`, and `gv_min_lat` …
`gv_max_lon` when located. Gestures are **moments** that keep their last
value: `gv_focus` (double-click), `gv_context` (secondary click),
`gv_edge_source` / `gv_edge_target` / `gv_edge_id` (edge click), `gv_pin_id`
/ `gv_pin_x` / `gv_pin_y` (and `_lat` / `_lon`) at the end of a drag, and
`gv_bg_x` / `gv_bg_y` (background click).

Because the graph CTEs re-run when a signal they read moves, a graph can grow
by query. An `edges` CTE that reads `{gv_focus:String}` re-centres on the node
you double-click, and a `vertices` CTE that feeds `gv_pin_*` back into `pin_x`
/ `pin_y` makes a drop stick. When only Graphview's own `gv_*` signals moved,
the rebuild keeps the camera and the surviving nodes' positions. A Run, an edit,
or another pane's signal reframes. The caps are 2000 nodes and 6000 edges.
The **Snippets** section "The graph contract, widened" walks through all of
this.

### Sankey

A flow-quantity diagram (ADR-0159) over **named CTEs**:

- `flows` (required) has `source`, `target` and a positive `value`, plus an
  optional `label` and `tone`. Rows repeating a pair are **summed**, unlike
  Network. Rows with an empty endpoint, a value that is not a positive finite
  number, or a self-flow are dropped and counted.
- `nodes` (optional) has an `id`, plus `label`, `stage` (a column index ≥ 0),
  `order` (the position within a stage), `group` and `tone`.

When **every** node, including those named only by `flows`, has a `stage`, the
diagram is an **alluvial**: fixed columns, ordered within each column. Otherwise
it is a Sankey laid out from the flows, and a cycle cannot be laid out. The
**mode** control forces one or the other, and an alluvial without stages says
which node lacks one. **fill** chooses polygons or columns (with an optional
gradient), and **hide labels** clears the bars. The caps are 300 nodes and 1500
flows. Hover reads a bar's in and out or a ribbon's share of the total, and a
click pins it. A pinned bar writes its node id to `selection_key`, and a pinned
ribbon or no pin writes the empty string. The **Snippets** library has three
diagrams ("Flow diagram", "Where the disk went", "Query outcomes").

### Distribution

Distribution summaries side by side (ADR-0161): one row per series, in the
contract the `descriptiveStatistics(…)` macro emits, so most queries start
there. By exact name:

- `series` (any type), `n` (an integer count), and `ps` / `qs` (arrays of
  `Float64` or `Float32`). These are the quantile levels and their values, at
  least two of each, with `ps` strictly increasing inside (0, 1) and `qs`
  non-decreasing.
- Optional: `n_null`, `x_min` / `x_max` (the extremes, for the boxen view),
  `estimator` (`exact-hf7` drops the "excludes sketch error" caveat), and the
  histogram triple `hist_lo` / `hist_hi` / `hist_w`, which must come all three
  together or not at all.

One row that breaks the grid rules rejects the whole result, naming the row.
The views are **ECDF**, **Shift** (when two or more series share one `ps`
grid), **Boxen**, and **Histogram** (when every series carries the histogram
columns, which the macro does not emit). The pane draws 32 series and
confidence bands on up to three, then only on the selected one. Clicking a
series chip writes `selection`, whose row is the series, and the selection is
the baseline the shift function compares against. A Table click selects it too.
The **Snippets** library has four ("Distribution summary", "Same mean and sd,
four different shapes", "Two treatments and the shift function", "A real
corpus").

### Icicle

A hierarchy as an icicle plot or a flamegraph (ADR-0160). It accepts either of
two contracts, by exact name:

- **Paths**: a `stack` array (any element type, read as text) and a `value`,
  one row per root-to-leaf path. Rows sharing a path are summed and interior
  frames are created. A path in a string column becomes an array with
  `splitByChar('/', path) AS stack`.
- **Nodes**: one row per node with `id`, `parent` and `value`, and an optional
  `label`. An empty or `NULL` parent is a root. An unknown parent or a
  self-reference becomes a root and is counted, and a repeated id keeps its
  first row.

`value` is the node's **own** amount, not its subtree's. It must be a finite
number ≥ 0, and a numeric string works too. An optional `unit` labels it. The
toolbar sets orientation (icicle or flame), sibling order (value, name,
input), colour (branch, label, depth), a prune threshold, and labels. The caps
are 20,000 frames and depth 256. Clicking a frame pins it and writes its label
to `selection_key`. The **Snippets** library has "Where the bytes went in a
leeway table", "The same disk, as an icicle" and "One row per node".

### Treemap

The same hierarchy as nested areas (ADR-0166), over Icicle's contracts, plus a
second measure. An optional **`color`** column colours the cells. A numeric
`color` is a scale, which `color_min` / `color_max` / `color_unit` can pin;
parents take their children's value-weighted mean. A string `color` is a
category; parents take it only when all their children agree, and categories
past seven reuse hues. A forest is gathered under a root named `all`. The
toolbar sets the colour reading (value or category, or depth) and how deep to
show (drill, three, four, all). Drilling is a place in the view and is not
published. Clicking a **leaf** pins it and writes its name to `selection_key`.
The **Snippets** library has "The same hierarchy as area, and a second measure"
and "Rolling the tail up into its parent".

### Series

Numbers against a time axis (ADR-0163). The one result pane that claims by
**type**, not by name: the first `DateTime64`, `Date` or `Date32` column is
the axis, and every integer or float column is a lane, up to twelve. A plain
`DateTime` arrives as a bare `UInt32` and becomes a lane rather than an axis,
so wrap it in `toDateTime64(…, 3)`. The pane says so when it is missing. It
draws gaps and `NULL`s as breaks and fills nothing in. The toolbar classifies
the grid (regular, regular with gaps, irregular, unordered, too short) and
offers to write the missing `ORDER BY`, `WITH FILL` or `GROUP BY
toStartOfInterval` into the editor. Above a few points per pixel the line is
drawn as a min/max envelope, so a one-sample spike survives. Clicking writes
`selection`, the nearest row.

Two optional CTEs overlay the plot. `scores` (a time column plus a numeric
`score`, optional `warm_up`) adds a linked score plot underneath, and `spans`
(`_tl_band_from`, `_tl_band_to`, `_tl_band_color`, optional `_tl_band_label`)
shades ranges. When either CTE fails or is still running, the status line
says so.

The `ts*` functions produce exactly those shapes. They run **in play**, not on
the server: `tsSmooth(t, v, halfWidth)`, `tsProfile(t, v, window)`,
`tsAnomalyScores(t, v, window)` and `tsAnomalySpans(t, v, window, k)`. Each
must be the only select item of a top-level CTE, reading another CTE of the
same query with no other clauses:
`scores AS (SELECT tsAnomalyScores(t, v, 60) FROM base)`. What reaches
ClickHouse is `base`, and the transform runs over its rows. Integer arguments
may be `{name:Type}` slots, so a signal can drive the window. Nothing in the
statement may read a client-computed CTE; bind a pane to it instead. With a
`scores` node over the charted lane, the score plot gains a baseline and
confirm / false-alarm buttons per flagged span, which append to
`boxer.tslabels`. The **Snippets** library covers this from "A number against
time" through "Reading a client node".

### Chart

The plain chart the specialised panes leave uncovered (ADR-0172), by exact
name. With a **`z`** column the result is a heatmap: `x` and `y` are the grid
keys and are required, `z` the numeric cell value, one row per cell. Duplicate
cells and grids past 40,000 cells are refused with the `GROUP BY` that fixes
them. Otherwise every integer or float column other than `x` is a **lane**,
including one named `y`. `x` is optional (rows number themselves without it),
and a `series` column splits the rows into groups. A key's axis follows its
type: time-typed keys are shown in UTC, numbers are sorted, and anything else is
a category in first-seen order, never sorted. The mark chips choose bars, lines
or points (bars by default for a categorical `x`), and **log** appears when
every value is positive. The pane reads 100,000 rows and draws 24 lanes,
counting the rest. Clicking writes `selection`. The **Snippets** section "Bars,
lines and a heatmap" has six examples.

### Map

An in-database-rendered geo raster over a pannable map (ADR-0096), for tables with
`mercator_x` / `mercator_y` columns (e.g. the ADS-B demo loader's
`planes_mercator`): the visible viewport is rendered to pixels by a ClickHouse
query on each pan/zoom settle. Table, sampling, colour mode and opacity are panel
controls — this tab queries on its own, independent of the editor's result. The
table field takes a table or a table function. With **refine** on, a settled view
is drawn first from the most-sampled table and then refined towards the full one,
by the naming convention `<base>_sample100` → `<base>_sample10` → `<base>`; a
level the server does not have is skipped.

The colour modes are **Altitude & Speed** (needs `altitude` and `ground_speed`),
**Density** (any table with the two mercator columns), **Speed** (needs
`ground_speed`) and **Custom**. A custom expression is spliced into the raster
query: it must define `red`, `green` and `blue` (0–255), may define `alpha`, and
can read `total`, `max_total`, `transparency` and aggregates of any column.

A **time column** names the source's timestamp. While the Timeline has a window
brushed, the raster keeps only rows inside `tl_from` / `tl_to`, so the map follows
the brush.

The settled viewport is published as `vp_min_x`, `vp_max_x`, `vp_min_y`,
`vp_max_y` (packed-mercator bounds) and `vp_w`, `vp_h` (output pixels), all
`UInt32`, so any query can reference `{vp_min_x:UInt32}` … to cross-filter
against the visible extent. They block until the map has settled once.

### Vector field

A gridded two-component field — a wind, an ocean current — drawn as particles
drifting on a map (ADR-0250). It reads the `vector_field` CTE of the query **by
name**: `lat` and `lon` in degrees, `u` and `v` as the eastward and northward
components, and optionally `t` (a `Date`, `DateTime` or `DateTime64`) for a time
series. The grid must be regular in latitude and longitude with one row per
node per step; filter a table that holds several levels, runs or members down
to one, or the pane refuses it rather than average them together. `NULL` and
non-finite components are missing data, and the particles stop short of them.

The pane never fetches the field. It reads the CTE's column names, and then asks
ClickHouse for a reduced window of the current view — one query per settled
pan, zoom or time step, with every value sent as a parameter. The status line
counts those requests and reports the last one's rows and duration, and
**Window query…** opens the last one in a playground of its own, ready for
`EXPLAIN indexes = 1`. A table ordered by `(t, lat, lon)` and a CTE that only
renames columns let the window read a small part of the table; a CTE that
computes `lat` from something else reads the whole step.

What the picture does not say: the animation shows direction and relative
speed, not transport. A particle's pace is a screen quantity and the same at
every zoom; colour follows the speed. Between two steps the field is blended,
so a moving front fades across instead of travelling.

A field with more than one step gets a **time strip** under the transport
buttons. Steps sit at their own times, so an hourly-then-three-hourly forecast
looks uneven because it is. Each step has a bar for the mean speed *inside the
current view* and a cap up to the largest speed read there, in the particles'
colours — so the strip answers "when is it windy here", and changes when you
pan or zoom. It costs one more query per settled view, over every step, and is
decimated to what the map shows; the maximum is of the nodes read, not of every
node. Bars are scaled to the largest mean, and a gust that runs past the top is
drawn clipped with a mark; the hover readout has its number. Under each step a
notch says what the layer has of it, by shape and not by colour, since the
colours are the data's: filled for loaded, hollow for loading, a cross for
missing, a short tick for a step nobody has asked for. The strip tints the time
before now, shades alternate days, and marks each model run it can see.

Drag the playhead to scrub — the field blends between steps, the strip marks
the step letting go would settle on, and the readout gives that step's speed —
or click to jump, or use **First / Prev / Play / Next / Last**. The band along
the top is the loop range's alone: drag it for a range, drag an edge or the
body to change it, double-click to clear it, and Escape while dragging puts
back what was there. **In** and **Out** set an end at the playhead without
dragging, and **Clear range** and the buttons that do not apply stay in place
greyed out rather than coming and going under your hand. The speed menu and the
**Loop / Bounce / Once** button set how playback runs, and playback holds the
last step for a second before a loop wraps; **Now** goes to the step nearest
the present, and is lit when you are already there. The field beside the
readout takes a time, a date, a clock time on the day shown, or a step as
`#12`.

With the strip focused: Space plays and pauses, the arrows step, Shift or
PageUp and PageDown move a stride of them, Ctrl an arrow moves a day and Alt an
arrow a model run, Shift+Home and Shift+End set the range's ends, and Delete
clears it. Playback waits at a step until the next one's window has arrived
rather than showing half of a blend; the readout names the step it waits for
and how long it has waited, and says the rate it is achieving when waiting has
pulled it under the one you chose. It does not wait for a step the source could
not serve. Because the rate is in steps, the playhead covers more ground per
second where the steps are further apart — the readout gives the length of the
step under it, so that the change of pace has a number.

The strip also tells the layer which steps playback reaches next, so they are
fetched before the playhead arrives instead of at the moment it does. That is
why stepping on often costs no wait, and why the request count in the status
line grows a little faster than one per step visited.

An optional `vector_field_opts` CTE is one row of settings: `name`, `unit` and
`speed_max`, the magnitude at the top of the palette (without it, a high
quantile of the first step). Hover reads the field under the pointer.

An optional `vector_field_sites` CTE marks **where the field was measured** —
one row per station, with `lat` and `lon` in degrees and an optional `label`
and `radius_km`. Nothing else on the map separates measurement from
interpolation: a field gathered from stations two hundred kilometres apart is
drawn exactly as smoothly as a model's own grid, and the particles are as
confident over an ocean nobody sampled as over an anemometer. The **sites**
toggle draws a dot at each one, **coverage** draws the `radius_km` circle
around it, and labels appear once few enough sites are in view to name. The
status line counts them. A field that came off a regular grid has no sites to
declare and should not declare any.

Hover's bearing is the **direction the flow comes from**, the wind convention.
For a field whose vectors are the direction something travels *towards* — a
migration, a current of drifting objects — the readout is a hundred and eighty
degrees from the quantity, and the CTE that fed it is the honest place to read
the heading.

The pane publishes the valid time of the step on display as `vf_t`
(`DateTime64(3, 'UTC')`) and the settled view as `vf_min_lat`, `vf_max_lat`,
`vf_min_lon`, `vf_max_lon`, so another query can follow the map; writing `vf_t`
— a `SET`, the signals section — moves the display time. Until the pane has
written them the names hold seeds that select nothing (the epoch, an empty
box), so a query that reads them runs from the start and returns no rows; tick
**Live** and it follows the pane from then on.

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
The structure is read off the physical column names, so a column subset that
keeps the names is still read as leeway, with each section's membership
channels (`low-card-verbatim`, `low-card-ref`, …) as the badge beside it; an
alias or an aggregate is not. Section and column names are spelled as the
columns spell them (`u32Array`, where leeway's canonical style is `u32-array`),
as the Table's headers, `leeway.columns` and `describe_table` print them.

**For an agent.** `get_schema` reads what the pane draws, for the result the
pane is fed or a named node: the plain columns and the tagged sections, each
section's membership channels, and each column's handle and canonical type.
`list_tables` and `describe_table` read the endpoint's catalog instead, with the
tables' comments.

### Docs

The server's own documentation for whatever the caret is on: a function, a data
type, a table engine, a format or a setting. When the caret rests (a quarter
second), the pane looks up the name under it and then the calls around it, so
inside `toHour(|)` it shows `toHour`. **Follow caret** turns that off, and the
**look up** field takes a name directly and wins over the caret.

The source is a query against the endpoint, run like any other (routing, auth,
rewrites):

```sql
SELECT name, toString(type) AS type, description, source
FROM system.documentation
WHERE lower(name) = lower({n:String})
```

That query is joined to the server's SQL user-defined functions
(`system.functions WHERE origin = 'SQLUserDefined'`), whose page is the
function's `CREATE` statement, so the leeway `LW_*` surface documents itself.
`system.documentation` arrived in ClickHouse 26.x, and on an older server the
pane says so. A name with several kinds (`Array`, `JSON`, …) gets a kind picker.
Code blocks on a page carry Insert / Replace, and links to other ClickHouse
pages open in the pane. To search all the documentation at once, including
play's help and the ADRs, use `docsearch('…')` in a query (see the **Snippets**
page). An embedder can replace the source (the `play-pluggable-docs` how-to).

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
Seven sections, in order. **Statement** is the parse status of the (debounced) editor buffer;
when boxer's built-in grammar rejects it, an `EXPLAIN AST` probe against the live
endpoint tells you whether that is just a boxer grammar gap (ClickHouse parses it —
the statement will run, with the canonical preview, parameter widgets, query-graph
split and pre-execute rewrites unavailable) or genuinely broken SQL (ClickHouse's
own diagnostic is shown, with positions matching the editor). **Pre-execute
rewrites** lists any rewrite that failed and was skipped, with its full error;
the statement ships without it (see [Passes](#passes)). **Column resolution**
names each leeway column handle in the buffer that does not resolve, with
suggestions, before anything reaches the server. **Security context** gives the
buffer's security class (read, read-egress or mutating), the construct that
raised it, and the tables the statement returns 1:1. **Query graph** is the
split status of the last Run. **Signal emits** reports panel signal writes the
store dropped. **Last run** carries the full execution error — the status bar
shows only its first line — or the usual result summary.

An agent reads the same seven sections with `get_diagnostics`: the statement's
status with ClickHouse's own diagnostic when the grammar rejects it, the skipped
rewrites once the pane has measured them, the unresolved handles with candidates,
the security class with its witnesses and the tables returned as stored, the split,
the dropped emits, and the last run's full error. `validate_sql` checks a statement
before it is set; `get_diagnostics` reads the buffer as it stands.

### Passes

The pre-execute rewrite pipeline (ADR-0108, ADR-0119) over the statement Run
would ship, drawn as a spine of passes in the order they apply. It is the same
catalog a query can read as `keelson('sql_passes')`. Most of the client macros
the Vocabulary lists are here: `descriptiveStatistics`, `docsearch`, the
`LW_ID_*`, `LW_COMPONENT*`, `LW_GET*` and constructor families, `gloss()`, the
lading snapshot functions, and leeway column-name resolution. Passes bound late
to this client are drawn recessed.

Each pass is coloured by what it did to **this buffer**: rewrote it, left it
alone, failed and was **skipped** (the statement ships without that rewrite,
and Run is not blocked), or **declined** because the client's binding does not
support it. A pass that took over 100 ms is amber. The line under the canvas
counts each outcome and names every skipped or declined pass with the first line
of its error, and Diagnostics has the full text. Clicking a pass shows its
description, properties, provenance and outcome on this buffer. A **rewrite
cost** section breaks the last Run into compile, server and transfer time and
the rewrite into its passes. Play's own steps (parameter extraction, SQL-valued
parameter splicing, the **Conditions** rewrite, appending `FORMAT`) appear in
the outcomes but have no box on the spine.

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
  extraction and constructor families (`LW_GET*`, `LW_PLAIN` / `LW_TV*`,
  `LW_COMPONENT` / `LW_COMPONENT_FILTER`, the lading snapshot functions `fs`,
  `fsdata` and `fssnap` —
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
its tag and counter, a CBOR item as diagnostic notation, a regular expression
highlighted with the verdict its engine gives it, and the ADR-0123
content types (markdown, code, images) as one family. Every gloss
has a one-line face for the Table grids and, some, a block face for Detail —
in the ad-hoc pane and, stacked under the row's other values, in the leeway
card.

Five routes bind a value to a gloss, in precedence order:

- **a row value** — a text column named `<label>_gloss` beside the column whose
  label is `<label>` names the gloss *per row*, in the alias's spelling
  (`image/png`, `gloss/duration;unit=ms`), and outranks everything below for
  the rows where it is set (ADR-0245); NULL or empty falls through to the
  column's own binding. It is how a table that stores `(content, mime)` pairs
  is shown with no projection per kind. A token binds once per result, and one
  companion binds at most 32 distinct tokens. The slash rule applies per
  value: outside the `card_` namespace a value with no slash is a value, not a
  declaration, since a table may simply have a `lip` and a `lip_gloss`; one
  with a slash that does not bind marks its cell in the warning tone;

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
  for `sem:json*`, `gloss/ipaddr` for the network canonical types `ct:v` /
  `ct:w` — and `gloss/raw` in a rule switches an affinity off for the columns
  it matches;
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

`gloss/ipaddr` is the other one worth calling out, because how automatic it is
depends on where the column came from. An address survives a query as one of
two things, neither of which reads as an address — switch **Raw cells** on to
see them: ClickHouse's `IPv4` arrives as a big-endian `UInt32`, so `1.2.3.4`
reads `16909060`, and its `IPv6` as 16 packed bytes, a hex blob. The gloss
writes both out, along with the CIDR forms that carry their prefix length in
a trailing byte.

Its affinity is the **canonical type**, `ct:v` / `ct:w` and their CIDR (`c`)
and array / set (`h`, `m`) spellings, so:

- a **leeway** column is glossed with nothing declared — the spec line carries
  its canonical type, which says the column *is* an address, a stronger claim
  than the `sem:` affinities make about their columns;
- a **plain** result carries no `ct:` token, only a name and an Arrow type, so
  an address column there is not glossed on its own. Sixteen bytes are sixteen
  bytes; nothing in the result distinguishes an address from a hash. Declare
  it — `` AS `peer@gloss/ipaddr` ``, `gloss(peer, 'gloss/ipaddr')`, or a
  `-- play: gloss gloss/ipaddr name:.*_ip` rule over a naming convention you
  keep.

A value the gloss cannot read as an address keeps its plain rendering in the
error tone rather than inventing one.

`application/cbor` is the content family's binary member. A column holding
CBOR bytes — a canonform record, a canonwire entity item, a pushout patch —
reads as a hex blob otherwise; the gloss shows RFC 8949 §8 diagnostic
notation instead. In Detail it is the pretty rendering, highlighted and
indented, one element per line inside a container that does not fit the line,
with the tags it knows named in a comment; in a Table cell it is the compact
one-line notation. `;sequence=1` reads the cell as an RFC 8742 sequence of
items rather than one item — without it, bytes after the first item are
reported as the truncation they usually are.

A malformed item is shown rather than hidden: what parsed is rendered, the
failure is an error comment naming the byte it stopped at, and the remainder
follows as hex — the cell shows the compact form of that in the error tone,
which is what makes the bad row findable in a grid of good ones. The column
must hold the bytes themselves; a hex or base64 source has no support yet and
`;encoding=` is refused rather than guessed at. Past a kilobyte a cell shows
the type and size, as an image cell does, and the full notation waits in
Detail. There is no affinity — no aspect says "these bytes are CBOR", and
being a `Binary` column does not make a column one — so bind it by alias,
by `gloss(…)`, or by rule.

`gloss/regexp` shows a stored pattern as a pattern. A rule table's match
column, a router's dispatch column, a redaction policy: patterns nobody
re-reads until one of them stops matching, and in a grid indistinguishable
from prose. The cell shows the pattern with the verdict Go's RE2 engine gives
it — a `✗` in the error tone when it no longer compiles, and nothing when it
does, so the colour is spent on the exception rather than on the column. Past
a kilobyte the verdict is dropped rather than recomputed every frame; a
pattern that long was not written by hand.

In Detail the pattern is parsed and highlighted — group parens coloured by
nesting depth, so a nested pattern's brackets pair up by eye — and under it
sits a magnifying glass, the compile dot and a toggle. The toggle opens the
**regex explorer** in a window tethered to the cell by a bezier and seeded
with that cell's pattern: a haystack to test against, the captures it takes,
the List and Replace tabs, and the same clickhouse-local lane the standalone
explorer runs on, so a pattern can be tried against the engine that will
actually run it. Editing the pattern in the explorer changes nothing in the
result — the window is a bench, not an editor for the row. It has no
affinity: nothing in a spec line says a text column holds a pattern, so
declare it or rule it.

The tab shows the **catalog** (each gloss with its accepted value kinds,
parameters, a sample rendering, its affinities, and two Insert buttons —
*Insert rule* drops a `-- play: gloss` line at the caret, *Insert call* a
`gloss(expr, …)` projection item with the same token), the buffer's effective
**rules** (compiled, or refused with why), and the current result's
**columns** with their spec line, what each resolved to, and the rules that
matched but lost: a later directive behind an earlier one, an affinity behind
a directive, any rule behind an alias. **Raw cells** on the Table toolbar
bypasses every gloss for the session.

### A language model

Play has no model pane of its own. Explaining a query, fixing its error or
turning a question into a query is the chat app's, which works in a play
window you share with it. Its model reads the endpoint through play's
operations without touching the buffer: `list_tables`, `describe_table` (for
a leeway table, its sections, the handles to write and the membership
channels) and `validate_sql`, which checks a draft — grammar, play's rewrite
of it, handles that do not resolve, and whether a run would be allowed —
without running it. `trace_rewrite` shows the same rewrite as the Passes tab
does: each pass in order with its outcome, time and error, and the statement
exactly as it would be sent. `list_datasets` names the ad-hoc datasets this
window has bound, which the model reads with `keelson('<alias>')`; a grant
names one as `keelson:<alias>`. `bind_dataset` binds an alias another window
published — the window waits for it when nothing is published under it yet,
and `list_datasets` lists a waiting alias with why — and runs nothing.

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
