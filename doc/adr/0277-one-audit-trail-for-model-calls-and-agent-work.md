---
type: adr
status: accepted
date: 2026-10-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-04
---

# ADR-0277: One audit trail for model calls and agent work — shared components, always written

## Context

A conversation in the chat app leaves rows in several places: the model
service's call record ([ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD4),
the kept messages ([ADR-0264](./0264-retained-model-conversations-on-facts.md)),
the dispatcher's action record
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD9),
and whatever the operated app writes on its own account — play's query runs
([ADR-0115](./0115-query-observability-data-plane-strategy.md)), launches, the bus audit. The
questions these rows exist for — which calls made up a conversation, what a
task read, what a person authorised, which build answered — are joins across
them. A review on 2026-10-04 found the joins do not hold by key:

- The `llmCall`, `llmMessage` and `agentAction` kinds carry no run id and
  spell app and window on memberships of their own, although
  [ADR-0191](./0191-runtime-instance-attribution.md) §SD1 decided
  every runtime-written row carries `(run id, instance key)` on the shared
  ones. A window key is a per-run counter, so without the run it names
  nothing across runs, and a call cannot be traced to its build.
- The conversation id and the parent call are recorded only on the retained
  subject. A correlation id was made to depend on an opt-in about text.
- The action record names no conversation, no model call and no plan,
  though ADR-0269 §SD9 says it does. Its only bridge to the model call is
  the provider's tool-call id inside kept message text.
- That provider-minted id is also the dispatcher's idempotency key for the
  life of a task. A provider that repeats ids across responses would have a
  later call answered with an earlier call's outcome.
- Work an agent's call causes inside an app is not attributed to the task:
  play's query stamp carries no task, a window a task opens writes no launch
  row, and the on-behalf-of context carries no call id for a callee to stamp.
- Grants, widenings and the person's confirmations exist only in process
  memory.
- Per-event identifiers ride the `symbol` section, whose value column is
  dictionary-encoded for low cardinality.

The owner's direction (2026-10-04): an audit layer that is always written,
whether or not a chat is kept; keeping a chat adds to the same fact rather
than writing a second one; leeway components throughout; existing rows of
the affected kinds may be dropped to get the model and vocabulary right.

## Design space (QOC)

**Q1 — Where do the shared identifiers live?**

- *O1: repeat them as fields of each kind's DTO.* What exists. Each kind
  mints its own membership for the same thing, so a join names a different
  column per kind and a new kind can forget one. Rejected: it is how the
  findings above came about.
- *O2: small context components every row composes.* A component is
  satisfied by slots, so a row written by any lane — generated or
  hand-written — is readable as that component when it carries them
  ([ADR-0146](./0146-leeway-marshall-component-read-contract.md) §D5). One
  membership per identifier, one place a reader looks. **Chosen.**
- *O3: a side table of edges (row id → correlation ids).* Keeps the kinds
  untouched, but every question becomes a join through the edge table and
  the edges can lag the rows. Rejected.

**Q2 — How does keeping text relate to the audit row?**

- *O1: a separate kind for kept messages* (ADR-0264 §SD3). The audit then
  has no per-message row, so tool calls cannot be followed without the text.
- *O2: a message row always, text as a further component on the same row.*
  The audit knows every message's role, size and tool-call ids; keeping adds
  the body. **Chosen**, per the owner's direction.

**Q3 — One store or one per domain?**

- *O1: `llmfacts` and `agentfacts` stay separate and each declares the
  context components.* A generated store declares each membership once per
  package, so the context DTOs would be copied per store.
- *O2: one generated store over all trail components.* One entity type
  whose archetype says what a row is. **Chosen.**

## Decision

Model calls, their messages, agent actions and grant decisions are written
as one audit trail on `boxer.facts`: rows composed from shared context
components and one domain component each, by one generated store, on every
host that has a durable backend, independent of text retention.

### SD1 — Context components

Four components carry the identifiers a join needs. Each identifier has one
membership, whoever writes it.

