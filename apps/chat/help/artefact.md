---
type: explanation
audience: end-user
status: draft
title: The artefact and its export
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The artefact and its export

A conversation may hold an **artefact**: one markdown document the model
reads and edits through tools, under the ceiling, which you review in the
**Artefact** tab (ADR-0282). It is on when the Artefact option was on at
the conversation's first message (`BOXER_CHAT_ARTEFACT` seeds it); the tab
says how to turn it on otherwise. The document lives in memory with the
conversation; a resume reads its head back from the journal.

## Revisions

The tab's first line is `revision N · M lines`, with **Copy**, **Export**
and **Open in mdedit**. Revision 0 is the empty document every artefact
starts as. Every change is a new revision: the model's, named by the turn,
the tool and the call's title; a **Revert to this** from the list, which
makes an earlier revision current as a new one; your removal of a
screenshot. The **Revisions** sub-tab lists them with the lines each
changed; **Lint** shows what renders differently from what was meant
(links to missing headings, skipped levels, unclosed fences, invalid
frontmatter, footnotes without definitions); **Images** is the screenshot
set.

The policy line under the first says what the model may do: "The model may
read the artefact, not change it" when **At most** is below Edit; "Each
change the model makes waits for you here" under **Ask first**, where a
write is a proposal with **Accept** and **Reject**; "The model's changes
apply directly; Revisions can take any back" under **Apply directly**.

The document carries the conversation's properties in its frontmatter,
stamped by the chat: `chat_conversation`, `chat_title`, `chat_started`,
`chat_model`, `chat_endpoint`, `chat_run`, `chat_app`, `chat_window`,
`chat_task` and `chat_keep`.

## The model's tools

| Tools | Do |
| --- | --- |
| `artefact_read`, `artefact_outline`, `artefact_find`, `artefact_inspect`, `artefact_lint` | read: the text by numbered lines, the headings, matches of a text or RE2 pattern, the parsed structure, the lint findings |
| `artefact_write`, `artefact_edit`, `artefact_insert`, `artefact_replace_section`, `artefact_set_frontmatter` | write: the whole text, one unique occurrence, lines at a place, a section's body, top-level properties |
| `artefact_images`, `artefact_capture`, `artefact_copy_image`, `artefact_crop_image`, `artefact_remove_image` | the screenshot set: list, capture windows of the task, name a copy, crop, remove or purge |
| `artefact_view_image` | look at a screenshot, as Pixels allows |

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
and purging in the **Images** sub-tab is the way out of a full budget, and
is a revision like the model's.

## Endorse

**Endorse** records that you reviewed this revision and stand behind it for
publishing (ADR-0311): the record names the revision and what changed since
you last endorsed one. A later model edit voids it; your own edit does not.
The line reads `endorsed` while the head is covered.

## Export

**Export** writes the head revision's text, as `artefact.md`, and the
screenshots it names, as one snapshot of this conversation's mount in the
lading store, through the host's publish service (ADR-0342). You confirm
first; the confirmation says the revision, how many files and bytes, the
label's tier, that the snapshot is kept for 30 days and readable by anyone
who can read the store, and — when no endorsement covers the revision —
that confirming also endorses it for publishing. If the artefact moved
since you asked, the confirmation says so and offers no Export.

A conversation that **holds confined content** cannot export: the
confirmation refuses, names the call that confined it, and offers no
Export. The service may refuse too, before anything is endorsed. Each
refusal leaves a journal entry of its own, named `export-refused`, with the
stage — confirmation, check or publish — the reason, and the confining call
where that is the reason; a successful export leaves an `artefact` entry
with the mount, the snapshot time, the text's digest and the images'
hashes. The status line reads `exported revision N to the lading store, kept
until <date>`.

**Open in mdedit** opens the artefact as a new, unsaved document in
mdedit, where you can type; nothing comes back to the chat. With
screenshots, it is exported first, by the same confirmation, so mdedit can
show them from the snapshot.
