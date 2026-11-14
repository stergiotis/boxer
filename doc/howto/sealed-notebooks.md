---
type: how-to
audience: developer or agent writing a report that holds its own data
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to write, collect and read a sealed notebook

The task-oriented walk: write a report as one markdown file, compute its
data once, and hand on a file and a folder that read the same wherever they
are opened. Why it is shaped this way is
[ADR-0371](../adr/0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md).

One fact carries the page. **A record computes on the server and is stored;
a cell only picks columns of what is stored and names them.** Everything a
reader sees is drawn from the stored results, with no server, so a filter,
an order or an aggregate belongs in a record.

## 1. Write the text

A notebook is markdown. Three spellings are its own, each starting in
column 0; everything else is prose and is drawn as markdown draws it.

````markdown
# Ingest lag, week 45

The 99th percentile was [?](val:stats.p99_ms "gloss/duration;unit=ms").

```sql
SELECT quantile(0.99)(lag_ms) AS p99_ms FROM ingest_lag
```

^stats

```sql
SELECT toStartOfHour(ts) AS t, region, quantile(0.99)(lag_ms) AS p99
FROM ingest_lag GROUP BY t, region ORDER BY t
```

^p99-by-hour

```sql table
SELECT t AS hour, region, p99 AS "p99@gloss/duration;unit=ms" FROM p99_by_hour
```

```sql chart
SELECT t AS x, region AS series, p99 FROM p99_by_hour
```
````

- **A record** is a `sql` fence followed by a block id on a line of its own.
  It may be any single `SELECT`. Later cells read it under its id, with `-`
  written `_`; a later record may read it too.
- **A presentation cell** is a `sql` fence with a renderer as its second
  word. Its list holds `*` and bare columns, a column optionally under an
  alias, from one record, with an optional `LIMIT`.
- **A figure** is a link whose target is `val:<record>.<column>`, in a
  record of one row. The link's title names the gloss the value is shown
  through. Write any label; collection replaces it.

A recording is the image form alone on a line, naming a `.wav` beside the
notebook. An image is ordinary markdown, read from its file beside the
notebook. A picture or a recording whose file is named by its content hash,
as a record's blob is, is checked against that name; one that cannot be read
is listed as a problem, and the notebook is not sealed.

## 2. Choose a renderer

`table` is always there. Every other name is a result panel of play, under
its panel id — `chart`, `series`, `dist`, `graphview`, `sankey`, `treemap`
and the rest of the list in `apps/play/play_notebook_renderers.go`. A panel
reads columns by name, which is what the alias is for: the chart above reads
`x`, `series` and every number left. The contract of each panel is play's
own, and a panel that does not read a result says why in the cell.

A panel that reads more than one relation takes the others from the cell's
`WITH` list, each member named for the channel it fills. A member with no
`FROM` is a row of settings:

```sql
WITH edges      AS (SELECT source, target FROM calls),
     graph_opts AS (SELECT 'hierarchical' AS layout)
SELECT * FROM edges
```

## 3. Try the records in play

```sh
./boxer.sh markdown notebook buffer report.md
```

prints the records as one statement, each a `WITH` query in document order.
Paste it into a play window to work on the statements against live data, or
pick the notebook with play's **Load .sql…**, which reads it the same way.

The way back is play's **Copy as notebook** (the operation `get_notebook`
for an agent): each `WITH` query of the buffer as a record cell, its own
`SELECT` as the record `main`, and one table cell over it. Paste the cells
over the notebook's records, or into a new file to start a notebook from a
buffer. A buffer with parameters or a `WITH` expression is refused: a
notebook holds neither.

## 4. Collect

```sh
./boxer.sh markdown notebook collect report.md
```

runs each record once on the server the `CLICKHOUSE_*` variables name,
writes each result into `report.blobs/` under the hash of its bytes, lists
the blobs in the notebook's frontmatter and rewrites every figure's label to
its stored value. The notebook is then **sealed**: the file and the folder
are the whole report. Collecting again after an edit replaces what changed;
blobs no longer listed are left where they are.

## 5. Check

```sh
./boxer.sh markdown notebook check report.md
```

connects to nothing. It reads the blobs, the recordings and the images, compares each blob with
its name, each figure with its value and each record's statement with the
one its blob was computed from, and answers every presentation cell. It exits non-zero when
the notebook is not sealed or a cell has no answer, which makes it the test
an agent runs on a notebook it wrote.

A record edited since collection is **stale**, and so is every record that
reads it: what is stored is still shown, and marked. Editing prose or a
presentation cell makes nothing stale.

## 6. Read

mdedit draws a notebook in its **Read** mode when the document was loaded
from a snapshot, the place its blobs are found beside it
([lading snapshot store](./lading-snapshot-store.md)); the other modes show
the text. The widget gallery's notebook demo draws a fixture, and
`widgets/notebookview` is the widget for any other host.

## What a cell refuses, and where it belongs

| In a presentation cell | Instead |
| --- | --- |
| `WHERE`, `ORDER BY`, `GROUP BY`, `JOIN` | a record of its own |
| an expression or a cast, `p99 * 2`, `toString(p99)` | compute it in the record |
| a column another item's alias names, `SELECT a AS b, b` | name each column once: ClickHouse reads `b` as the alias |
| a server table, `system.tables` | a record that reads it |

## Things to know

- A time column must be a `DateTime64` in the record. ClickHouse writes a
  plain `DateTime` to Arrow as an unsigned integer, and a panel then sees a
  number.
- What a reader sets on a panel's own controls — a chart switched to bars —
  lasts as long as the view. It is not written into the notebook.
- A record is recomputed from its statement and the records before it, so a
  record read by three later ones is computed four times at collection.
