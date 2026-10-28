---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-stream.script.jsonl"
    BOXER_CHAT_DRAFT: "How does streaming look?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a streamed answer grows in the waiting bubble

A streamed model answer
([ADR-0286](../../../doc/adr/0286-streamed-model-answers-over-a-private-inbox.md)),
against the scripted model in [chat-stream.script.jsonl](./chat-stream.script.jsonl),
which writes its one answer word by word over eight seconds. While it
does, the running model step reads "writing the answer" and the waiting
bubble shows the answer's latest words under the trail; once the reply
lands, the transcript holds the reply.

```jsonl trace
{"do":"note","text":"chat: a streamed answer, growing and then landed"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"writing the answer","role":"label","comment":"the running model step, streaming"}
{"do":"wait","valueContains":"Streaming shows the answer","role":"label","comment":"the answer so far, under the trail"}
{"do":"capture","text":"chat-stream-growing"}
{"do":"wait","contains":"Regenerate","role":"button","comment":"the turn landed"}
{"do":"wait","valueContains":"stays the answer.","role":"label","settleMs":500,"comment":"the landed reply"}
{"do":"capture","text":"chat-stream-landed"}
```
