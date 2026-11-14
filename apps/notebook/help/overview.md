---
type: explanation
audience: end-user
status: draft
title: The notebook window
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The notebook window

A sealed notebook (ADR-0371) is a markdown report that holds its own data:
prose, **records** — SQL statements whose results were computed once and
stored beside the text — and **presentation cells** that pick columns of a
record and name them for a renderer. This window shows one notebook two
ways, lets its statements be worked on, and collects it: computes the
records and stores the results.

## Opening a notebook

A notebook is opened from a lading snapshot, where its stored results lie
beside its text. **Open…** lists the markdown files of the store — the
newest snapshot of each mount, at its root and one folder down — and a
click opens one; a file that holds a record is a notebook. A new window is
given one with the variable `BOXER_NOTEBOOK_SNAPSHOT`, as
`<mount, hex>@<snapshot, unix nanoseconds>:<path>`.
Another app opens a notebook here by the launch kind `notebookLaunch`,
as mdedit's **Open in notebook** does. An empty window offers the three
samples: `minimal`, a notebook in two
records; `report`, a report that leads with its figures; and `renderers`,
one cell per renderer.

## The two faces

- **Read** draws the notebook from what it stores. No server is asked. A
  record is a line that unfolds to its statement, a presentation cell is
  its picture, and a figure in a sentence is the stored value.
- **Records** is a play window whose buffer is the notebook's records:
  each record a `WITH` query in document order, the last the buffer's own
  `SELECT`. Run it, bind panes to its nodes and change the statements
  against live data, as in any play window.

**Take the buffer back**, on the Records face, writes the buffer's
statements into the record cells of their names. Prose, presentation cells
and a record whose statement did not change keep their text. A query the
buffer adds becomes a record cell after the one before it. A buffer that
drops one of the notebook's records, or orders two of them the other way
round, is refused and the status row says which record: both are edits of
the text around the statements.

## Editing the text

The window has no editor for prose and presentation cells; the text is
edited elsewhere and comes back.

- **Edit in mdedit** opens mdedit on the notebook's text, with the snapshot
  the notebook is of. There, **Open in notebook** opens a notebook window
  on the text as it then stands. Each hand-over opens a window, and neither
  window follows the other afterwards.
- Where the host registers an editor app of its own, the bar offers **Edit
  in** that editor instead. Each save there replaces the text here, unless
  the text was changed here meanwhile; then the save waits until it is
  taken or the text here is kept.

A text that comes back is shown over the stored results of the snapshot it
left with, and the status row says **edited**: a record whose statement
changed is stale, and nothing is stored until the notebook is collected. A
text that left without a snapshot, as a sample's does, comes back alone and
finds no stored result beside it.

## Collecting

**Collect…** asks first, in the window: it says how many records will run,
and asks for the **confidentiality tier** of what they read — CLEAR, GREEN,
AMBER, AMBER+STRICT or RED. The window cannot know what the records read,
so the tier is your statement; it starts on the highest tier the store may
hold. **Collect and publish** then runs every record once on the server
the `CLICKHOUSE_*` variables name, each statement as it is written, and
publishes the notebook with its stored results to the lading store as a
new snapshot. Everyone who reads the store reads them. The window opens
the snapshot that was written, and the status row names it.

A collection is refused before anything runs when a record reads sealed
data, which the store does not hold. The store refuses a tier above its
ceiling, and the status row says so. A running collection has a row with
**Cancel**.

A notebook that was opened from a snapshot is not changed by a collection:
a snapshot never changes, and a file on disk the snapshot was taken from
is not written. A notebook that is a file is collected on the command
line, `boxer markdown notebook collect`.

## The status row

The badge reads **sealed** when the notebook shows what it stores, **not
sealed** otherwise, and **reading** while the stored results are read. The
line beside it counts the records, the problems, the stale records and the
presentation cells with no answer. **edited** says a buffer was taken back
since the notebook was opened.

A record whose statement was edited is **stale**, and so is every record
that reads it: what is stored is still shown, and marked, until the
notebook is collected.

## Operating the window as an agent

The window's catalog has the operations over the document. The operations
over a buffer are play's, in a play window.

| Operation | Kind | What it does | Refused when |
| --- | --- | --- | --- |
| `get_document` | query | the notebook's text, where it was opened from, its records in order, and a revision that names the text | no notebook is open |
| `check_notebook` | query | whether the notebook is sealed, and what is not: problems, stale records, records that read a stale one, presentation cells with no answer | no notebook is open; the text is not a notebook; the stored results are still being read |
| `set_records` | command, document | writes a play buffer into the record cells by name; the buffer's own `SELECT` is the last record, or the one `Sink` names | no notebook is open; the buffer is not a notebook's (a parameter, a `SET`, a `WITH` expression); it drops or reorders a record |
| `collect_notebook` | command, consequential, async | runs every record once on the server and publishes the notebook with its stored results as a new snapshot under the `Tier` the caller states; settles with the snapshot, and the person confirms an agent's call | no notebook is open; a collection is running; the tier is not one of the five; a record reads sealed data; the window has no bus; the store refuses the tree |
| `show_face` | command, view | shows the read face or the records face | the face is neither |
| `open_snapshot` | command, document | opens a notebook of a lading snapshot, named as `<mount, hex>@<snapshot, unix nanoseconds>:<path>`; the read lands in a later frame | the name is not of that form |
| `open_sample` | command, document | opens a sample in place of the document | no sample of that name |

To work on a notebook's statements: read the text with `get_document`,
hand it to a play window with play's `set_notebook`, change and run the
buffer there, read it with play's `get_state`, and write it back here with
`set_records`. Then `check_notebook` says which records are stale, and
`collect_notebook` cures them. State the tier of the most confidential
table the records read; when that is not known, ask the person.

What a presentation cell may hold, and what each renderer reads, is in
play's help under **Notebook renderers**.
