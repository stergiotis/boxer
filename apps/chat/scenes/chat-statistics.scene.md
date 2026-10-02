---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-statistics.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Say hello."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the Statistics panel, and its statistics in play

One answered turn from the scripted model in
[chat-statistics.script.jsonl](./chat-statistics.script.jsonl), of two
model calls: with Apps on it lists its windows, then answers. The
Statistics button opens the panel beside the transcript, which counts the
turn and its calls and draws the token distributions; Open in play publishes them as the ad-hoc datasets
`chat_turns` and `chat_calls` and opens a play window that reads the turns.
`BOXER_CHAT_ADVANCED=false` would hide the button and the panel.

```jsonl trace
{"do":"note","text":"chat: token and answer statistics, and Open in play"}
{"do":"wait","valueContains":"→ ","role":"label","comment":"the host offers a model"}
{"do":"click","contains":"Send","role":"button","comment":"the turn, seeded as the draft"}
{"do":"wait","valueContains":"a short answer","role":"label","nth":0,"comment":"the turn landed"}
{"do":"click","contains":"Statistics","role":"button"}
{"do":"wait","valueContains":"1 turns · 1 answered · 2 model calls","role":"label"}
{"do":"wait","valueContains":"needs two values to draw, has 1","role":"label","comment":"one answer time: no curve yet"}
{"do":"wait","valueContains":"all 2 values are 1: no spread to draw","role":"label","comment":"both calls answered with one token"}
{"do":"capture","text":"chat-statistics"}
{"do":"click","contains":"Open in play","role":"button"}
{"do":"wait","valueContains":"opened in play: 1 turns as chat_turns","role":"label","comment":"published and play asked for"}
{"do":"wait","valueContains":"1 row","role":"label","nth":0,"settleMs":1500,"comment":"play reads the turn from chat_turns"}
{"do":"capture","text":"chat-statistics-play"}
```
