---
type: adr
status: accepted
date: 2026-10-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-06
---

# ADR-0287: Pixels — a chat setting for which screenshots the model may see, and with whose consent

## Context

The chat's artefact keeps screenshots
([ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md)):
the model captures windows of its task, crops and copies them, and the
person sees them in the panel. The model sees only metadata — name, size,
source. Showing the pixels to a vision-capable model was deferred to a
decision of its own (ADR-0284 §SD6,
[ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)
§SD7, [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD5).

The client can already carry them: `openaichat.Message.Images` sends a
user turn as a text part followed by `image_url` parts, and the vizeval
judge sends PNGs that way.

What is undecided is the security question. A screenshot can hold more
than the operations of its windows expose — everything drawn, not only
what a query returns — and sending it to a model service moves that
content off the host. It is also untrusted input: text inside a picture
reaches the model as instructions might. People will want different
answers for different conversations, so this ADR makes the answer a
setting.

ADR-0281 also captures a window as SVG, which any model can read as text.
This ADR covers pixels only; SVG is deferred (SD7).

## Design space (QOC)

**Q1 — What the setting ranges over.** One ladder of levels from no pixels
to all of the chat's captures (chosen), or independent toggles per source
and per consent style. A ladder reads at a glance and every position has a
name, as the ceiling does
([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)
§SD3). **Chosen** by the owner, with four levels.

**Q2 — What one consent covers.** Each send (most control, tiring once the
model compares two images), each image by content hash (consent to a
content, not to a transmission), or the conversation. The ladder offers
all three as levels. **Chosen** by the owner.

**Q3 — Where locality enters.** A switch beside the ladder that confines
pixels to a local model at any level (chosen); a remote model lowering the
level by one; or nothing beyond ADR-0281's confined-capture rule. The
switch keeps *whose consent* and *where may it go* apart. **Chosen** by the
owner.

**Q4 — How long a sent image stays in the model's context.** For the turn
that asked for it, then a placeholder (chosen); or for the rest of the
conversation. The history is sent again with every request, so an image
that stays is disclosed again each round, and to any endpoint the
conversation is later pointed at; it also costs its tokens every round.
**Chosen** by the owner.

## Decision

### SD1 — The setting

**Pixels**, in the Settings panel (ADR-0280 §SD5), one of four levels:

| Level | The model may see | Consent |
| --- | --- | --- |
| **Metadata only** | no pixels; ADR-0284 as built | — |
| **Ask each time** | an image of the set, after the person allows that send | per send |
| **Ask once per image** | an image of the set, after the person allows its content once | per content hash |
| **This chat's captures** | any image whose source is a capture made in this conversation, or a copy or crop of one | the grant of the captured windows |

and a switch, **Only to a local model**, that applies to every level above
Metadata only.

- **The default is Metadata only.** Existing conversations and new ones
  behave as before until the person moves it.
- **Moving the setting binds at once**, as the ceiling does (ADR-0280
  §SD2): a view waiting for consent that the new level does not allow ends
  refused, one waiting under a level raised to This chat's captures goes
  through as the setting's decision, not the person's, and the next request
  is built under the new level. What was already sent cannot be recalled;
  the setting's tooltip says so.
- **Lowering below Ask once per image forgets the remembered consents.**
  Raising it again starts with none.
- **The chat enforces the level**, as it does the artefact's policy
  (ADR-0282 §SD3); the host never sees an artefact call. Locality is the
  host's (SD4).
- **The host may cap it.** `BOXER_CHAT_PIXELS_MAX` is the highest level a
  chat on the host may use, and `BOXER_CHAT_PIXELS_LOCAL_REQUIRED` makes
  Only to a local model the host's rule. The Settings panel offers nothing
  above the cap and shows a required switch as a statement, not a
  checkbox; the coordinator applies the cap to whatever the window sends.
  `BOXER_CHAT_PIXELS` and `BOXER_CHAT_PIXELS_LOCAL` seed a new window's
  setting, under the cap (ADR-0009).

### SD2 — The tool

`artefact_view_image(name)`, offered when Artefact and Apps are on, Pixels
is above Metadata only and the pixels can reach the model — not under Only
to a local model with a model off this machine. It is a read: it needs no
Edit and names no base revision.

- **Its result is metadata and a status** — shown, refused, declined —
  with the reason. The pixels travel in a separate user-role message the
  chat appends after the tool results, since chat-completions endpoints
  take images in user turns, not tool results.
- **That message says what it is**: a text part naming the image and its
  source, stating it was attached by the chat at the model's request and
  that its content is untrusted, then the image part. It is not the
  person's turn. The transcript shows the view's tool line, not the
  message.
- **The image is sent as the set holds it**, a PNG. Providers bill an
  image by its pixel dimensions, not its encoding, so the encoding is not
  the lever on cost or disclosure; downscaling is (SD7).
- A **purged** image, a name not in the set, and an image the level does
  not reach are refused with a reason, and nothing waits. A second view of
  an image already attached this turn is answered as such, without asking
  and without a record, since nothing more leaves the host.
- **The model is told what it may see when that is news**: the first time
  it may see pixels, whenever that moved since, and when it no longer may;
  a model that never could is told nothing more. The system prompt's
  screenshot part (ADR-0284 §SD3) names `artefact_view_image` as the one
  way it sees pixels.

### SD3 — Consent

Under the two Ask levels a view waits for the person, as an artefact
proposal does under Ask first (ADR-0284 §SD5), in the Artefact panel.

- **The panel shows the image that would be sent**, fitted to the panel,
  with the size it is sent at, its name and source, and the model endpoint
  it would go to, with **Allow** and **Decline**.
- **Under Ask once per image**, Allow records the content hash. A later
  view of that hash under any name does not ask; a crop, a new capture or
  any other derived image has a new hash and asks.
- **Declining** returns a declined result to the model; the turn goes on.
  Stopping the turn ends the wait as declined, recorded with the reason.
- **Reads that wait.** ADR-0280's *Changes* tooltip says reads never wait;
  it gains the exception for pixels under the Ask levels.

### SD4 — Locality

**Only to a local model** labels every request that carries pixels
`SensitivityConfined`. The host already sends a confined request only to a
loopback or trusted endpoint and refuses it elsewhere
([ADR-0254](./0254-model-inference-as-a-keelson-capability.md)), so the
switch adds no locality predicate of its own.

- Refusing the request would end the turn. The chat therefore checks,
  before attaching, whether the host admits confined content to its model
  — `llm.Description.Local`, which the Settings panel already shows — and
  returns a refused tool result if not; the label stays the enforcement.
- Independently of the switch, a confined capture already labels the whole
  conversation confined (ADR-0281 §SD6), so its pixels cannot reach a
  remote model after the endpoint changes.

### SD5 — One turn, then a placeholder

- The image message is part of the requests of the turn that asked for it
  — every round until the answer — so the model can look at it while it
  works.
- From the next turn on, the history carries a text placeholder in its
  place, naming what was shown and what let it through. Viewing it again is
  a new view, under the level then in force.
- **The host stores that next request in full**, as a rewrite (ADR-0264
  §SD3). A request declares one omitted range of the logical conversation,
  and a turn's views leave several; a placeholder in place of the image is
  then no worse than an omission. Keeping only what is new needs several
  ranges per request (SD7).
- Rewind, take-back and branch carry the placeholder, never the pixels.
- Export (ADR-0265 §SD4) writes the transcript, which holds the view's tool
  line and never the pixels.

### SD6 — Records and the ladder

- **Every view the setting decides is one `agentDisclosure` row**
  ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)),
  whatever its retention: the image's name, its BLAKE3 digest, the digest
  of the capture it descends from by copies and crops, how it came to be,
  its size, the level and switch in force, the decision — shown, declined,
  refused — and who took it: the person, a consent they gave that content
  earlier, a level that does not ask, or the chat refusing. Its context is
  the coordinator window, the conversation and turn, the model call that
  asked, and the task when one still holds the coordinator.
