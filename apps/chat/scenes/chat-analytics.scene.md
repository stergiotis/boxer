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
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-coordinator-play.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Show the first three apps in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the agent surface in the Analytics panel

ADR-0283 on the headless host, over the first turn of
[chat-coordinator-play](./chat-coordinator-play.scene.md): a test grant lets
the task open play, and the model reads, writes and runs there. The Analytics
panel then shows the surface: the running task; play's window with its
operations granted and the three it called, each with its outcome; opening
play granted and used once; every other app not granted. Four cells were
used, with one call each.

```jsonl trace
{"do":"note","text":"ADR-0283: what the chat's model could reach, was granted and used"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"wait","valueContains":"Play runs the query","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ run in window 2 · rendered","role":"label"}
{"do":"click","name":"Window","comment":"play opened over the chat: lay the two side by side"}
{"do":"click","contains":"Side by side"}
{"do":"key","text":"Escape","comment":"close the menu"}
{"do":"click","contains":"Analytics","role":"button"}
{"do":"wait","valueContains":"is running","role":"label","comment":"the test grant's task: the surface was read"}
{"do":"read","valueContains":"granted used","role":"label","pattern":"(?P<used>\\d+) of (?P<granted>\\d+) granted used · (?P<calls>\\d+) calls"}
{"do":"expect","of":"used","eq":4,"comment":"get_state, set_sql, run, and opening play"}
{"do":"expect","of":"calls","eq":4}
{"do":"capture","text":"chat-analytics","sidecars":["tree"]}
{"do":"wait","valueContains":"window 2 · SQL playground","role":"label"}
{"do":"wait","valueContains":"1 call: 1 done","role":"label","nth":0}
{"do":"wait","valueContains":"1 call: 1 done","role":"label","nth":3,"comment":"four used cells, one call each"}
{"do":"click","name":"As graph","role":"button","comment":"the same cells as a graph"}
{"do":"wait","valueContains":"hover a node for its detail","role":"label","settleMs":1500}
{"do":"scroll_into_view","value":"observe only","role":"label","settleMs":500,"comment":"the legend's colour key"}
{"do":"hover","value":"observe only","role":"label","settleMs":800,"comment":"a legend entry explains itself on hover"}
{"do":"capture","text":"chat-analytics-graph"}
```
