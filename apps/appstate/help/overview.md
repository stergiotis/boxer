---
type: explanation
audience: end-user
status: draft
title: App state
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# App state

Apps keep state across restarts, and this window shows what each one
keeps and lets you clear it. It reads one introspection table,
`keelson('app_state')`, and writes nothing of its own: a **Delete** on an
entry and a **Forget…** on an app are requests to the runtime's app-state
service, which appends to the store.

## Three kinds of durable state

Every entry is one of three kinds, all on one store:

| kind | what it is | example |
| --- | --- | --- |
| `persist` | a value an app stored under a key of its own | an editor's draft, a window's pane split |
| `workingset` | a saved launch configuration under a name, written when a window closes | a tally session's mounts and query |
| `column_width` | a table column width a person dragged, kept per app, tier and column | play's result grid |

The table has one row per live entry whatever its kind: `kind`, `app_id`,
`key` (the persist key, the workingset name, or the width's
tier/scope/column key), `entity_id` (the store's own key), `payload_bytes`
(0 for a width), `detail` (the kind's metadata: a workingset's launch kind
and close reason, a width's points and font size), `written_at`, `run_id`
and `instance_key`. Payloads are described, never shown: a value's bytes
are the owning app's to read.

## The window

The **status row** has **Refresh** and the count: "N entries across M apps,
read Ns ago". Under it, one badge shows the last error, how many requests
are in flight, or the last verb's note; when there is nothing to report
the row explains what a clear is.

The **app list** on the left groups the entries by app, in app order, with
each app's entry count and total payload size. The app's short name is its
import path's last segment; the full id is on hover. A click selects the
app, and **Forget…** arms a forget for it.

The **entries pane** on the right lists the selected app's entries: kind,
key, size, detail, when written, and **Delete**. A delete names the entry
as the table names it, and the note reads "cleared *kind* *key* of *app*".

## Clearing and forgetting

A clear appends a tombstone: the app starts from its defaults at its next
open, and the trail keeps the old rows. It is a clear, not an erasure.

**Forget…** needs a confirmation. Arming draws a banner that names the app
and how many entries it had when armed — "Forget all N entries *app*
keeps? It starts from its defaults at its next open." — with a button
carrying the same count and a **Cancel**. Only that button clears; it
clears every kind the app has and reports a per-kind count, "forgot
*app*: persist ×2, column_width ×1". A forget attempts every kind and does
not stop at the first failure, so the note says what happened per kind
rather than one verdict. A refusal or a partial clear is shown as an error:
"not cleared: *reason*".

Deleting and forgetting are the person's own clicks and are not on the
operations catalog: they write the store outside the window, and no task
has a reason to do it that the person cannot do faster at the window. An
agent sees what is kept and asks; the **Operations** page says how.

## How the rows arrive

A poller reads the table off the frame every three seconds and after every
verb. A failed read keeps the rows you had and shows the error. Where the
host minted no bus for the window, the status row says there is nothing to
read through. The window asks for two grants: reading `keelson('app_state')`,
which is remembered, and the app-state verbs, which are not — a remembered
grant to clear every app's state is one that should not exist, so the
broker asks each time the window opens.
