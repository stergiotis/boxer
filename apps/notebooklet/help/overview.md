---
type: explanation
audience: end-user
status: draft
title: A notebooklet
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# A notebooklet

A notebooklet is one committed notebook (ADR-0371) opened from the
launcher to be **read live** (ADR-0380). When the window opens, every
record of the notebook is computed on the server the `CLICKHOUSE_*`
variables name, read-only, and the notebook is shown over the results:
its prose, its figures and its presentation cells. Nothing is stored and
nothing is published.

The window is the notebook and nothing else. It has no Records face, no
collection and no store. **Open in notebook** opens the notebook window on
the same text, where its records are worked on in play and it is
collected; that window gets the text and none of the stored results.

## The reading on screen

The line above the cells says which reading is shown: **live**, with when
it was computed and how long it took, or **stored**, with whether the
stored results are sealed. A notebook committed with its stored results
shows them while the first run is under way and when no server answers.

What is on screen is replaced only when every record answered. A run that
fails or is cancelled leaves the earlier reading in place, and the line
names the record and its error. **Run again** repeats a run; nothing runs
on a timer. **Show stored** returns to what the notebook stores.

Only a notebook whose records read is committed as a notebooklet: one
whose records reach outside the server or change it is kept out by a test.
One record's result is bounded at 1,048,576 rows and 256 MiB; a record
over either fails the run rather than being cut short. The records are
separate requests, each over the tables as it finds them, so two cells
over a table that is being written to can disagree.

## Operating the window as an agent

| Operation | Kind | What it does | Refused when |
| --- | --- | --- | --- |
| `get_document` | query | the notebook's text, the book it is committed in, its path and its slug | never |
| `get_reading` | query | which reading is shown, when a live one was computed, how the last run ended | the notebook cannot be read live |
| `run_live` | command, run, async | computes every record again, read-only, and shows the results; settles with the reading | a run is under way; a record fails or is over its bound (the earlier reading stays) |
| `show_stored` | command, view | shows what the notebook stores and lets the live reading go | the notebook cannot be read live |
| `open_in_notebook` | command, view | opens the notebook window on this notebook's text | the host gives the window no bus; one is being opened |