- **The coordinator reports, the host writes.** Only the host's services
  hold the trail's recorder, so the chat reports each view on
  `runtime.agent.disclose`, which only a registered coordinator may call.
  What it reports is the coordinator's account, as the conversation and
  turn of every row are the app's. A report the host refuses — from an app
  that is no coordinator, or a decision and decider that do not belong
  together — is a refused row of the actions file, as a refused call is.
- **A shown view is recorded before it is attached.** When the host does
  not record it — no agent service, a trail the host requires and cannot
  keep — the view is refused and nothing is sent. Declined and refused
  views are reported as they happen; losing one of those loses no pixels.
- **The rows join.** A disclosure's digest is the one the `llmMessage` row
  of the call that carried the pixels names; its root digest is an
  `agentCapture` row's digest, which names the task's grant, the windows
  and the capture's decision. The message's own text also says what let
  each image through, kept with the conversation under Keep.
- **Pixels move the scale within its band, not up a level** (ADR-0280
  §SD3). Viewing a granted window's pixels is a read of a window the task
  already holds; what it changes is how much of it leaves the host. The
  three upper levels move the position up in that order, and the local
  switch moves it back down.

### SD7 — Deferred, recorded

- **SVG captures.** ADR-0281 exports one window as SVG. It would reach a
  model without vision, hold the window's text and numbers exactly, and is
  the only capture the browser tab allows. It is deferred, not rejected:
  it needs SVG entries in the set (ADR-0284 names PNG only), a consent view
  for markup — the tree has no SVG rasterizer — and a size ceiling, since
  an SVG can be far larger than the PNG of the same window. When it comes,
  this setting should govern it too: an SVG discloses at least what a PNG
  does, so a level per format would protect no one. The model, not the
  chat, would pick the format, since it knows whether it needs to read
  text or see a layout.
