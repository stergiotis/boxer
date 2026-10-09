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
    BOXER_AGENT_DEADLINE: "10s"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-afk-late-call.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Show the apps in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a write after the task's deadline waits for more time

ADR-0269 and ADR-0265, updates of 2026-10-08. The task's deadline
(`BOXER_AGENT_DEADLINE`, shortened here to 10 s) passes before the model
writes, so the write is held while the person is asked for more time. The
chat reads the held call's status as held, not denied, and waits with it;
approving gives the task more time, the write goes through, and the model
reads its outcome. Tagged slow, since it waits out the deadline.

```jsonl trace
{"do":"note","text":"ADR-0269: a late task's held call is reported as held"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"asks to start a task","role":"label"}
{"do":"click","name":"Approve","role":"button"}
{"do":"sleep","settleMs":12000,"comment":"the model takes its time; the deadline passes"}
{"do":"wait","valueContains":"deadline has passed","role":"label"}
{"do":"wait","valueContains":"waiting for you","role":"label"}
{"do":"sleep","settleMs":11000,"comment":"past the chat's ten seconds"}
{"do":"wait","valueContains":"waiting for you","role":"label"}
{"do":"capture","text":"chat-afk-late-call-waiting"}
{"do":"click","name":"Approve","role":"button","comment":"more time"}
{"do":"wait","name":"rendered","role":"radio_button","comment":"the model read the write's outcome"}
{"do":"wait","valueContains":"SELECT 5151 AS chat_afk_marker","role":"multiline_text_input"}
```
