---
type: explanation
audience: end-user
status: draft
title: The capability broker demo
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The capability broker demo

An app reaches the runtime through the subjects its manifest declares, and
this window exercises four of them, one section each, so what a
declaration buys and what it does not can be seen. Nothing here touches the
filesystem or the clipboard directly: every verb is a request on the bus,
answered by a broker, and runs off the frame on the window's lifetime
scope, with its outcome drawn as a badge on the next frame.

## What the manifest declares

| subject | direction | why |
| --- | --- | --- |
| `fs.dialog.read` | publish | ask the broker to open the file picker |
| `fs.dialog.watch` | publish | ask the broker to open the folder-watch picker |
| `clipboard.write` | publish | copy a code block to the clipboard, labelled |
| `runtime.persist.<own alias>.>` | publish, injected by the host | the one persisted key, `scratchpad`, named in `PersistedKeys` |

The persist subject is not written in the manifest: naming the key in
`PersistedKeys` makes the host inject the grant for the app's own alias,
so no app repeats that pattern.

## What it deliberately does not declare

`fs.handle.>` is absent, and that is as much the point as the three
declared subjects. A granted file or folder is addressed under
`fs.handle.<uuid>.>`, and the broker adds exactly that narrow subject to
this window's live grants the moment the person approves a dialog, and
revokes it when the handle closes. Declaring the wildcard would trade a
per-file, revocable, person-approved authority for a standing one over
every handle the broker ever mints, and buy nothing: the round trips run
on the dynamic grant alone, watching included. For a watch handle the
broker grants both directions, so the subscribe to its `.event` subject is
allowed too.

## The sections

**fs.dialog.read** — *Pick a file…* publishes the request; the broker
queues it and the picker overlay resolves it. While it is open the section
says so. On approval the window reads the file through the granted
handle's `.read` subject and shows the handle subject, how many bytes came
back and the first kilobyte. *Close handle* releases it; further reads on
that subject fail. A denial shows the broker's reason.

**runtime.persist.scratchpad** — a text box and *Save*, *Load*, *Delete*
over the one persisted key. The note says what happened: "saved N bytes at
hh:mm:ss", "loaded N bytes", "get: key absent", "deleted". A Load hands the
value to the next frame before the text box binds, so a value never lands
mid-frame.

**fs.dialog.watch** — *Pick folder to watch…* opens the folder picker,
subscribes to the handle's `.event` subject before asking the watch to
start, and shows the handle, the backend the broker chose and the events
as they arrive, the last twelve of a fifty-event buffer. *Force poller
backend* asks for polling at 250 ms; *Recursive* watches the subtree.
*Stop watching* unsubscribes and keeps the handle, so a watch can start
again; *Close handle* releases it.

**clipboard.write** — a rendered document with a *Copy* button on each
fenced block. A click hands the block's text to the clipboard broker as a
labelled payload whose source is `capdemo.clipboard-doc`, so the broker's
record and the journal's copy entry say what left and from where. The
note that comes back says how it left: as plaintext, or by handle, in
which case the text is pasted inside boxer by reference.

## Without the runtime

Every section needs the app runtime: a bus with the brokers, and storage
for the persist section. Under a bare mount context each verb shows the
same line, "no bus wired — the broker needs the app runtime; open the demo
from the app center under the windowed host", or "no storage wired" for
the persist verbs.

## For agents: a read-only catalog, and no tour

The window's catalog has one resource, `sections`, and one query,
`get_view`, which reads what each section holds: the file handle and
whether the picker is open, the scratchpad's length and the persist
section's last note, the watch handle, backend, whether it is active and
how many events arrived, the clipboard's last note, and whether the window
has a bus and storage at all. It has no commands: every verb here is a
dialog the broker puts to the person — a file pick, a folder pick, a copy
to their clipboard — or a write to the window's own persisted key, and
none of them is an agent's to ask for. An agent that wants a file picked
reports it and asks.

The window registers no tour entry either. Its sections wait on a person
approving a picker, which the screenshot driver cannot do; the demo's
purpose is interactive validation of the broker, and the registration
comment says so.
