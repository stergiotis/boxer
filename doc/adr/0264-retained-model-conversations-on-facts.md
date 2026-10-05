---
type: adr
status: proposed
date: 2026-09-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0264: Retained model conversations — an opt-in bodies kind for `llm.*` on `boxer.facts`

## Context

[ADR-0254](./0254-model-inference-as-a-keelson-capability.md) makes every
model call a declared request answered by one host service, and records
each call (§SD4). The durable `llmCall` row carries counts and the verdict,
never text. Text exists in one place only: the in-process ring behind
`keelson('llm_calls')`, under `BOXER_LLM_KEEP_MESSAGES`, flattened into one
prompt string and lost with the process. That switch is a deployment's
debugging aid — prompts are composed from buffers, query results and
schema, and may carry confined content (§SD3) — so it is off by default.

§SD4 names the missing piece and why it was left out: text a deployment
keeps "belongs on a kind of its own that can be purged or masked without
touching the counts, and that kind is not built"; §SD7 defers it. It also
names the viewer: the [ADR-0239](./0239-play-chat-panel-and-chatview-widget.md)
chat pane "once bodies are kept".

The pressure now is an LLM chat whose storage layer is `boxer.facts`.
Every turn of such a chat is already an `llm.complete` request carrying
the whole message history, so the service sees the conversation in full.
What it lacks is permission to keep it, a way to tie calls into a
conversation, and a place to put the text. Building a separate chat
service instead would need an app-side write to `boxer.facts`, which no
capability grants — every facts writer today is a host service — and a
second place where sensitivity is decided.

## Design space (QOC)

**Question.** Where is the text of a model conversation kept, and who
decides that it is?

**Options.**

- **O1** — a chat service of its own (`chat.*`) that the app writes turns
  to, beside `llm.*`.
- **O2** — the deployment switch alone: `BOXER_LLM_KEEP_MESSAGES` extended
  to the durable table, every call's text kept.
- **O3** — the `llm` service keeps text when the deployment allows it *and*
  the app asks under a grant of its own.
- **O4** — O3 on a dedicated table ordered by conversation, with TTL and
  deletes (the [ADR-0223](./0223-watchbill-durable-work-on-facts.md)
  reasoning).

**Criteria.**

- **C1** — one policy point for what is kept, the §SD3 point.
- **C2** — the app that causes text to be kept is the one that declared it.
- **C3** — new surface: capabilities, services, tables.
- **C4** — read and purge on the storage: tail of one conversation,
  removal of a conversation.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | ++ |
| C2 | +  | −− | ++ | ++ |
| C3 | −− | ++ | +  | −  |
| C4 | −  | −  | −  | ++ |

O1 is killed on C1 and C3: two services, two sensitivity rules, and a
write capability to `boxer.facts` that does not exist. O2 is killed on C2:
mdedit's and play's calls would be kept because a chat wanted keeping.
O4 is the stronger storage and is not chosen, because `boxer.facts` as
the storage layer is the premise; what O4 would buy is recorded as the
cost in *Consequences*, and moving to it later changes the table, not the
capability.

## Decision

We will let the `llm` service keep conversation text on `boxer.facts`
when the deployment permits it and the calling app asks for it under a
grant of its own; the chat is then a view over what the capability
already audits.

### SD1 — Two opt-ins: a deployment ceiling and an app grant

`BOXER_LLM_RETAIN` replaces `BOXER_LLM_KEEP_MESSAGES` with three levels:

- `off` — counts only, the default.
- `ring` — today's `KEEP_MESSAGES=true`: text on the in-process rows of
  `keelson('llm_calls')` for every call, nothing durable.
- `durable` — `ring`, and text on `boxer.facts` for *retained* requests.

A retained request is one sent on `llm.retain.complete`. `llm.*` matches
one token, so no existing grant covers it; an app declares
`llm.RetainCaps(reason)` (`llm.retain.*`, publish, not sticky) and the
bus refuses the subject to an app that did not. The service does not
inspect grants: the broker's refusal is the app-side opt-in, and the
manifest and capinspector show that this app keeps conversations.

Text is kept durably only where both hold. A retained request under a
ceiling below `durable` is served, not refused, and its reply says it was
not kept (SD4).

### SD2 — Conversations and turns

A retained request carries a `Conversation` id, minted by the app, and a
`ParentCallId`, the call whose reply this turn continues (empty on a
conversation's first turn). Both land on the `llmCall` row as well, so
the counts and the text join on the call id and group on the
conversation.

Branching is a parent id, not an edit: resending from an earlier turn
with a changed message is a new call whose parent is that earlier turn.
Nothing is updated in place.

### SD3 — One row per message, only what is new

The text is a component, `LlmMessageBody` — content, reasoning, the
reply's tool calls with their arguments — added to a message's audit row.
That row, `llmMessage`, is written for every call whether or not anything
is kept ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD2): call id, position in the conversation, role,
size, digest, sensitivity, tool-call ids. Keeping a conversation adds the
body to the same row; it writes no row of its own. App and conversation are
the row's `Origin` and `Conversation` components. Images are kept as hash
and size, not bytes.
Shapes stay within the generated lane
([facts-bound record stores](../explanation/facts-bound-record-stores.md)):
scalars, `unit`, string arrays; content in a text section, the
`mddocContent` precedent of
[ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md).