| Component | Slots | Who vouches for it |
| --- | --- | --- |
| `Origin` | run, app, instance — on `runtimeRun`, `runtimeApp`, `runtimeLifecycleTileKey` | the host: the process's run and the bus envelope's sender (ADR-0191 §SD2), never a payload |
| `Conversation` | conversation id, turn id, round | the app that owns the conversation |
| `Delegation` | task, epoch, dispatcher call id | the dispatcher, through the on-behalf-of context |
| `Cause` | the model call whose reply asked for this, the provider's tool-call id, its index in that reply | the coordinator |

`Origin` is on every trail row. The others are present when they apply; an
absent component is a legal reading, not an error. The table's third column
is part of the contract: a reader weighing a row as evidence needs to know
that `Origin` cannot be claimed by an app and the other three are stated by
a party the host trusts to different degrees. The dispatcher does not check
`Cause` against the model service's record.

### SD2 — Domain components and archetypes

| Row | Components | Written |
| --- | --- | --- |
| model call | `Origin`, `LlmCall`, and `Conversation` / `Delegation` when they apply | when the call is answered, refused or fails |
| model message | `Origin`, `LlmMessage`, the call's `Conversation` / `Delegation`; `LlmMessageBody` when kept | request messages before the request leaves; the reply with the call row |
| agent action | `Origin` (the coordinator window), `Conversation`, `Delegation`, `Cause`, `AgentAction` | at the dispatcher's decision and at the final phase; once for a window the task opens |
| grant event | `Origin`, `Conversation`, `Delegation`, `AgentGrant` | at each request, decision, widening, confirmation, mode change, and at the task's end |
| egress fetch | `Origin`, `HttpFetch`, `Delegation` when agent-caused | when the fetch is answered or refused, flushed behind it |

`LlmCall` holds what ADR-0254 §SD4 lists, and in addition: the parent call,
the first ordinal of the messages this call wrote, the retention verdict, the
provider's response id, the model name the provider reported, a digest of the
tool definitions offered, and the token ceiling asked for. `LlmMessage`
holds call id, ordinal, role, size, a digest of the content, sensitivity
label, the tool-call id a tool message answers, and the ids and names of the
tool calls an assistant message issued. The digest is BLAKE3 over the exact
bytes sent: it answers "which calls carried this text" without the text, and
anyone holding a candidate text can test for it — accepted by the owner
(2026-10-04). `AgentAction` keeps its fields except those the context components
now carry; the window it addressed stays its own field, since `Origin` is the
actor. `AgentGrant` holds the event, the plan and its digest, entries,
launches, destinations, budget, deadline and who decided.

The generated store is `runtime/trail`, replacing `llm/llmfacts` and
`agent/agentfacts`. Its `Recorder` is the one writer: it stamps `Origin`,
composes each row's natural key and components, and owns the buffer and its
flushes. The model service, the dispatcher and the egress service hold the
host's recorder and call its verbs; none touches the store. The lane is the
one [facts-bound record stores](../explanation/facts-bound-record-stores.md)
describes.

### SD3 — Always written, and written ahead

The trail does not depend on `BOXER_LLM_RETAIN` or on the subject a request
arrived on. A host with a durable backend writes it for every call.

The messages a request adds are flushed before the request is sent to the
provider. A process that dies mid-call therefore leaves message rows without
a call row, which reads as "sent, outcome unknown" — the case that matters
when the question is what left the machine. The call row and the reply's
message row follow in one flush.

Only what is new since the parent call is written, by the mechanism of
ADR-0264 §SD3; a call with no parent writes all of its messages.

A write-ahead that fails — the store is down, or the host has no durable
backend — is decided by `BOXER_TRAIL_REQUIRED`: unset, the call proceeds,
the gap is logged and `keelson('llm_calls')` marks the call not durable; set,
the call is refused. Egress fetches are not written ahead: a screenful of map
tiles is dozens of fetches, and a flush before each would cost more than the
fetch. Their rows are flushed behind them, and under `BOXER_TRAIL_REQUIRED`
a fetch is refused only where the host has no durable trail at all.

