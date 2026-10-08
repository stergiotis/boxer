---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1600x1000
  tags: [slow]
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-afk-stop-withdraws.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Show the apps in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — stopping a turn withdraws the call it waits on

ADR-0269 and ADR-0265, updates of 2026-10-08. As in
[chat-afk-held-widening](./chat-afk-held-widening.scene.md), the model's
write is held as a widening, but the person stops the turn instead of
deciding. The chat cancels the held call: the dialog closes, and the
Analytics panel counts the write as failed — a held call would count as
asked — so nothing is left to approve after the turn that would read it.
Tagged slow with the other scenes of the person being away.

```jsonl trace
{"do":"note","text":"ADR-0269: cancel withdraws a held call"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"asks to start a task","role":"label"}
{"do":"click","name":"Approve","role":"button","settleMs":1500}
{"do":"wait","contains":"agent · act","role":"button"}
{"do":"click","contains":"agent · act","role":"button","settleMs":500,"comment":"the play window's badge"}
{"do":"click","name":"observe","settleMs":500}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","valueContains":"asks to call set_sql","role":"label"}
{"do":"wait","valueContains":"waiting for you","role":"label"}
{"do":"click","contains":"Cancel","role":"button","comment":"the person stops the turn"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Analytics","role":"button","settleMs":800}
{"do":"wait","valueContains":"window 2 · SQL playground","role":"label"}
{"do":"wait","valueContains":"1 call: 1 failed","role":"label","comment":"set_sql, withdrawn"}
{"do":"capture","text":"chat-afk-stop-withdraws"}
```
