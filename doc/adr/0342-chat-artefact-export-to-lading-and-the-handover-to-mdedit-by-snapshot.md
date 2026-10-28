---
type: adr
status: proposed
date: 2026-10-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0342: The chat's artefact exported to lading, and handed to mdedit by snapshot

## Context

The chat's artefact ([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md))
is one markdown document per conversation, with screenshots beside it
([ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md)).
The screenshots are sealed files that die with the process. That is
ADR-0284 §SD1's rule, and its §SD6 deferred keeping them any longer. Since
ADR-0284's update of 2026-10-26, the artefact panel draws the screenshots
the text embeds.

Two gaps follow from the sealing:

- **Open in mdedit hands over the text alone.** The launch config carries a
  name and the text, so in mdedit every `![[name]]` is a link to nothing.
  mdedit also has no image resolver of its own, not even for a document it
  reads from a lading snapshot in its Files pane.
- **Nothing of the artefact outlives the process.** A resumed conversation
  ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
  §SD6) gets its text back from the journal, but its screenshots are gone.

ADR-0341 (proposed) gives an app a way to publish a tree into the lading
store, under its own mount tag, with a placement check before anything is
written. This ADR is its first client.

## Decision

We will let the person export the artefact's current revision, its text and
the screenshots it names, as one lading snapshot of a mount that belongs to
the conversation. Open in mdedit will hand over that snapshot instead of the
text whenever the artefact has screenshots. Everything not exported stays
under ADR-0284 §SD1.

### SD1 — Export is the person's gesture

