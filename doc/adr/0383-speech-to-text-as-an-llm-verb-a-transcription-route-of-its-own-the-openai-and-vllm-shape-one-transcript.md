---
type: adr
status: accepted
date: 2026-11-19
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-19
---

# ADR-0383: Speech to text as an `llm` verb — a transcription route of its own, the OpenAI and vLLM shape, one transcript

## Context

The `llm` capability ([ADR-0254](./0254-model-inference-as-a-keelson-capability.md))
offered chat completion only. An app holding audio had no declared,
audited path to a speech-to-text model; the only path was its own
`net/http`, which is what the capability exists to take away (§SD2). No
ADR recorded speech recognition as decided or deferred.
[ADR-0372](./0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md)
defers microphone capture; recordings on disk are readable through
`public/science/audio` ([ADR-0208](./0208-audio-waveform-player-widget.md)).

When this was decided, on 2026-11-19, the repository held no consumer.
The capability is wanted ahead of one, so the cut is the verb, the route, the transcript shape and the
ways to exercise them without an app.

The endpoint is `POST /v1/audio/transcriptions`. Many servers claim it;
they do not agree on it. The dated survey,
[speech-to-text-api-survey](../adr-background-work/speech-to-text-api-survey.md),
records what OpenAI, vLLM and five other implementations do. What bears
on the decision:

- **Nothing on the wire says what a server supports.** No model listing
  read carries a task or modality field.
- **Response formats, streaming, usage and diarization differ** between
  servers in ways a client cannot detect before it is refused, and on
  some servers cannot detect at all (a silent downgrade to bare text).
- **OpenAI and vLLM agree on most of it**: the same form fields, the same
  `json`, `verbose_json` and `diarized_json` bodies, diarization selected
  by a diarizing model, and vLLM ignores the OpenAI fields it does not
  implement rather than refusing them — read from vLLM's source, not
  tested against a running server. Where they differ — vLLM's plain
  stream is chat-completion-style chunks, its seconds are integers, it
  gives no word times — the difference is small and known.
- **The transcription model is rarely the chat model, and often not on
  the same server**; a host whose chat route has no audio endpoint at all
  is the expected deployment. ADR-0254 §SD2's one endpoint and one model
  cannot express it.

Audio is content in exactly ADR-0254's sense: one place decides whether
it may leave, every call is a record, and an app holds a grant instead of
a socket. A recording of voices is at least as sensitive as the text a
chat sends.

## Decision

Speech to text is a verb of the `llm` family, `llm.transcribe`, on a
transcription route configured apart from the chat route, developed
against OpenAI and vLLM and nothing else. Every answer is normalised into
one transcript. The verb passes through the service's existing policy
point, extracted so that both verbs call the one function.

### SD1 — One verb, `llm.transcribe`, under the family's grant

A request carries the audio's bytes, its media type and a file name, and
the options a caller may set: language, a prompt, temperature, whether to
diarize, up to four known speakers (a name and a reference clip each),
and the timestamp granularity wanted. The reply is a transcript (SD3).

The subject is one token after `llm.`, so `llm.ClientCaps()`'s `llm.*`
covers it, as the wildcard reads lexically. An app that may ask the
host's model a question may also send it a recording; a deployment that
wants the two apart has ADR-0254 §SD7's per-purpose grants to take up.

The bytes travel inline in the request. The bus is in-process and the
file broker already moves whole files as one message; the service
refuses a request above the route's byte ceiling before anything leaves.
The ceiling counts the request as it leaves — the recording and any
known speakers' clips — which is what a server's upload limit counts.

`llm.describe` reports the transcription route beside the chat route,
each configured or not on its own: model, diarizing model, host, whether
it is local or trusted, the byte ceiling and the timeout.

### SD2 — A transcription route, OpenAI and vLLM only

