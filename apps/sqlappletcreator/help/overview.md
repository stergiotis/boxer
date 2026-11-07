---
type: explanation
audience: end-user
status: draft
title: Authoring a SQL applet
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Authoring a SQL applet

The SQL applet creator turns a buffer into an applet document (ADR-0132
§SD1): a markdown file whose frontmatter names the applet and whose one
`sql` fence is the query it runs. The SQL playground opens the creator on
its current buffer; opened on its own, the window starts with an empty
editor and takes a paste. The window has no state beyond its drafts: the
SQL, a slug, a title, a summary and an icon, and the endpoint the buffer
was authored against.

## What the form composes

The document is composed one way, by the store contract's constructor,
so what the creator writes is exactly what the store's gate parses back:

- `type: reference`, `audience: end-user`, `status: draft`, and the
  title, summary and icon as frontmatter. Title and summary are required;
  the summary is the one line the launcher shows under the title.
- `endpoint: introspection` when the buffer was authored against the
  in-process keelson endpoint, where a bare `keelson('…')` resolves. A
  buffer authored against the default ClickHouse endpoint carries no
  `endpoint` key.
- a provenance line, then the whole buffer as the single `sql` fence. A
  buffer with more than one statement is stored whole; the note under the
  editor says which statement the caret is in, since an applet runs its
  buffer and not a statement under the cursor.

A buffer that contains a fence line (three backticks at the start of a
line) is refused before anything is composed, because the document would
re-parse differently from what was written. The slug is trimmed and used
as the store key and the export file's stem; the store checks its
spelling.

## Save: the store is the gate

**Save applet** sends the document to the applet store service
(`applet.store.save`, ADR-0132 O4 "A"). The store validates it exactly as
the committed-book corpus test does, classifies the SQL, persists the
document and mints a live manifest, so the applet joins the Apps menu
without a restart. The creator does none of that: it composes, sends, and
relays the reply. A refusal is shown as `refused: <reason>` in the status
line, in the store's words; a success names the slug and the class the
store assigned. Saving a slug the store already holds overwrites it.

The save runs as a task owned by the window, so the task monitor lists it;
the button is replaced by `Saving…` until the reply lands.

## Export: a file through the Powerbox

**Export .md…** writes the same document to a file you choose (O4 "B").
The creator never sees your filesystem: it asks the fs broker for a save
dialog with `<slug>.md` as the suggested name, the host draws the picker,
and the broker answers with a one-shot write handle for the path you
picked, which the creator writes the bytes through. Cancelling the dialog
leaves `export cancelled` in the status line; a refused write names the
broker's reason. An exported file is a user-owned artifact, outside the
store: committing it to a book is how it becomes a shipped applet.

## Operations

The creator declares no operations catalog. An agent reaches it through
the launch config on the **Opening the creator** page, and through play's
own catalog for the buffer it composes.