- **Any image in the chat** — images the person attaches, and captures the
  person starts (ADR-0284 §SD6) — as a fifth level, once those sources
  exist.
- **Detecting whether the endpoint takes images.** A text-only endpoint
  rejects the request; the turn fails with its error.
- **Downscaling before sending**, to bound tokens and what is disclosed; a
  ceiling on the long edge would be an environment variable
  ([ADR-0009](./0009-environment-variable-registry.md)).
- **Labels per image.** The set's entries do not record their capture's
  label; the conversation's label covers it (SD4).
- **A capture tool that returns its pixels**, saving the model one call
  under This chat's captures.
- **Several omitted ranges per request** (ADR-0264 §SD3), so the turn after
  one that showed pixels is kept as what is new rather than in full.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| Settings panel | Pixels (four levels) and Only to a local model, with tooltips; the *Changes* tooltip's exception |
| chat tools | `artefact_view_image` |
| model requests | an image-bearing user message for the turn; placeholders after it; `SensitivityConfined` under the switch |
| Artefact panel | the consent prompt: the image, its size, source and endpoint, Allow and Decline |
| `agent` | `PixelsE` and `Ceiling.ScoreBeside`: the scale's position with what lies beside a ceiling; `runtime.agent.disclose`, coordinators only |
| trail | a new kind, `agentDisclosure`, and its memberships in the runtime vocabulary |
| scale | Pixels and the switch as position within the band |
| environment | `BOXER_CHAT_PIXELS_MAX` and `BOXER_CHAT_PIXELS_LOCAL_REQUIRED`, the host's cap; `BOXER_CHAT_PIXELS` and `BOXER_CHAT_PIXELS_LOCAL`, seeds (ADR-0009) |

## Alternatives

- **Showing pixels whenever a capture is granted, with no setting.** The
  grant decides which windows the model works in, not whether their
  rendering leaves the host; folding the two together would make every
  Apps conversation a disclosure of pixels.
- **Consent per window rather than per image.** It matches the grant, but
  the person would consent to a window without seeing what the capture
  holds at that moment — the frame may have changed since the grant.
- **Images in the tool result.** Fewer messages, but chat-completions
  endpoints do not take images in tool results, and the client does not
  send them there.
- **Images kept for the conversation.** Simpler history, at the price of
  re-disclosing every image on every request and paying its tokens each
  round (Q4).
