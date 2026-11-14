---
type: adr
status: proposed
date: 2026-11-13
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0371: Sealed notebooks — a report as a markdown document of typed cells, with SQL as the expression language

## Context

A result a person wants to keep and hand on — figures, a table, a chart, a
recording, and the prose that says what they mean — has no document form. The
parts exist separately. The markdown widget draws a document
([ADR-0180](./0180-markdown-rendering-fidelity-pass.md)). Play's Detail pane
draws one row whose cells declare a content type
([ADR-0123](./0123-play-content-typed-detail-cells.md)), through renderings
the gloss catalog names by media type
([ADR-0186](./0186-play-gloss-catalog.md),
[ADR-0245](./0245-play-cards-panel-and-row-value-glosses.md)). An applet is a
markdown document around one SQL buffer
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)). A chat artefact is a
markdown document with images sealed by content hash and exported as a
snapshot ([ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md),
[ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md)).
A markdown document is ingested into `boxer.facts` as rows
([ADR-0218](./0218-markdown-ingestor-facts-model.md)). A table an app holds is
served to SQL without being stored
([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md)), and a
statement that computes nothing is answered without ClickHouse
([ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md)).

Two earlier records left this ground open. ADR-0132 rejected composing a page
from many independent queries, because N query graphs on one surface share no
signal store, and deferred documents holding several applets. ADR-0097 §SD12
deferred explicit multi-cell authoring until the single buffer fails someone.
This record takes up both: a reading audience and stored results are needs
one live buffer does not serve.

Two kinds of prior art frame the choice. HTML is a tree of nested elements
with layout, some of them (`<iframe>`, `<object>`) whole documents of their
own. A Wolfram notebook is a flat list of typed cells grouped by their
headings, in which an output is stored beside the input that produced it and
a programme can write cells. The second is the model followed here, with one
difference that shapes SD6: a Wolfram cell holds any expression, and a SQL
value is a relation with typed columns.

The forks below were put to the maintainer on 2026-11-13 and decided: an
element is a top-level block; the text with its blobs is the artefact; a
figure in a sentence stays a reference to a stored value; a notebook is
written whole, by a programme or an agent, and agents may edit one
afterwards; a notebook's SQL cells form one play buffer; collection and
transformation are a phase apart from presentation, and only the first is
stored; a presentation statement may rename a column, which the trivial
evaluator learns, and may not filter or order; the renderers are play's
panels; the first milestone covers stored output alone.

The reader, the cell stack, the renderers and collection were built on
2026-11-13 and 2026-11-14, ahead of this record's review, to test the
design against the tree. Where a subdecision rests on what that showed, it
says so with a date.

## Design space (QOC)

**Question.** What is the contract of a report: what is authored, stored and
handed on?

**Options.**

- **O1** — Embedded applets: a markdown document in which each visualization
  is an applet instance drawn inline, each with its own query lane.
- **O2** — Composed in Go: a report is a widget that lays out other widgets,
  and markdown is one of them.
- **O3** — Markdown with new elements: the markdown widget gains a hook that
  draws registered fences and embeds wherever they occur.
- **O4** — Typed cells: a report is a sequence of top-level cells, each a
  body with a media type; markdown is the text form of the sequence and a
  cell type within it.

**Criteria.**

- **C1** — Reads without boxer: in a diff, on a forge, by a model.
- **C2** — Reads from SQL: the report's parts are rows.
- **C3** — Holds still: what a reader sees does not depend on a server or on
  the day it is opened.
- **C4** — Cost of a frame, and of the markdown widget's contract.
- **C5** — A query or an agent can write one.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | −− | +  | ++ |
| C2 | −  | −− | +  | ++ |
| C3 | −− | −  | +  | ++ |
| C4 | −− | +  | −  | +  |
| C5 | +  | −− | +  | ++ |

## Decision

A **notebook** is one markdown document that is a sequence of typed cells,
with the blobs its cells name. SQL is the expression language, in two phases.
**Record** cells collect and transform on the server, and their results are
stored. **Presentation** cells select from records and are evaluated where
the notebook is read. A notebook whose records are all stored and fresh is
**sealed**, and a **report** is a sealed notebook read with its SQL hidden.

