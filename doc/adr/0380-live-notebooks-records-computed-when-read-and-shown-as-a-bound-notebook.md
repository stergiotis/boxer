---
type: adr
status: proposed
date: 2026-11-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0380: Live notebooks — a notebook's records computed when it is read, and shown as a bound notebook

## Context

A sealed notebook
([ADR-0371](./0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md))
shows what was computed once and stored. A reader who wants the same report
over the data as it stands has two ways, and each loses something.
Collecting publishes a new snapshot under a tier the person states, a step
made for handing a report on. Opening the records in play shows live data
without the prose, the figures and the presentation cells. An applet
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) is live, and is one
buffer under a dock: it has no document.

ADR-0371 holds the parts of a third way and does not join them:

- §SD6 gives the records a live binding, and a *bound* notebook: one drawn
  over relations its host hands over, with nothing stored.
- §SD8 makes storing a step apart from computing.
- `mdnotebook.Collect` computes and stores in one call, and every bound
  notebook in the tree is fed by a host that already held its relations
  (observed 2026-11-17).

ADR-0371 §SD9 keeps out of a notebook "a query run against the server on
open". This record leaves that standing for the text and for the sealed
reading, and adds a reading beside them.

## Decision

A notebook is read **live** in two stages. Its records are computed on the
server, as collection computes them, and held in the process. Then they are
shown as a bound notebook. The text form is unchanged and nothing is stored.

### SD1 — Stage one is the computing half of collection

Computing the records becomes a step of its own in `mdnotebook`, and
collection is that step followed by `Seal`. A live reading sends the
statements collection sends, composed the same way, so a live result and a
stored one differ only in when they were computed.

Each record's statement carries the records before it, so the requests do
not depend on each other's answers and may be sent concurrently.

### SD2 — Stage two is a bound notebook

The computed records are handed to the cell stack by name (ADR-0371 §SD6).
What follows is that section's, unchanged:

- A presentation cell is answered by the trivial evaluator (ADR-0371 §SD7).
  A filter, an order or a computation is still a record.
- A figure is drawn as the rendering of its value; the label in the text is
  held to nothing and is not rewritten.
- The notebook is not sealed and has no pin.
- A cell whose text did not change keeps its renderer from one run to the
  next (ADR-0371 §SD4).

### SD3 — Only a notebook that reads is run live

The buffer of the notebook's records is classified as an applet's buffer is
(ADR-0132 §SD5). A notebook whose class is *read* may be run live, and its
statements are sent with `readonly` set. Any other class is refused, with
the witness that forced it. Such a notebook is collected, which a person
confirms. The class is the gate for every document, reviewed or not, as it
is for an applet no one reviewed
([ADR-0299](./0299-sql-applets-a-tab-loads-from-its-origin.md)).

A result has a bound on its rows and bytes, and a record that exceeds it
fails. It is not cut short: a truncated record would differ from the stored
one in more than its time.

### SD4 — A run is whole, and the pane says which reading it shows

A run is a task: it reports each record and can be cancelled. It replaces
what is shown only when every record answered. A run that fails or is
cancelled leaves the earlier reading in place and names the record and its
error.

The pane states which reading is on screen: stored, with the seal's state,
or live, with the time of the run. Where no endpoint answers, a notebook
that has stored results shows them and says they are stored.

A run is repeated by a gesture. Nothing runs on a timer.

### SD5 — One pane, built by one constructor, drawn by three hosts

The live reading is a **pane**: stage one, the cell stack, and the line of
SD4. One constructor builds it from a notebook document and what its host
hands over, and applies SD3. It is to a notebook what `sqlapplet.NewEmbedded`
is to an applet and `chat.NewEmbedded` to a chat. Three hosts draw it:

- a **notebooklet** window (SD6), under a minted manifest;
- an **embedding app**, in a region of its own frame;
- the notebook window (ADR-0371 §SD10), as the live reading of its Read
  face.

So what the launcher opens, what an app embeds and what the notebook window
shows are one thing under one set of rules. When a run first starts is the
host's: a notebooklet on open, the notebook window by the person's gesture.

An embedded pane follows the rules an embedded applet and an embedded
chatlet follow
([ADR-0134](./0134-adhoc-datasets.md) §SD7,
[ADR-0368](./0368-an-embedded-chat-a-chatlet-drawn-in-another-apps-window-under-the-hosts-governance.md)):

