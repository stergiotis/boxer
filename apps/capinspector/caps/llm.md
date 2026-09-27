---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the llm cap.

# llm — sending text to a model

[ADR-0254](../../../doc/adr/0254-model-inference-as-a-keelson-capability.md)
makes model inference a declared capability. Before it, an app that called a
model held its own client behind an environment variable, placed where the
static capability gate saw the least of it; no manifest said so and no audit
row named the app.

An app declares the grant with its purpose and calls through the typed
client:

```go
Caps: llm.ClientCaps("mdedit: transform the selection through a model"),

func (inst *App) Mount(ctx app.MountContextI) error {
    inst.model = llm.NewClient(ctx.Bus())
    return nil
}

d, err := inst.model.Describe(ctx)          // is a model offered, which, where
res, err := inst.model.Complete(ctx, llm.Request{
    Purpose:  "improve-style",
    Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: text}},
})
```

## What the schematic shows

- **Subjects.** `llm.describe` and `llm.complete`, request/reply. The host
  owns the model and the endpoint; a request carries neither.
  `llm.cancel` is a publish that stops the sender's own completion in
  flight; the typed client sends it when the caller's context is cancelled.
  `llm.retain.complete` is `complete` whose conversation the host keeps
  ([ADR-0264](../../../doc/adr/0264-retained-model-conversations-on-facts.md));
  it has two tokens, so no `llm.*` grant covers it and an app declares
  `llm.RetainCaps(reason)` beside `ClientCaps`.
- **Backends.** One service, under the id `runtime.llm`, over the
  repository's one chat-completion client, configured once by
  `BOXER_LLM_ENDPOINT`, `BOXER_LLM_MODEL`, `BOXER_LLM_APIKEY`,
  `BOXER_LLM_MAXTOKENS` and `BOXER_LLM_TIMEOUT`. Unconfigured, it answers
  `describe` with the reason and refuses `complete`.

## What guards it

- **One policy point.** A request declares the sensitivity of what it was
  composed from; confined content (sealed data, ADR-0145) is refused unless
  the endpoint is loopback. The declaration is the caller's, so a consumer
  that composes from query results forwards the run's label.
- **Tools run under the app's grants.** A model's tool calls come back
  unexecuted; the app runs each through the bus client it holds, so the
  service never exercises a capability the app lacks.
- **Consent and audit.** The grant is not sticky; the call is a request,
  so the bus records it with the app as sender.

## Where the calls are read

- `keelson('llm_calls')` — every completion this process answered or
  refused: app, purpose, sensitivity, model, sizes, tokens, elapsed, how it
  ended. Prompt and completion text only at `BOXER_LLM_RETAIN=ring` or
  above, and only here; the same row without the text lands on
  `boxer.facts` as the `llmCall` kind wherever the host's persist backend
  reaches it.
- `boxer.facts`, kind `llmMessage` — the messages of retained requests, one
  row per message, only what is new since the call's parent, at
  `BOXER_LLM_RETAIN=durable`. Read with SQL; no verb reads it back. The
  text is kept until removed by hand: `llmfacts.DitchMessagesSQL` removes
  all of it, or one app's, and leaves the `llmCall` rows — the way to check
  an app does not rely on it.
- `keelson('llm_prompts')` — every registered prompt document: what a
  model may be asked to do here. `purpose` is `book/slug` on both tables.
