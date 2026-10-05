---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-trail.script.jsonl"
    BOXER_CHAT_DRAFT: "What is on my desktop?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the trail of a running turn, and its steps afterwards

The tool loop's trail against the scripted model in
[chat-trail.script.jsonl](./chat-trail.script.jsonl), whose third model
call takes eight seconds. While it waits, the waiting bubble lists the
turn's steps — two model calls, two tool calls, and the running model call
— and a finished step opens to its arguments and what came back. Once the
turn lands, each tool line in the transcript keeps its steps under
Details.

```jsonl trace
{"do":"note","text":"chat: the trail of a turn, live and afterwards"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"waiting for the answer","role":"label","comment":"the slow third model call"}
{"do":"wait","valueContains":"model calls · 2 tool calls","role":"label","comment":"the trail's summary line"}
{"do":"click","contains":"Reading the desktop (query_windows)","settleMs":500,"comment":"open a finished step"}
{"do":"wait","valueContains":"came back","role":"label"}
{"do":"capture","text":"chat-trail-running"}
{"do":"wait","valueContains":"One window is open","role":"label","settleMs":500}
{"do":"wait","valueContains":"⚙ Reading the work area · read 1 row(s) of keelson('desktop')","role":"label"}
{"do":"click","name":"Details","nth":2,"settleMs":500,"comment":"the third tool line's steps"}
{"do":"wait","valueContains":"SELECT work_w, work_h","role":"label"}
{"do":"capture","text":"chat-trail-details"}
```