- **The host drives it.** The host calls the pane's mount, frame and
  unmount from its own, with its own context. The pane's run is a task of
  the host's, and the host's drain waits for it.
- **The capabilities are the host's to declare**, from a list the pane's
  package exports. A document widens none of them. The list holds what a
  run needs and nothing of collection: a pane does not publish to the store
  and hands no text to an editor.
- **A composed identity.** Runs are attributed to `<host app id>#<slug>`.
- **By slug, or by a document of the host's own.** A host embeds a
  registered notebook by its slug, or parses a document no launcher lists.

A host that already holds the relations a notebook shows needs no pane. It
uses the cell stack, bound (ADR-0371 §SD6), with no server and no
capability.

### SD6 — A notebooklet is a committed notebook minted as a launcher entry

A notebook in a registered book is minted as an app of kind `notebooklet`,
by a minter package of its own, as `sqlapplet` and `chatlet` mint theirs
(ADR-0132 §SD2). The committed corpus is what stands behind it. A test
walks every registered book and requires that each notebook reads, composes
its buffer and classifies *read*.

Its window is the pane and nothing else: no Records face, no collection, no
store. It offers the document to the notebook window by that app's launch
kind, as an applet offers its buffer to play
([ADR-0135](./0135-app-launch-requests.md)).

Its window serves reading the document, reading the pane's state and
starting a run. It serves nothing that changes the text. An agent's
unfiltered `describe` leaves notebooklets out, as it leaves out the other
minted kinds.

### SD7 — What a live reading does not hold

Each is deferred, with what would bring it back:

- **Parameters.** A record with a `{name:Type}` slot has no spelling for its
  value (ADR-0371 Q3). Trigger: a notebooklet that is useless without one.
- **A memo of records.** Every run computes every record. Trigger: a
  measured notebook whose run is too slow to repeat.
- **Cells that react to each other.** A click in a cell selects nothing, as
  in a sealed notebook. Trigger: a need that play's Records face does not
  meet; that is also when play's graph is reconsidered as the engine (see
  Alternatives).
- **One snapshot across records.** See Consequences.
- **Storing a live reading.** It is `Seal` over what is held, under
  collection's confirmation. Trigger: someone asks to keep what they see.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `mdnotebook` exported API | the computing step added; `Collect` keeps its signature and its results | collection's tests and the fixture, which must not change |
| `app.KindE` | `notebooklet` added | `keelson('apps')` `kind` values; the launcher's badge and filter; readers switching on kind; `runtime.agent` describe |
| The pane (SD5) | added: its constructor, its configuration and the capability list a host declares | a host's manifest; the notebook window, which draws it |
| The notebook app | a live reading on its Read face, its phase, and operations to run one and read its state | its help book; the vocabulary's assignment table |
| Notebook books | added: a registration seam and a minter, in a package of their own | the corpus test of SD6 |
| ADR-0371 §SD9, §SD10 | a dated update that names the live reading | the how-to; the positioning page's "Not live" boundary |
| Notebook text form; the trivial evaluator; play | unchanged | — |

## Alternatives

- **Play's graph as the engine.** An embedded play answers the records on
  its lanes, with a memo, supersession and signals. Not taken for this cut:
  `PlayApp` hands an embedder the sink's result alone (observed 2026-11-17),
  so each record's result needs a new seam into play, and play's run path
  bounds and rewrites a statement (ADR-0371 §SD10), so a live result would
  differ from a stored one in more than its time. It is the engine to
  reconsider when cells must react at interaction rate.
- **An applet per cell.** ADR-0371's O1; its rejection stands.
- **The notebook window as the notebooklet's window.** A minted entry would
  carry the Records face, collection and the store, and the capabilities
  of all three. An applet's window is its attenuated surface and not play;
  and an embedder would get something other than what the launcher opens.
- **Embedding through the cell stack alone, the host running the records.**
  Each host would write its own stage one, and the class gate, `readonly`
  and the bound of SD3 with it.
- **Collect into a scratch snapshot and read it sealed.** Each record is
  encoded, hashed and published to be read once, and the store fills with
  snapshots nobody keeps.
