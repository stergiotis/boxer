---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-watch.script.jsonl"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — top task: come back to an earlier conversation and continue

The fourth of the chat's top tasks (ADR-0404 §SD7, proposed), against the
scripted model in [chat-watch.script.jsonl](./chat-watch.script.jsonl), which
has one answer: the turn after it is told the script ended. Questions is on,
so the conversation runs under a journal, which is what a resume reopens; a
conversation with no tools keeps none. The person asks,
starts a new conversation, opens Conversations from the View menu, resumes
the earlier one and sends a second message into it.

```jsonl trace
{"do":"note","text":"top task: resume and continue"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"The layer is drawn","role":"label","settleMs":500}
{"do":"click","contains":"New conversation","role":"button"}
{"do":"wait","value":"ready","role":"label","comment":"a new conversation"}
{"do":"click","name":"View","role":"button","comment":"the window's View menu"}
{"do":"click","pointer":true,"contains":"Conversations","role":"check_box"}
{"do":"click","contains":"Closed · ","role":"button","comment":"the earlier conversation, closed, once the roster has read the runs"}
{"do":"wait","name":"Resume","role":"button"}
{"do":"click","name":"Resume","role":"button","settleMs":1000}
{"do":"wait","valueContains":"The layer is drawn","role":"label","comment":"its transcript is back"}
{"do":"type","name":"Message","role":"multiline_text_input","text":"Make it blue."}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"(the script has ended)","role":"label","settleMs":500,"comment":"the second turn of the same conversation"}
{"do":"wait","value":"answered","role":"label"}
```
