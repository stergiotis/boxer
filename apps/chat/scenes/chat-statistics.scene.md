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

# chat — the Analytics panel, and its statistics in play

One answered turn from the scripted model in
[chat-statistics.script.jsonl](./chat-statistics.script.jsonl), of two
model calls: with Apps on it lists its windows, then answers. The
Analytics button opens the panel beside the transcript, which counts the
turn and its calls and draws the token distributions, and lays out the
agent surface (ADR-0283), above them: no task was granted, so every cell is not granted
or above the ceiling, as a list and as a graph. Open in play publishes them
as the ad-hoc datasets `chat_turns`, `chat_calls` and `chat_surface` and
opens a play window that reads the turns.
`BOXER_CHAT_ADVANCED=false` would hide the button and the panel.

```jsonl trace
{"do":"note","text":"chat: token and answer statistics, and Open in play"}
{"do":"wait","valueContains":"→ ","role":"label","comment":"the host offers a model"}
{"do":"click","contains":"Send","role":"button","comment":"the turn, seeded as the draft"}
{"do":"wait","valueContains":"a short answer","role":"label","nth":0,"comment":"the turn landed"}
{"do":"click","contains":"Analytics","role":"button"}
{"do":"wait","valueContains":"1 turns · 1 answered · 2 model calls","role":"label"}
{"do":"wait","valueContains":"needs two values to draw, has 1","role":"label","comment":"one answer time: no curve yet"}
{"do":"wait","valueContains":"all 2 values are 1: no spread to draw","role":"label","comment":"both calls answered with one token"}
{"do":"wait","valueContains":"no task yet: nothing is granted","role":"label","comment":"the surface was read"}
{"do":"wait","valueContains":"open a window","role":"label","comment":"the launches"}
{"do":"wait","valueContains":"0 of 0 granted used","role":"label"}
{"do":"wait","valueContains":"1 turns · 1 answered","role":"label","settleMs":1000}
{"do":"capture","text":"chat-statistics","sidecars":["tree"]}
{"do":"click","name":"As graph","role":"button","comment":"the same cells as a graph"}
{"do":"wait","valueContains":"hover a node for its detail","role":"label","settleMs":1500}
{"do":"scroll_into_view","valueContains":"hover a node for its detail","role":"label"}
{"do":"capture","text":"chat-statistics-graph"}
{"do":"click","contains":"Open in play","role":"button"}
{"do":"wait","valueContains":"surface cells as chat_surface","role":"label","comment":"published and play asked for"}
{"do":"wait","valueContains":"1 row","role":"label","nth":0,"settleMs":1500,"comment":"play reads the turn from chat_turns"}
{"do":"capture","text":"chat-statistics-play"}
```
