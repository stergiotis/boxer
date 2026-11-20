---
type: adr
status: accepted
date: 2026-11-13
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-15
---

# ADR-0371: Sealed notebooks — a report as a markdown document of typed cells, with SQL as the expression language

## Context

A result a person wants to keep and hand on — figures, a table, a chart, a
recording, and the prose that says what they mean — has no document form.
The parts exist separately:

- the markdown widget draws a document
  ([ADR-0180](./0180-markdown-rendering-fidelity-pass.md));
- play's Detail pane draws one row whose cells declare a content type
  ([ADR-0123](./0123-play-content-typed-detail-cells.md)), through renderings
  the gloss catalog names
  ([ADR-0186](./0186-play-gloss-catalog.md),
  [ADR-0245](./0245-play-cards-panel-and-row-value-glosses.md));
- an applet is a markdown document around one SQL buffer
  ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md));
- a chat artefact is a markdown document with images sealed by content hash
  and exported as a snapshot
  ([ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md),
  [ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md));
- a markdown document is ingested into `boxer.facts` as rows
  ([ADR-0218](./0218-markdown-ingestor-facts-model.md));
- a table an app holds is served to SQL without being stored
  ([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md)), and
  a statement that computes nothing is answered without ClickHouse
  ([ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md)).

Two earlier records left this ground open. ADR-0132 rejected composing a
page from many independent queries, because query graphs on one surface
share no signal store, and deferred documents holding several applets.
ADR-0097 §SD12 deferred explicit multi-cell authoring until the single
buffer fails someone. A reading audience and stored results are needs one
live buffer does not serve.

Two kinds of prior art frame the choice. HTML is a tree of nested elements
with layout. A Wolfram notebook is a flat list of typed cells grouped by
their headings, in which an output is stored beside the input that produced
it and a programme can write cells. The second is the model followed here,
with one difference that shapes SD6: a Wolfram cell holds any expression,
and a SQL value is a relation with typed columns.

The design was built on 2026-11-13 and 2026-11-14, ahead of this record's
review, to test it against the tree. Where a subdecision rests on what that
showed, it says so with a date.

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

A cell is one of four kinds:

- a **record** or a **presentation cell**, each a `sql` fence (SD3, SD6);
- a **media cell**, a blob the markdown widget does not draw: a recording;
- a **prose** cell, everything between two of the others: headings,
  paragraphs, images, and fences in any other language.

A cell that is not prose is one top-level block of the parse, the unit
[ADR-0334](./0334-mdedit-blocks-over-the-source.md) §SD2 edits by, and
starts in column 0. So the cells tile the text: every byte of the body is
in exactly one cell, a reader finds the cells with a line scan, and writing
a notebook back is concatenation. A test keeps the line scan and the
markdown parse in agreement over this repository's documents.

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

- A record's result is always a blob, and is checked against its name when
  it is read.
- A picture or a recording may be named by its content hash, and is then
  checked the same way. Under any other name it is read as the file it is,
  as in any markdown: a notebook written by hand names its pictures as its
  author did.
- A file the text names that cannot be read is a problem the reader lists,
  and the notebook is not sealed.

The relation of cells — one row per cell — is a projection of the text, not
a second source. One package defines it.

### SD3 — Spelling: a text body is a fence, a blob body is an embed

| Cell | Spelling | Drawn as |
| --- | --- | --- |
| Prose | markdown | by the markdown widget |
| Record | a `sql` fence, and an id on a line after it | not drawn; one line, or its statement |
| Presentation cell | a `sql` fence with a renderer as its second and last word (`sql chart`) | its result, by that renderer |
| Media | the image form alone on a line, naming a blob the markdown widget does not draw | by the blob's extension |

What is not a cell of its own:

- An image is prose: the markdown widget draws it from its file.
- A fence in any other language is prose.
- A value shown through a gloss is a figure in a sentence (SD5).
- A panel's settings are a row of literals (SD6): a fence's info string
  holds nothing after the renderer.

A media cell is written in the CommonMark image form, alt text and the
blob's path, so outside boxer it reads as its alt text linking the blob.

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

**How it is drawn.** A reader shows the statements or hides them; with them
hidden it shows the report. Every cell that is not prose has a header,
which says what the cell is and whether something is wrong with it. The
header folds the whole cell — its statement and its result — to that one
line, and a folded cell is not built. With the statements hidden a record
is its header alone.

The sketch below is the example read with its statements hidden. It shows
which cell becomes what; it does not fix a look.