### SD1 — The model: a sequence of cells

A notebook has four kinds of cell. A **record** and a **presentation cell**
are `sql` fences (SD3, SD6). A **media cell** is a blob the markdown widget
does not draw — a recording. Everything between two such cells is one
**prose** cell of type `text/markdown`: headings, paragraphs, images, and
fences in any other language.

A record, a presentation cell and a media cell are each one top-level block
of the parse, the unit
[ADR-0334](./0334-mdedit-blocks-over-the-source.md) §SD2 edits by, and each
starts in column 0. The cells then tile the text: every byte of the body is
in exactly one cell. A reader finds them with a line scan, and writing a
notebook back is concatenation. On the markdown documents of this repository
the line scan and the markdown parse name the same top-level fences
(observed 2026-11-13); a test keeps the two in agreement.

A fence or an embed inside a list item, a blockquote or a callout is
indented, so it is not a cell. It is drawn as the markdown widget draws it.

A record carries an id, the Obsidian block id of
[ADR-0347](./0347-mdedit-links-a-url-scheme-a-location-grammar-and-launch-options.md)
§SD4. Headings group the cells under them, as they group sections for a
section filter.

### SD2 — The artefact: text and blobs

The markdown text and the blobs it names are the notebook. A blob is a file
beside the text, named by its content hash as ADR-0284 names a screenshot,
with an extension that says its media type. The text holds no blob bytes in
any encoding.

A record's result is always a blob: collection names it, and the pin (SD8)
is a hash over those names. A picture or a recording may be one, and is then
checked against its name like a record. Under any other name it is read as
the file it is, as in any markdown: a notebook written by hand names its
pictures as its writer did, and a reader that drew fewer of them than the
text names would show less than the markdown it also is. A file the text
names that cannot be read is a problem the reader lists, and the notebook is
not sealed.

The relation of cells — one row per cell — is a projection of the text, not
a second source. One package defines it.

### SD3 — Spelling: a text body is a fence, a blob body is an embed

| Cell | Spelling | Drawn as |
| --- | --- | --- |
| Prose | markdown | by the markdown widget |
| Record | a `sql` fence, and an id on a line after it | not drawn; its statement, folded |
| Presentation cell | a `sql` fence with a renderer as its second and last word (`sql chart`) | its result, by that renderer |
| Media | the image form alone on a line, naming a blob the markdown widget does not draw | by the blob's extension |

An image is prose: the markdown widget draws it from its file. A fence in
any other language is prose. A value shown through a gloss is a figure in a
sentence (SD5). A fence's info string holds nothing after the renderer: a
panel's settings are a row of literals (SD6).

A media cell is written in the CommonMark image form — alt text, the blob's
path: outside boxer it reads as its alt text linking the blob.

````markdown
---
title: Ingest lag, week 45
records:
  stats: {blob: w45.blobs/91c2….arrow, of: 0b6a…}
  p99_by_hour: {blob: w45.blobs/3fa9….arrow, of: 8c07…}
---

# Ingest lag, week 45

The 99th percentile was [412 ms](val:stats.p99_ms "gloss/duration;unit=ms").

```sql
SELECT toStartOfHour(ts) AS t, quantile(0.99)(lag_ms) AS p99
FROM ingest_lag GROUP BY t
```

^p99-by-hour

```sql chart
SELECT t AS x, p99 FROM p99_by_hour
```

```sql table
SELECT t AS hour, p99 AS "p99@gloss/duration;unit=ms" FROM p99_by_hour
```

![Stand-up, Tuesday](w45.blobs/7be0….wav)
````

Read with its statements folded, the same text is drawn roughly as below.
The sketch shows which cell becomes what; it does not fix a look. A reader
draws the statements one of three ways — hidden, folded under their cells,
or open — and the report is the first: a record is then one line that says
what is stored, and a presentation cell is its result alone.

