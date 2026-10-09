---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  tags: [slow]
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_AGENT_REQUEST_TIMEOUT: "12s"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-afk-grant-expiry.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Show the apps in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a grant request expires while the person is away, and is asked again

ADR-0265, update of 2026-10-08. The model asks for access and nobody
answers: the host's dialog expires (`BOXER_AGENT_REQUEST_TIMEOUT`, shortened
here to 12 s from 30 min), the model reads that the person did not decide in
time, and the turn ends. When the person comes back and says so, the model
makes the same request again, and it reaches a new dialog: a refusal is
remembered within its turn only, so the earlier expiry does not answer it.
Tagged slow, since it waits out the timeout.

```jsonl trace
{"do":"note","text":"ADR-0265: an expired grant request does not block asking again"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"asks to start a task","role":"label"}
{"do":"capture","text":"chat-afk-grant-expiry-asked"}
{"do":"sleep","settleMs":14000,"comment":"the person is away past the request timeout"}
{"do":"wait","valueContains":"the person did not decide in time","role":"label"}
{"do":"wait","valueContains":"The request expired before you decided.","role":"label"}
{"do":"type","id":13282790709624229129,"text":"I am back, try again.","comment":"the composer"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"asks to start a task","role":"label","comment":"a new dialog, not the repeat check"}
{"do":"capture","text":"chat-afk-grant-expiry-asked-again"}
```
