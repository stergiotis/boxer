---
type: adr
status: proposed
date: 2026-09-01
---

# ADR-0216: mdedit — pluggable LLM text transformations behind an env gate

## Context

mdedit ([ADR-0178](./0178-mdedit-markdown-editor.md)) edits a markdown buffer
beside its rendered form. The wish this ADR answers: run a one-shot text
transformation — translate, summarize, tighten — over the selection or the
document, from a discoverable set of prompts a deployment can extend.

The repository holds exactly one LLM client, `public/llm/openaichat`: one-shot
chat completions against any OpenAI-compatible endpoint, no streaming, no
timeout of its own. Its shipping consumers are CLI-side (commitdigest,
text2sql2). For an *app-side* LLM surface, the recorded intent is
[ADR-0120](./0120-play-natural-language-ask-panel.md) (still proposed): the LLM dependency
lives in a sibling package the app imports, the feature registers only when an
endpoint env var says so, and the endpoint host is shown where the gesture is
made. No app has shipped that shape yet — this is the first, so what it fixes
becomes the precedent play's Ask panel will meet.

Prompts need a home with three properties: discoverable in the UI, extensible
without touching mdedit, and gated against silent drift. The repository
already has that mechanism — the applet book
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)): an embedded fs of
markdown documents, the filename base as public slug, frontmatter as the
descriptor, an open `Register*` seam for contributed corpora, and a corpus
test holding the in-tree set to zero parse errors.

## Decision

**SD1 — the LLM dependency lives in `apps/mdedit/transform`, a sibling
package.** mdedit itself never imports `openaichat`. This is ADR-0120 §SD2's
shape, and it is also what keeps the capability gate quiet and honest: with
the network call two packages away, capslock attributes the egress to the
transform package rather than smearing it across everything mdedit touches.

**SD2 — a transformation is a markdown document; the body IS the system
prompt.** YAML frontmatter carries `title` and `summary` (required), `icon`,
`scope: selection|document` (default selection; unknown values are parse
errors, the ADR-0132 §SD6 posture), `temperature` and `max-tokens`
(optional). Everything after the frontmatter is the system prompt verbatim —
unlike an applet's SQL, a prompt is prose and has no surrounding commentary
to fence off. The filename base is the slug (`^[a-z0-9][a-z0-9-]*$`), durably
public the way an applet slug is. `transform.RegisterPromptBook(id, fs.FS)`
is the open seam; the in-tree book ships `improve-style`, `summarize`,
`translate-to-english` and is held to zero errors by a corpus test, while a
contributed book that fails to parse costs its own entries and a log line,
never the bar.

**SD3 — the surface is env-gated and the egress is visible.**
`BOXER_MDEDIT_LLM_ENDPOINT` *and* `BOXER_MDEDIT_LLM_MODEL` must both be set
or nothing renders — no dead control, no probing; a wrong default model is
worse than a refusal, so neither has a default. The endpoint's host renders
beside the picker. The API key is `BOXER_MDEDIT_LLM_APIKEY` (sensitive, empty
valid for local endpoints) with deliberately no fallback to `GEMINI_API_KEY`
(provider-specific) or `LLM_API_KEY` (a commitdigest CLI alias, not a
registered spec): a sensitive value gets exactly one name per consumer.
`BOXER_MDEDIT_LLM_MAXTOKENS` and `BOXER_MDEDIT_LLM_TIMEOUT` bound a run — the
context is the client's only clock.

**SD4 — preview-then-apply, never rewrite-on-arrival.** The result lands in a
bottom pane rendered as markdown beside the live preview, under a header
stating model, host, elapsed time and token counts. Apply splices it over
exactly the byte span that was sent and refuses if the buffer changed since
the run started — a splice computed against one buffer must not land on
another, and re-anchoring is a judgment this gesture must not make silently.
The alternative — replacing the selection as the completion arrives — was
rejected because a whole-buffer rebind is invisible to the editor's own undo
(ADR-0178 M3's standing caveat): an unreviewed transformation would be a
destructive one. Discard and Copy are the other verdicts; Copy deliberately
does not advance the dirty checkpoint, because the result never was the
buffer.

**SD5 — one attempt, cancellable, truncation is a result.** The run sits
behind a `bgjob.Runner` with an indeterminate progress row and a cancel
button; the client is built without a retry policy because the surface is
interactive — the re-click is the retry, and a failure should read as a
sentinel-mapped line in the pane rather than as silent backoff. A completion
that hit the token ceiling with content already produced is handed over
marked truncated, not swallowed: the reader sees exactly what they would
apply, plus the badge saying it may stop mid-thought.

## Consequences

- mdedit gains its first env registrations (category `boxer-mdedit`) and its
  first — gated — network egress. A host that sets no endpoint is exactly as
  network-free as before, which is also what keeps the demo tour reproducible.
- Prompt slugs are public identity; renaming one is a deprecation event.
- The prompt-doc format has no parameters, so a transformation with a knob
  (target language, summary length) is a separate document per setting.
  Parameterized prompts, streaming, a per-prompt model override and a diff
  view of the result are deferred until asked for; the same goes for a
  quiescence-style re-anchoring of Apply onto a moved buffer.
- ADR-0120, when picked up, should inherit SD2–SD5 rather than re-deciding
  them.

## Verification

The corpus gate test parses the embedded book to zero errors and pins the
launch slugs and their scopes. The transform package is tested against a fake
`openaichat.ClientI` (message composition, ceiling fallbacks, truncation and
context handling); the splice, its staleness refusal and the scope resolution
are pure functions under table tests in mdedit. The end-to-end path needs a
live endpoint and stays manual, driven with LM Studio or Ollama via
`BOXER_MDEDIT_LLM_ENDPOINT`.