```
┌────────────────────────────────────────────────────────┐
│ Ingest lag, week 45                                    │  prose
│ ═══════════════════                                    │
│                                                        │
│ The 99th percentile was 412 ms.                        │  prose with a val: link
│                         ╌╌╌╌╌╌                         │
│                                                        │
│ ▸ record p99-by-hour — 168 rows × 2 columns            │  record, folded: not drawn
│                                                        │
│ ▸ sql chart                                            │  presentation cell, folded
│ ╭─ p99_by_hour ────────────────────────────── chart ─╮ │  the record, t named x
│ │ 600 ┤              ▂▄▂                             │ │
│ │ 400 ┤       ▂▄▆▇▇▇▇███▇▆▄▂                         │ │
│ │ 200 ┤ ▂▃▅▆▇██████████████████▇▆▅▃▂                 │ │
│ │     └─┬──────┬──────┬──────┬──────┬─               │ │
│ │       Mon    Tue    Wed    Thu    Fri              │ │
│ ╰────────────────────────────────────────────────────╯ │
│                                                        │
│ ▸ sql table                                            │  presentation cell, folded
│ ╭─ p99_by_hour ────────────────────────────── table ─╮ │  the same record, renamed
│ │ hour               p99                             │ │
│ │ ─────────────────  ──────                          │ │
│ │ Mon 2026-11-02 00  212 ms                          │ │
│ │ Mon 2026-11-02 01  198 ms                          │ │
│ │ …                                                  │ │
│ ╰────────────────────────────────────────────────────╯ │
│                                                        │
│ ▶  ▁▂▅▇▅▃▂▁▂▃▆█▆▃▁▁▂▄▇▅▂▁▁▂▃▂▁   0:00 / 3:12           │  audio
└────────────────────────────────────────────────────────┘
```

Every spelling is plain markdown. A reader that knows nothing of notebooks
shows prose, highlighted code and links. A notebook reader that does not
know a renderer shows the cell's result as a table and says so.

### SD4 — Renderers are registered by name, and the markdown widget is not extended

The notebook is drawn by a widget that stacks cells: a prose cell through
the markdown widget, every other cell by its kind. The markdown widget gains
no hook and no knowledge of records, panels or audio.

A presentation cell names one renderer: `table`, which the cell stack draws
itself, or a name a package registered. A registration is a name and a
factory, made at start-up by the package that owns the drawing. Play
registers its result panels under their panel ids, so a `sql chart` cell is
drawn by the panel that draws a chart tab. A host that shows notebooks links
play and does not import it; in a binary without play the same cell is drawn
as a table and says which renderer is missing.

A panel is handed the cell's relations through the interface play's own
dispatch uses (`PanelI`): the cell's relation is the channel `main`, and a
member of its `WITH` list (SD6) is the channel of its name. The timeline's
`events` channel, which play fills from the buffer's own result, is offered
the cell's relation too. There are no
signals: a click that selects a row in play selects nothing in a notebook.
A panel that refuses the result's shape gives its reason, which the cell
shows above the result as a table. How a value in a column is shown follows
the column's gloss token, as in any result.

Showing a notebook has two halves. Reading the blobs, answering the cells
and decoding media draws nothing, so a host does it on a goroutine of its
own; the cell stack is handed the result on the render goroutine.

The cell stack builds one renderer per cell. What a renderer keeps between
frames — a layout, a camera, a mark type the reader chose
([ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md))
— is that cell's, and is not stored in the notebook. A renderer draws in a
box of bounded height: a panel written to fill a dock tab fills the box, not
the rest of the window.

Only a panel whose drawing needs nothing but its relations is a renderer.
Play's table, detail, schema, cards and chat panels read their play
instance, and are not registered; `table` is the cell stack's own.

Observed 2026-11-13, in the widget gallery: two `chart` cells over one
record and a `graphview` cell fed by two members, each drawn by play's
panel, and a change of mark type in one chart leaving the other as it was.

### SD5 — A figure in a sentence is a link to a stored value

