---
type: adr
status: proposed
date: 2026-10-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

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

- *A private inbox the requester subscribes to* (chosen): `BusI` gains a
  way to subscribe to a fresh inbox (`SubscribeInbox`), the request names
  it, and the service publishes deltas there. Private for the same reason
  a reply is; no new capability.
- *A subject family, `llm.delta.<key>`*: no bus change, but a subscriber
  holding `llm.delta.>` reads every app's answers as they are written —
  text across apps, past the wall. Rejected.
- *Polling, `llm.peek` with the call's cancel key*, scoped to the sender as
  `llm.cancel` is: no bus change, and coalescing comes free, but every
  streaming call costs a request per tick and the latency is the tick.
  Kept as the fallback if the `BusI` change is refused.

**Q2 — What a delta is.** Accumulated offsets with the new text
(chosen), separately for content and reasoning, plus the names of tool
calls as they start; or whole snapshots. Offsets let a consumer detect a
gap and ask for nothing: a gap means wait for the reply, since deltas are
advisory.

**Q3 — How often.** Coalesced in the service (chosen): at most one delta
per 100 ms or 2 KB, whichever comes first, so the bus carries a few
messages a second whatever the provider's chunking.

**Q4 — The timeout.** Under streaming the provider call could be bounded
by the time between chunks rather than the whole call, so a long answer
that keeps arriving is not cut off at `BOXER_LLM_TIMEOUT`. Open (Q-A
below).

## Decision

### SD1 — The provider side

`openaichat` gains `CompleteStream(ctx, req, onDelta)`: the same request
with `stream: true` and `stream_options.include_usage`, read as server-sent
events. Content and reasoning deltas go to `onDelta` as they arrive; tool
call fragments are accumulated by index and the call's name reported when it
first appears. The result is the same `CompletionResponse` as `Complete`'s,
assembled from the stream, so everything downstream of it is shared. An
endpoint that refuses `stream` is called again without it, once, and the
caller gets no deltas.

### SD2 — The service and the wire

A request may name a `StreamInbox`. With one, the service calls
`CompleteStream` and publishes coalesced deltas to the inbox:
`{v, callKey, seq, contentOffset, content, reasoningOffset, reasoning, tool}`.
Without one, nothing changes. The reply is sent as before, after the last
delta, and is the only thing a consumer may treat as the answer: no
terminal delta exists, and a stream that stops is read as "wait for the
reply" — the runstream rule that absence is the safe reading
([runstream](../../public/keelson/runtime/runstream/runstream.go)). The
wall, the retention and the call record are the reply's, unchanged; a
cancelled call's partial text is not kept.

### SD3 — The bus

`app.BusI` gains `SubscribeInbox(handler) (inbox string, unsubscribe func(), err error)`:
an inbox subject unique to the client, subscribed without a capability check
as reply inboxes are. In-proc it reuses the reply-inbox allocator; on NATS
it is `NewInbox` and a subscription. The noop bus and the test fakes return
an error, and a client that gets one sends no `StreamInbox`.

### SD4 — The client and the chat

`llm.Client.CompleteStreaming(ctx, req, onDelta)` subscribes an inbox,
sends the request naming it, hands deltas to `onDelta` off the bus
goroutine, and unsubscribes when the reply arrives or the context ends. The
chat's tool loop passes the deltas to its trail: the running model step
shows the answer and the reasoning as they grow, and the waiting bubble
shows the answer's text under the trail. The landed turn is drawn from the
reply, as now.

### SD5 — Deferred, recorded

Streaming for other consumers (mdedit transforms, play's Model panel);
streaming through the scripted model beyond what a scene needs; a
partial answer kept after a cancel.

## Open questions

- **Q-A.** Should a streamed call be bounded by the time between chunks,
  with `BOXER_LLM_TIMEOUT` as that idle bound and a separate, longer
  ceiling on the whole call?
- **Q-B.** Should a cancelled turn's partial answer stay visible in the
  transcript, marked as cancelled, though it is not resent or kept?

## Alternatives

- **Deltas on the task progress subjects.** The bgjob/task progress plane
  carries numbers and a note, is read by any observer, and would put
  answer text where progress was promised.
- **Streaming the whole reply as runstream frames, no separate reply.**
  Every consumer would have to collect frames to get what one reply gives
  it now, and the call record would be written from a collector rather
  than from one message.

## Consequences

### Positive

- A long answer is readable as it is written, and a stalled call is
  distinguishable from a slow one.
- Consumers that do not stream are untouched.

### Negative

- `BusI` grows a method, which every implementation and fake must carry.
- The SSE parser is new code against several providers' dialects of the
  stream (reasoning fields, usage chunks, tool-call fragments).
- A delta and the reply can disagree when the provider's stream and its
  final usage do; the reply wins.

### Neutral

- The bytes on the bus grow by the answer's size once more, coalesced.

## Migration — Tier 1

None: the stream is opt-in per request.

## Verification plan — Tier 1

- `openaichat`: a recorded SSE stream (content, reasoning, two tool calls in
  fragments, a usage chunk) assembles into the response `Complete` gives
  for the same exchange; an endpoint that rejects `stream` is retried once
  without it.
- Service: deltas arrive in order with contiguous offsets, coalesced; none
  after the reply; a refused call publishes none; a cancel stops them.
- Bus: an inbox from `SubscribeInbox` receives what is published to it and
  no other client can subscribe to it by name.
- Chat: a scene against a scripted model that streams in steps shows the
  answer growing in the waiting bubble.

## Status

Proposed 2026-10-05. Nothing built.

## Updates

None.

## References

- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD1, §SD3, §SD7 — `llm.complete`, the wall, streaming deferred.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD6 — the chat's tool loop and its trail.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — what the reply's call record feeds.
- [runstream](../../public/keelson/runtime/runstream/runstream.go) — no terminal frame means incomplete.