```
┌────────────────────────────────────────────────────────┐
│ Ingest lag, week 45                                    │  prose
│ ═══════════════════                                    │
│                                                        │
│ The 99th percentile was 412 ms.                        │  prose with a val: link
│                         ╌╌╌╌╌╌                         │
│                                                        │
│   record p99-by-hour — 168 rows × 2 columns            │  record: one line, not drawn
│                                                        │
│ ▾ chart                                                │  presentation cell, its header
│ ╭─ p99_by_hour ────────────────────────────── chart ─╮ │  the record, t named x
│ │ 600 ┤              ▂▄▂                             │ │
│ │ 400 ┤       ▂▄▆▇▇▇▇███▇▆▄▂                         │ │
│ │ 200 ┤ ▂▃▅▆▇██████████████████▇▆▅▃▂                 │ │
│ │     └─┬──────┬──────┬──────┬──────┬─               │ │
│ │       Mon    Tue    Wed    Thu    Fri              │ │
│ ╰────────────────────────────────────────────────────╯ │
│                                                        │
│ ▾ table                                                │  presentation cell, its header
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

Rejected: a fold of the statement alone under each cell, with the result
always drawn. It was built first, and gave a reader two things to fold in
one cell and no way to put a long chart away.

### SD4 — Renderers are registered by name, and the markdown widget is not extended

The notebook is drawn by a widget that stacks cells: a prose cell through
the markdown widget, every other cell by its kind. The markdown widget gains
no hook and no knowledge of records, panels or audio.

**Who draws a presentation cell.** A cell names one renderer: `table` or
`kv`, which the cell stack draws itself, or a name a package registered.
`kv` draws a relation as labelled values, a row a block and a column a
line: the few facts of one thing, where a grid's header says nothing. A
registration is a name and a factory, made at start-up by the package that
owns the drawing. Play registers its result panels under their panel ids,
so a `sql chart` cell is drawn by the panel that draws a chart tab. A host
that shows notebooks links play and does not import it. In a binary without
play the same cell is drawn as a table and says which renderer is missing.

A panel is a drawing made for a dock tab. Package `notebookcells` registers
two made for a report, which need nothing of play: `marks`, instants and
intervals on one axis, and `points`, places on a map.

**What a panel is handed.** The cell's relations, through the interface
play's own dispatch uses (`PanelI`): the cell's own relation fills the
panel's main channel, and a member of the cell's `WITH` list (SD6) fills
the channel of its name. There are no signals: a click that selects a row
in play selects nothing in a notebook. A panel that refuses the result's
shape gives its reason, which the cell shows above the result as a table.
How a value in a column is shown follows the column's gloss token, as in
any result.

**Which panels are renderers.** Only a panel whose drawing needs nothing
but its relations. A panel that reads its play instance is not registered
(Q8), and `table` is the cell stack's own.

**What a renderer keeps.** The cell stack builds one renderer per cell.
What it keeps between frames — a layout, a camera, a mark type the reader
chose
([ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md))
— is that cell's, and is not stored in the notebook. A cell whose text is
unchanged keeps its renderer when the notebook is set again, over other
records or with cells added around it, and its prose keeps its parse; a
renderer is told when its cell is gone. A renderer draws in a box of
bounded height: a panel written to fill a dock tab fills the box, not the
rest of the window. The height is the reader's for every cell, and a cell's
own where its statement says so in a settings member (SD6):
`WITH cell_opts AS (SELECT 160 AS height)`.

**A report inside another surface.** A host may ask for the notebook's
content alone: no line saying whether it is sealed, and no header or rule
on a cell that has nothing wrong. A cell that is refused, stale or unread
keeps both. The glosses a column or a figure names are resolved in the
catalog the host hands over, which is the built-in one when it hands none.

**Off the frame.** Reading the blobs, answering the cells and decoding
media draw nothing, so a host does them on a goroutine of its own and hands
the cell stack the result on the render goroutine.

**Sound.** A recording is played by the cell stack's host, through the
output the runtime gives
([ADR-0372](./0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md)).
The stack draws the waveform and the transport and holds no output.

### SD5 — A figure in a sentence is a link to a stored value

A link whose target has the scheme `val:` names a column of a record of one
row; the one in SD3's example names the column `p99_ms` of `stats`. The
label is the gloss rendering of the value when it was stored, so the
sentence reads without boxer. The gloss is named by the link's title, or
failing that by the column's own name, as a result column names one
(ADR-0123's `label@<media type>`).

The notebook reader claims `val:` links, shows the value's provenance on
hover, and reports a label that no longer equals the rendering of its
value. A `val:` link inside a code span or a fenced block is a sample of
the spelling, not a figure: it is neither checked nor rewritten. Neither is
one whose bracket is escaped.

In a bound notebook (SD6) nothing was stored, so the label is a
placeholder: the reader draws the rendering of the value in its place and
holds the label to nothing.

### SD6 — One buffer, two phases

**A record** is a named relation: a `sql` fence with an id, read by later
cells under that id with `-` written `_`. It is one `SELECT` of any shape,
with no `SET` before it and no `FORMAT` after it.

**A presentation cell** is a `sql` fence with a renderer. Its statement
reads the notebook's records by name and must be one the trivial evaluator
answers (SD7): `*` and bare columns, a column optionally under an alias,
from one record, with an optional `LIMIT`. Anything else is refused, in the
cell, naming what was refused. So every computation, filter and order is in
a record a person can read, and a presentation cell picks columns and names
them. Its column types are the record's: a cast is a computation.

**A panel that takes more than one relation** is fed as play feeds it: the
cell's statement carries its own `WITH` list, and a member named for a
channel fills that channel. This is the body of a `sql graphview` cell over
the records `calls` and `services`:

```sql
WITH edges      AS (SELECT source, target FROM calls),
     vertices   AS (SELECT id, label FROM services),
     graph_opts AS (SELECT 'hierarchical' AS layout)