A request resends the whole history, so keeping it per call grows with
the square of the conversation. The service keeps only the messages new
since the parent. It holds, per kept call, one hash per message of the
*logical* conversation — everything said so far — and a request that
repeats the parent's conversation is stored from the first message the
parent lacks, at ordinals that continue it.

A chat that outgrows the model's context sends a window instead: it
leaves out part of the conversation and says which, as a range of the
logical conversation (`OmitFrom`, `OmitTo`). The service checks the
request against the parent's conversation with that range taken out and
keeps what follows it, still at the logical ordinals; the call row
records the range, so a reader knows what the model saw on that call.
A sliding window, which leaves out more each turn, keeps only what is
new on every turn.

A request that matches neither — the app rewrote history, or declared an
omission that does not fit — is stored in full from ordinal 0 and
marked by keeping from 0 on a call that has a parent, so a reader never
reassembles a conversation the model did not see. A summary that
replaces the omitted range is not modelled: it would be stored as a new
message of the conversation, which it is not (SD7).

### SD4 — The reply says whether it was kept

The reply gains a retention verdict: kept, or not kept with the reason
(ceiling, no durable backend, write failed). The rows are written before
the reply is sent, one flush per turn, so "kept" means flushed. The
request's messages are kept on a refused or failed call too, with the
call row's reason, so a chat shows the turn that failed.

§SD4's audit rule — a failed write is logged and the call stays
answered — is kept for the counts. A chat learns of a lost turn from the
verdict instead of discovering the gap later.

### SD5 — Confined text is kept with its label

A retained confined request, served under §SD3's locality rule, is kept
with `sensitivity = confined` on every row. The label is what purge and
masking will select by (SD7); until then confined text sits on the table
like the rest.

### SD6 — The capability writes; reading is SQL, and ditchable

The family has no read verb. What the service keeps is a record, read
with SQL over `boxer.facts` through the
[leeway SQL read surface](../explanation/leeway-sql-read-surface.md) —
play, an applet, the ADR-0239 chat pane over a query shaped for its
columns (`ts`, `sender` from role, `body`, `reply_to`, `conversation`).
An app's conversation while it runs is the history it holds and resends;
the kept rows are not its state.

Keeping the read outside the capability is what makes one property
checkable: an app does not rely on the full messages. Because the text
is the only component of a message row on the text section, it can be
ditched with one statement — a mutation that empties that section on
message rows, all of them or one app's — and the app run again. If it
behaves the same, it did not rely on it; the message rows keep their
audit columns, and the counts and every other kind are untouched. The ditch is a verification device, not a
purge: it makes no claim about sensitivity or retention (SD7).

An app that does read the record back — a chat resuming a conversation
across sessions — is relying on it by choice, and the ditch shows that
too.

### SD7 — Deferred, recorded

- **Purge and masking.** Removal of a conversation or of everything with
  a label as a policy — who may, when, with what record. `boxer.facts`
  has no TTL (ADR-0184 §SD7); until a policy is decided, kept text is
  kept, and `durable` says so in its env description. The SD6 ditch is
  the mechanism a purge would likely use, and the columns it would
  select by — app, conversation, sensitivity — are on every row from the
  first write.
- **Locality of the store.** §SD3 sends confined content only to a local
  model, but the server holding `boxer.facts` may itself be remote.
  Whether confined text may be kept on a non-local store is open (Q1).
- **Non-call conversation events** (rename, delete-from-list). No model
  call carries them; a small write verb is the next decision. A title the
  model wrote is a model call: the chat sends it on the retained subject
  when the conversation is kept, naming the first turn, so the title is
  the reply body of that call (purpose `chat/title`). A title the person
  typed is still kept nowhere.
- **Streaming** stays deferred (ADR-0254 §SD7); a streamed turn would be
  kept once, when it completes.
- **Summaries in a window.** A message that stands in for an omitted
  range (compaction) is context the model saw, not a turn of the
  conversation. Keeping it needs a role or flag of its own; until then
  a window with a summary is a rewrite and is kept in full.
- **The chat app** is the first consumer and its own decision.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `llm.>` subject family | +`llm.retain.complete`; `ServiceCaps` subscribes `llm.retain.*` | capinspector's registry and `caps/llm.md` |
| `llm.RetainCaps` | new client grant, not sticky | manifests of consuming apps; cap-count pins |
| Wire forms | request +`Conversation`, `ParentCallId`, `OmitFrom`, `OmitTo`; reply +call id, retention verdict; `wireVersion` bumps | the client's `Request` / `Response` |
| Env registry (ADR-0009) | `BOXER_LLM_RETAIN` (off/ring/durable) added; `BOXER_LLM_KEEP_MESSAGES` retired | `doc/env-vars.md` regeneration; launch scripts setting the old name |
| Runtime vocabulary | `llmCall` cohort +conversation, parent, where its kept messages start, history hash, declared omission; new `llmMessage` cohort | the assignments golden; `llmfacts` regenerates |
| `boxer.facts` kinds | +`llmMessage`, append-shaped, externally provisioned | nothing in chstore; `VerifySchema` is the guard |
| `keelson('llm_calls')` | +conversation, parent columns | the introspection table docs |