The service holds a second route with its own endpoint, model, diarizing
model, key, timeout and byte ceiling — the `BOXER_LLM_TRANSCRIBE_`
variables, under the ADR-0009 registry. Neither endpoint nor model has a
default, for ADR-0254 §SD2's reason. The trusted-hosts list is shared,
since it names machines, not routes, and the route is a sink of the cell
of its own when it sits outside it.

The client speaks the request and the answers OpenAI and vLLM share, and
absorbs the few places they differ. No other server is a target: the
survey's per-server problems — a different path, a format silently
downgraded, a diarization switch of another kind, usage in another shape
or none — are recorded there and not worked around here. A server that
matches the OpenAI and vLLM shape works; one that does not fails in the
way the survey says it will. This replaces the declared dialects an
earlier draft proposed, which would have made each server's quirks a
setting to maintain.

### SD3 — One transcript shape; the format is the client's choice

The service never forwards a caller's choice of `response_format`. The
client asks for `diarized_json` for a diarized request, `verbose_json`
when times are wanted, `json` otherwise, and normalises every answer
into one shape: the text, the language and duration when reported,
segments with a string id, start, end, text and a speaker label when
diarized, words with times where the answer gives them, and usage with
every quantity optional.

A request for times is answered whole: vLLM refuses `verbose_json` with
`stream`. Subtitle files (SRT, WebVTT) are rendered from segments by the
client, not requested from the server — vLLM refuses them and the newer
OpenAI models answer `json` only. They exist where an answer has
segments: whisper-1 and Whisper-style vLLM models with times, or a
diarized answer; asked of a model that gives none, the command line says
so rather than writing an empty file. A body that is not JSON is read as
the text and marked so, rather than failing.

### SD4 — Diarization by its own model

OpenAI and vLLM both diarize with a model of its own and
`diarized_json`. A diarized request goes to the configured diarizing
model; a host without one refuses it with the variable to set. A prompt
on a diarized request is passed on and left to the server: OpenAI's
diarizing model refuses one, a vLLM model may not. Known
speakers travel as OpenAI's `known_speaker_names[]` and
`known_speaker_references[]`, the clips as data URLs, at most four; the
client checks the count and leaves clip length to the server. A diarized
request always carries `chunking_strategy=auto`, which OpenAI requires for
input over 30 seconds and vLLM ignores.

Speaker labels are opaque and scoped to one transcript: `A` on one call
and `A` on the next are not the same person. An answer is diarized only
when its segments carry labels; a diarizing model that labelled no one
reads as segments with times, and the call's speaker count says zero.

### SD5 — The same policy point, records and meter

The checks `llm.complete` ran inline — the journal entry written before
the call (ADR-0357 §SD1), the delegation check (ADR-0269 §SD6), the
sensitivity wall (ADR-0254 §SD3), the tier wall and its approval path
(ADR-0312 §SD2–§SD3), the canaries (§SD6), admission (ADR-0300 §SD5) and
the write-ahead (ADR-0277 §SD3) — are one function both verbs call, in
that order. ADR-0254 §SD3's "the only such function in the tree" holds by
construction rather than by a copy that drifts. A verb supplies what the
checks read: the route, the content digest, the bytes that leave and the
text the canaries scan.

What each check reads of a transcription:

- **The content digest** the journal entry records covers every option,
  the audio's digest and each known speaker's clip digest.
- **The bytes an approval binds to and the exit budget counts** are the
  multipart body as it is sent whole, encoded deterministically — its
  boundary is derived from the content — so an approval of a refused
  recording covers the same recording resent, and no other. A streamed
  request leaves with one more field and a boundary of its own; it is
  approved and counted as its whole form. A stream the server refuses is
  not sent again whole: the refusal is the answer, rather than the
  recording leaving twice.
- **The canaries** read the text fields: prompt, language, file name,
  media type, speaker names. The audio cannot be read for a planted token
  (Consequences).

