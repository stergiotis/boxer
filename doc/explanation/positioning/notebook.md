---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# notebook — positioning

Positions the sealed notebook as a document format and its evaluation
model, against the notebooks and report tools a team would otherwise use.
The decision behind every clause is accepted
([ADR-0371](../../adr/0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md)).
A live reading of the same text is proposed
([ADR-0380](../../adr/0380-live-notebooks-records-computed-when-read-and-shown-as-a-bound-notebook.md))
and stays in the boundary. The workbench its statements are written in is
positioned by [play](./play.md), the store a notebook is kept in by
[lading](./lading.md), the panels its pictures are drawn with by
[widgets](./widgets.md).

## Short form

For whoever writes a report over data in ClickHouse, a sealed
notebook[^sealed] is a markdown document format evaluated in two phases:
records[^record], SQL statements computed once on the server and stored
beside the text, and presentation cells[^cell] that can only pick and
rename columns of a stored result, answered in process with no database
engine.

Unlike a notebook evaluated in a kernel session, as in Jupyter or Wolfram,
where a saved output is whatever the last evaluation left and nothing ties
it to the input beside it, a sealed notebook stores each result with the
hash of the statement that made it: reading computes nothing, and a check
names any stale result without running a query. The cost is SQL as the only
language, no parameters or filtering by the reader, a collection[^collect]
step against the server, and charts that need boxer to draw.

## Full form

**For** whoever writes a report over data in ClickHouse — an analyst, an
operator, or an agent that emits the text and checks it,

**a sealed notebook[^sealed] is** a markdown document format evaluated in
two phases: records[^record] computed once on the server and stored beside
the text, presentation cells[^cell] that pick and rename a record's columns
for a renderer, and figures in sentences that link to stored values,

**that** reads without computing anything, writes every figure into the
prose as text, and says of each stored result, without a query, whether
the statement it came from was edited since,

**unlike** a notebook evaluated in a kernel session, as in Jupyter or
Wolfram, where an output is what the last evaluation left, in the order
cells were run,