## Alternatives

The QOC section carries the killed options. Two more were weighed:

- **A flag on `llm.complete` instead of a subject.** Rejected: the service
  would have to check the sender's grants itself, which it has no
  primitive for; the broker already refuses subjects.
- **The whole request history on every call.** Rejected: quadratic in the
  conversation, and a reader would have to pick the longest copy.
- **Read verbs scoped to the sender** (`llm.retain.history`,
  `.conversations`). Rejected: they would make the record an app's
  state through the capability, which is the reliance SD6 wants
  checkable, and they duplicate what SQL already reads.

## Consequences

### Positive

- A chat needs `llm.*` and `llm.retain.*` and nothing else: no write
  capability to `boxer.facts` and no second service.
- Whether an app relies on the kept text is a test, not a code review:
  ditch the rows and run it.
- What is kept is decided at the one point that already decides what is
  sent; the label that governs sending is on every kept row.
- The counts and the text join on the call id, so cost per conversation
  is a query.

### Negative

- `boxer.facts` is ordered by `ts`: one conversation's history is a scan
  over its time range, filtered by membership, not a key read. O4's
  `(conversation, ts)` order is what this gives up.
- Kept text is removable only by hand (the SD6 ditch) until SD7's purge
  policy exists. A deployment that sets `durable` accepts that.
- A lightweight delete on the shared table marks rows across every part
  in its time range; cheap for a check on a test deployment, not free on
  a large one.
- One flush per turn, synchronous before the reply: fine per message, and
  the reason a streamed turn is kept once rather than per token.

### Neutral

- `ring` behaves as `KEEP_MESSAGES=true` did; mdedit and play, which do not
  send retained requests, are unaffected at any level.

## Migration — Tier 1

- **Breaks.** `BOXER_LLM_KEEP_MESSAGES`.
- **Path.** `true` becomes `BOXER_LLM_RETAIN=ring`. The env registry has no
  retired-name mechanism, so a host still setting the old name gets no
  diagnostic; the old variable is ignored and text is not kept.
- **Regeneration.** The `llmfacts` store and DDL from the vocabulary; the
  env table.
- **Old shape.** Removed outright; it has no consumer outside the service.

## Verification plan — Tier 1

- **Lane: default `go test`.** The service over a fake client: a retained
  request under each ceiling keeps text durably only at `durable`; an app
  without `RetainCaps` is refused `llm.retain.complete` by the bus; a
  continued turn stores only its new messages; a declared window stores
  only its new messages at ordinals that continue the conversation, and
  an undeclared or mismatched one is stored in full and marked, as a
  rewritten prefix is; a confined turn's rows carry the label.
- **Lane: integration.** Rows land in a ClickHouse server's facts table; a
  SQL read reassembles a conversation with one branch and a sliding
  window over two turns; the SD6
  ditch removes every `llmMessage` row (and, scoped, one app's) while the
  `llmCall` rows and other kinds count the same before and after. What
  fails: the delete cannot select the kind through the membership
  columns, or it touches another kind.
- **Gap.** No live model here; the model's side is the fake client's.

## Status

Proposed — awaiting review by the code owner. Open:

- **Q1** — may confined text be kept when the facts server is not local
  (SD7)? Built as SD5 states: kept, with the label.

Built 2026-09-27, in the working tree: the subject and grant, the ceiling,
the request and reply fields, the declared omission, the `llmMessage` kind in the `llmfacts`
store, the call row's retained fields, `llmfacts.DitchMessagesSQL`, and
capinspector's entry. Both verification lanes pass, the integration one
over clickhouse-local, where the ditch selects the kind through its
membership and one app through the symbol value. Three points where the
build is narrower than the text:

- **The parent is looked up in-process.** The call row records the
  history hash, where its messages start and any declared omission, but
  the service reads its successor's parent — the per-message hashes —
  from a bounded in-process map rather than from the table. After a
  restart, or past that bound, the next turn keeps its whole request,
  marked as a rewritten prefix is.
- **A refused retained request** keeps its messages (SD4), but the reply
  is the refusal error and carries no verdict.
- **No diagnostic for the retired variable** (see *Migration*).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — §SD3 the policy point, §SD4 the record and the bodies kind it deferred, §SD7.
- [ADR-0239](./0239-play-chat-panel-and-chatview-widget.md) — the chat pane as the viewer.
- [ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md) — large text as an append-only facts kind.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) — why watchbill left `boxer.facts`; O4's reasoning.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) §SD7 — no TTL on facts kinds.
- [ADR-0145](./0145-sealed-app-data.md) — the sensitivity label SD5 carries.
- [facts-bound record stores](../explanation/facts-bound-record-stores.md) — how the kind is added.