A link whose target has the scheme `val:` names a column of a record of one
row; the one in SD3's example names the column `p99_ms` of `stats`. The label
is the gloss rendering of the value at the time it was stored, so the
sentence reads without boxer. The gloss is named by the link's title, or
failing that by the column's own name, as a result column names one
(ADR-0123's `label@<media type>`).

The notebook reader claims `val:` links, shows the value's provenance on
hover, and reports a label that no longer equals the rendering of its value.
A `val:` link inside a code span or a fenced block is a sample of the
spelling, not a figure: it is neither checked nor rewritten.

### SD6 — One buffer, two phases

A **record** is a named relation: a `sql` fence with an id, read by later
cells under that id with `-` written `_`. It is one `SELECT` of any shape,
with no `SET` before it and no `FORMAT` after it.

A **presentation cell** is a `sql` fence with a renderer. Its statement
reads the notebook's records by name, and must be one the trivial evaluator
answers (SD7): a list of `*` and bare columns, a column optionally under an
alias, from one record, with an optional `LIMIT`. Anything else is refused,
in the cell, naming what was refused. So every computation, filter and order
is in a record a person can read, and a presentation cell picks columns and
names them. Its column types are the record's: a cast is a computation.

A panel that takes more than one relation is fed as play feeds it: the
cell's statement carries its own `WITH` list, and a member named for a
channel fills that channel. This is the body of a `sql graphview` cell over
the records `calls` and `services`:

```sql
WITH edges      AS (SELECT source, target FROM calls),
     vertices   AS (SELECT id, label FROM services),
     graph_opts AS (SELECT 'hierarchical' AS layout)
SELECT * FROM edges
```

The list is flat: a member reads a record, or a member before it, by name,
and has no `WITH` of its own. Each member is held to the same shape. A
member that takes a record's name is an error, as one name given twice in a
`WITH` list is. Members are a cell's own: two cells may each have an
`edges`.

The third member above is the one further shape admitted: a `SELECT` of
literals, each under an alias, with no source. It is how a cell writes a
panel's settings row, and the trivial evaluator already answers it.

The notebook's records, in document order, are one play buffer: each
record a `WITH` query and the last the statement's own `SELECT`. The buffer
has two bindings. **Live**, a record's body is its statement, and the
buffer is an ordinary one: its graph is recovered by the analysis ADR-0097
describes, and nothing in play changes to evaluate it. **Sealed**, a record
is its stored result, bound by name to the evaluator that answers the
presentation cells (SD7).

There is no session state. A cell is a `SELECT`; a statement of any other
kind is refused when the buffer is built. A cell cannot hold a relation as a
value, so a cell that shows data names a record — the difference from the
Wolfram model.

### SD7 — A rename is not a computation; a filter is

One record is often drawn twice, as a table and as a plot, under the column
names each renderer reads. Were a rename a computation, the record would be
stored once per rendering.

So **the trivial evaluator accepts a bare column, with or without an alias,
in a select list** (ADR-0290, update of 2026-11-13). It still evaluates
nothing per row: it hands on some of the source's columns under other
names, sharing their buffers. A caller binds tables to it by name, and a
statement over them is answered as ClickHouse would answer it with each
table written before it as a `WITH` query: that is how a cell reads the
notebook's records.

What it answers it answers as ClickHouse does, and one rule of ClickHouse's
decides what it must refuse. ClickHouse resolves a name in a select list to
an alias of the same list before it looks at the source's columns: `SELECT a
AS b, b` returns `a` twice, and `a AS b, b AS a` does not swap. A column
that another item's alias names is therefore refused, not answered the other
way. So are a list alias equal to a `WITH` name, two output columns of one
name, and a qualified column (parity against ClickHouse 26.9 over a table of
statements, observed 2026-11-13).

`WHERE`, `ORDER BY` and every expression stay refused, by the evaluator and
so in a presentation cell. A filtered or an ordered view is a record. What
this buys: every presentation cell is answered in process, on every host, a
browser tab among them, and reading a sealed notebook needs no server, no
local worker and no capability.

The rule of admission is the evaluator's alone. The strata of
[ADR-0367](./0367-sql-statements-leave-through-one-fairlead-and-each-is-placed-in-a-stratum.md)
and the passthrough classifier of
[ADR-0117](./0117-passthrough-table-classifier.md) are unchanged; they go on
placing a renaming statement in *derive*, which is right for their purpose:
a renamed column is not returned as stored.

### SD8 — Collection stores records under one pin

Collection computes each record on the server, once, in document order.
The statement it sends for a record is the record's own, with the records
before it as `WITH` queries ahead of its own `WITH` list: the record stays
the outermost `SELECT`, so its `ORDER BY` is the order of what is stored.
Each result is stored as a blob: an Arrow stream of one batch. Collection
rewrites what the server returns into that form. The server's own stream
splits the same rows into more or fewer batches with its block size, and a
blob's name is the hash of its bytes (observed 2026-11-13: one result as 1
batch and as 7, the same bytes once rewritten).

The frontmatter lists, per record, its blob and the hash of the statement
and parameters it was computed from. The **pin** is one hash over the record
names and their blob hashes: the identity of the data the notebook presents,
unchanged by any edit to prose or presentation. It is computed from that
list and written nowhere: stored, it would be a second copy that could
disagree with the first.

Collection also rewrites every figure's label (SD5) to the rendering of its
new value, so the text a person reads without boxer is what the records
hold. A frontmatter entry with no cell — a relation stored by other means —
is kept as it is. Blobs no longer listed are not removed.

A reader checks each blob against its name and holds the records in process.
Presentation results are not stored; they are computed from the records when
the notebook is read.

A record whose statement no longer hashes to what the frontmatter records is
**stale**, and so is every cell that reads it. A record that reads a stale
record is stale with it, through any number of records: its own statement
is as collected, and what is stored for it was computed from the earlier
one. The reader marks a stale cell and does not hide it. Editing prose or a presentation cell makes nothing
stale and needs no server.

```
┌────────────────────────────────────────────────────────┐
│ ▾ record p99-by-hour        ⚠ edited since collection  │  record, unfolded
│   SELECT toStartOfHour(ts) AS t,                       │
│          quantile(0.95)(lag_ms) AS p99                 │
│   FROM ingest_lag GROUP BY t                           │
│                                                        │
│ ▸ sql chart                                            │
│ ╭─ p99_by_hour ───────────────────── ⚠ stale record ─╮ │  reads a stale record
│ │ (the chart of the stored record, still shown)      │ │
│ ╰────────────────────────────────────────────────────╯ │
└────────────────────────────────────────────────────────┘
```

A notebook is collected whole. There is no collection of a single record and
no surface for it: a notebook is written as text, its buffer is tried in
play, and it is collected in one step. Collecting, checking and printing the
buffer need no window; they are commands (`boxer markdown notebook`).

### SD9 — What a notebook does not hold

- Content that changes while it is read: a query run against the server on
  open, a signal shared between cells, a parameter the reader moves.
- Evaluation where it is read: a filter, an order, an expression.
- Layout: columns, grids, anything but one cell under another.
- Scripting in the text: conditions, loops, templates, cells written by a
  query.
- An embedded app or applet.

A surface that needs one of these is a play buffer, an applet or a
workstatelet, which exist. A notebook with a section per region is written
that way by whoever writes it: a programme or an agent emits the text.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Notebook text form (SD1–SD3, SD5, SD8's frontmatter) | added | package `mdnotebook`, its fixture, and the test that the line scan and the parse agree |
| Renderer registry by name (SD4) | added, in the cell-stack package `notebookview` | play registers its panels; a panel added to play is registered there |
| The trivial evaluator's accepted shape (ADR-0290 §SD3) | a bare column with an optional alias in a select list, the refusals of SD7, and `ReadTables` over tables bound by name | its parity tests against ClickHouse, and ADR-0290's update of 2026-11-13 |
| `boxer markdown notebook` | added: `collect`, `check`, `buffer` | the how-to |
| mdedit's Read mode | a snapshot document that is a notebook is drawn as one | mdedit's help page |
| Play's panels (`PanelI`) | unchanged; a second caller beside play's dispatch | a panel whose face starts reading its play instance leaves the registry |
| Strata (ADR-0367), passthrough classifier (ADR-0117) | unchanged | — |
| Markdown widget `Input` / `Result` | unchanged | — |
| Play buffer and its graph | unchanged; a notebook is one more producer of a buffer | — |
| Markdown document facts (ADR-0218) | a cell kind is added, so prose cells are rows | the facts DDL and the vocabulary's assignment table |

## Alternatives

- **Embedded applets (O1).** Each visualization would be a whole play
  instance: a query lane and a task per chart, drawn every frame, with its
  capabilities joined into the viewer's manifest and its state changing
  inside a document meant to hold still. ADR-0132's objection to N graphs on
  one surface applies unchanged. An embedding seam for live apps
  ([ADR-0155](./0155-app-embed-seam.md)) is a separate need.
- **Composed in Go (O2).** A report would be code: not written by a query or
  an agent, not ingested, not read outside the process that draws it.
- **A hook inside the markdown widget (O3).** It reaches fences nested in
  lists and callouts, which SD1 excludes on purpose, and it makes every
  consumer of the widget carry a contract for records and players. Stacking
  cells outside the widget is what the block editor already does.
- **The relation as the artefact, markdown as an export.** Closest to the
  Detail pane, and unreadable in a diff or by a model without a renderer.
  The Wolfram notebook file is the cautionary case: text in form, with typeset
  content and outputs inline, and a second plain format added for version
  control.
- **Results inline in the text.** Base64 or a table of every row in the
  markdown fails C1 for the same reason.
- **A stored output per drawn cell.** Each view of a record would be a blob
  of its own, paired with its input by a convention, and a change to a
  filter or a renderer would need the server again.
- **Records that carry the presentation's column names.** It leaves the
  evaluator as it was, and stores a record twice when a table and a plot
  read it under different names.
- **Several statements in one cell, one per channel.** A statement has no
  name, so position would have to say which channel it fills; ClickHouse
  scopes a `WITH` to its statement and refuses a body of several; and play
  runs one statement of such a buffer, not all of them.
- **One fence per channel.** The applet's `sql bands` fence is this shape.
  A notebook holds many cells, so each channel fence would need pairing with
  its panel's cell.
- **A subquery with its own `WITH`.** It depends on an outer list being
  visible inside a subquery, and it is outside the shape the trivial
  evaluator answers; one list, records first, needs neither.
- **A panel's settings in a record.** A display option would be data: part
  of the pin, and changed only by collecting again.
- **Binding columns in the fence's parameters.** A second binding language
  beside the alias that panels and gloss tokens already read; the same
  statement pasted into play would bind differently.
- **Letting *restrict* admit a rename.** ADR-0117's triage and the meaning of
  *restrict* both rest on rows returned as stored, names included.
- **A *rename* stratum as the rule of admission.** Without filtering, what a
  cell may say is no union of strata; the stratum and the evaluator would be
  two definitions of one set, free to disagree.
- **A trivial evaluator with `WHERE` and `ORDER BY`.** Evaluating a predicate
  per row is an engine, which ADR-0290 declines to grow into.
- **Cells that filter and order through a local ClickHouse worker.** A
  notebook would then read differently on a host without one, a browser tab
  first; each open notebook would hold its records as ad-hoc datasets
  against ADR-0240's quotas; and the cell would need a list of functions
  whose result depends only on their arguments, which the tree does not
  have. A filtered view as a record costs a blob and needs none of it.
- **A record that generates cells.** It needs a schema for cells, a mark on
  what was generated so that collecting again replaces it, and a rule that
  stops one generator feeding another. The text is already something a
  programme can write.
- **Renderers of the notebook's own.** A chart written for the notebook was
  tried first: a second chart beside play's, with its own reading of columns
  and fewer of the cases handled.
- **Renderers handed to the cell stack by its host.** The host would import
  play to get them, and an app does not import another app.
- **A stored pin.** See SD8.
- **A fence that is a gloss value.** A figure in a sentence already shows a
  stored value through a gloss.
- **A cell kind for images, and one for code.** The markdown widget draws
  both, so they are prose.
- **Words after the renderer in the fence.** A second way to pass a panel's
  settings beside the row of literals.
- **A kernel with session state.** Temporary tables and settings carried from
  cell to cell make a result depend on the order cells were run in, which is
  what the dependency graph removes.
- **Collecting one record at a time.** A second interactive surface beside
  play and the editor, for a loop play already serves on the live buffer.
- **Choosing a renderer by the result's schema alone.** Several panels accept
  the same schema; a cell shows one.

## Consequences

### Positive

- A report is readable where markdown is, and its parts are rows where SQL
  is.
- The data a notebook presents has one identity, the pin, and one place it
  was computed, the records.
- A change to prose, to which columns a cell shows and under what names, or
  to a renderer needs no server and makes nothing stale.
- Reading a sealed notebook evaluates nothing per row and is the same on
  every host.
- One record serves a table, a plot and the figures in a sentence.
- A panel added to play is a notebook renderer by one line of registration,
  and a chart in a report is the chart in play.

### Negative

- A filtered or an ordered view is a record: one more blob, part of the pin,
  and changed only by collecting again.
- The pin identifies stored bytes. Rewriting to one batch removes the
  server's chunking from them; the same rows from another server version, or
  in another order, need not hash alike.
- A panel written for a dock tab brings its controls into the cell — the
  chart its mark types, the graph its layout row — and what a reader sets
  with them lasts as long as the view.
- Collection recomputes a record once for itself and once inside every
  later record that reads it: there is no session to hold a result in.
- A record's time column must be a `DateTime64`: ClickHouse writes a
  `DateTime` to Arrow as an unsigned integer (observed 2026-11-13, 26.9).
- A presentation cell cannot change a column's type. A panel that needs a
  number gets it from the record.
- Cells do not react to each other. A reader who wants to filter one chart by
  another opens the buffer in play.
- An element cannot sit inside a list item or a callout.
- Repeated structure is written out by whoever writes the notebook; the
  format has no loop.
- Not every panel is a renderer (SD4), and the ones that are were written
  against play's types: the registration lives in play.
- The text form is a contract from its first use. A change to a spelling is a
  migration of stored notebooks.

### Neutral

- An applet remains its own document kind. A notebook with one record and an
  applet overlap in form and differ in that one is live and one is stored.
- A notebook's immutability is the snapshot's that holds it
  ([ADR-0198](./0198-fs-snapshot-store.md)), not a property of the format.

## Migration — Tier 1

- **Breaks.** Nothing stops compiling or parsing. A statement the trivial
  evaluator refused only for a bare column in its list is answered.
- **Path.** None needed: the evaluator's change is additive.
- **Regeneration.** The evaluator's parity table.
- **Old shape.** None kept.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the reader over a committed fixture notebook
  — its cells, that they tile the text, its records against their names; the
  line scan against the markdown parse over the repository's documents; a
  table of presentation statements, each answered or refused with what was
  refused, the row of literals among them; the trivial evaluator's table of
  statements, classed by how its answer relates to ClickHouse's; collection
  against a server that is a function, which is also how the fixture is
  made, so the committed fixture is what collection writes; play's panels
  drawing the fixture's records. A headless scene
  ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md))
  over the fixture asserts each cell kind is drawn, with play's panels as
  the renderers. The widget conformance lane covers the cell-stack package.
  The integration lane runs the evaluator's table and a collection through
  clickhouse-local.
- **What would fail.** A fence the scan and the parse disagree on; a
  computing or filtering statement admitted as a presentation cell; a
  renamed column the evaluator and ClickHouse answer differently; a cell
  kind that draws nothing; a renderer that takes the window.
- **Gap.** That a notebook reads sensibly outside boxer is checked by eye on
  the fixture. That a label equals the rendering of its value (SD5) and that
  a blob matches its name (SD8) are checked when a notebook is read, not in a
  lane. Parity with ClickHouse is checked against one version. A notebook
  drawn in mdedit from a real snapshot is checked by a test over an
  in-memory file tree, not by a scene.

## Milestones

- **M1 — Stored output.** The reader, the cell stack and the `val:` links,
  over a fixture whose records are blobs. Presentation cells drawn as a
  table; prose, a recording. A scene.
- **M2 — Rename.** The trivial evaluator's column list and bound tables
  (SD7).
- **M3 — Play's panels as renderers.** The registry, and play's
  registration (SD4).
- **M4 — Collection.** Each record computed and stored as one batch, the
  frontmatter and the figures rewritten, staleness, the commands (SD8).
- **M5 — A first host.** mdedit's Read mode, for a document from a
  snapshot.
- **M6 — Cells as facts.** The projection of SD2 in `boxer.facts`.

M1 to M5 are in the tree as of 2026-11-14. M6 is not built.

## Open questions

- **Q1 — The name.** "Notebook" is a working name beside applet, chatlet and
  workstatelet.
- **Q2 — Hosts beyond the first.** mdedit's Read mode draws a notebook
  loaded from a snapshot, where its blobs are found as a snapshot
  document's images are. A notebook opened from a single file or held in
  the persist store has no place to read blobs from and is drawn as
  markdown; whether a file dialog grants the folder, or a viewer app of its
  own does, is open. A host takes a document for a notebook when it holds a
  record, listed or written as a cell: an applet document spells its panels
  as a notebook spells presentation cells, and without a record such a cell
  has nothing to read. So is showing the statements in the reading mode: it
  draws the report, and the text is in the other modes.
- **Q3 — Parameters.** A buffer's `SET param_*` prelude has no place in the
  text: SD6 refuses a record that starts with `SET`. A notebook with
  parameters would need them in its frontmatter and in the hash SD8 keeps
  of each statement.
- **Q4 — A pin over rows.** A canonical encoding of a record would make the
  pin identify data, not bytes.
- **Q5 — A panel's controls in a report.** SD4 draws a panel as play does,
  toolbar included. A report may want the drawing alone, or the controls
  only when the statements are shown.
- **Q6 — Settings a reader chose.** A panel with a settings channel takes
  them from the cell's text; the chart's mark type has no such channel, so
  a report cannot say "bars".
- **Q7 — A cell's height.** The box of SD4 is one height for every cell.
- **Q8 — Panels that read their play instance.** Whether the table and the
  detail pane become renderers, and whether the cell stack's `table` and
  play's then remain two.
- **Q9 — Media beyond a recording.** A media cell is drawn by its blob's
  extension, and the Detail pane's block faces (ADR-0123) draw content by
  media type. Whether the two share one registry is not decided; a recording
  is the only media cell so far.

## Status

Proposed — awaiting review by the maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the local ClickHouse
  worker a filtering cell would have needed.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the buffer and its
  recovered graph; §SD12 deferred multi-cell authoring.
- [ADR-0117](./0117-passthrough-table-classifier.md),
  [ADR-0367](./0367-sql-statements-leave-through-one-fairlead-and-each-is-placed-in-a-stratum.md)
  — rows returned as stored, and the strata.
- [ADR-0123](./0123-play-content-typed-detail-cells.md),
  [ADR-0186](./0186-play-gloss-catalog.md),
  [ADR-0245](./0245-play-cards-panel-and-row-value-glosses.md) — content-typed
  cells and the gloss catalog.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — SQL in committed
  markdown; the rejected page composition and the deferred multi-applet
  document.
- [ADR-0218](./0218-markdown-ingestor-facts-model.md) — a markdown document
  as rows.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md),
  [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md)
  — a table served to SQL without being stored.
- [ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md),
  [ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md)
  — blobs by content hash, and a document exported with them.
- [ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) —
  the trivial evaluator.
- [ADR-0334](./0334-mdedit-blocks-over-the-source.md),
  [ADR-0347](./0347-mdedit-links-a-url-scheme-a-location-grammar-and-launch-options.md)
  — top-level blocks and block ids.
- [ADR-0340](./0340-cited-replies-assertions-the-window-checks-and-verdicts-in-the-journal.md)
  — a claim about data that the reader checks.
- Wolfram Language reference: [`Cell`](https://reference.wolfram.com/language/ref/Cell.html),
  [`TemplateBox`](https://reference.wolfram.com/language/ref/TemplateBox.html),
  [`InterpretationBox`](https://reference.wolfram.com/language/ref/InterpretationBox.html)
  — typed cells, a value shown through a named rule, a display paired with
  its value.
- D. E. Knuth, *Literate Programming*, The Computer Journal 27(2), 1984 —
  one source read as a document and extracted as a programme.