The write-ahead holds for a transcription as for a completion: before
the recording leaves, the trail gets one message row naming it — the
model, the media type, the size and the digest of the audio, never its
bytes — and after the answer the transcript is the reply's row. Every
transcription is a row on the call record and the trail beside
completions: the verb, the audio's media type and digest, its length in
milliseconds as the answer reported it, and on a diarized request the
number of speakers labelled. The audio is never kept, at any retention
level; the transcript's text follows `BOXER_LLM_RETAIN` as a completion's
does.

The ration ledger gains one quantity, audio milliseconds, charged from
the answer's duration or, failing that, the seconds its usage was billed
in. Tokens are charged where the answer reported them. A quantity the
answer did not report charges nothing, so a rule on audio length is
honest about the calls it could not count. A streamed vLLM transcription
is one of them: its stream reports no audio length, and tokens only when
asked, which the client does not, so it is charged neither while the same
request answered whole is charged its length. A reported length that is
negative or past any recording is not taken. A call
refused after admission sent nothing and is charged no bytes.

### SD6 — Streaming through the existing inbox

A caller that names a stream inbox (ADR-0286 §SD2) receives the
transcript's text as it arrives, and on a diarized stream each segment as
it completes; the reply is still the whole transcript. The client reads
OpenAI's typed `transcript.text.*` events and the chat-completion-style
chunks of a plain vLLM stream, and accepts a whole JSON answer from a
server that did not stream. A stream that ends without saying it is done
— no done event, no `[DONE]` — or that stopped on its length is an
incomplete answer, not a transcript. The delta message gains a segments member; a
reader that does not know it reads the text as before.

### SD7 — The client beside `Complete`

The transcription client lives in `public/llm/openaichat`, as methods of
the same client: it shares the transport, the retry policy, the response
ceiling, the observer, the error classification and the server-sent-event
reader, which the chat stream and the transcription stream both use.
Of the optional form fields, a request sends only those the caller set;
`response_format`, and `chunking_strategy` on a diarized request, are the
client's own.

### SD8 — Exercised without a consumer

- Tests against fake servers pin each request's fields and each answer's
  normalisation, in both stream shapes; the service's tests hold the
  walls, the journal digest, the record and the meter for transcriptions.
- `BOXER_LLM_TRANSCRIBE_SCRIPT` gives the headless host a scripted
  transcriber, answer bodies in the servers' own shapes, so a scene can
  assert a transcript without a model.
- `boxer llm transcribe` sends a file through the configured route from
  the command line and writes text, JSON, SRT or WebVTT, so a deployment
  can try its route before any app exists. It is host-side code and goes
  around the bus and the record, as the command line does elsewhere.
- An integration-lane test sends a recording through the service to the
  configured route, and skips when none is configured.

## Alternatives

- **Speech as a chat completion with an audio content part.** Reuses the
  chat route; gives no segments, times or diarization on the servers
  surveyed, and the transcription models most servers serve are not chat
  models. Recorded as a possible second mechanism behind the same verb.
- **A two-token subject under a grant of its own.** Proposed in the first
  draft so that a chat grant would not reach recordings; rejected for the
  grant as its wildcard reads (SD1).
- **The chat route's endpoint with a model override.** Fails the expected
  deployment where the chat provider has no audio endpoint.
- **Declared dialects per server family.** The first draft's answer to
  servers that differ; rejected for targeting the two that agree and
  documenting the rest (SD2).
- **Probing the server for what it supports.** Nothing on the wire
  answers; a probe sends audio to find out and reads a silent downgrade
  as success.
- **Forwarding `response_format` and the server's body.** Leaves every
  caller to absorb the differences, and makes subtitles fail where the
  server refuses them.
- **A separate `stt` capability family.** A second policy point, record
  and meter for the same act — content leaving for a model.

## Consequences

### Positive

- A deployment points speech at a local vLLM and chat at a remote
  provider, or the reverse, and each route's own host meets the walls.
