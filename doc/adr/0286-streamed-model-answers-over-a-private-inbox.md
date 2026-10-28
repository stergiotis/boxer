---
type: adr
status: accepted
date: 2026-10-05
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-28
---

> **Status: accepted.** Reviewed by the code owner on 2026-10-28.

# ADR-0286: Streamed model answers — advisory deltas to a private inbox, the reply unchanged

## Context

`llm.complete` ([ADR-0254](./0254-model-inference-as-a-keelson-capability.md)) is one bus
request and one reply. A long answer, or a long reasoning before a short
one, shows nothing until the whole completion is back: the chat's waiting
bubble ([ADR-0265](./0265-chat-app-over-retained-model-calls.md)) shows the
turn's trail and a clock, and the model call itself reads as "waiting for
the answer" for as long as it takes — tens of seconds for a reasoning model,
and up to `BOXER_LLM_TIMEOUT`. ADR-0254 §SD1 deferred a streaming verb to
its first consumer; the chat is that consumer.

The constraints a stream has to keep:

- **One reply stays the contract.** The reply carries the call id, the
  retention verdict, the tokens, the finish reason and the tool calls; the
  call record and the trail ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)) are written
  from it. A consumer that never looks at a stream must get exactly what it
  gets now.
- **The sensitivity wall** (ADR-0254 §SD3) is checked before the provider
  is called. Deltas come from that call, so they are inside the wall; they
  must not reach anyone the reply would not.
- **The bus.** `app.BusI` has publish, subscribe and request. A request's
  reply inbox is allocated inside `Request` and is invisible to the caller,
  and an inbox subscription bypasses the capability check because no app
  can name one. NATS has inboxes natively.

## Design space (QOC)

**Q1 — Where the deltas travel.**

- *A private inbox the requester subscribes to* (chosen): the bus client
  can subscribe to a fresh inbox, the request names it, and the service
  publishes deltas there. Private for the same reason a reply is; no new
  capability.
- *A subject family, `llm.delta.<key>`*: no bus change, but a subscriber
  holding `llm.delta.>` reads every app's answers as they are written —
  text across apps, past the wall. Rejected.
- *Polling, `llm.peek` with the call's cancel key*, scoped to the sender as
  `llm.cancel` is: no bus change, and coalescing comes free, but every
  streaming call costs a request per tick and the latency is the tick.
  Rejected once the inbox needed no change to `BusI` itself (SD3).

**Q2 — What a delta is.** Byte offsets with the new text (chosen),
separately for content and reasoning, plus the names of tool calls as
they start; or whole snapshots. Offsets let a consumer detect a gap and ask
for nothing: a gap means wait for the reply, since deltas are advisory.

**Q3 — How often.** Coalesced in the service (chosen): at most one delta
per 100 ms, or sooner once 2 KB of text wait, so the bus carries a few
messages a second whatever the provider's chunking.

**Q4 — The timeout.** A streamed call could be bounded by the time between
chunks rather than the whole call, so a long answer that keeps arriving is
not cut off at `BOXER_LLM_TIMEOUT`. Not taken: the whole-call bound stays,
for streamed and plain calls alike (SD2); the idle bound is deferred (SD5).

## Decision

### SD1 — The provider side

`openaichat` gains `CompleteStream(ctx, req, onDelta)` behind an optional
`StreamingClientI`: the same request with `stream: true` and
`stream_options.include_usage`, read as server-sent events. Content and
reasoning deltas go to `onDelta` as they arrive; tool call fragments are
accumulated by index and the call's name reported when it first appears.
The result is the same `CompletionResponse` as `Complete`'s, assembled from
the stream, so everything downstream of it is shared. An endpoint that
refuses `stream` is called again without it, once, and the caller gets no
deltas.

### SD2 — The service and the wire

A request may name a `StreamInbox`, which must be an inbox subject. With
one, and a provider that implements `StreamingClientI`, the service calls
`CompleteStream` and publishes coalesced deltas to the inbox:
`{v, call_key, seq, content_offset, content, reasoning_offset, reasoning, tools}`,
`call_key` being the request's cancel key. Otherwise nothing changes. The
stream is closed before the reply is sent, so the reply comes after the
last delta, and the reply is the only thing a consumer may treat as the
answer: no terminal delta exists, and a stream that stops is read as "wait
for the reply" — the runstream rule that absence is the safe reading
([runstream](../../public/keelson/runtime/runstream/runstream.go)). The
wall, the admission, the retention and the call record are the reply's,
unchanged: a refused call never reaches the provider and publishes
nothing. A cancelled or expired call's waiting text is dropped, and its
partial answer is not kept. The call is bounded by `BOXER_LLM_TIMEOUT` as
a plain one is.

### SD3 — The bus

The inbox is an optional capability of a bus client, `app.BusInboxI`,
beside `BusCloserI` and `BusInstanceI`:
`SubscribeInbox(handler) (inbox string, unsubscribe func(), err error)` —
an inbox subject unique to the client, subscribed without a capability
check as reply inboxes are, and released by the client's Close. In-proc it
reuses the reply-inbox allocator; on NATS it is `NewInbox` and a
subscription. `BusI` itself is unchanged, so the noop bus and the test
fakes carry nothing; a client whose bus lacks it sends no `StreamInbox`.

### SD4 — The client and the chat

