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
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-afk-held-widening.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Show the apps in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a write held for a widening waits for the person

ADR-0265, update of 2026-10-08. The person approves the task, then lowers
play to observe; the model's next write is held as a widening. The turn
waits on it past the chat's ten seconds for a call that does not wait on
the person — the trail says "waiting for you" — and once the person
approves, the model reads the write's own outcome, and play holds the SQL.
Tagged slow, since it waits out those ten seconds.

```jsonl trace
{"do":"note","text":"ADR-0265: a held call waits with the turn"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"asks to start a task","role":"label"}
{"do":"click","name":"Approve","role":"button","settleMs":1500}
{"do":"wait","contains":"agent · act","role":"button"}
{"do":"click","contains":"agent · act","role":"button","settleMs":500,"comment":"the play window's badge"}
{"do":"click","name":"observe","settleMs":500,"comment":"the person lowers play to observe"}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","valueContains":"asks to call set_sql","role":"label","comment":"the write is held as a widening"}
{"do":"wait","valueContains":"waiting for you","role":"label"}
{"do":"sleep","settleMs":12000,"comment":"the person is away past the chat's ten seconds"}
{"do":"wait","valueContains":"waiting for you","role":"label","comment":"still waiting, not answered input_required"}
{"do":"capture","text":"chat-afk-held-widening-waiting"}
{"do":"click","name":"Approve","role":"button","comment":"the person comes back"}
{"do":"wait","name":"rendered","role":"radio_button","comment":"the model read the write's outcome"}
{"do":"wait","valueContains":"SELECT 5252 AS chat_afk_marker","role":"multiline_text_input"}
{"do":"capture","text":"chat-afk-held-widening-approved"}
```
