---
type: explanation
audience: end-user
status: draft
title: Shared writing between two documents
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Shared writing between two documents

Writing style scope takes two Markdown documents and asks where one holds
writing from the other. Paste two documents. Every section of the first is
compared against every section of the second by compressing the pair
together: a pair that shares wording compresses better than a pair that does
not. The app returns no verdict and draws no threshold; it shows where each
pair sits among all the other pairs of the same two documents, and reading
that is the person's job. The measure, the section rule and the band are on
the **Compression distance between sections** page; this page is the
window.

An agent drives the window through its operations catalog, and another app
opens it on two documents through its launch config; both, with the phases
of the analysis, are on the **Operating writing style scope as an agent**
page. The only thing the window publishes for SQL is what the person or an
agent hands to play from the Matrix tab.

## The status row

Above the tabs, the analysis's phase chip — *no result*, *sweeping*,
*result*, *stale* or *failed*; a click opens its graph and history — sits
beside a line saying what the phase holds: the section and pair counts of
the result, or why the sweep refused.

## Documents tab

Two paste panes. **Document A — rows**: Document A. Its sections become the
rows of the matrix. Markdown headings mark the section boundaries; a
document with no headings is compared as a whole. **Document B — columns**:
Document B. Its sections become the columns of the matrix. Each pane takes
at most 1 MiB.

Under them, the controls in the order the procedure runs:

- **Analyze** — Run the sweep. Cost grows with the product of the two
  section counts, so it runs when you ask rather than as you type. An
  Analyze pressed while a sweep runs is queued, and runs over the edited
  text when the first finishes, rather than dropped.
- **Load example** puts the built-in pair in both panes and sweeps.
  **Clear** empties both and sweeps, which refuses, since there is nothing
  to compare.
- **section-length floor (bytes)** — Sections shorter than this are left out
  of the sweep. Compression distance between very short texts measures
  framing overhead more than content, and an empty nesting heading is mostly
  framing. Default 200, range 0 to 4000.

The first sweep runs on the first frame after the window opens, over
whatever the panes hold then.

The readout below gives, per document, the sections that cleared the floor
and how many did not, then the pair count and the sweep time, then the
document-level readings — *profile NCD*, *profile CCC* and the *instance
sweep* line. These compare the documents as wholes and are deliberately
blind to a single copied section; the method page says what each measures.

### Refusals

A sweep that cannot run leaves no previous matrix behind and says why in the
readout, as *cannot sweep:* followed by one of these:

- *document too large* — a pane is over 1 MiB. Paste less.
- *nothing to compare: too few sections reach the length floor* — one
  document has no section at or above the floor. Lower the floor, or paste
  a document with more text under its headings.
- *too many sections — raise the section-length floor to merge short
  sections out of the sweep* — a document has more than 128 sections above
  the floor. The floor is the remedy, as the message says.

A measurement failure inside the sweep aborts it rather than leaving a hole
in the matrix, because a hole would be indistinguishable from a low score.

### Stale results

The result is cached against the text and the floor it was computed from.
Once either changes, a badge appears — *stale — the text or the floor
changed since this result was computed* on the Documents tab, *showing the
previous sweep — the text has changed since* on the other two — and the
result stays on screen, labelled, until the next Analyze. The text has
changed since this result was computed. Press Analyze to refresh it.

### The sweep task

Each sweep is a keelson task of kind `writingstylescope.sweep`, titled
*section-pair sweep*, so it has a row on the bus and a cancel there as well
as in the window. While it runs the standard progress row appears under the
controls and on the result tabs: The sweep in progress: section pairs
measured so far, the rate, and the time left. Cancel keeps the previous
result. Progress is reported after each row of the matrix, so the fraction,
the rate in pairs per second and the ETA move in row-sized steps.
Cancelling resets the runner to idle and leaves the cached result as it was.

## Matrix tab

The heatmap is every A section (rows) against every B section (columns).
Bright is a low distance — the pair compressed well together. The colour
range spans this matrix's own minimum and maximum, so brightness is relative
to these two documents and not to any fixed scale. Axis ticks carry section
titles up to 22 sections a side; past that they fall back to indices.
Hovering a cell names the pair under the cursor, its distance, and where
that distance falls among all the pairs in this matrix.

**Closest pairs** lists up to 25 cells, ascending by distance, with the
fraction of all pairs each one beats and the byte sizes of both sections.
The closest pair measured reads near 100% — worth opening both sections
side by side.

### Open in play

The button under the table publishes the whole cross-matrix — every pair,
not just the ranked ones — as an ephemeral dataset and opens it in the SQL
playground, seeded with the query behind this table. The dataset lives as
long as this window does. What is published:

- the alias `pairs`, one row per cell in row-major order, with the columns
  `a_idx`, `b_idx`, `a_section`, `b_section`, `a_level`, `b_level`,
  `a_bytes`, `b_bytes`, `ncd` and `quantile`. `quantile` is the fraction of
  all pairs at or below that cell, carried into SQL so a query can reproduce
  the table's *beats* column without recomputing the distribution;
- a play window on the introspection endpoint with the dataset bound under
  `pairs`, which runs `SELECT … FROM keelson('pairs') ORDER BY ncd ASC LIMIT
  25` at once and is free to drop the limit.

The handover is a task of its own, kind `writingstylescope.handover`, titled
*open the pairs in play*. The button reads *Opening…* while it runs and then
reports *opened in play — N pairs as `<handle>` (rev R)* or *handover
failed:* with the reason. Pressing it again republishes onto the same handle
at a new revision rather than minting a second dataset; the runtime retracts
the dataset when the window closes. On a host without the app runtime the
button does nothing and the line *(needs the app runtime — no bus on this
host)* says so.

## Distribution tab

The ECDF of all the pairwise distances, with the closest pairs marked as
vertical lines. The distribution of all the pairwise distances. The bulk of
the curve is this document pair's own background: what 'unrelated' looks
like for this subject matter, in this language, at these section lengths. A
shared section is a point detached from the bulk on the left. The status
line under the plot is the crosshair reading; the lines after it name the
band actually on screen, and the last line gives the closest pair, the
median pair and the gap between them.

The shaded band is a 95% simultaneous confidence band. Its coverage
guarantee assumes independent observations, and these are not independent —
every section appears in many pairs. Read it as a scale reference, not as a
calibrated probability statement. The exact band is solved once per sample
size in a background job; until it is ready the plot shows the wider
closed-form band and a line reports the warm-up's progress. The job is
cancelled with the window.

A matrix with fewer than two finite cells, or with every cell at the same
distance, has no distribution to draw, and the tab says so.

## What persists

Three things are stored under the app's persisted keys and restored when a
window opens: the two drafts and the floor. A window opened on documents of
its own — by a launch config or by `BOXER_WRITINGSTYLESCOPE_DOCS` — neither
restores nor stores them, so the person's drafts are left as they were. The sweep writes them from its
own goroutine, and the window writes them once more when it closes, for
edits made after the last sweep. The result is a recomputable cache and is
not stored; the first sweep after reopening recomputes it. Nothing else is
written anywhere — the measurement touches no filesystem.