- A caller writes against one transcript shape; moving between OpenAI and
  vLLM is a deployment change.
- Transcriptions, their audio length, refusals and speakers found are
  rows on the same call table and trail as completions.
- The policy point exists once, with two callers.

### Negative

- **Audio defeats the canaries.** A planted token in a recording is not
  found; the tier the caller declares is the only label the wall sees.
  A deployment that keeps audio confidential relies on the declaration
  and on a local route.
- **Other servers are unsupported by design.** speaches, whisper.cpp's
  server, LocalAI, Groq and Mistral each fail in a way the survey names;
  a deployment on one of them gets a refusal or a degraded transcript,
  not a workaround.
- **A grant to ask the model is a grant to send recordings** (SD1).
- **A capability with no consumer** is carried on its tests until one
  arrives.
- Audio up to the byte ceiling sits in memory on the bus and in the
  service for the call's length.

### Neutral

- Translation to English, the endpoint family's other verb, is deprecated
  by OpenAI and served by few vLLM models; it is left out.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `llm` subjects | `llm.transcribe`; a segments member on the stream delta | the service's dispatch |
| `llm.describe` reply | the transcription route's block | the client's `Description` |
| `keelson('llm_calls')`, the `llmCall` row | verb, audio media type, digest, length, speakers | the runtime vocabulary and its golden, the generated trail store |
| ration quantities | `audio_ms` | `keelson('llm_usage')` |
| cell declaration | the transcription route as a second model-route sink | `keelson('sinks')`, hostboot |
| environment registry | the `BOXER_LLM_TRANSCRIBE_` variables | `doc/env-vars.md` |
| `openaichat` | transcription methods, the shared stream reader, the transport's content type | the chat stream's tests |
| command line | `boxer llm transcribe` | — |

## Migration — Tier 1

Nothing breaks. The verb and its variables are new; an unconfigured
transcription route leaves `describe` saying so. Rows written before the
verb carry no verb and read as completions. The policy point's extraction
leaves `llm.complete`'s order of checks and its refusals as they were; its
existing tests are the check, beside two that pin paths no test reached
before (an approval given back when admission refuses, the caller's
deadline).

## Verification plan — Tier 1

- The `llm` service tests for completions, unchanged, stay green across
  the extraction; the transcription tests hold the walls, the approval's
  binding to the recording, the journal digest, the record and the meter.
- The `openaichat` tests pin the request fields per mode, the
  deterministic encoding (a property test, and the boundary stepping past
  one the body holds),
  each answer shape, both stream shapes and the subtitle rendering.
- The vocabulary golden goes red on a re-pointed membership id.
- The integration lane runs a live transcription when a route is
  configured.

## Deferred

- **Microphone capture** — ADR-0372's deferral, unchanged.
- **Translation**, deprecated upstream.
- **Realtime transcription** over a socket.
- **Client-side splitting** of audio above the ceiling; vLLM splits long
  audio itself, OpenAI does not.
- **Durable retained transcripts** and log probabilities.
- **A floor on the tier of audio** — whether a cell should raise every
  recording to a minimum tier rather than trust the caller's declaration.
- **Audio inside chat completions** as a second mechanism.

## Status

Accepted 2026-11-19.

## References

- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the
  family, the single policy point, the call record.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — retention.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  — agent-caused calls and their grant.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) —
  the trail and the write-ahead.
- [ADR-0286](./0286-streamed-model-answers-over-a-private-inbox.md) — the
  stream inbox.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md) —
  the ledger and its quantities.
- [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)
  — the tier wall, approvals, canaries and the exit budget.
- [ADR-0357](./0357-a-model-call-names-the-journal-entry-written-before-it.md)
  — the request entry written before a call.
- [ADR-0372](./0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md)
  — audio output, and the deferred capture.
- [speech-to-text-api-survey](../adr-background-work/speech-to-text-api-survey.md)
  — the servers, dated, and the problems with those not targeted.
