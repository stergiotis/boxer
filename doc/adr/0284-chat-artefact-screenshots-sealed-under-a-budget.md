---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0284: Screenshots beside the chat's artefact — collected, copied and cropped by the model, sealed on disk under a budget

## Context

The chat's artefact ([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md))
is one markdown document per conversation. Its SD1 gave it a set of
attachments addressed by content hash and left the set empty: no verb adds
one, so that adding images later changes no tool's result.

Window captures now exist as pixels
([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)):
a task's grant permits a PNG of one or more of its windows, cropped to a
rect of the frame, through one policy enforcement point. The capture is
sealed (below), `read` hands the task its bytes in the process, and the
task's captures are released when the task ends. ADR-0281 §SD7 defers
captures a person or an app starts, since they need a subject other than a
task grant.

What is missing is a place in the conversation where captures are kept and
reworked. Keeping them beyond the process, and referencing them from the
markdown, are later decisions. Held in memory, a conversation that collects
screenshots would grow until the process does; written to disk as they are,
they would be plaintext at rest.

Ad-hoc datasets ([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md))
already solve the second: a dataset is a `sealed.File` — an unnamed inode
encrypted under an ephemeral key that exists only in the process, freed when
the file is closed.

## Design space (QOC)

**Q1 — Who adds, copies and crops.** Model tools in the tool loop
(chosen), operations on the chat's window for another agent, or both. The
model is the one that can name what it is capturing and why; a capture also
needs a task grant, which only the coordinator's model holds.

**Q2 — What a copy is.** A new entry in the set from an existing one
(chosen), or a copy to the system clipboard. The set is content-addressed,
so a copy costs no bytes until it is cropped; the clipboard is a panel
affordance that can come later.

**Q3 — What a full budget does.** Refuse the new image (chosen), or evict
the oldest. Eviction removes something the person may have looked at and the
model may have planned around; a refusal says what is used and leaves the
choice to whoever asked.

**Q4 — Whether an image change is a write.** Yes (chosen): adding, copying,
cropping and removing are artefact writes, under Edit and under Ask first as
the text writes are. The alternative — collecting freely, since a capture is
a read of a window the task already holds — would let the artefact change
without the person's say under Ask first.

## Decision

### SD1 — The set