`llm.Client.CompleteStreaming(ctx, req, onDelta)` subscribes an inbox,
sends the request naming it, and unsubscribes when the reply arrives or the
context ends. The bus handler only queues; a goroutine of the client's
hands deltas to `onDelta`, so a slow consumer does not hold the publisher
(on the in-process bus, the service's own goroutine). `onDelta` hears
contiguous text only — after a gap, nothing more — and is not called after
`CompleteStreaming` returns; a cancelled call's queued deltas are dropped.

The chat's tool loop streams its model calls. The running model step grows
with the deltas and its line says what the model is doing — reasoning,
writing the answer, asking for named tools — with the latest words of the
reasoning under it while no answer has started; the waiting bubble shows
the latest words of the answer under the trail, as plain text. The step
and the landed turn are drawn from the reply, as before. Under Ask first
both the refused call and the approved retry go through the stream.

The scripted model streams too: a reply's `streamMs` writes its content
word by word over that long, and a tool reply names its tool, so a scene
can watch an answer grow.

### SD5 — Deferred, recorded

- Streaming for other consumers: the chat's compaction and title calls,
  mdedit transforms, play's Model panel.
- An idle bound between chunks with a longer ceiling on the whole call
  (Q4).
- A cancelled turn's partial answer kept or shown in the transcript.
- The live answer rendered as markdown, and in a scroll area that follows
  the text, which needs a stick-to-bottom scroll in the bindings; the
  bubble shows a bounded tail instead.
- Streaming through the scripted model beyond what a scene needs.

## Alternatives

- **Deltas on the task progress subjects.** The bgjob/task progress plane
  carries numbers and a note, is read by any observer, and would put
  answer text where progress was promised.
- **Streaming the whole reply as runstream frames, no separate reply.**
  Every consumer would have to collect frames to get what one reply gives
  it now, and the call record would be written from a collector rather
  than from one message.
- **`SubscribeInbox` as a `BusI` method**, as first proposed: every
  implementation and fake would carry it to return an error. An optional
  interface is the shape the bus already uses for what only some clients
  offer.

## Consequences

### Positive

- A long answer is readable as it is written, and a stalled call is
  distinguishable from a slow one.
- Consumers that do not stream are untouched, on the wire and on the bus.

### Negative

- The SSE parser is new code against several providers' dialects of the
  stream (reasoning fields, usage chunks, tool-call fragments).
- A delta and the reply can disagree when the provider's stream and its
  final usage do; the reply wins, and the step is redrawn from it.
- A long answer can still be cut off at `BOXER_LLM_TIMEOUT` while it is
  visibly arriving.

### Neutral

- The bytes on the bus grow by the answer's size once more, coalesced.
- Delivery on the in-process bus is synchronous, so the service's publish
  costs one decode and one queue append per delta on its goroutine.

## Migration — Tier 1

None: the stream is opt-in per request.

## Verification plan — Tier 1

- `openaichat`: a recorded SSE stream (content, reasoning, two tool calls in
  fragments, a usage chunk) assembles into the response `Complete` gives
  for the same exchange; an endpoint that rejects `stream` is retried once
  without it.
- Service: deltas arrive in order with contiguous offsets, coalesced; none
  after the reply; a refused call publishes none; a cancel stops them.
- Bus: an inbox from `SubscribeInbox` receives what is published to it and,
  in-process, no other client can subscribe to it by name. On NATS that is
  the server's authorization to enforce; the NATS case checks delivery
  only, and runs where a `nats-server` binary is installed.
- Chat: the running step grows with the deltas and the landed step is the
  reply's; the scene [chat-stream](../../apps/chat/scenes/chat-stream.scene.md),
  against a scripted model that streams, shows the answer growing in the
  waiting bubble.

## Status

Proposed 2026-10-05. SD1 built 2026-10-25; SD2–SD4 built and the decision
accepted 2026-10-28, with Q4 and the cancelled turn's partial answer
decided for the light cut. Changes from here are dated entries under
Updates.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-28 — accepted with SD1–SD4 built

Accepted after the service, the bus inbox and the chat were built against
the proposal. Changed from the proposal before acceptance: the inbox is an
optional `app.BusInboxI` rather than a `BusI` method; Q-A (the idle bound)
and Q-B (a cancelled turn's partial answer) were decided against for now
and moved to SD5; a delta's tool names are a list, since one coalesced
delta can carry several.

### 2026-10-28 — the inbox must be the sender's; the tail on NATS

From a review before the first commit. Any subscriber to a request's
subject sees its reply inbox, so a client could name another client's
inbox as its `StreamInbox` and have the service publish there; a plain
`Request` waiting on that inbox would take the delta as its reply. The
service now streams only to an inbox the requesting app instance holds
(`inprocbus.Client.InboxHeldBy`), and answers anything else without
deltas.

On NATS the inbox and the reply are delivered on separate goroutines, so
the reply can arrive while the last deltas are still queued, and the
unsubscribe that follows it drops them. The ordering SD2 promises holds
where deltas are published, not where they are received; the step is
redrawn from the reply either way.

The receiver also checks `seq`, so a lost delta that carried only tool
names is a gap too.

## References

- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD1, §SD3, §SD7 — `llm.complete`, the wall, streaming deferred.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD6 — the chat's tool loop and its trail.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — what the reply's call record feeds.
- [runstream](../../public/keelson/runtime/runstream/runstream.go) — no terminal frame means incomplete.