**a sealed notebook** keeps computing and showing apart in the format: what
shows can only select from what is stored, and what is stored names the
statement that made it. The cost is SQL as the only language, no
parameters or filtering by the reader, a collection[^collect] step against
the server, results stored whole, and charts that need boxer to draw.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | a report over data in ClickHouse | [why-boxer](../why-boxer.md) P3 (one machine-readable data spine); [ADR-0371](../../adr/0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md) Context |
| For | an agent that emits the text and checks it | ADR-0371 §SD9 (a programme or an agent writes the text, through `mdnotebook.Writer`), §SD8 (checking is a command); QOC criterion C5 |
| is a | a markdown document format: prose, records, presentation cells and recordings | ADR-0371 §SD1, §SD3 |
| is a | two phases: records on the server, presentation in process | ADR-0371 Decision, §SD6 |
| is a | results stored beside the text, each named by the hash of its bytes | ADR-0371 §SD2, §SD8 |
| is a | a presentation cell picks and renames columns, with no engine | ADR-0371 §SD4, §SD7; [ADR-0290 §SD3](../../adr/0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) and its update of 2026-11-13 |
| is a | a figure in a sentence is a link to a stored value | ADR-0371 §SD5 |
| that | reads without computing anything | ADR-0371 §SD7, §SD8 (Reading); Consequences (reading evaluates nothing per row) |
| that | every figure written into the prose as text | ADR-0371 §SD8 (collection rewrites every figure's label to its value) |
| that | a stored result names its statement; an edited one is stale, with every record that reads it | ADR-0371 §SD8 (Staleness) |
| unlike | a kernel session: an output depends on the order cells were run | ADR-0371 Context (the Wolfram notebook as the model followed, with outputs beside inputs), Alternatives (a kernel with session state) |
| notebook | what shows only selects from what is stored; a filter or an order is a record | ADR-0371 §SD6, §SD7, §SD9 |
| trade | SQL as the only language | ADR-0371 Decision (SQL is the expression language) |
| trade | no parameters, no reader's filter, no cell reacting to another | ADR-0371 §SD9, Consequences, Open question Q3 |
| trade | a collection step against the server; a notebook in the store is collected as a new snapshot under a tier the person states | ADR-0371 §SD8, §SD10; [ADR-0341 §SD3](../../adr/0341-lading-publish-a-write-only-capability-with-the-placement-check-at-the-door.md) |
| trade | results stored whole; a filtered view is one more stored result | ADR-0371 Consequences |
| trade | boxer draws the pictures; elsewhere the prose and its figures read as markdown, and the charts are SQL | ADR-0371 §SD3, §SD4, §SD8 |

## Compared with existing notebooks

What each kind of tool does with the four things a report has to settle.
The other tools' rows were checked against their public documentation on
2026-11-18; a tool's behaviour may have moved since.

| | Where a cell computes, and when | What the document keeps of its results | A number in the prose | What says a result is out of date |
| --- | --- | --- | --- | --- |
| **Jupyter** | a kernel session, when a person runs a cell, in any order | the last output of each cell, inside the `.ipynb` | typed in a markdown cell; extensions such as MyST-NB's `glue` insert one | nothing; execution counts show the order cells ran in |
| **Wolfram notebook** | a kernel session, when a person evaluates a cell | outputs beside their inputs, in the `.nb` | typed in a text cell | nothing; `In`/`Out` numbers show the order of evaluation |
| **Observable notebook, marimo** | a reactive runtime, again whenever a cell it depends on changes, and on every open | the code only | an expression in the text, computed when read | nothing is out of date: every read recomputes, and needs the data and the runtime |
| **Quarto with `freeze`** | when the project is rendered | the rendered output, and frozen results in `_freeze/` beside the source | inline code, evaluated at render | a changed source re-runs the document; changed data is not tracked |
| **Evidence, Observable Framework** | at build time, into a static site | query results cached as files the site serves (Parquet for Evidence) | a component or expression over a query result | a changed loader re-runs (Framework); changed data waits for the next build |
| **Sealed notebook** | records on the server at collection; presentation in process at reading, select and rename only | each record's result, beside the text, named by its hash and listed with its statement's hash | a link to a stored value; collection writes the value into the text | per record, without a query: a statement whose hash differs, and every record that reads it; changed data is not tracked |

Where the others are ahead:

- **Language.** Jupyter, Wolfram, Quarto and marimo run a general-purpose
  language and its libraries. A sealed notebook computes in ClickHouse SQL
  and nothing else.
- **Interaction.** Observable, marimo, Wolfram's dynamic output and
  Evidence's in-browser queries let a reader move a control and see the
  result follow. A sealed notebook has no control a reader moves.
- **Parameters.** Papermill runs a Jupyter notebook with parameters, and
  Quarto documents take them. A sealed notebook has none (ADR-0371 Q3).
- **Readers without the tool.** Quarto, Evidence and Framework emit HTML
  any browser opens, and forges render a Jupyter notebook's saved outputs.
  A sealed notebook's prose and figures read as markdown anywhere; its
  charts are drawn by boxer.

The difference the clauses rest on is in the last row: presentation cannot
compute, so a result is either stored and named by its statement or not
shown, and the check that it still matches is a comparison of hashes, not a
run.

## Boundary

- **The live reading is not what is handed over** (ADR-0380, proposed).
  The same text can be read with its records computed, read-only, when it
  is read — in the notebook window, in a notebooklet listed in the
  launcher, or in a pane another app embeds. That reading is not sealed,
  has no pin and is not stored, and it is how a reader sees data that has
  changed since collection.
- **Without a query is not without the server.** The notebook window
  opens a notebook from a lading snapshot, which is in ClickHouse. A
  notebook that is a file and its folder is checked and printed by the
  command line with no connection, and a notebooklet's book is compiled
  into the binary.
- **The store is a sibling's.** Where a notebook is kept, for how long,
  and who reads it there are [lading](./lading.md)'s: a notebook in the
  store lasts as long as its snapshot is kept.
- **Not a data story.** A notebook has no layout beyond one cell under
  another, no annotation an author places on a picture, and no settings an
  author fixes for a panel (ADR-0371 Open questions Q5, Q6, Q7).
- Unbuilt and absent from the clauses: notebooks as rows of the facts
  table (ADR-0371 milestone M6); a tier derived from what the records read
  (Q10); an editor for the text inside the window.
- Narrower than the clauses say: recordings are heard in the notebook
  window alone, and a statement that relies on a rewrite play applies on
  the client runs in play and not at collection.

## Further reading

- [why-boxer](../why-boxer.md) — the premises the clauses compress.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [How to write, collect and read a sealed notebook](../../howto/sealed-notebooks.md);
  the samples in
  [`mdnotebook/samples`](../../../public/semistructured/markdown/mdnotebook/samples).
- Decisions: [ADR-0371](../../adr/0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md),
  [ADR-0380](../../adr/0380-live-notebooks-records-computed-when-read-and-shown-as-a-bound-notebook.md)
  (proposed),
  [ADR-0290](../../adr/0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md),
  [ADR-0341](../../adr/0341-lading-publish-a-write-only-capability-with-the-placement-check-at-the-door.md).
- The tools compared: [Quarto, managing execution](https://quarto.org/docs/projects/code-execution),
  [Observable Framework, data loaders](https://observablehq.com/framework/data-loaders),
  [Evidence](https://docs.evidence.dev),
  [marimo](https://docs.marimo.io/guides/overview.html).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook

[^sealed]: A notebook is sealed when every result it shows is stored with it and matches the statement written beside it, so it reads the same wherever it is opened.
[^record]: A named SQL statement in the notebook whose result was computed on the server and stored; later parts of the notebook read it by name.
[^cell]: A presentation cell names a stored result, the columns to take from it and the picture to draw; it cannot filter, order or calculate.
[^collect]: Collection runs every record once on the server and stores the results with the text; it is the only step that stores a result.
