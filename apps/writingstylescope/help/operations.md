---
type: reference
audience: end-user
status: draft
title: Operating writing style scope as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating writing style scope as an agent

Writing style scope holds no model grant of its own. An agent reaches a
window through its operations catalog under a task grant the person approved
(ADR-0269, ADR-0318), replaces a document, sets the floor, runs the sweep and
reads the matrix; the person sees every change in the window and in the
command log, and the same handlers run when they press a button.
`keelson('operations')` lists the operations with their arguments; this page
says how to use them together and what each refuses.

## Opening a window on two documents

Another app opens writing style scope on a pair of documents through its
launch config, kind `writingstylescopeLaunch`
(`apps/writingstylescope/launchcfg`): document A and document B as markdown
text, and optionally the section-length floor in bytes, where zero leaves
the default of 200. The documents travel as text rather than as references
to files, because the host caps a launch config at 64 KiB, which holds the
pairs the window is for, and a reference would make the open depend on a
snapshot store holding both files first. A config whose document is over
1 MiB or whose floor is outside 0 to 4000 fails the open with the reason
rather than opening a plain window. The first sweep runs on the first frame.

For scenes and headless runs, `BOXER_WRITINGSTYLESCOPE_DOCS` carries the
two documents' text as a JSON object with `DocA` and `DocB`; the window
reads no file. A value that does not decode, or lacks either document,
leaves the window on the person's drafts and says why on the Documents
tab. A launch config wins over the variable. A window opened
either way neither restores nor stores the drafts.

## The resources

- `documents` — the two drafts as the panes hold them. Written by
  `set_document`. While the person types in a pane the resource reads as
  being edited, and an agent's write lands as a conflict: the person wins a
  tie.
- `floor` — the section-length floor in bytes, 0 to 4000. Written by
  `set_floor`; while the person drags the slider it reads as being edited.
- `analysis_state` — the phase of the analysis machine, below.

## The phases

The status row's chip is a state machine the window mirrors every frame
(keelson-app T1): **no result**, nothing swept yet; **sweeping**; **result**,
a matrix that describes the panes as they are; **stale**, a matrix computed
from other text or another floor; **failed**, the last sweep refused and left
no matrix. `analyze` moves any phase to sweeping; an edit moves a result to
stale and undoing the edit moves it back. A cancel keeps the matrix the sweep
would have replaced, so it ends in result or stale, or in no result when
there was none. `analysis_state` returns the current phase, the edges open
from it with the gesture that causes each, and the latest steps;
`analysis_machine` returns every state and edge.

## get_view

A query over a snapshot of the window: the phase, each document's length in
bytes and its revision, the floor, whether a sweep is running or the matrix
is stale, the refusal of a failed sweep, and, when there is a matrix, its
section and pair counts, the sections dropped below the floor, the sweep's
wall-clock and the document-level readings (profile NCD and CCC, the
instance sweep's statistics). It also reports an *Open in play* request in
flight and what the last one opened or why it failed. It carries no matrix
cell; those are `get_pairs` and `get_distribution`.

## get_pairs

A query returning the closest section pairs of the whole matrix, not only
the 25 the table shows: the rank, each section's index, heading and length,
the distance, and `Beats`, the fraction of all pairs this one is strictly
closer than. `Limit` bounds the rows, 25 by default and at most 200, from the
closest; `Truncated` says the matrix has more. `Stale` says the matrix was
computed from other text. The headings are the documents' own text and come
back marked untrusted. `Method` states what the distance is and what it
does not say: normalized compression distance with zstd over raw markdown,
with no absolute scale across subjects, languages or section lengths, so a
pair is placed only in these two documents' own background.

The refusals name the remedy: *there is no result yet; analyze launches a
sweep*; *the sweep is running; read get_view until its phase is result*;
*the last sweep refused:* with the reason.

## get_distribution

A query returning the empirical distribution of every pair's distance as
quantiles, evenly spaced from 0 to 1 by nearest rank, with the minimum, the
median and the maximum. `Points` asks for 21 by default, at least 2 and at
most 101; more than 101 returns 101 and sets `Truncated`. The pairs share
sections and are not independent, and the result says so: no confidence
statement is made about the distribution here.

## set_document

A command replacing document `A` or `B`, quoting the revision `get_view`
returned for it, in the shape of mdedit's `propose_edit`: a document that
changed since that revision is refused (*read it again with get_view*), and
so is a text over 1 MiB, naming the cap. The command sets and never sweeps;
the matrix goes stale until `analyze`. The person's gesture is typing or
pasting in the pane.

## set_floor

A command setting the section-length floor. A value outside 0 to 4000 is
refused naming the range (*floor is between 0 and 4000 bytes*); a fraction
is truncated to whole bytes. The matrix goes stale until `analyze`. The
person's gesture is the slider.

## analyze

A command sweeping the two documents as they are. It is asynchronous
(ADR-0358): the call stays running while the sweep does and settles with the
phase it ended in — `result` with the section and pair counts, `failed` with
the refusal, or, after a cancel, the phase the kept matrix is in and
`Cancelled`. While a sweep runs the catalog marks it unavailable; the running
sweep settles through the call that started it. The person's gesture is the
**Analyze** button, which queues a second sweep behind a running one rather
than dropping it.

## open_in_play

A command publishing the whole matrix as an ephemeral dataset and opening
the SQL playground on it, seeded with the ranked query — the Matrix tab's
**Open in play** button. It writes outside the window, so it is
consequential: the host asks the person to confirm it every time an agent
asks. It is unavailable without a matrix and while a request is in flight,
and refused on a host without the app runtime. The request runs off the
frame; `get_view`'s handover fields say what opened or why it failed.

## What is not an operation

The dock layout, the heatmap's hover and the plot boxes are the person's
view and have no operation. **Load example** and **Clear** replace both
documents at once and are the person's; an agent reaches the same text
through `set_document`. The confidence band on the Distribution tab is a
drawing aid with an assumption the data does not meet, and no query returns
it.
