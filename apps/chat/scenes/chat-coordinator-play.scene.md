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

# chat — the coordinator drives play, and meets the person's edit

ADR-0269 M6 on the headless host. The chat is the coordinator (ADR-0265
§SD6); its model is the scripted one in
[chat-coordinator-play.script.jsonl](./chat-coordinator-play.script.jsonl),
whose replies are picked by the conversation's position. A test grant stands in
for the person's approval.

- Turn 1: the model asks for play, opens it, reads its state, replaces the
  buffer and runs it. Play draws three rows; the window's badge names the task
  and is not paused — the result that landed is the app's own change, not the
  person's.
- The person then rewrites play's buffer. The task had read it, so the change
  pauses the task in that window.
- Turn 2: the coordinator's turn lists the change and lifts the pause; the
  model writes without reading again, and the write ends as a conflict. The
  person's text stands.

Node ids anchor the two text fields: the chat is the first window and play
the second, and an id is a function of the window and the widget's path.

```jsonl trace
{"do":"note","text":"ADR-0269 M6: the chat as coordinator, against a scripted model"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"wait","valueContains":"Play runs the query","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ opened play as window 2","role":"label"}
{"do":"wait","valueContains":"⚙ Reading play's buffer · get_state in window 2 · completed","role":"label","comment":"the call's title, before its own line"}
{"do":"wait","valueContains":"⚙ set_sql in window 2 · rendered","role":"label"}
{"do":"wait","valueContains":"⚙ run in window 2 · rendered","role":"label"}
{"do":"wait","valueContains":"3 rows ·","role":"label","comment":"play's own summary of the agent's run"}
{"do":"wait","name":" agent · act","role":"button","comment":"the badge: the task works here and is not paused"}
{"do":"capture","text":"chat-coordinator-play"}
{"do":"focus","id":15565883789046076047,"comment":"the person, in play's editor"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":15565883789046076047,"text":"SELECT name FROM keelson('apps') LIMIT 2"}
{"do":"wait","name":" agent · act · paused","role":"button","comment":"the task had read the buffer"}
{"do":"focus","id":13282790709624229129,"comment":"the chat's composer"}
{"do":"type","id":13282790709624229129,"text":"Now the last three instead."}
{"do":"click","contains":"Send","role":"button","comment":"turn 2"}
{"do":"wait","valueContains":"The buffer changed since I read it","role":"label","settleMs":1000}
{"do":"wait","valueContains":"⚙ set_sql in window 2 · conflict","role":"label","comment":"a write that expects the revision the task last saw"}
{"do":"wait","value":"SELECT name FROM keelson('apps') LIMIT 2","role":"multiline_text_input","comment":"the person's text stands"}
{"do":"capture","text":"chat-coordinator-play-conflict"}
```
