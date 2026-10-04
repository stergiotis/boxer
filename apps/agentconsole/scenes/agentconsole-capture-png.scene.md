---
type: reference
audience: contributor
status: draft
scene:
  launch: agentconsole
  size: 1400x900
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# agentconsole — a pixel capture of one granted window

ADR-0281 on the headless host. The console opens the operations demo, takes
a test grant over it and asks for a PNG capture. The host records the next
frame's stream, replays only the demo window's span into a capture context
and rasterizes it: the PNG shows the demo window and nothing of the console
or the shell.

```jsonl trace
{"do":"focus","id":10463678445297493114,"comment":"the console's app field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":10463678445297493114,"text":"opsdemo"}
{"do":"click","name":"Open","role":"button","settleMs":2500}
{"do":"read","valueContains":"opened window","role":"label","pattern":"opened window (?P<ops>\\d+)"}
{"do":"click","name":"Request grant","role":"button"}
{"do":"wait","valueContains":"grant task-","role":"label"}
{"do":"click","name":"Capture PNG","role":"button"}
{"do":"wait","valueContains":"capture png · completed","role":"label","settleMs":1500}
{"do":"capture","text":"agentconsole-png","settleMs":500}
```
