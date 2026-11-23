---
type: explanation
audience: end-user
status: draft
title: The artefact and its export
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The artefact and its export

A conversation may hold an **artefact**: one markdown document the model
reads and edits through tools, under the ceiling, which you review beside
the transcript as **Artefact** (ADR-0282). It is on when the Artefact option
was on at the conversation's first message (`BOXER_CHAT_ARTEFACT` seeds it);
**View** › **Artefact** opens the view; in a conversation started without an
artefact the item is greyed and its hover says how to have one. The view
comes forward by itself when the model first writes the artefact and when a
decision waits in it. The artefact lives in memory with the
conversation. A resume reads its revisions back from the journal, one per
turn that moved it, the last as the head; the writes inside a turn do not
come back (ADR-0379 §SD4). Screenshots come back only with an export.

## Revisions

Once the artefact has a revision, the view's first line is
`revision N · M lines`, with **Copy**, **Export** and **Open in mdedit**.
Revision 0 is the empty document every artefact starts as; before the first
change there is no such line, and a first proposal heads the view on its
own. While the export question is open, **Export** is not offered beside it,
nor **Open in mdedit** when that is what asked to export first: the answer
is in the question. Every change is a new revision: the model's, named by the turn,
the tool and the call's title; a **Revert to this** from the list, which
makes an earlier revision current as a new one; your removal of a
screenshot. Under the first lines, the artefact's own views are tabs:
**Read**, the artefact as it reads, and **Source**, the **Revisions** tab, which lists them with
the lines each changed; **Lint**, which shows what renders differently from what was meant
(links to missing headings, skipped levels, unclosed fences, invalid
frontmatter, footnotes without definitions); and **Images**, the screenshot
set. A tab can be dragged beside another, Source beside Read say.

The policy line under the first says what the model may do, unless a
question waits in the artefact, which says it for itself: "The model may
read the artefact, not change it" when **At most** is below Edit; "Each
change the model makes waits for you here" under **Ask first**, where a
write is a proposal; "The model's changes apply at once; the Revisions tab
can undo any of them" under **Apply directly**. A proposal says what **Accept**
does (writes the change as the next revision, and the model's turn goes on)
and what **Reject** does (discards it, and the model is told), with its diff
after the answers. A screenshot the model asks to see is drawn the same way,
with **Allow** and **Decline** and the preview after them.

The artefact carries the conversation's properties in its frontmatter,
stamped by the chat: `chat_conversation`, `chat_title`, `chat_started`,
`chat_model`, `chat_endpoint`, `chat_run`, `chat_app`, `chat_window`,
`chat_task` and `chat_keep`. The **Read** view, a proposal's diff and
a revision's diff leave them out, since the model neither wrote them nor
can change them; **Source**, **Copy**, **Export** and **Open in mdedit**
keep them.

**Copy** copies the text with the citations its references name as a foot,
as the hand-off to mdedit does. A copy or a hand-off of text that cites a
claim that did not hold, or that names no check, is held once: the reason
shows under the buttons, with **Anyway**.

## The model's tools

| Tools | Do |
| --- | --- |
| `artefact_read`, `artefact_outline`, `artefact_find`, `artefact_inspect`, `artefact_lint` | read: the text by numbered lines, the headings, matches of a text or RE2 pattern, the parsed structure, the lint findings |
| `artefact_write`, `artefact_edit`, `artefact_insert`, `artefact_replace_section`, `artefact_set_frontmatter` | write: the whole text, one unique occurrence, lines at a place, a section's body, top-level properties |
| `artefact_images`, `artefact_capture`, `artefact_copy_image`, `artefact_crop_image`, `artefact_remove_image` | the screenshot set: list, capture windows of the task, name a copy, crop, remove or purge |
| `artefact_view_image` | look at an image of the set, as Pixels allows |

Every write names the `base_revision` it was made against; a write against
a revision that is no longer the head is refused with "the artefact is at
revision N, not M: read it again and redo the change". The write tools are
offered only when the policy lets the model write.

## Screenshots and their budget

Captures are sealed on disk and die with the process (ADR-0284). Per
conversation they are bounded by `BOXER_CHAT_IMAGE_BYTES` and
`BOXER_CHAT_IMAGE_COUNT`, and an image whose pixel count passes
`BOXER_CHAT_IMAGE_PIXELS` is refused before it is decoded (§SD4). A tool
that would pass a ceiling is refused with the use and the ceiling. Removing
and purging in the **Images** tab is the way out of a full budget, and
is a revision like the model's.

## Adding an image, and sending one

**Add image**, in the **Images** tab, opens the host's file dialog and
reads the one PNG or JPEG file you choose into the set (ADR-0385). The set
keeps the file's pixels, turned upright, and nothing else of it: no
location, time or device, no comment, and not its name, which only the
tab shows you. The chat names the image `upload-<n>`. Adding is a
revision of yours, under the same budget; a file that is neither format, or
past a ceiling, is refused and changes nothing. The dialog lists the files
of the machine the host runs on.

**Attach to next message**, on any image of the set, puts it into your next
message. The composer names what is chosen, and each name there takes it
out again. The model gets the image with your text for that turn;
afterwards the message keeps your text and the image's name, and the model
can look again with `artefact_view_image`. Sending is offered above
Metadata only, where Pixels lets pixels reach the model, and each image sent
is recorded as shown by you. When the host cannot record it, nothing is
sent and the draft comes back with the reason. A message that carried an
image marks the conversation as having read untrusted content.

Under This chat's captures the model views its own screenshots without
asking; an image you added, or a copy or crop of one, asks once.

## Endorse

**Endorse** records that you reviewed this revision and stand behind it for
publishing (ADR-0311): the record names the revision and what changed since
you last endorsed one. A later model edit voids it; your own edit does not.
The line reads `endorsed` while the head is covered.

## Export

**Export** writes the head revision's text, as `artefact.md`, and the
screenshots it names, as one snapshot of this conversation's mount in the
lading store — the window calls it the shared file store — through the
host's publish service (ADR-0342). You confirm first. The confirmation is a
decision you started, so **Export** is the filled answer and **Cancel** the
other. It says the revision, how many files and bytes, that the snapshot is
kept for 30 days and readable by anyone who can read the store, and — when
no endorsement covers the revision — that exporting also records that you
reviewed it and stand behind it for publishing. After the answers, a line
says who the export's label lets read it ("Its label lets it be shared with
anyone."); its hover names the label's TLP tier, the conversation's own. If the artefact moved since you asked, the confirmation says so, and
Export stays greyed with the reason under it.

A conversation that **holds confined content** cannot export: the
confirmation refuses, names the call that confined it, and keeps Export
greyed with the reason. The service may refuse too, before anything is endorsed. Each
refusal leaves a journal entry of its own, named `export-refused`, with the
stage — confirmation, check or publish — the reason, and the confining call
where that is the reason; a successful export leaves an `artefact` entry
with the mount, the snapshot time, the text's digest and the images'
hashes. The status line reads `exported revision N to the shared file store, kept
until <date>`.

**Open in mdedit** opens the artefact as a new, unsaved document in
mdedit, where you can type; nothing comes back to the chat. With
screenshots, it is exported first, by the same confirmation, so mdedit can
show them from the snapshot.
