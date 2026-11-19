---
type: explanation
audience: designer of the llm capability's transcription verb
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> **Provenance.** Compiled 2026-11-19 from public API references, guides
> and server source files read on that date; each section names its
> sources. Nothing here is a decision. Servers change these surfaces
> between releases, so a row is a reading on that date, not a contract;
> a fact the sources did not settle is marked **[unconfirmed]**.

# Speech to text over the OpenAI-compatible endpoint: what servers do

The background to ADR-0383. The question it answers: what a
client of `POST /v1/audio/transcriptions` can rely on across servers that
claim compatibility, and where it has to be told rather than guess.

## The reference shape (OpenAI)

Sources: the API reference for
[transcriptions](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create)
and
[translations](https://developers.openai.com/api/reference/resources/audio/subresources/translations/methods/create),
the [speech-to-text guide](https://developers.openai.com/api/docs/guides/speech-to-text),
and the Python SDK's
[audio types](https://github.com/openai/openai-python/tree/main/src/openai/types/audio).

The request is `multipart/form-data`: `file` (up to 25 MB; flac, mp3, mp4,
mpeg, mpga, m4a, ogg, wav, webm) and `model`, then optional fields whose
validity depends on the model:

| Field | Constraint read on 2026-11-19 |
| --- | --- |
| `language` | ISO-639-1 |
| `languages[]`, `keywords[]` | newest model only; `languages` replaces `language`, not both |
| `prompt` | not on the diarizing model |
| `response_format` | `json` `text` `srt` `verbose_json` `vtt` `diarized_json`; most models accept `json` only, the diarizing model `json` `text` `diarized_json` |
| `timestamp_granularities[]` | `word` / `segment`, needs `verbose_json`, whisper-1 only |
| `stream` | ignored by whisper-1 |
| `include[]` | `logprobs` only, `json` only, not the diarizing model |
| `chunking_strategy` | `"auto"` or a server-VAD object; **required by the diarizing model for input over 30 s** |
| `known_speaker_names[]`, `known_speaker_references[]` | at most 4 each; references are data URLs of 2–10 s clips |

Diarization is selected by **model** (a diarizing model id) plus
`response_format=diarized_json`. The diarized body is
`{task, duration, text, segments:[{id, start, end, speaker, text, type}], usage?}`;
`id` is a string here and an integer in `verbose_json`'s segments.
Speakers are the known names when given, otherwise `A`, `B`, ….

`usage` comes in two variants: `{type:"tokens", input_tokens,
output_tokens, total_tokens, input_token_details?:{audio_tokens,
text_tokens}}` and `{type:"duration", seconds}`.

Streaming is SSE with typed events: `transcript.text.delta` (`delta`, and
`segment_id` when diarizing), `transcript.text.segment` (diarizing only,
one per finished segment, with speaker and times) and
`transcript.text.done` (full text, usage). Whether a `[DONE]` sentinel
follows is **[unconfirmed]**.

`/v1/audio/translations` (to English) takes `file`, `model`, `prompt`,
`response_format`, `temperature`, works with whisper-1 only, and is marked
deprecated in favour of a realtime endpoint.

The deprecation notice on the reference page lists every model but the
newest for shutdown in February 2027; a route's model id is therefore
configuration, never a default.

## vLLM

Sources: the
[speech-to-text serving docs](https://docs.vllm.ai/en/latest/serving/online_serving/speech_to_text/),
the [supported models list](https://docs.vllm.ai/en/latest/models/supported_models/)
and the protocol and serving modules under
[`vllm/entrypoints/speech_to_text/`](https://github.com/vllm-project/vllm/tree/main/vllm/entrypoints/speech_to_text).

- **Models.** Whisper and a long list of other speech models (Voxtral,
  Granite Speech, Qwen3-ASR, Canary and others). Two of them diarize.
- **Fields.** `file`, `model`, `language`, `prompt`, `response_format`,
  `temperature`, `stream`, `timestamp_granularities[]`, plus extensions
  (`hotwords`, `to_language`, `stream_include_usage`, sampling knobs).
  `include`, `chunking_strategy` and the known-speaker fields are not in
  its protocol, and are ignored rather than refused: the request model
  allows extra fields and logs them at debug level (`OpenAIBaseModel`,
  `extra="allow"`). `timestamp_granularities[]=word` is accepted and has
  no effect. Read from the source, not tested against a running server.
- **Formats.** `text` `json` `verbose_json` `diarized_json`; `srt` and
  `vtt` are declared in the type but **refused** by the serving code.
  `json` is exactly `{text, usage:{type:"duration", seconds}}`.
  `verbose_json` only on models with segment timestamps, without `words`.
  `diarized_json` only on the diarizing models, in OpenAI's shape (string
  segment ids, a `speaker` per segment), with the speaker labels as the
  model emits them and duration usage only.
- **Streaming.** Two shapes. A plain request streams
  **chat-completion-style chunks** (`object:"transcription.chunk"`,
  `choices[0].delta.content`, then `data: [DONE]`), not OpenAI's typed
  events. A diarized request streams the typed events — delta with
  `segment_id`, segment, done — then `[DONE]`. `verbose_json` with
  `stream` is refused.
- **Usage.** A plain answer reports `{type:"duration", seconds}` with an
  integer, rounded up; translations report none; a stream reports usage
  only when asked, chat-style (`prompt_tokens`, `completion_tokens`).
- **Length.** A 25 MB default upload ceiling and a 600 s default decode
  ceiling, both server settings. Audio longer than the model's clip is
  split server-side at low-energy points, and segment times are offset.

## Others, briefly

| Server | Path | Formats | Streams | Diarizes | Usage |
| --- | --- | --- | --- | --- | --- |
| [speaches](https://speaches.ai/) | `/v1/audio/transcriptions` | text json verbose_json srt vtt | yes, event shape **[unconfirmed]** | no | none |
| [whisper.cpp server](https://github.com/ggml-org/whisper.cpp/tree/master/examples/server) | `/inference` unless reconfigured | json text srt vtt verbose_json; an unknown format silently becomes `{text}` | no | stereo only, a `diarize` flag | none |
| [LocalAI](https://localai.io/docs/models/capabilities/audio/audio-to-text/) | `/v1/audio/transcriptions` | json verbose_json text srt vtt lrc | yes, OpenAI's typed events | a `diarize` flag on some backends; labels `0`, `1`; may return none | zero tokens |
| [Groq](https://console.groq.com/docs/speech-to-text) | `/openai/v1/audio/transcriptions` | json verbose_json text | not documented | no | none |
| [Mistral](https://docs.mistral.ai/api/endpoint/audio/transcriptions) | `/v1/audio/transcriptions` | **[unconfirmed]** | yes, events **[unconfirmed]** | a `diarize` boolean | chat-style plus `prompt_audio_seconds` |

## No server says which models transcribe

None of the model listings read on that date carries a task or modality
field: OpenAI's model object is id, owner, dates; vLLM's cards add
`max_model_len`; speaches keeps task filtering on its own registry
endpoint. A client is told which model transcribes, and what the route
supports, or it probes by trying.

## Where OpenAI and vLLM differ

The two targets share the request fields that matter, the three JSON
answer bodies and diarization by a diarizing model. A client written for
both absorbs:

1. **The plain stream's shape.** OpenAI sends typed
   `transcript.text.delta` / `transcript.text.done` events; vLLM sends
   chat-completion-style chunks (`choices[0].delta.content`) and `[DONE]`.
   A diarized vLLM stream uses OpenAI's typed events.
2. **Usage.** OpenAI's newer models report tokens, whisper-1 reports
   seconds; vLLM reports integer seconds, rounded up, on a whole answer,
   and on a plain stream only chat-style tokens, and only when asked
   (`stream_include_usage`). A streamed vLLM answer therefore says nothing
   of the audio's length.
3. **Times.** Word times come from OpenAI's whisper-1 only; the newer
   OpenAI models and vLLM models without segment timestamps refuse
   `verbose_json` with a 400.
4. **Fields one of them lacks.** vLLM ignores `chunking_strategy`, the
   known-speaker fields and `include[]`, so sending OpenAI's form is safe
   on both.

## Problems with the implementations not targeted

ADR-0383 develops against OpenAI and vLLM only. The other servers read
on 2026-11-19 each break the shared shape somewhere; this is the record
of where, so that a deployment on one of them knows what to expect.

- **speaches.** No `usage` at all, so nothing can be metered but calls
  and wall time. Diarization is not offered. Streaming exists with an
  event shape the sources did not settle. VAD always runs, and
  `timestamp_granularities[]` without `verbose_json` is only a warning in
  its log.
- **whisper.cpp's server.** Not at `/v1/audio/transcriptions` unless
  started with `--inference-path`; a client pointed at a base URL reaches
  nothing. An unknown `response_format` silently becomes `{text}`, so a
  request for segments comes back as bare text with a 200. Diarization is
  a `diarize` flag that works on stereo input only and labels `0`, `1`,
  `?`. The language defaults to English unless detection is asked for,
  and without `--convert` (and ffmpeg) only WAV decodes. No streaming, no
  usage.
- **LocalAI.** Diarization is a `diarize` boolean on the plain model, not
  a model of its own, and may silently return no labels; labels are `0`,
  `1`. Token counts are always zero. Its docs disagree on the default
  format. A separate `/v1/audio/diarization` path exists beside the
  shared one.
- **Groq.** The path is under `/openai/v1`. Only `json`,
  `verbose_json` and `text`; streaming is not documented; no `usage`, an
  `x_groq` member instead. The upload ceiling depends on the account
  tier.
- **Mistral.** Diarization is a `diarize` boolean; the segment speaker
  member and the stream's event types were not settled by the sources.
  Usage is chat-style with `prompt_audio_seconds`, a sixth shape.
  Extra fields (`context_bias`, `file_url`, `file_id`) are its own.

The pattern: a client that targeted all of them would carry a setting per
server for the path, the diarization switch, the format each accepts and
the usage shape, and would still not detect whisper.cpp's silent
downgrade before it happened. Against the OpenAI and vLLM client those
downgrades are visible after the fact, not prevented: an answer of bare
`{text}` to a request for segments reads as a transcript without
segments, a diarized request answered without labels reports zero
speakers, and a body that is not JSON is read as text and marked so.
