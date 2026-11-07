---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-wake.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_CALL_WAIT: "1s"
    BOXER_CHAT_DRAFT: "Run a slow query in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a slow run ends the turn, and its result starts the next

ADR-0358 on the headless host, against the scripted model in
[chat-wake.script.jsonl](./chat-wake.script.jsonl). The tool call's wait is
cut to one second, and play's query sleeps for three.

- Turn 1: the model opens play, writes the query and runs it. The run is still
  going when the wait ends, so the call is pending, the model answers, and the
  turn ends with the work going on in the background.
- The run lands. Nobody has spoken, and the window starts a turn for the
  result, which the transcript marks as not the person's, with the reason. The
  model reads the note and answers.

```jsonl trace
{"do":"note","text":"ADR-0358: a pending call, and a turn its completion starts"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"wait","value":"1 operation running in the background","role":"label","comment":"the wait ended with the run going; the status line says so during the turn"}
{"do":"wait","valueContains":"The query is still running","role":"label","comment":"the model answered; turn 1 ended"}
{"do":"wait","valueContains":"⚙ run in window 2 · running","role":"label","comment":"the turn's lines reach the transcript as it ends"}
{"do":"wait","valueContains":"1 rows ·","role":"label","comment":"play's own summary: the run landed"}
{"do":"wait","value":"⚙ work finished in the background, so the model was asked to read it (1 result, wake 1 since you last spoke)","role":"label","comment":"a turn nobody asked for, and why it started"}
{"do":"wait","valueContains":"The slow query has landed","role":"label","comment":"the woken turn's answer"}
{"do":"capture","text":"chat-wake"}
```