### SD4 — Keeping text adds a component to the same row

A kept message is the audit row plus `LlmMessageBody`: content, reasoning,
and the tool calls' arguments. The two opt-ins of ADR-0264 §SD1 — the
deployment ceiling and the app's grant on the retained subject — decide
whether the body is added, and nothing else about the row.

`LlmMessageBody` is the only component of a message row on the `textArray`
section. Ditching kept text (ADR-0264 §SD6) becomes clearing that section's
columns on message rows instead of deleting rows, so the audit survives it by
construction.

### SD5 — Conversation, turn and parent travel on every request

`llm.complete` and the retained subject both accept conversation, turn, round
and parent call id, and both record them. The chat app mints a turn id per
message the person sends and passes it on every round of the turn and on the
title call. An app with no conversation sends none.

### SD6 — The idempotency key is the coordinator's, not the provider's

A coordinator keys a tool call by the model call id and the tool call's index
in that reply. The provider's tool-call id is recorded in `Cause` and on the
message row, and is never a key.

### SD7 — What an agent's call causes carries the task

- The on-behalf-of context gains the dispatcher's call id.
- Play's query stamp gains task, epoch and call id, and the query-run row
  carries them on the `Delegation` slots — hand-written by the capture, read
  back as the component.
- A window a task opens leaves an agent action row naming the window it
  opened, so which task opened a window is a join on `(run, instance)` with
  the window's lifecycle row. A refused or failed launch leaves its row too.
- A completion or a fetch made on behalf of a task records `Delegation` from
  the fields the request carries.

### SD8 — Run and app on the plain channel; a build id on the run row

`runtimeRun`, `runtimeApp` and `runtimeLaunchCaller` are written as
attribute values on the plain low-card-ref channel by every writer, as `boxer.persiststate` already does
(ADR-0191 §SD5). The hand-written kinds wrote them on the mixed channel with
the same bytes as value and parameter, which carried nothing the value does
not and kept those rows from being read as `Origin`. The hand-written lanes
stay hand-written where the generator refuses their shape; they satisfy
`Origin` by the slots they write.

The runtime-start row gains a build id: a digest of the running executable.
The VCS revision does not identify a build from a modified tree; the digest
does, and under reproducible builds
([ADR-0215](./0215-retire-mimalloc-reproducible-builds.md)) it is the same for
the same source. Rows reach it through the run.

### SD9 — Where an identifier rides, and what `ts` means

- An identifier minted per event or per conversation — call, message, turn,
  conversation, task, tool call, digests — rides `stringArray` as a unit
  value. An identifier of something long-lived and few — app, run, model,
  purpose, operation, role — rides `symbol`.
- `ts` is when the recorded thing began: for a call, when the request reached
  the service. The elapsed time is a field.
- A row's natural key is built from the identifiers that make it unique and
  its id is the hash of that key, as the kinds did before.

### SD10 — Deferred, recorded

- **A turn-outcome row written by the chat app.** A turn's outcome is
  derivable from its calls; an app-side facts write needs a capability no app
  has.
- **A digest over normalised text.** The message digest is over the exact
  bytes; a second digest over markdown-normalised content would match text
  that differs only in formatting. It needs a normaliser, and would be a
  further membership beside the first.
- **An index for correlation reads.** The table orders by row id, so a read
  by conversation or task scans. Whether that needs a skip index or a
  materialized view is a measurement, not taken here.
- **The other facts-bound stores** (watchbill presence, sysmetrics, vizeval,
  stevedore) adopting `Origin`.
- **A person or OS-user identity.** The principal stays "the person".
- **The client binary's build id** beside the host's.
- **Log rows naming task and call.** The per-window logger tags run and
  instance; handler loggers do not yet derive from the call.
