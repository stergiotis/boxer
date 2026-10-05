---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-title-context-export.script.jsonl"
    BOXER_LLM_CONTEXT_TOKENS: "2"
    BOXER_CHAT_DRAFT: "Which models did my apps call?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — title, context meter, and the conversation in mdedit

ADR-0265 §SD4 against the scripted model in
[chat-title-context-export.script.jsonl](./chat-title-context-export.script.jsonl).
The first send titles the conversation with its first line; after the first
answer the model's title replaces it. `BOXER_LLM_CONTEXT_TOKENS` states a
context of two tokens, which the scripted turn (one in, one out) fills: the
meter is full and the composer warns. Open in mdedit hands the transcript
to a new mdedit window, which holds it unsaved and does not autosave it.

```jsonl trace
{"do":"note","text":"chat: the title, the context meter and Open in mdedit"}
{"do":"wait","valueContains":"Chat with scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"That is all.","role":"label","settleMs":500}
{"do":"wait","contains":"Calls by model, last hour","role":"button","comment":"the model's title replaced the first line"}
{"do":"wait","valueContains":"fills 100 % of the model's context","role":"label"}
{"do":"capture","text":"chat-title-context"}
{"do":"click","contains":"Calls by model, last hour","role":"button","comment":"rename"}
{"do":"wait","valueContains":"Calls by model, last hour","role":"text_input"}
{"do":"click","name":"\ue182","role":"button","comment":"the check keeps the title, now named by the person"}
{"do":"click","contains":"Open in mdedit","role":"button"}
{"do":"wait","valueContains":"opened the conversation in mdedit","role":"label"}
{"do":"wait","valueContains":"not autosaved","role":"label","settleMs":1500,"comment":"mdedit holds it as a document of its own"}
{"do":"capture","text":"chat-title-context-mdedit"}
```