- **A row limit that truncates.** See SD3.
- **Live for every class, behind a confirmation.** Collection is that.
- **Each record computed once, in a session or a temporary table.**
  ADR-0371 rejected a kernel with session state; a result would depend on
  what the session held.

## Consequences

### Positive

- A report can be read over the data as it stands, with its prose, figures
  and presentation cells, and without publishing anything.
- The reader, the text form and the evaluator are the sealed notebook's.
- A notebook that is both stored and live reads without a server and says
  which of the two it shows.

### Negative

- A run costs what a collection costs the server: a record is computed once
  for itself and once inside every later record that reads it.
- The records are separate requests, each over the tables as it finds them.
  A figure and a chart over a table that is being written to can disagree.
- Nothing reacts. A reader's filter is a record, and a changed record is a
  whole run.
- One text now has two readings that can show different numbers. The
  pane's line tells them apart; a screenshot does not.
- Computed records are held whole in memory, up to the bound of SD3.
- A live reading needs an endpoint, so it is not the same on every host.
- An app that embeds a pane imports the notebook app's package, as an
  applet's embedder imports `sqlapplet`.
- The server a run dials is declared in the host's manifest and not
  enforced at its bus: the declaration is a reviewer's, as it is for the
  notebook window's collection.

### Neutral

- An applet stays its own kind. An applet and a notebooklet of one record
  differ in a dock against a document.
- There are three ways to put a notebook in an app, by what the app holds:
  the relations (the cell stack, bound), a document whose records the
  server answers (the pane), or nothing but a slug (the pane, by slug).

## Migration — Tier 1

Nothing to migrate: the changes are additive. A stored notebook reads as it
did, and `Collect` returns what it returned.

## Verification plan — Tier 1

- **Default lane.** The computing step against a server that is a function,
  as collection's tests have one; collection's fixture unchanged; a table of
  buffers that are run or refused by class; a prepared live notebook that is
  not sealed and holds no pin; a pane in a test host, whose runs carry the
  composed identity; the corpus test of SD6; the minted manifest's
  capabilities held to the pane's list.
- **Integration lane.** Each sample notebook run live through
  clickhouse-local and compared row for row with its stored records.
- **Scene.** The notebook window: a sample read stored, run live, and a run
  that fails with the earlier reading kept. A notebooklet opened from the
  launcher, and the same document embedded in a demo host.
- **What would fail.** A live record that differs from the collected one
  over the same data; a notebook of another class that runs; a failed run
  that replaces what was shown; a truncated result shown as whole; a
  notebooklet window that can collect.
- **Gap.** `readonly` and the result bound are checked against the server
  the lane finds. That two requests over a moving table disagree is stated,
  not tested.

## Milestones

- **M1 — The computing step.** `Collect` split, with the bound of SD3.
- **M2 — The pane.** The constructor, the class gate, the run as a task and
  the two readings, drawn by the notebook window (SD2–SD5).
- **M3 — Notebooklets.** The kind, the book seam, the minter and the corpus
  test (SD6).
- **M4 — A first embedder.** A demo host that draws a pane beside content
  of its own (SD5).

## Open questions

- **Q1 — The name.** "Notebooklet" follows applet, chatlet and workstatelet;
  ADR-0371 Q1 leaves "notebook" itself a working name.
- **Q2 — Where a parameter is spelled.** In the frontmatter, in a `SET`
  prelude the record cells would have to admit, or in the minted manifest.
- **Q3 — Whether a closed window keeps its reading.** A workingset keeps the
  notebook (ADR-0371 §SD10); whether it reopens live is not decided.

## Status

Proposed 2026-11-17.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0371](./0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md)
  — the text form, the bindings of §SD6, collection, and what §SD9 excludes.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — minted manifests,
  the security class and `readonly` for the read class.
- [ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md)
  — the trivial evaluator a presentation cell is answered by.
- [ADR-0097](./0097-play-reactive-query-graph.md) — play's graph, the engine
  not taken here.
- [ADR-0299](./0299-sql-applets-a-tab-loads-from-its-origin.md) — a document
  no one reviewed runs only when it classifies *read*.
- [ADR-0378](./0378-dashboardlets-a-kind-for-a-dashboard-minted-from-a-committed-document.md)
  (proposed) — the kinds a committed document is minted as.