SELECT * FROM edges
```

- The list is flat: a member reads a record, or a member before it, by
  name, and has no `WITH` of its own. Each member is held to the same shape
  as the cell.
- A member that takes a record's name is an error, as one name given twice
  in a `WITH` list is.
- Members are a cell's own: two cells may each have an `edges`.
- A member with no source, a `SELECT` of literals each under an alias, is
  how a cell writes a panel's settings row. It is the one further shape
  admitted, and the trivial evaluator already answers it.

**The records are one play buffer.** In document order, each record is a
`WITH` query and the last is the statement's own `SELECT`. The buffer has
three bindings. *Live*, a record's body is its statement, and the buffer is
an ordinary one: its graph is recovered by the analysis ADR-0097 describes,
and nothing in play changes to evaluate it. *Sealed*, a record is its
stored result, bound by name to the evaluator that answers the presentation
cells (SD7). *Bound*, a record is a relation the host that reads the
notebook holds and hands over under the record's name.

**A bound notebook** is for a programme that writes a notebook to show data
it already has: the row a person selected, say, with a section for each
kind of thing the row holds. Stored first, each relation would be encoded,
hashed and decoded to be read once. Bound, the evaluator answers from the
host's own columns; the text lists no record and needs no record cell, so
it depends on which sections there are and not on what they hold. A bound
notebook is not sealed and has no pin (SD8), and storing it is the step
that makes it so. Measured on 2026-11-15 for a notebook of four sections
over three records, text written, read, prepared and set: 47 kB and 361
allocations bound, against 120 kB and 925 stored and read back
(`BenchmarkShowNextRow` in `notebookview`, the arms `bound-host-catalog`
and `blobs`).

**The reading goes both ways**, and play holds both ends of it. A notebook
loaded into a play window is the buffer of its records. A buffer read as a
notebook has each query of its `WITH` list as a record cell and its own
`SELECT` as the last record, under a name its reader gives, with one table
cell over it. The two are inverse: the notebook of a notebook's buffer has
its records, and the buffer of a buffer's notebook answers as the buffer
does (checked against ClickHouse, 2026-11-14). A buffer no notebook can
hold is refused, and play says which part: one that binds or reads a
parameter, one whose `WITH` list holds an expression or is `RECURSIVE`, one
with a node play computes itself. Play collects nothing and draws no
notebook: what it gains is the two readings.

**There is no session state.** A cell is a `SELECT`; a statement of any
other kind is refused when the buffer is built. A cell cannot hold a
relation as a value, so a cell that shows data names a record — the
difference from the Wolfram model.

### SD7 — A rename is not a computation; a filter is

One record is often drawn twice, as a table and as a plot, under the column
names each renderer reads. Were a rename a computation, the record would be
stored once per rendering.

So **the trivial evaluator accepts a bare column, with or without an alias,
in a select list** (ADR-0290, update of 2026-11-13). It still evaluates
nothing per row: it hands on some of the source's columns under other
names, sharing their buffers. A caller binds tables to it by name, and a
statement over them is answered as ClickHouse would answer it with each
table written before it as a `WITH` query. That is how a cell reads the
notebook's records.

What it answers, it answers as ClickHouse does, and one rule of ClickHouse's
decides what it must refuse. ClickHouse resolves a name in a select list to
an alias of the same list before it looks at the source's columns: `SELECT
a AS b, b` returns `a` twice. So a column that another item's alias names
is refused, not answered the other way. So are a list alias equal to a
`WITH` name, two output columns of one name, and a qualified column. A
table of statements holds the evaluator to ClickHouse's answers (first run
2026-11-13).

`WHERE`, `ORDER BY` and every expression stay refused, by the evaluator and
so in a presentation cell. A filtered or an ordered view is a record. What
this buys: every presentation cell is answered in process, on every host, a
browser tab among them, and reading a sealed notebook needs no server, no
local worker and no capability.

The rule of admission is the evaluator's alone. The strata of
[ADR-0367](./0367-sql-statements-leave-through-one-fairlead-and-each-is-placed-in-a-stratum.md)
and the passthrough classifier of
[ADR-0117](./0117-passthrough-table-classifier.md) are unchanged. They go on
placing a renaming statement in *derive*, which is right for their purpose:
a renamed column is not returned as stored.

### SD8 — Collection stores records under one pin

**Collection** computes each record on the server, once, in document order.

- The statement sent for a record is the record's own, with the records
  before it as `WITH` queries ahead of its own `WITH` list. The record stays
  the outermost `SELECT`, so its `ORDER BY` is the order of what is stored.
- Each result is stored as a blob: an Arrow stream of one batch. The server
  splits the same rows into more or fewer batches with its block size, and
  a blob's name is the hash of its bytes, so collection rewrites what the
  server returns into that one form: the same rows get the same name.
- Collection rewrites every figure's label (SD5) to the rendering of its
  new value, so the text a person reads without boxer is what the records
  hold.
- A frontmatter entry with no cell — a relation stored by other means — is
  kept as it is. Blobs no longer listed are not removed.
- Storing is a step of its own, apart from computing: the same step seals
  a bound notebook (SD6) over the relations its host held, which have no
  statement to hash. A blob holds no schema metadata, which is its
  writer's, so two writers of the same rows name the same blob.

**The pin.** The frontmatter lists, per record, its blob and the hash of
the statement it was computed from. The pin is one hash over the record
names and their blob hashes: the identity of the data the notebook
presents, unchanged by any edit to prose or presentation. It is computed
from that list and written nowhere: stored, it would be a second copy that
could disagree with the first.

**Reading.** A reader checks each blob against its name and holds the
records in process. Presentation results are not stored; they are computed
from the records when the notebook is read.

**Staleness.** A record whose statement no longer hashes to what the
frontmatter records is *stale*, and so is every cell that reads it. A
record that reads a stale record is stale with it, through any number of
records: its own statement is as collected, and what is stored for it was
computed from the earlier one. The reader marks a stale cell and still
shows what is stored. Editing prose or a presentation cell makes nothing
stale and needs no server.

**Whole, and by command.** A notebook is collected whole: there is no
collection of a single record. Collecting, checking and printing the buffer
need no window; they are commands (`boxer markdown notebook`).

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
that way by whoever writes it: a programme or an agent emits the text. A
programme writes it through `mdnotebook.Writer`, which keeps what SD1 asks
of a cell and escapes what it is given as data, so no value opens a cell
or a figure.

### SD10 — An app of its own: one document, two faces

A notebook is worked on in an app of its own, which embeds play as an
applet window does
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD8). It holds one
document and shows it two ways:

- **Read.** The cell stack of SD4 over what is stored. No server is asked.
- **Records.** A play instance whose buffer is the notebook's records
  (SD6). Its panes, its diagnostics and its query graph work on the
  statements against live data.

**Taking the buffer back** writes it into the text: each record's statement
into the cell of its name, and a record the buffer adds as a cell after the
one before it in the buffer. Prose, presentation cells and a record whose
statement did not change keep their bytes. A buffer that drops a record or
orders two records the other way round is refused, naming the record: either
is an edit of the text around the statements. A record whose statement
changed is stale (SD8) until the notebook is collected.

**Where the document is.** The app opens a notebook from a lading snapshot
([ADR-0198](./0198-fs-snapshot-store.md)), as mdedit opens a snapshot
document: the text is a file of the snapshot and its blobs are the files
beside it. A snapshot does not change, so every edit the app makes — a
buffer taken back, a collection — leads to a new one, and a file on disk
the snapshot was taken from is not written. The command line of SD8 stays
the way to collect a notebook that is a file. A window that is closed keeps
its notebook as a workingset
([ADR-0148](./0148-app-workingsets.md)).

**Collection** is the app's, not play's.

- Each record's statement is sent as the command line sends it, on a
  connection of the app's own. Play's run path bounds a result's rows and
  rewrites the statement, which serve exploring and would store something
  other than what the statement answers.
- The text with its blobs is published as a new snapshot through the host's
  write path into the store
  ([ADR-0341](./0341-lading-publish-a-write-only-capability-with-the-placement-check-at-the-door.md)),
  under a mount tag of the app's own. The window then shows that snapshot.
- The person confirms it. Checking, by contrast, asks no server.

**The label of what is published.** The write path takes the tree's
confidentiality tier as the caller's statement (ADR-0341 §SD3), and a
notebook's stored results are whatever its records read, which the app does
not know. So the person states the tier where they confirm the collection,
starting from the highest tier the store may hold. An agent's call names it
as an argument, and the person confirms the call. Whether the tree is
confined is derived, not stated: a notebook whose records name sealed data,
by the test play's dispatch applies, is refused before anything runs.

**Editing the text.** The app has no editor for prose and presentation
cells: the text is handed to one and comes back.

- Where the host registers an editor app, the window offers it as play
  offers it for its buffer
  ([ADR-0365](./0365-external-edit-a-text-handed-to-the-hosts-editor-app-and-each-save-returned.md)),
  and each save replaces the text.
- Where it registers none, the window hands the text to mdedit by mdedit's
  launch config, with the snapshot the document is of, and mdedit hands a
  text that holds a record back by the app's own launch kind. The launch
  kind is also how any other app opens a notebook here.

A text that comes back is shown over the stored results of the snapshot it
left with, as an edit: nothing of it is stored until the notebook is
collected.

**Operations.** The split follows what an operation concerns. Play has the
ones over *the buffer as records*: `get_notebook`, and its inverse
`set_notebook`, which replaces the buffer with a notebook's records. The
app has the ones over *the document and its files*: reading it, checking
it, writing a buffer into its records, and asking for collection. An agent
works the statements in a play window and hands the buffer to the app.
Serving the embedded play's catalog from the app's window, as an applet
window does, would make that one window; it waits for a use that needs it.

**Rejected.**

- *Collection in play.* Play has no folder, and would be a window that
  writes files beside a document it does not hold.
- *A pane of play that draws the notebook.* Play's buffer is one statement
  and has no place for prose or presentation cells.
- *An editor for the text in the first cut.* It doubles the app for a need
  an editor already meets.
- *A folder grant in the file broker, as the document's place.* It keeps a
  notebook an ordinary file for every tool, and costs a change to what the
  broker may hand an app: the broker grants one file to read or to write
  and a folder to watch, and none of them reads a folder's files or writes
  into one.
- *The app's own data area, as the document's place.* A notebook there is
  hidden from every other tool and travels without its blobs.

The store was chosen (2026-11-14) as the place that needs no new grant and
that mdedit and the snapshot browser already read.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Notebook text form (SD1–SD3, SD5, SD8's frontmatter) | added | package `mdnotebook`, its fixture, and the test that the line scan and the parse agree |
| Renderer registry by name (SD4) | added, in the cell-stack package `notebookview` | play registers its panels; a panel added to play is registered there |
| Records bound by the host, the step that stores them, and the writer (SD6, SD8, SD9) | added: `mdnotebook.Bind`, `Seal`, `Writer`; `notebookview.PrepareFrom` | the test that a writer's data stays out of the cells, and the one that a bound notebook sealed reads back sealed |
| Renderers for a report (SD4) | added: `kv` in the cell stack; `marks` and `points` in package `notebookcells` | a host links the package; the gallery's bound notebook |
| The trivial evaluator's accepted shape (ADR-0290 §SD3) | a bare column with an optional alias in a select list, the refusals of SD7, and tables bound by name | its table of statements against ClickHouse, and ADR-0290's update of 2026-11-13 |
| `boxer markdown notebook` | added: `collect`, `check`, `buffer` | the how-to |
| Play's operations catalog and toolbar | added: `get_notebook`, `set_notebook`, **Copy as notebook**; Load reads a notebook as its buffer | play's help |
| Play's help book | added: the topic that lists the renderers and what each reads | a test holds it to the registered renderers and to the sample |
| Sample notebooks, package `mdnotebook/samples` | added; every record makes its own rows | a panel registered as a renderer needs a cell in the renderers sample |
| The notebook app, its launch kind and its operations (SD10) | added | its help page; the vocabulary's assignment table |
| mdedit | its Read mode draws a snapshot document that is a notebook; it hands a text that holds a record to the app | mdedit's help page |
| Markdown document facts (ADR-0218) | a cell kind is added, so prose cells are rows (M6) | the facts DDL and the vocabulary's assignment table |
| Play's panels (`PanelI`) | unchanged; a second caller beside play's dispatch | a panel whose face starts reading its play instance leaves the registry |
| Play buffer and its graph | unchanged; a notebook is one more producer of a buffer | — |
| Markdown widget `Input` / `Result`; strata (ADR-0367); passthrough classifier (ADR-0117) | unchanged | — |

## Alternatives

**The shape of a report.**

- **Embedded applets (O1).** Each visualization would be a whole play
  instance: a query lane and a task per chart, drawn every frame, with its
  capabilities joined into the viewer's manifest and its state changing
  inside a document meant to hold still. ADR-0132's objection to many graphs
  on one surface applies unchanged. An embedding seam for live apps
  ([ADR-0155](./0155-app-embed-seam.md)) is a separate need.
- **Composed in Go (O2).** A report would be code: not written by a query or
  an agent, not ingested, not read outside the process that draws it.
- **A hook inside the markdown widget (O3).** It reaches fences nested in
  lists and callouts, which SD1 excludes on purpose, and it makes every
  consumer of the widget carry a contract for records and players. Stacking
  cells outside the widget is what the block editor already does.
- **The relation as the artefact, markdown as an export.** Closest to the
  Detail pane, and unreadable in a diff or by a model without a renderer.
  The Wolfram notebook file is the cautionary case: text in form, with
  typeset content and outputs inline, and a second plain format added for
  version control.
- **A record that generates cells.** It needs a schema for cells, a mark on
  what was generated so that collecting again replaces it, and a rule that
  stops one generator feeding another. The text is already something a
  programme can write.

**What is stored.**

- **Results inline in the text.** Base64 or a table of every row in the
  markdown fails C1 for the same reason.
- **A stored output per drawn cell.** Each view of a record would be a blob
  of its own, paired with its input by a convention, and a change to a
  filter or a renderer would need the server again.
- **Records that carry the presentation's column names.** It leaves the
  evaluator as it was, and stores a record twice when a table and a plot
  read it under different names.
- **A stored pin.** See SD8.
- **A kernel with session state.** Temporary tables and settings carried
  from cell to cell make a result depend on the order cells were run in,
  which is what the dependency graph removes.
- **Collecting one record at a time.** A second interactive surface beside
  play and the editor, for a loop play already serves on the live buffer.

**What a cell may say.**

- **Letting *restrict* admit a rename.** ADR-0117's triage and the meaning of
  *restrict* both rest on rows returned as stored, names included.
- **A *rename* stratum as the rule of admission.** Without filtering, what a
  cell may say is no union of strata; the stratum and the evaluator would be
  two definitions of one set, free to disagree.
- **A trivial evaluator with `WHERE` and `ORDER BY`.** Evaluating a predicate
  per row is an engine, which ADR-0290 declines to grow into.
- **Cells that filter and order through a local ClickHouse worker**
  ([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)). A notebook would then
  read differently on a host without one, a browser tab first; each open
  notebook would hold its records as ad-hoc datasets against ADR-0240's
  quotas; and the cell would need a list of functions whose result depends
  only on their arguments, which the tree does not have. A filtered view as
  a record costs a blob and needs none of it.

**How a panel gets its relations and settings.**

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
- **Words after the renderer in the fence.** A second way to pass a panel's
  settings beside the row of literals.

**Renderers and cell kinds.**

- **Renderers of the notebook's own.** A chart written for the notebook was
  tried first: a second chart beside play's, with its own reading of columns
  and fewer of the cases handled.
- **Renderers handed to the cell stack by its host.** The host would import
  play to get them, and an app does not import another app.
- **Choosing a renderer by the result's schema alone.** Several panels accept
  the same schema; a cell shows one.
- **A fence that is a gloss value.** A figure in a sentence already shows a
  stored value through a gloss.
- **A cell kind for images, and one for code.** The markdown widget draws
  both, so they are prose.

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
  `DateTime` to Arrow as an unsigned integer (observed 2026-11-13).
- A presentation cell cannot change a column's type. A panel that needs a
  number gets it from the record.
- Cells do not react to each other. A reader who wants to filter one chart by
  another opens the buffer in play.
- An element cannot sit inside a list item or a callout.
- Repeated structure is written out by whoever writes the notebook; the
  format has no loop.
- Not every panel is a renderer (SD4), and the ones that are were written
  against play's types: the registration lives in play.
- A bound notebook (SD6) is as good as the relations its host hands over:
  nothing checks them against a name, and two readers bound to different
  relations read different reports from one text.
- What a figure in a bound notebook reads depends on the catalog of the
  host that draws it, as a column's gloss does.
- A statement that relies on a rewrite play applies on the client runs in
  play and not at collection, which sends the statement as written (SD10).
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
- **Regeneration.** The evaluator's table of statements.
- **Old shape.** None kept.

## Verification plan — Tier 1

- **Default lane.** The reader over a committed fixture: its cells tile
  the text and its records match their names. The line scan against the
  markdown parse. Tables of statements, for what a presentation cell is
  answered or refused and for how the evaluator's answer relates to
  ClickHouse's. Collection against a server that is a function, which is
  also how the fixture is made. The sample notebooks: each sealed, each
  cell drawn by the panel it names, a cell for every registered renderer.
  The app's catalog, a buffer written back, and a collection against a
  stood-in server and store.
- **Scenes** ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md)).
  The fixture and the samples in the gallery, with play's panels as the
  renderers; the app's two faces, a recording played, and the text handed
  to mdedit and back.
- **Integration lane.** The evaluator's table and a collection through
  clickhouse-local; each sample collected again and compared row for row;
  the app's collection, the store's refusal of a tier above its ceiling,
  and the opening of the snapshot it wrote, against a server and a store of
  the test's own.
- **What would fail.** A fence the scan and the parse disagree on; a
  computing or filtering statement admitted as a presentation cell; a
  renamed column the evaluator and ClickHouse answer differently; a cell
  kind that draws nothing; a renderer that takes the window.
- **Gap.** That a notebook reads sensibly outside boxer is checked by eye.
  That a label equals the rendering of its value (SD5) and that a blob
  matches its name (SD8) are checked when a notebook is read, not in a
  lane. Parity with ClickHouse is checked against the version the lane
  finds. A notebook in mdedit is checked over an in-memory file tree, not
  by a scene. The editor app of ADR-0365 is not in this tree, so the app's
  offer of it is not exercised.

## Milestones

`✓` marks what is in the tree as of 2026-11-14.

- **M1 — Stored output.** ✓ The reader, the cell stack and the `val:`
  links, over a fixture whose records are blobs.
- **M2 — Rename.** ✓ The trivial evaluator's column list and bound tables
  (SD7).
- **M3 — Play's panels as renderers.** ✓ The registry, and play's
  registration (SD4).
- **M4 — Collection.** ✓ Records computed and stored, the frontmatter and
  the figures rewritten, staleness, the commands (SD8).
- **M5 — A first host.** ✓ mdedit's Read mode, for a document from a
  snapshot.
- **M6 — Cells as facts.** The projection of SD2 in `boxer.facts`.
- **M7 — The buffer both ways.** ✓ Play reads a notebook as its buffer and
  its buffer as a notebook's records (SD6).
- **M8 — Samples and the renderer reference.** ✓ Notebooks to read and to
  start from, and play's help topic of what each renderer reads.
- **M9 — The buffer written back.** ✓ `set_notebook` in play, and a
  notebook's records replaced from a buffer with the rest of its text kept
  (SD10).
- **M10 — The app.** ✓ The two faces, check, collection, the hand-over of
  the text, and the app's operations (SD10).
- **M11 — A notebook written for what a host holds.** Built 2026-11-15,
  not yet in a host beside the gallery: bound records and the step that
  stores them, the writer, the plain reading, a cell's own height, and the
  renderers `kv`, `marks` and `points` (SD4, SD6, SD8, SD9).

## Open questions

- **Q1 — The name.** "Notebook" is a working name beside applet, chatlet and
  workstatelet.
- **Q2 — Hosts beside the app.**
  - Whether mdedit's Read mode stays beside the app. Both open a notebook
    from a snapshot; a notebook opened from a single file or held in the
    persist store has no place to read blobs from and is drawn as markdown.
  - Whether a reading mode shows the statements: it draws the report, and
    the text is in the other modes.
  - A hand-over of the text that is one edit and not two windows. mdedit as
    an editor app in ADR-0365's sense needs the service that keeps a handed
    text, which is the host's and not in this tree.
  - What a host takes for a notebook. As built: a document that holds a
    record, listed or written as a cell. An applet document spells its
    panels as a notebook spells presentation cells, and without a record
    such a cell has nothing to read.
- **Q3 — Parameters.** A buffer's `SET param_*` prelude has no place in the
  text: SD6 refuses a record that starts with `SET`. A notebook with
  parameters would need them in its frontmatter and in the hash SD8 keeps
  of each statement.
- **Q4 — A pin over rows.** A canonical encoding of a record would make the
  pin identify data, not bytes.
- **Q5 — A panel's controls in a report.** SD4 draws a panel as play does,
  toolbar included. A report may want the drawing alone, or the controls
  only when the statements are shown. The cell stack's own header is
  settled (SD4); a panel's toolbar is not: each panel draws its own, and
  none has a way to be asked for less.
- **Q6 — Settings a reader chose.** A panel with a settings channel takes
  them from the cell's text; the chart's mark type has no such channel, so
  a report cannot say "bars".
- **Q7 — A cell's settings.** A cell says its own height (SD4). Whether it
  says more that way — a caption, a width — is open; a height was the one
  setting a report could not do without.
- **Q11 — A map with tiles and cells.** `points` draws country outlines and
  no basemap: a renderer is built with no way to reach the host's tile
  service. H3 cells need the H3 runtime, which the caller of the overlay
  owns. Both wait for a renderer to be handed something of its host.
- **Q8 — Panels that read their play instance.** Whether the table and the
  detail pane become renderers, and whether the cell stack's `table` and
  play's then remain two.
- **Q9 — Media beyond a recording.** A media cell is drawn by its blob's
  extension, and the Detail pane's block faces (ADR-0123) draw content by
  media type. Whether the two share one registry is not decided; a recording
  is the one media cell built.
- **Q10 — A tier that is derived.** SD10 has the person state the tier of
  a collected notebook. Deriving it — from the snapshot the notebook was
  opened from, or from a label the tables its records read would carry —
  needs a label to read, and a query's result carries none in this tree.

## Status

Accepted 2026-11-15.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-11-17 — A live reading beside the stored one (SD9, SD10)

[ADR-0380](./0380-live-notebooks-records-computed-when-read-and-shown-as-a-bound-notebook.md)
(proposed) adds a reading of a notebook beside the stored one: its records
computed on the server when it is read, held in the process and shown as
a bound notebook (SD6). Nothing is stored, the reading is not sealed and
has no pin, and only a notebook whose records classify read is run this
way.

- **SD9.** "A query run against the server on open" stays out of the text
  and out of the stored reading: a notebook still reads without a server,
  and what it stores is what collection computed. The live reading is the
  exception ADR-0380 makes, outside the text. The rest of SD9 stands:
  nothing is evaluated where the notebook is read, and nothing reacts.
- **SD10.** The Read face is drawn by the pane of ADR-0380 §SD5. It shows
  what is stored until the person runs the records, says which reading is
  on screen, and returns to the stored one by a gesture; a text that
  changes drops the live reading. Collection is unchanged and is still the
  one path that stores. The same pane is a notebooklet's whole window and
  can be embedded in another app's frame; neither collects.

### 2026-11-20 — The frontmatter names the format

A notebook carries `notebook: <n>` in its frontmatter
(`mdnotebook.FormatKey`, `mdnotebook.FormatVersion`, 1). Collection writes
it first in the block when absent; a notebook without it reads as format 1,
so every notebook sealed before this reads unchanged. `mdnotebook.Read`
refuses a format above the one it reads, saying "written by a newer boxer
(format N; this build reads up to M)", rather than reading the text by
rules it was not written under. A non-numeric value is refused as well.

The format moves only when an older reader would misread a newer text — a
key it would ignore while that key changes what the text means. A key an
older reader may safely skip needs no bump. The samples and the fixture
carry the key.

## References

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
- [ADR-0198](./0198-fs-snapshot-store.md),
  [ADR-0341](./0341-lading-publish-a-write-only-capability-with-the-placement-check-at-the-door.md)
  — the store a notebook is opened from, and its write path.
- Wolfram Language reference: [`Cell`](https://reference.wolfram.com/language/ref/Cell.html),
  [`TemplateBox`](https://reference.wolfram.com/language/ref/TemplateBox.html),
  [`InterpretationBox`](https://reference.wolfram.com/language/ref/InterpretationBox.html)
  — typed cells, a value shown through a named rule, a display paired with
  its value.
- D. E. Knuth, *Literate Programming*, The Computer Journal 27(2), 1984 —
  one source read as a document and extracted as a programme.