- An **image** is a name, the content hash of its PNG bytes (BLAKE3, as the
  capture's digest), its size in pixels and bytes, and where it came from:
  the capture (windows, crop, the capture's job), or the image it was
  copied or cropped from.
- **Names** are what the markdown will reference (`![[name]]`, deferred):
  unique in the set, lower-case letters, digits, `-`, `_` and `.`, ending in
  `.png`. The model may pass one; otherwise the chat picks
  `screenshot-<n>.png`.
- **Bytes are stored once per hash, sealed.** Each hash's PNG is a
  `sealed.File`, as an ad-hoc dataset is: no name on disk, no plaintext at
  rest, and the key only in the process. Memory holds the keys, the
  entries and the panel's thumbnails, not the images. Two names on one
  hash cost the bytes once.
- **From the capture to the set without plaintext.** The capture service
  seals what it hands out, and `read` returns the bytes in the process
  (ADR-0281 §SD6). The chat seals them again into a file of its own at once:
  the task's captures are released when the task ends, and a screenshot in
  the artefact outlives the task that took it.

### SD2 — Revisions

The set is part of a revision, as the text is: a revision carries the text
and the set's entries. Revert, take-back and branch therefore restore the
images with the text, and need no rule of their own (ADR-0282 §SD1).

Bytes are held while any revision names their hash. Removing an image from
the current revision does not free its bytes while an older revision still
names it; **purging** does — the sealed file is closed, which frees the
inode and drops the key, and older revisions keep the entry marked purged. Reverting to such a revision restores the text and reports
the image as purged.

### SD3 — The tools

Offered with the write tools of ADR-0282 §SD2, when Artefact is on and the
ceiling allows Edit. Each names its base revision and is refused if the
artefact moved, as the text writes are.

| Tool | Does |
| --- | --- |
| `artefact_capture` | captures windows of the task as a PNG — the windows, an optional crop in logical points — and adds it. Needs Apps and a grant naming the windows. |
| `artefact_copy_image` | adds a new name on an existing image's bytes |
| `artefact_crop_image` | adds a new image cut from an existing one, by a rect in the image's pixels |
| `artefact_remove_image` | removes a name from the set; `purge` also frees the bytes |

A read tool, offered whenever Artefact is on:

| Tool | Returns |
| --- | --- |
| `artefact_images` | the set: name, hash, size, source, whether purged, and the budget's use |

- **A capture is cut to its windows.** ADR-0281 captures the whole frame
  with only the granted windows drawn; without a crop of the model's,
  `artefact_capture` crops to the union of the windows' outer rects, read
  from `keelson('windows')` — geometry only, no titles — and keeps the frame
  when they cannot be read. The entry says which it was.
- **The model does not see the pixels.** The results are metadata. Showing
  a screenshot to a vision-capable model is ADR-0281 §SD7's chat capture
  tool, a decision of its own.
- **Untrusted and labelled, as every capture.** The host taints the
  conversation when the chat reads a capture, and refuses a confined
  capture to a model that is not local (ADR-0281 §SD6); the chat adds
  nothing to either.

### SD4 — The budget

- **Per conversation**, a ceiling on the sealed bytes on disk and on the
  number of names, as ad-hoc datasets bound their store (ADR-0240 §SD2). A
  tool that would pass either is refused with the use and the ceiling, and
  changes nothing.
- **Memory is bounded by what is decoded.** Cropping and thumbnails decode
  a PNG to RGBA; an image whose pixel count passes a ceiling is refused
  before it is decoded, so a small file of a huge image cannot exhaust
  memory. A thumbnail is small and kept; a full decode lives for one crop.
- The ceilings are environment variables declared per
  [ADR-0009](./0009-environment-variable-registry.md), with defaults sized
  for a session's worth of window captures.

### SD5 — Ask first, and the panel

- Under Ask first, an image change waits as a proposal, as a text write
  does, and the panel shows the image with Accept and Reject.
- The Artefact panel gains the set: a thumbnail per name, its size and
  source, and the budget's use. The person can remove and purge there, as
  the way out of a full budget; that is a revision like the model's.

### SD6 — Deferred, recorded

- **Durable storage**: keeping the images beyond the process — sealed
  files die with it, by design — on disk or on `boxer.facts`, and with
  Keep.
- **References from the markdown**: resolving `![[name]]` through
  `obsidian/resolver`, and the linter's rule for a name that is not in the
  set.
- **Showing images to the model** (ADR-0281 §SD7) — decided in
  [ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md).
- **Copying to the clipboard** from the panel.
- **Captures the person starts**, which need ADR-0281's other subjects.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| chat tools | `artefact_capture`, `artefact_copy_image`, `artefact_crop_image`, `artefact_remove_image`, `artefact_images` |
| artefact revision | carries the image set |
| environment | the budget's ceilings, registered |
| Artefact panel | the set, thumbnails, remove and purge; image proposals |

## Alternatives

- **Images as files beside a document in mdedit.** Storage would come with
  them, but the artefact lives in memory with the conversation (ADR-0282
  §SD7), and the images should share its lifetime and its revisions until
  storage is decided for both.
- **Images in memory.** No disk at all, but a conversation's screenshots
  would count against the process's memory, which is the bound that
  matters most for a long-running host.
- **A set outside the revisions.** Simpler bytes accounting, but take-back
  and revert would need their own rule for images, and a reverted text could
  reference images added after it.

## Consequences

### Positive

- The artefact's attachment slot is used the way ADR-0282 reserved it,
  without changing the text tools.
- No screenshot is plaintext at rest, and none outlives the process.
- Disk use is bounded per conversation by bytes and names; memory by decoded
  size.

### Negative

- Removing an image does not free memory until it is purged, and a purged
  image does not come back with a revert.
- A screenshot is sealed twice while its task lives — the capture
  service's copy and the chat's — until the task ends.
- A host whose `BOXER_ADHOC_DIR` filesystem lacks unnamed files (`O_TMPFILE`)
  cannot hold screenshots; there is no plaintext fallback, as for ad-hoc
  datasets.
- Five more tool schemas in every request with Artefact on and Edit allowed.

### Neutral

- A conversation that never captures pays nothing.

## Migration — Tier 1

None: new tools behind the Artefact option and Edit, and an empty set for
every existing conversation.

## Verification plan — Tier 1

- The set: a copy shares bytes; no plaintext file appears under the sealed
  directory or the capture's; a crop out of bounds is refused; names are
  validated and unique.
- The budget: a capture, copy and crop past each ceiling are refused and
  change nothing; an image past the decode ceiling is refused before it is
  decoded; purging frees the bytes and removing does not.
- Revisions: take-back and revert restore the set; a revert to a revision
  naming a purged image reports it.
- Ask first: an image change waits, Accept adds it and Reject leaves the set
  as it was.
- A scene on the headless host with a scripted model: capture a window under
  a grant, crop it, see both in the panel.

## Status

Proposed 2026-10-04, built. The verification plan holds: the unit tests of
the store (sealing once per hash, both budgets, the pixel ceiling before
decoding, pins, purge), the crop, the names, revisions carrying the set
through revert, rewind and purge, the tools under the settings and Ask
first, and the scene
[chat-artefact-images](../../apps/chat/scenes/chat-artefact-images.scene.md)
on the headless host: a capture under a grant, a crop of it, both in the
Images tab. Not verified: the desktop host, and a real model's use of the
tools.

## Updates

None.

## References

- [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md) §SD1, §SD2, §SD3, §SD7 — the artefact, its tools, the ceiling, images deferred.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) §SD6, §SD7 — labels and taint of a capture; the chat's capture tool deferred.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) §SD1, §SD2 — sealed files and the store's quotas.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — the ceiling.
- [ADR-0009](./0009-environment-variable-registry.md) — environment variables.
