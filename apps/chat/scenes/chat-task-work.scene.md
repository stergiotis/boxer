---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-settings.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Write a note in the demo app."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — top task: let the model work in a window, and decide what it asks

The second of the chat's top tasks (ADR-0404 §SD7, proposed), against the
first turn of the scripted model in
[chat-settings.script.jsonl](./chat-settings.script.jsonl), with no test
grant: the person answers the request. The model asks to open the demo app;
the request waits in the host's queue (ADR-0324) and the window's state chip
says it is the person's turn. Approved, the model opens the window and
writes its note; Stop task is on the surface while the task holds it.

```jsonl trace
{"do":"note","text":"top task: work in a window, decide what it asks"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","value":"your turn","role":"label","comment":"the state chip: the turn waits on the person"}
{"do":"wait","contains":"your turn","role":"button","comment":"the host's queue holds the request"}
{"do":"capture","text":"chat-task-work-asks","sidecars":["svg","tree"]}
{"do":"click","contains":"your turn","role":"button"}
{"do":"click","contains":"Next","role":"button","comment":"its card, non-modal"}
{"do":"wait","valueContains":"asks to start a task","role":"label","nth":0}
{"do":"click","name":"Approve","role":"button"}
{"do":"wait","valueContains":"The note is written.","role":"label","settleMs":1000}
{"do":"wait","valueContains":"set note in window 2 · done","role":"label","comment":"the step, in the reader's words"}
{"do":"wait","contains":"Stop task","role":"button","comment":"the task holds the window; stopping it stays on the surface"}
{"do":"wait","value":"answered","role":"label"}
{"do":"capture","text":"chat-task-work-done","sidecars":["svg","tree"]}
```