The artefact panel gets an **Export** button beside Copy and Open in mdedit.
A click opens a confirmation that names what goes where: the revision, the
files, the retention class (30 days), and that everyone who can read the
lading store can read them until the class ends. Confirming is the deliberate
gesture
[ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
§SD3 asks for. When no endorsement covers the head, the confirmation says so,
and confirming writes the `endorse` entry for the publish class before
publishing (law L15). An endorsement covers the head when the revision it
names is in the artefact with the text it endorsed, and no model has written
a revision since. When the export runs, the head is checked again, and a
revision written in between refuses the export. Then the screenshots are
read and the service is asked for a dry run (ADR-0341 §SD1), all before the
endorsement, so a refusal, such as a tier above the store's ceiling, leaves
no consent behind. The confirmation does not know the ceiling, which only the
service holds. The button is drawn whether or not the host runs the publish
service; without one, a click fails within seconds with a note in the status
line, the posture of [ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md)
§SD5. Nothing exports on its own, and the model has no export tool.

### SD2 — One snapshot per export, flat

An export is one snapshot: `artefact.md` holding the revision's text, and
each screenshot the revision names under its name, at the root. A purged
entry has no bytes and is left out. The names are already what a wikilink
embed can address without escaping (ADR-0284 §SD1), so the document's
`![[name]]` resolves inside the snapshot as written.

### SD3 — One mount per conversation, journaled

The chat's mount tag is a value claimed through `tagmint`, so no other
claimant in the link set takes it. The first export mints a mount under it
(ADR-0341 §SD2), and every later export of the conversation writes another
snapshot of it. The chat journals each export as an extension-private entry
(`KindIgnorable`, named `artefact`, form `exported`), which the projection
skips: the revision, the mount, the snapshot instant, the digest of the text
and each screenshot's hash. No journal kind is added. A resumed conversation
finds its mount from that entry. A query over the
store's tag, joined to the journal, lists a conversation's exports.

### SD4 — The label is the conversation's

The export's label is the run's label when the export runs (ADR-0311 §SD2),
joined with the integrity of the model's own output, as a copy to the
clipboard is labelled.
It is confined when the conversation is, or when its tier reaches the confined
tier. Per-revision
labels are deferred (ADR-0311 §SD5), and the screenshots carry no label of
their own: a capture's confinement is recorded on the conversation, not on the
entry. The conversation's label covers both, and over-states each, because it
is the join of everything the run read. A confined conversation therefore
cannot export, and the confirmation says so before the person confirms.
The publish service makes the same refusal (ADR-0341 §SD3).

### SD5 — The handover by snapshot

When the artefact has screenshots, Open in mdedit exports first, through the
same confirmation, and the launch config names the mount, the snapshot and
`artefact.md`, beside the text. Without screenshots, it hands over the text as
now, and no export is needed. mdedit shows the text at once, reads the
document from the snapshot over its own connection, and replaces the text
with the snapshot's when the read lands, the way its Files pane opens one, as
a document not bound to a file. The replacement happens only while the buffer
still holds the text the launch carried: edits made in the meantime stay, and
the images still come from the snapshot. Opening another document cancels the
read. When the snapshot cannot be read, the text stays and the status line
says why.

### SD6 — mdedit shows a snapshot document's images

When mdedit reads a document from a snapshot, from its Files pane or by a
handover, it also reads the images the document embeds from the same
snapshot. The parse never reads the store: it resolves an embed from the
images already decoded, and notes each one it lacks. A background read then
fetches those, decodes each under a pixel bound and a bound on its longer
side, and the document is parsed again when they arrive. The first load and an
embed typed later take the same path, and a name found missing is not asked
for again. A path is taken relative to the document's directory first, as a
CommonMark image's is; failing that, by name anywhere in the snapshot, the
shortest path winning, as Obsidian finds `![[x.png]]`. A
snapshot never changes (ADR-0198 §SD1), so what was read stays valid for as
long as the document is open.

### SD7 — Resume brings the exported revision back

A resumed conversation offered an empty artefact before this decision, since
the revisions live in the artefact store, not in the journal. When the journal
holds an export, resume reads the latest one back: the text, and each
screenshot into a fresh sealed store, each checked against the digest the
export recorded. The artefact starts with that one revision, under its own
number, so an endorsement of it still names it, and written by a model, so a
read of it re-enters as model output (ADR-0311 §SD4, §SD5). Later revisions
are numbered above every revision the journal names, by an endorsement or an
export, so a number from before the resume is not handed out again. With or
without an export, the text comparison of SD1 keeps an endorsement of an
earlier revision from covering a new one. The chat reads the store
over its own connection, as every lading reader does (ADR-0341). The journal
also keeps the artefact's head as text (`syncArtefactJournal`, ADR-0282 §SD7);
when that head is later than the export, it goes on top of the exported
revision without screenshots, and a revert reaches the export's. Revisions
between the two are not recovered, and a read that fails leaves the journal's
head, or an empty artefact, with the reason in the resume note.

### SD8 — Deferred, recorded

- **Per-screenshot labels**, so a confined capture does not stop the export
  of a conversation's other screenshots. Trigger: per-revision labels
  (ADR-0311 §SD8).
- **Copy** carrying the images. The clipboard holds text.
- **Withdrawing an export** before its class ends: a per-mount purge, which
  ADR-0341 leaves to the operator.
- **Export to a sealed store**, for a confined conversation.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `launchcfg.MdeditLaunch` | optional mount, snapshot and path | the generated `launchcfg.out.go` and its golden; mdedit's hand-over |
| chat journal | an extension-private `exported` entry; no new kind | resume, which reads it |
| identity | the tag value `chatArtefactMount`, claimed through `tagmint` | — |
| chat manifest | declares `lading.publish` and the chat's mount tag | ADR-0341's registration check |
| artefact panel | Export, and its confirmation | the endorsement path of ADR-0311 §SD3 |

## Alternatives

- **The screenshots in the launch config.** Up to the image budget over the
  bus, and the bytes would leave the sealed store for an app that is not
  bound by ADR-0287's settings, outside any placement check.
- **An ad-hoc dataset holding the files** (ADR-0240). Sealed, so ADR-0284
  §SD1 would hold, but the dataset lives as long as the chat window that
  published it and mdedit cannot extend it. A table handed to SQL is also not
  what mdedit reads.
- **`ladingadhoc`.** It writes with the chat's own credentials under a shared
  tag, checks no label, and keeps the tree for one to two days. The export
  needs the placement check and a mount the conversation owns.
- **Export on every revision.** It would put every screenshot outside the
  sealed store without a gesture, which ADR-0284 §SD1 refused, and would
  remove the endorsement moment law L15 needs.

## Consequences

### Positive

- The handed-over document in mdedit shows its screenshots, and so does any
  document mdedit reads from a snapshot.
- A conversation's artefact can outlive the process, and resume recovers its
  screenshots up to the last export.
- Every export is an endorsed publish with a journal entry and a trail row.

### Negative

- Exported screenshots are unencrypted in the store for the retention class.
  That ends ADR-0284 §SD1's guarantee for what the person exports.
- A confined conversation cannot export at all, and a conversation's label
  over-states its screenshots until per-revision labels exist.
- The store shares nothing between snapshots (ADR-0198 §SD1), so each export
  writes every screenshot again.

### Neutral

- Open in mdedit asks for a confirmation whenever the artefact has
  screenshots.

## Migration — Tier 1

Nothing to migrate. The launch config's new fields are optional; a launch
without them is the text handover as before.

## Verification plan — Tier 1

- **Lane.** The default `go test` lane, against the publish service with no
  store behind it: the plan's files and endorsement, the endorsement written
  before the publish, the refusal of a confined conversation and of a head
  that moved, and a restore from an `fstest.MapFS`. mdedit's resolver over an
  `fstest.MapFS`. The integration lane against a provisioned store: an export
  read back through the adapter, its journal entry, a second export into the
  same mount, and a resume in another window that gets the revision back.
- **What would fail.** An export without the `endorse` entry before it; an
  `endorse` entry for an export the service refused; a file missing from the
  snapshot or one the revision does not name; a parse that reads the store; a
  resume that does not restore the revision, or whose next revision takes a
  number the journal already names; a handover read that replaces edits.
- **Scene.** [chat-artefact-export](../../apps/chat/scenes/chat-artefact-export.scene.md)
  runs the gesture on the headless host against a scripted model and a
  provisioned store: the confirmation's files, retention and endorsement
  note, the export's status line, and the handover, with mdedit showing
  the screenshot inline. It is skipped where the store's tables do not
  exist.

## Status

Proposed — awaiting review by the code owner, after ADR-0341.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0341](./0341-lading-publish-a-write-only-capability-with-the-placement-check-at-the-door.md) (proposed) — the publish service.
- [ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md) §SD1, §SD6, update of 2026-10-26 — the screenshots, sealed; durable storage deferred.
- [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md) — the artefact.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) §SD2, §SD3, §SD5 — the run's label, endorsement, mixed documents.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD6 — resume.
- [ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md) — who may see a screenshot.
- [ADR-0198](./0198-fs-snapshot-store.md) §SD1 — snapshots never change and share nothing.
- [ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md) §SD5 — buttons that fail in the status line.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) — ad-hoc datasets.