- **Host verification of `Cause`.**

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| runtime vocabulary | context and new domain memberships added; the per-kind app, instance, conversation, task and actor memberships retired, their ordinals not reused |
| `llm.complete`, `llm.retain.complete` wire | conversation, turn, round, parent on both |
| `runtime.agent` call wire | key composed by the coordinator; cause fields |
| on-behalf-of context | call id |
| query stamp (`log_comment`) | task, epoch, call |
| `keelson('llm_calls')`, `keelson('agent_actions')` | columns follow the components; `keelson('agent_grants')` stays this run's grants, the trail holds their history |
| `keelson('http_calls')` | unchanged; each row also lands on the trail |
| `BOXER_LLM_RETAIN` | decides the body component only |
| `BOXER_TRAIL_REQUIRED` | new: refuse work whose trail row cannot be written first (SD3) |
| runtime-start row | a build id |

## Alternatives

- **Add the missing fields to the three kinds and keep them apart.** Smaller,
  and it leaves each kind with its own spelling of run, app and conversation —
  the state ADR-0191 was written to end.
- **OpenTelemetry traces as the correlation layer.** Trace and span ids would
  give causality, but the rows already live in a table the host queries with
  SQL, and a second store for the same events is a second thing to keep
  consistent. The GenAI field names are followed where one exists.
- **Two rows per model call, sent and answered.** Uniform with the action
  record's two rows, but the request's message rows already are the
  write-ahead evidence, and one call row keeps counts from needing a phase
  filter.

## Consequences

### Positive

- Every join in the Context section has a key:
  call ↔ run ↔ build through `Origin`; call ↔ conversation and turn through
  `Conversation`; action ↔ model call through `Cause`; action ↔ query run,
  launch, fetch and nested completion through `Delegation`.
- Tool calls can be followed without kept text.
- What a person authorised survives the process.
- A kind added later composes the same context components instead of
  inventing its own.

### Negative

- Rows of the retired kinds are dropped, and the hand-written kinds' rows
  written on the mixed channel stop being read by the moved readers; the
  owner has cleared dropping them.
- Every model call costs one more flush, before the request is sent.
- Message rows are written for every call, kept or not, so the trail grows
  with conversation length rather than call count.
- The readers of the hand-written kinds and the query-run capture change
  with the channel move, in one step with the writers.
- A correlation read scans until SD10's index question is measured.

### Neutral

- Conversation, turn and cause are stated by apps. The trail records who
  stated them; it does not make them true.

## Migration — Tier 1

1. The store, the context components and `LlmCall` / `LlmMessage` /
   `LlmMessageBody`; the model service writes ahead and always; the wire
   carries conversation, turn, round and parent; chat mints turns.
2. `AgentAction` on the store with `Cause` and the coordinator's key;
   `AgentGrant` events.
3. The on-behalf-of call id; play's stamp and the query-run capture; action
   rows for task-opened windows.
4. Run and app on the plain channel in the hand-written writers and their
   readers; the build id.
5. `HttpFetch` on the store.

Existing rows are not migrated. Rows of the retired memberships decode as
absent, and rows written with run and app on the mixed channel are not found
by the moved readers, so a deployment empties `boxer.facts` once when it
takes this change (`TRUNCATE TABLE boxer.facts`); `boxer.persiststate` is
untouched. SQL a person saved that reads run or app through the mixed channel
moves to the plain one. ADR-0264 and ADR-0265, both then proposed, are revised in place;
ADR-0191, ADR-0254, ADR-0262 and ADR-0269 take dated Updates when this ADR
is accepted.

## Verification plan — Tier 1

- A golden of the vocabulary and of each archetype's row.
- On `clickhouse local`: one scripted conversation with Apps on, Keep off,
  then SQL that walks conversation → turns → calls → tool calls → actions →
  query runs → run → build by key alone, with no time-range predicate.
- The same conversation with Keep on reads back identical audit columns, and
  after the ditch statement reads back as the Keep-off run did.
- A process killed between the write-ahead flush and the answer leaves
  message rows and no call row.
- A scripted model that repeats a tool-call id across replies gets two
  distinct outcomes.
- Component reads over a hand-written row (launch, query run) return
  `Origin`.

## Status

