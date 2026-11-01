---
type: adr
status: accepted
date: 2026-11-01
reviewed-by: "stergiotis"
reviewed-date: 2026-11-01
---

# ADR-0353: mdgraph — a document graph of notes, links and tags

## Context

A Markdown corpus — a docs tree, an ADR log, a vault — is a graph: notes
link to notes and carry tags. Obsidian's graph view made that picture the
expected way to see such a corpus: a node per note, an edge per link
between two notes, optionally a node per tag joined to the notes carrying
it, and the links that point nowhere shown as ghost nodes. Its local view
centres on the open note and shows its neighbourhood to a chosen depth.

boxer already reads the parts: `mdextract` gives a document's links (with
their spelling — wikilink, embed, inline, image, autolink), its tags from
the text and the `tags` property, and its `aliases`. What it lacks is the
step from many documents to one graph, and with it the one rule that is
not obvious: which file a link means. Each consumer that drew a graph
would otherwise write its own link resolution, and two pictures of one
corpus would disagree about its edges. The first consumer is a source
browser in a downstream repository that shows the graph beside an open
Markdown file.

The design follows Obsidian's observable behaviour (its documentation and
what its graph view shows), not its code.

## Decision

We will add `public/semistructured/markdown/mdgraph`, a pure package with
no UI and no I/O, that turns a corpus of extracted documents into a graph.

**SD1 — Input.** The corpus is a slice of notes: a slash path relative to
the corpus root, with its extension, and the `mdextract.Document` read
from it. The caller reads and extracts; mdgraph sees no file system, so a
caller can cache extraction per file and rebuild the graph cheaply.

**SD2 — Nodes and edges.** A node is a note, a tag or an unresolved
target, keyed by kind and key: the note's path, the tag's name
lowercased (Obsidian matches tags case-insensitively), the unresolved
target normalised. A nested tag `a/b` is one node; its parents are not
added. An edge runs from a note to a note (kind link or embed) or from a
note to a tag (kind tag); repeated occurrences between the same pair and
kind collapse into one edge whose weight counts them. A link of a note to
itself adds no edge. Tag nodes and unresolved nodes are each optional.
The graph is struct-of-arrays, notes first in input order, the rest in
first-seen order, so a build is deterministic.

**SD3 — Link resolution.** A link is skipped when it is external, when it
has only a fragment (a link into the same note), or when its target has
an extension other than `.md` (an attachment). Otherwise:

- A **wikilink or embed** names a page, matched case-insensitively and
  with or without `.md`. A page with a `/` matches the notes whose path
  ends in it; a bare name matches the notes with that base name, then the
  notes listing it among their `aliases`.
- An **inline link or image** is a path: relative to the linking note's
  directory, then relative to the corpus root (Obsidian's "absolute path
  in vault" spelling); a leading `/` means the root only. Each is tried
  as written and then with `.md` added. A bare name that misses falls back to
  the wikilink rule, because Obsidian writes a note's shortest unique name
  into a Markdown link too.

When several notes match, the one whose directory shares the longest
prefix with the linking note's wins, then the shortest path, then the
first in byte order. A link that matches nothing is unresolved.

## Alternatives

- **Resolution in each consumer.** Rejected: the edges of a corpus would
  depend on which tool drew them.
- **The graph inside the facts store.** `mddocfacts` already records links
  and tags as facts, and a SQL view could join them into edges. Rejected
  as the first step: resolution needs the whole corpus's names, which is a
  Go pass either way, and a browser should not need a database to draw
  the open note's neighbourhood. The package's output can feed the store
  later.
- **Hierarchical tags as a chain of nodes.** Rejected for now: it doubles
  the tag nodes of a deep taxonomy for a picture that reads the same
  through the full names; a consumer can add the parent edges from the
  keys.

## Consequences

### Positive

- One resolution rule for every graph of a corpus, testable on its own.
- Rebuilding after an edit costs only the changed files' extraction plus a
  linear pass, because mdgraph takes extracted documents.

### Negative

- Ambiguous names resolve by a proximity rule that may differ from what an
  Obsidian vault shows for the same tie; such ties are rare in a
  well-named corpus, and the rule is deterministic.

### Neutral

- Attachments are not nodes. A consumer that wants them can add a node
  kind later without changing the note and tag rules.
