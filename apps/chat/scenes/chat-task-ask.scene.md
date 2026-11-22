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

# chat — top task: ask a question and read the answer

The first of the chat's top tasks (ADR-0404 §SD7, proposed), against the
scripted model in [chat-stream.script.jsonl](./chat-stream.script.jsonl),
which writes its answer word by word. The window opens on the transcript
and the composer, with the seeded question in it; nothing beside them. The
person sends; the state chip says the model is thinking while the answer
grows in the waiting bubble; the chip ends on answered.

```jsonl trace
{"do":"note","text":"top task: ask and read"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"wait","value":"ready","role":"label","comment":"the state chip, before the first message"}
{"do":"capture","text":"chat-task-ask-open","sidecars":["svg","tree"]}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"working…","role":"label"}
{"do":"wait","valueContains":"Streaming shows the answer","role":"label","comment":"the answer as it is written"}
{"do":"capture","text":"chat-task-ask-midturn","sidecars":["svg","tree"]}
{"do":"wait","valueContains":"stays the answer.","role":"label","settleMs":500}
{"do":"wait","value":"answered","role":"label","comment":"the state chip: the turn ended answered"}
```