Accepted 2026-10-04, with all five migration steps built. What the
verification plan asks for holds on `clickhouse local`, and the hand-written
kinds' writers and readers against a ClickHouse server:

- a Keep-off call leaves its call row and message rows, written before the
  provider is asked, with the conversation, turn and round it named;
- a kept conversation reads back the same audit columns plus text, and after
  the ditch every row remains with its text gone;
- an action row carries all four context components, and a grant leaves its
  events;
- a model call, the action it asked for and the query run that action caused
  — the last hand-written by the capture — join by key alone, and the
  query-run row reads back as `Origin` and `Delegation`;
- the same provider tool-call id under two model calls is two dispatches.

On the headless host, the chat coordinator's scene over a scripted model
(`apps/chat/scenes`, the one that drives play) passes against a ClickHouse
server, and the rows that host wrote walk by key: every call of a turn
carries the turn and its round and continues its parent, every action row —
the opened window included — names the model call that asked for it, and the
grant leaves its approval and its end.

Not verified: a run of the desktop host, a real provider, and the kill
between the write-ahead flush and the answer (the write-ahead itself is
tested by reading the rows while the provider answers). Under test grants a
request is approved where it is made, so no `requested` row precedes the
approval. The vocabulary golden covers the ids;
there is no per-archetype row golden.

Known gaps against the text above: the pipeline benchmark's timeline fixture
still spells run and app on the mixed channel (it is a parse corpus, not a
reader); log rows name no task or call (SD10).

## Updates

### 2026-10-04 — what a real-provider session left out

A chat session against a real provider, read back from `boxer.facts`
alone, could not answer four questions the trail exists for. Each is now
a row or a field:

- **Which model call asked for a read.** `describe`, `help` and `list`
  wrote no action row, and the desktop verbs and captures wrote rows
  without a turn or a `Cause`. Each now carries both when the request
  names a model call; the coordinator's own reads name none and stay
  unrecorded. A grant's `requested` event carries the `Cause` of the
  request — the model's `request_access`, or the call held for widening.
- **Who ended a task.** The stop request carries `by` (`person` or
  `coordinator`), a reason and a `Cause`; the chat's Stop button and a new
  conversation say `person`, the model's `stop_task` says `coordinator`.
  A `person` that arrives through the coordinator is the coordinator's
  claim, weighed as §SD1 weighs `Cause`.
- **When the person intervened.** A change that pauses a task in a window
  writes a `paused` grant event (decided by `person` or another task, the
  window and resources in the reason) on the transition only; the turn that
  lifts it writes `resumed`.
- **What the model said each call was for.** `AgentAction` gains the call's
  title and the model's reason, bounded (60 and 200 runes, one line), on
  the dispatch row, in every retention mode. They are what the person was
  shown when the call acted, which is why `AgentGrant.Plan` is kept
  regardless of `BOXER_LLM_RETAIN`; this extends that reasoning to the call.
  Message text otherwise stays under §SD4.

§SD5's title call now names the conversation's first turn, and it is kept
with the conversation when the conversation is kept (ADR-0264).

A turn that runs out of rounds sends its last call a note the history does
not keep, so the next turn's request no longer continued its parent and
the whole conversation was written again — under `durable`, its text
twice. The next request now declares the note's slot left out (ADR-0264
§SD3's omission range), and only what is new is written.

Query runs reach the trail only while `queryrunsd` runs (ADR-0115). Play's
history pane reports whether the capture view is refreshing, and its
reader returns the task, call, epoch and window a run carries.

## References

- [ADR-0191](./0191-runtime-instance-attribution.md) — the `(run id, instance key)` decision this extends to the generated kinds.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD4 — the call record.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — retention, the message kind and the ditch this reshapes.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the chat app and its coordinator.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6, §SD9 — grants, the on-behalf-of context, the action record.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) §SD7 — the query stamp.
- [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) §SD5 — the egress call record.
- [ADR-0146](./0146-leeway-marshall-component-read-contract.md), [ADR-0183](./0183-leeway-component-consumer-simplification.md) — the component read contract and id regime.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md), [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) — generated stores.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md).