- **A simpler encoding than PNG, such as QOI.** Model endpoints accept
  PNG, JPEG, GIF and WebP, so a QOI image would be transcoded to PNG at
  every send. Providers bill by pixel dimensions, so the encoding does not
  change the cost; the capture encodes once, so its speed is not on the
  turn's critical path; and PNG's filters suit the flat colour of UI
  captures. A lossy encoding for the request alone (WebP, JPEG) is the
  answer if an endpoint's per-image size limit binds, at the cost of
  blurred text.

## Consequences

### Positive

- The person chooses, per conversation, how far pixels go and who consents,
  and can see each image before it is sent under the Ask levels.
- Locality reuses the host's rule; no second predicate can disagree with
  it.
- The trail answers, without Keep, which content reached which model call,
  from which capture, under which level and on whose decision.
- A host can bound what any chat on it may disclose.
- An image leaves the host with the rounds of the turn that viewed it, not
  with every later request.

### Negative

- A view under an Ask level stalls the turn until the person answers.
- A shown view waits for the host to buffer its row, and is refused when
  the host cannot; a chat that is not a registered coordinator shows none.
- The disclosure row is the coordinator's account: a coordinator that lies
  about the decision is not caught by it. The `llmMessage` rows, which the
  host writes from the request itself, are what a disclosure is checked
  against.
- The model can no longer see an image in a later turn without viewing it
  again — and, under Ask each time, asking again.
- Text inside a screenshot reaches the model in a user-role message; the
  preamble marks it untrusted, which a model may or may not respect.
- A model without vision gains nothing until SVG is decided.
- The turn after one that showed pixels is stored on the trail in full,
  repeating the rows of the messages before it (SD5).
- One more tool schema in every request with Artefact on and Pixels above
  Metadata only.

### Neutral

- A conversation left at Metadata only is unchanged.

## Migration — Tier 1

None: the default is Metadata only, the behaviour of ADR-0284.

## Verification plan — Tier 1

- Each level with a scripted model: Metadata only offers no view tool; Ask
  each time asks for every view; Ask once per image asks once per hash and
  again for a crop; This chat's captures does not ask for captures and
  their derivatives.
- Lowering the level ends a waiting view refused and forgets remembered
  consents; raising it does not restore them.
- The local switch: a request carrying pixels is labelled confined; with a
  non-local endpoint the view is refused before attaching and the turn goes
  on.
- Retention: the image is in every request of its turn and replaced by the
  placeholder in the next; rewind carries the placeholder, export no pixels.
- The host refuses a request carrying pixels under the switch to a model
  off this machine, even when the chat believes it local.
- Every view is reported — shown with who let it through, declined,
  refused — a crop names its capture's digest, and a view the host cannot
  record is not shown. Over clickhouse-local, a report lands as one row
  joined to the task and the model call that asked; one from an app that is
  no coordinator, or with a decision and decider that do not belong
  together, is refused, leaves no disclosure row and is a refused row of the
  actions file.
- The host's cap clamps the level and forces the switch in the coordinator
  and in the panel.
- A scene on the headless host: capture, view under Ask each time, Allow in
  the panel, the next request carries the image.

## Status

Accepted 2026-10-06, built. The verification plan holds in the tests of
`apps/chat` and `runtime/agent` and in the scenes
[chat-artefact-pixels](../../apps/chat/scenes/chat-artefact-pixels.scene.md)
and [chat-pixels-host-cap](../../apps/chat/scenes/chat-pixels-host-cap.scene.md).
Not verified: the desktop host, a real vision-capable model reading the
images, and a disclosure row read back from a host's durable trail outside
a test.

## Updates

None.

## References

- [ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md) §SD3, §SD5, §SD6 — the screenshots, their tools, showing them deferred.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) §SD5, §SD6, §SD7 — PNG and SVG from one replay; labels and taint of a capture; the chat's capture tool deferred.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) §SD2, §SD3, §SD5 — the ceiling, the ladder, the Settings panel.
- [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md) §SD3 — the chat enforces the artefact's policy.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — sensitivity and the locality rule.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD4, §SD5 — export; the coordinator; a capture tool deferred.
- [ADR-0009](./0009-environment-variable-registry.md) — environment variables.
