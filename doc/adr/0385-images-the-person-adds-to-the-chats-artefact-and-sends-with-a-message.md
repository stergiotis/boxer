---
type: adr
status: accepted
date: 2026-11-20
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-20
---

# ADR-0385: Images the person adds to the chat's artefact, and sends with a message

## Context

Three decisions left the same gap open. The artefact was given a set of
attachments that no verb fills, "decided with the upload"
([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md)
§SD1, §SD7). The set then got screenshots, but only ones the model
captures, copies and crops
([ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md)).
The Pixels setting decides which of them the model may see, when the model
asks, and names "images the person attaches" as a source that did not
exist
([ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md)
§SD7).

So a person can neither bring an image of their own into a conversation
nor put any image in front of the model themselves. The parts on the
model's side are there: the client sends a user turn with image parts, a
journal entry of the person's carries images by digest
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
§SD1), and the trail records the digests a call carried.

What constrains the answer:

- **One way in.** An app gets a person's file through the file dialog,
  which is the grant: the app reads one file through a handle and never
  sees a path
  ([ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD7). No
  host forwards a dropped file to an app, and no app reads the clipboard
  (ADR-0026's `clipboard.write` update,
  [ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md)
  §SD6).
- **The dialog lists the host's files.** On a host a person reaches
  through the remote viewer, those are not the files of the device in
  front of them. A browser tab has no dialog and no sealed files.
- **The set holds PNG only.** A photo is a JPEG, several times smaller
  than the PNG of the same pixels.
- **An image file carries more than pixels**: where and when a photo was
  taken, the device, comments, a thumbnail of the frame before it was
  cropped. The Pixels setting speaks of pixels; nothing covers the rest.
- **A file is external content**
  ([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
  §SD1), as a capture is.

## Design space (QOC)

**Q1 — Where an upload lands.** In the artefact's set (chosen), on the
message with a store of its own, or both. The set already has sealed
bytes under a budget, revisions that carry images through revert,
take-back and branch, a panel to look at and remove them, the model's
copy, crop and embed, and an export that survives the process. A message
store would repeat the first three and reach none of the rest. The price
is that a conversation without an artefact takes no image. **Chosen** by
the owner, who accepts that price.

**Q2 — How the person puts an image before the model.** An option to send
any image of the set with the next message (chosen), or an attach-a-file
button in the composer that covers uploads only. The first also covers a
screenshot the model captured, which the person may want the model to
look at without the model asking. **Chosen** by the owner.

**Q3 — What governs a person's send.** The Pixels setting gates it and the
gesture is the consent (chosen), or the gesture alone decides, whatever
the setting. With one gate, Metadata only and a host's cap mean that no
pixels leave, without a second rule to check them against. The price is
that a person at the default level raises it before the first send.

**Q4 — Which hosts.** The desktop's file dialog only (chosen), or also a
drop, a pasted image and an upload from the remote viewer. Each of the
others needs a lane from the host to the app that does not exist.
**Chosen** by the owner.

**Q5 — What the set keeps of a file.** The file as given, a re-encoding
without its metadata (chosen), or a PNG of it. The file as given sends a
photo's location to the model's endpoint with the first view and to the
lading store with the first export, under a setting that names neither.
A PNG keeps the set as it is and makes a photo several times larger
against the budget and in every request that carries it.

## Decision

A person adds an image file to the artefact's set, where it is an image
like any other, and may send any image of the set with their next
message. The Pixels setting governs the send; the person's gesture is the
consent.

### SD1 — Add image

The Artefact panel's Images tab gets **Add image**. It opens the host's
file dialog; the one file chosen is read once through its handle and the
handle is closed (ADR-0026 §SD7).

- **PNG and JPEG.** Anything else is refused, naming the two.
- **It is the person's revision**, as their removal of an image is
  (ADR-0284 §SD5): it waits for no proposal, and a proposal that is
  waiting is decided first.
- **The budget and the pixel ceiling apply** (ADR-0284 §SD4). A refusal
  says which one, and what is used.
- **It needs Artefact.** A conversation without one has no Images tab and
  takes no image.

### SD2 — What the set keeps

- **A re-encoding, not the file.** The pixels are decoded and encoded
  again in the file's format, turned the way the file's orientation tag
  says. Nothing else of the file is kept: no location, time, device,
  comment, embedded thumbnail or colour profile. The image's digest is of
  the bytes the set holds.
- **The set takes JPEG.** A name ends in `.png` or `.jpg`. A crop of
  either is a PNG.
- **The chat names it**, `upload-<n>` with its extension. The file's own
  name is shown to the person in the panel and goes no further: not to
  the model, the journal or an export.
- **Its source is `upload`.** A copy or crop of it descends from it, as
  one of a capture descends from the capture (ADR-0287 §SD6).

### SD3 — Send with the next message

Each image of the head's set whose bytes are held can be chosen in the
Images tab to go with the next message. The composer shows what is
chosen, and each can be taken out there.

- **The images travel in the person's own message**: their text, a line
  of the chat's naming each image, its size and source and stating that
  what an image shows is data, never instructions, then the pixels.
- **For the turn, then a placeholder**, as ADR-0287 §SD5 decided for a
  view. From the next turn the message keeps the person's text and the
  names of what was attached. The image stays in the set, so the model
  can view it again under the setting then in force.
- A view the model asks for of an image sent this turn is answered as
  already attached (ADR-0287 §SD2).

### SD4 — The Pixels setting governs

- **Offered above Metadata only**, where pixels can reach the model: not
  under Only to a local model with a model off this machine, and not
  above the host's cap (ADR-0287 §SD1). Where it is not offered, the tab
  says which setting keeps it back.
- **The gesture is the consent.** Each image sent is one disclosure row
  (ADR-0287 §SD6): shown, decided by the person, with the turn and no
  model call that asked. The rows are reported before the message is
  written. When the host does not record one, nothing is sent and the
  draft stays, with the reason.
- **Under Ask once per image and above, a send is that image's consent**:
  a later view of the same content does not ask.
- **An upload under This chat's captures asks once.** That level's
  consent is the grant of the captured windows, and no grant covers an
  upload. A view of an upload, or of a copy or crop of one, asks once per
  content, as under Ask once per image.
- **Pixels no longer needs Apps.** An upload needs no window, so the
  setting and `artefact_view_image` hold whenever Artefact is on.

### SD5 — Labels

- **The message that carries images is external content**, level 2,
  whatever the images' sources: a capture already is (ADR-0311 §SD6), and
  an upload is a file. The conversation reads as tainted from then on.
- **Adding alone does not taint.** What the model can read of an image
  that was added and not sent is a name the chat picked and its size.
- **Only to a local model** labels the request confined, as for a view
  (ADR-0287 §SD4).
- **Export is unchanged**
  ([ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md)):
  an upload is a file of the snapshot under its name, and the
  conversation's label covers it. The export records each image's source,
  so a resume brings an upload back as one. An image of an export that
  records none counts as a capture, as it did.

### SD6 — The transcript and its actions

- The person's message shows the names of what went with it, and their
  thumbnails while the set holds the bytes.
- **Edit** puts the message back in the composer with its images chosen
  again, those the set still holds.
- Rewind, branch and resume carry the placeholder, and the transcript's
  export the names, never the pixels (ADR-0287 §SD5).

### SD7 — Deferred, recorded

- **A dropped file and a pasted image.** Each needs a lane from the host
  to the app. Trigger: clipboard read (ADR-0317 §SD6).
- **An upload from the remote viewer and the browser tab.** The first
  needs a way for the page to hand over a file of the device; the second
  also a store that does not need sealed files.
- **A conversation without an artefact.** Trigger: a use that wants an
  image and no document.
- **GIF, WebP, and several files in one pick.** The dialog also takes no
  filter, so it lists every file and the refusal comes after the pick.
- **Downscaling before sending**, and **detecting whether the endpoint
  takes images** (ADR-0287 §SD7). A photo makes the first bind sooner
  than a window capture did.
- **A name of the person's choosing.** The model can copy an image to a
  name.
- **An operation that adds an image.** The dialog is the grant; an agent
  naming a file would go around it.
- **A level that shows the model every image of the set without asking**
  (ADR-0287 §SD7's fifth level), and **captures the person starts**
  (ADR-0284 §SD6).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| chat's manifest | the file dialog's read capability | the capability list's test |
| the artefact's set | JPEG entries; the source `upload`; names ending in `.jpg` | the store's header check, the crop, the model's image tools and their schema text, the export record and its restore; ADR-0284 §SD1, by a dated entry |
| Images tab | Add image; the option to send; the line saying why sending is not offered | the help book's artefact page |
| composer | what is chosen for the next message | the Edit action |
| model requests | the person's message with image parts; its placeholder afterwards | the resume's and the next request's strip of live pixels |
| Pixels setting | holds without Apps; an upload asks once under This chat's captures | ADR-0287 §SD1 and §SD2, by a dated entry; the Settings panel's tooltips |
| `agentDisclosure` | the source `upload`; a row no model call asked for | the record's description in `runtime/agent` |
| deferrals taken up | ADR-0282 §SD7 (images), ADR-0287 §SD7 (images the person attaches) | dated entries there |

## Alternatives

- **A message attachment with a store of its own** (Q1). It serves a
  conversation without an artefact, and repeats the sealed store, the
  budget and the panel for images the model could then not crop, embed
  or view again.
- **Attaching from a file in the composer, for uploads only** (Q2). One
  gesture fewer for the common case, and no way to show the model a
  screenshot it captured.
- **A send the setting does not gate** (Q3). The person asked for it, so
  it is consent enough; but a host that caps Pixels at none would then
  need a second rule to mean what it says.
- **The file as given** (Q5). The digest would be the file's, which a
  person could check against the file. It discloses what the person did
  not see when they chose it.
- **Everything as PNG** (Q5). No change to the set, its names or the
  export, at several times the bytes for a photo.
- **Removing metadata without decoding**, by dropping segments and
  chunks. Lossless for a JPEG, but turning the image upright needs the
  pixels anyway, and the set of segments to drop is a list to keep
  current.

## Consequences

### Positive

- A person can bring an image into a conversation and show any image of
  the set to the model, where before only the model could ask.
- No second store, budget or panel; revert, take-back, branch, export and
  resume treat an upload as they treat a screenshot.
- One gate for pixels: a host's cap and the local-only switch bind a
  person's send as they bind a view.
- The trail says which content the person sent to which endpoint, as it
  says for a view.
- A photo's location, time and device do not reach the model's endpoint
  or the lading store.

### Negative

- A conversation started without Artefact takes no image, and Artefact is
  fixed at the first message.
- At the default level nothing can be sent; the person raises Pixels
  first, which also lets the model ask for views.
- An image sent stays for one turn. A later question about it relies on
  the model viewing it again, which under an Ask level asks the person.
- A JPEG loses a generation of quality when added, and any image its
  colour profile.
- The set's digest is not the file's: nothing ties an upload back to the
  file it came from.
- A photo above the pixel ceiling is refused, and one below it may still
  be more than an endpoint takes; so may several images in one message.
  The turn then fails with the endpoint's error.
- The whole image is decoded when added, one decode at a time, bounded by
  the pixel ceiling.
- Text in an image reaches the model in the person's own message. The
  chat's line marks it as data, which a model may or may not respect.
- Only the desktop host has a working way in.

### Neutral

- A conversation whose set holds no upload and whose person sends nothing
  behaves as before.
- The names `upload-<n>` say nothing about what an image shows; the model
  learns that only from the pixels or from the person.

## Migration — Tier 1

Nothing to migrate. The change is additive: a set without uploads, and an
export written before this decision, read as they did.

## Verification plan — Tier 1

- **Lane.** The default `go test` of `apps/chat` and `runtime/agent`, and
  a scene on the headless host.
- **What would fail.**
  - Adding: a JPEG with a location and an orientation tag is held upright
    and without either; a file of another format, one past the pixel
    ceiling and one past the budget are refused and change nothing; the
    file's name is in no tool result, journal entry or export.
  - Sending, with a scripted model: the request carries the person's text
    and the image; the next turn's carries the placeholder; Edit chooses
    the images again.
  - The setting: nothing is offered at Metadata only, under the host's
    cap, or under the local switch with a model off this machine; a send
    the host cannot record sends nothing and keeps the draft; a send
    under Ask once per image spares the later view its question; a view
    of an upload under This chat's captures asks once.
  - Records: each image sent is one disclosure row with the source and
    the person as decider, joined to the message row of the call that
    carried it.
  - The scene: Add image through the dialog from a fixture, choose it,
    send, and the scripted model's request carries it.
- **Gap.** The desktop host's dialog, a real model reading an image, and
  what a remote endpoint does with a large photo.

## Status

Accepted 2026-11-20. Nothing here is implemented.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md) §SD1, §SD7 — the artefact's attachments, left empty; the upload deferred.
- [ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md) §SD1, §SD2, §SD4, §SD5 — the set, its revisions, the budget, the person's removal.
- [ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md) §SD1–§SD7 — the setting, the view, one turn then a placeholder, the disclosure record.
- [ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md) §SD2, §SD7 — the export and what a resume reads back.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD7 — the file dialog as the grant.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) §SD1, §SD6 — a file and a capture as external content.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD1 — the images a user entry carries, by digest.
- [ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md) §SD6 — clipboard read deferred.
