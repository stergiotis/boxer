---
type: adr
status: proposed
date: 2026-09-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0265: A chat app over retained model calls — session-only, and a coordinator of apps

## Context

[ADR-0264](./0264-retained-model-conversations-on-facts.md) lets an app
ask the host to keep its model conversations on `boxer.facts`, and gives
it no verb to read them back: the kept text is a record, read with SQL,
and removable by one statement to check that no app relies on it
(§SD6). Nothing sends retained requests yet.

What chat products offer, and which of it touches the storage, is
surveyed in
[llm-chat-app-requirements-survey](../adr-background-work/llm-chat-app-requirements-survey.md);
the verdicts below follow its v1/defer split.

The parts a chat needs exist separately: `llm.complete` takes a
multi-turn history and returns one answer
([ADR-0254](./0254-model-inference-as-a-keelson-capability.md));
`widgets/chatview`
([ADR-0239](./0239-play-chat-panel-and-chatview-widget.md)) draws a
transcript from a model rebuilt every frame and lets the host draw a
message body itself; `widgets/markdown` renders one; `bgjob` and
`jobprogress` run a request off the render goroutine with a cancel.
What does not exist is a composer, a turn loop, and an app that holds
the two grants.

## Decision

We will build a standalone app, named chat, that holds a conversation
in memory for the length of its window session, sends its turns on
`llm.retain.complete` unless the user turned keeping off for that
conversation, and reads nothing back. With Apps on, a turn is a tool loop
over the host's `runtime.agent` services, and the app is the conversation's
coordinator under ADR-0269 (SD6).

### SD1 — A standalone app, not a play pane

The app declares `llm.ClientCaps` and `llm.RetainCaps` and nothing else.
Play stays what it is for conversations: the SQL viewer of what was
kept, through its Chat pane over a query on the `llmMessage` kind.
Putting the chat in play would give play the retain grant and a second
reason to exist beside running SQL; the manifest would no longer say
which of play's gestures keeps text.

### SD2 — The conversation is the app's, for one session

The history the app resends each turn is its state; a new window starts
an empty conversation with a fresh id. The app never reads the
`llmMessage` rows, so it passes the ADR-0264 ditch check by
construction — ditch every row and it behaves the same. Past
conversations are read in play. Resuming one in the app is the
deferred step, and would make the reliance deliberate (SD5).

### SD3 — The turn loop

A turn appends the user's message to the history and sends the whole
history with the conversation id and the previous reply's call id as
parent, as a background job. Its reply is appended as the assistant
message the next turn echoes, so the service's prefix check (ADR-0264
§SD3) holds and each turn keeps only what is new. One turn runs at a
time; the composer is disabled while it does. Cancel ends the wait and
stops the provider call (`llm.cancel`, ADR-0254's 2026-09-27 update); a
kept turn keeps its messages with the call's error, as a refused one
does (ADR-0264 §SD4). A failed, refused or cancelled turn stays in the
transcript as a failed bubble with the reason, and the next turn
continues from the last successful reply.

The whole answer arrives at once. Streaming is deferred with ADR-0254
§SD7; until it exists a pending turn is a placeholder bubble with a
spinner and the elapsed time.

The app never shortens the history it sends. A conversation that
outgrows the model's context fails with the provider's error, shown on
the failed bubble with New conversation as the way on. The capability
already accepts a declared window (ADR-0264 §SD3), so a later version
can send one without changing what is stored; choosing the window —
how much to drop, whether to keep the system message — is a decision of
its own (SD5).

### SD4 — What the app shows

- **The transcript** is chatview over the in-memory history, every
  message body — the user's as well as the model's — rendered as
  markdown.
- **The composer** is a multi-line text input highlighted as markdown
  with mdedit's lexer (which colours the text's own bytes, where the
  canonicalising highlighter would colour a rewrite), and a Send button;
  Ctrl+Enter sends, gated the way play's SQL editor gates Run: the chord
  is read for the whole process and acted on only by the instance whose
  window is focused (`app.WindowFocusI`), so two open chats do not both
  send. A focused text input does not compete for the chord.
- **The model and the host** from `llm.describe`, beside the composer,
  as ADR-0254 §SD1 asks; with no model configured the composer is
  replaced by the reason.
- **Whether a turn was kept.** A reply whose verdict is not kept shows
  the reason once per conversation, so a deployment below `durable` is
  visible rather than silent.
- **Keep this conversation**, a toggle beside New conversation, on by
  default. Off, the conversation's turns go on `llm.complete` and only
  the counts are recorded — the one "nothing kept" choice a user has,
  the temporary chat of the products surveyed. It is fixed at the first
  send, so a conversation is kept whole or not at all.
- **The context used**: the last call's input and output tokens, the
  only warning before a conversation outgrows the model (SD3). It is a
  count without a ceiling: `llm.describe` reports the completion limit,
  not the model's context size.
- **New conversation** clears the transcript and mints a new id.
- **Where past conversations are.** The app says, once, that a closed
  conversation is read in play, since a user expects history to survive
  the window and this one does not.

### SD5 — Deferred, recorded

- **Resume and a conversation list**: a host table over the
  `llmMessage` kind, read under a `keelson.query` grant
  ([ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md))
  and scoped to the caller — the `app_logs` shape.
- **Tools beyond the coordinator's** (SD6): a capture the model views as
  an image, the operations of apps that serve no catalog.
