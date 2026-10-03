---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-message-actions.script.jsonl"
    BOXER_CHAT_DRAFT: "How do I see which models my apps called in the last hour? Give me SQL."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a message's actions

ADR-0265 §SD3/§SD4 against the scripted model in
[chat-message-actions.script.jsonl](./chat-message-actions.script.jsonl).
Before the first send the transcript says what the window talks to. The
answer lands shown from its start; its SQL block opens in play, in the
editor and not run. Regenerate answers the last turn again; Edit takes the
turn back into the composer, and Cancel edit puts it back as it was.

The Edit locator carries the button's glyph, since play's Editor tab also
contains "Edit".

```jsonl trace
{"do":"note","text":"chat: message actions — open SQL in play, regenerate, edit and cancel edit"}
{"do":"wait","valueContains":"Chat with scripted","role":"label","comment":"the empty transcript says what it talks to"}
{"do":"capture","text":"chat-message-actions-empty"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"The end of the answer.","role":"label","settleMs":800}
{"do":"capture","text":"chat-message-actions-landed"}
{"do":"click","contains":"Open in play","role":"button","comment":"the sql block's button"}
{"do":"wait","valueContains":"opened the SQL in play","role":"label","settleMs":1500}
{"do":"wait","valueContains":"type SQL and press Run","role":"label","comment":"play has the SQL and did not run it"}
{"do":"click","contains":"Regenerate","role":"button"}
{"do":"wait","valueContains":"The end of the answer.","role":"label","settleMs":800}
{"do":"click","contains":" Edit","role":"button"}
{"do":"wait","valueContains":"Editing your last message","role":"label","settleMs":500}
{"do":"capture","text":"chat-message-actions-editing"}
{"do":"click","contains":"Cancel edit","role":"button"}
{"do":"wait","valueContains":"The end of the answer.","role":"label","settleMs":500,"comment":"the turn is back"}
{"do":"capture","text":"chat-message-actions-restored"}
```