- **Streaming**, **titles** (ADR-0264 §SD7), **edit-and-resend** and
  **regenerate** as branches — a call with an earlier parent, which the
  service already keeps.
- **A context window** (SD3): what to drop when the conversation
  outgrows the model, sent as a declared omission.
- **A system prompt** of the person's. The only one is the coordinator's
  (SD6), fixed before the first send of a conversation with Apps on, since
  changing it later rewrites ordinal 0 and costs a fully kept turn
  (ADR-0264 §SD3).
- **Deleting a conversation.** Every hosted product surveyed offers it;
  here kept text stays until removed by hand (ADR-0264 §SD6/§SD7). The
  delete is its own decision, taken with ADR-0264's purge policy.
- **Branching into a new conversation**: a parent from another
  conversation. Decided with resume.
- **Sensitivity**: typed text is `ordinary`. A chat that pastes query
  results would have to carry their label, and this one has no way to
  paste them.

### SD6 — The coordinator (ADR-0269)

With Apps on, the model may work in windows the person shares, and only
through the host:

- **Registration.** The person registers the app as a coordinator
  (`BOXER_AGENT_COORDINATORS`); the manifest's `runtime.agent` grant alone
  lets it ask for nothing.
- **Fixed tools.** `request_access`, `list_windows`, `describe_app`,
  `call_operation`, `open_window` and `stop_task`. Operation schemas load on demand
  through `describe_app`; a call's key is the model's tool-call id.
- **A turn.** Before the first model call the app asks the host for the
  changes others made since the previous turn (`runtime.agent.turn`) and
  puts them before the person's message; then it alternates model calls
  and tool calls, at most 24 rounds, and the transcript shows each tool
  call as a system line — also when the turn stops without an answer, which,
  unanswered, is not resent.
- **What the model reads.** Content an app marks untrusted, window titles,
  and every capture arrive between `<<untrusted …>>` delimiters with their
  source, and the fixed system prompt says such content is data. A
  confined result the host would not hand a remote model arrives as a data
  handle. Every model call declares the highest label the conversation
  holds.
- **One task per conversation.** The grant names the conversation; a new
  conversation stops the task, and the app's row shows the task, whether it
  read untrusted content and whether it holds confined content, with Stop.

## Alternatives

- **A play pane** (SD1). Rejected for the grant it would add to play.
- **Resume in v1** (SD2). Rejected for now: it needs a read surface
  ADR-0264 chose not to add to the capability, and a session-only chat
  shows the storage working without depending on it.
- **Tools in v1.** Rejected for now: the loop is play's, and lifting it
  is its own change.

## Consequences

### Positive

- The first consumer of ADR-0264 exercises the whole write path, and
  play already reads what it writes.
- The app holds two grants and no read access, which is easy to review.

### Negative

- Closing the window ends the conversation in the app; it survives only
  as rows to read in play.
- Without streaming, a long answer from a local model is a long wait
  behind a spinner.
- A long conversation ends in an error rather than degrading, until a
  window exists.
- A cancelled turn is kept like a failed one and has no successor. The
  call row's error says it was cancelled, which is what tells it from a
  regenerated answer the user did not follow; a reader that looks only
  at the message rows cannot.

### Neutral

- Nothing in the host changes; the app is a consumer.

## Verification plan — Tier 1

- **Lane: default `go test`.** The turn loop against the llm service
  over a fake provider and an in-process bus: a second turn names the
  first reply's call id as parent and resends it verbatim; a failed
  turn is not a parent; New conversation mints a new id; a conversation
  with Keep off sends nothing on `llm.retain.*`.
- **Lane: scene.** A headless scene that types, sends and captures the
  transcript needs a model endpoint. None exists where this was written;
  a local model or a stub OpenAI-compatible server is the prerequisite.
- **What would fail.** A reply appended differently from how it came
  back makes every turn keep its whole history; the fake-provider test
  pins it by checking `RetainedFrom` on the second call.

## Status

Proposed — awaiting review by the code owner. Revised in place on
2026-10-01 for ADR-0269's M5: the coordinator of SD6, built and tested
against a scripted model over the host's services.

Built 2026-09-27, in the working tree, as the chat app beside the other
apps. The default lane passes: the turn loop against the llm service over
an in-process bus, and — over clickhouse-local with the ceiling durable —
a second turn keeping only its new messages. A headless run against a
stub OpenAI-compatible endpoint on loopback sent a turn with Ctrl+Enter,
showed the pending bubble and Cancel, drew the markdown reply, the
context count and the not-kept reason under `ring`; it is the first run
of the ADR-0254 capability against an endpoint. It is not a maintained
scene, since the scene runner has no model stub to start. Two additions
to the text above:

- `BOXER_CHAT_DRAFT` seeds the composer, because the driver cannot focus
  an empty unnamed text input — play's `BOXER_PLAY_SQL` shape.
- The Keep badge shows what the host did: a keep the host declined reads
  "not kept", beside the reason, and one with no verdict yet "keep asked".
- While a turn runs, Cancel takes Send's place — a button cannot be
  disabled here.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — what the app sends and why it reads nothing back.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the contract the coordinator of SD6 drives apps under.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the capability, its timeout, and `llm.cancel`.
- [ADR-0239](./0239-play-chat-panel-and-chatview-widget.md) — the transcript widget and play's pane as the reader.
- [LLM chat app requirements survey](../adr-background-work/llm-chat-app-requirements-survey.md) — the requirements, and which touch the storage model.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — the grant a resume would read under.
