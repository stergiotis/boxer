---
type: reference
audience: contributor
status: draft
scene:
  launch: agentconsole
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS: agentconsole
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# agentconsole — the person approves, lowers and widens

ADR-0269 §SD5 and §SD6 with the person in the loop: no test grants, the
console registered as a coordinator. The console's request waits in the
host's dialog until the scene approves it, and the demo window then carries
the task's badge. Lowering the mode to observe from that badge makes the
next write wait as a widening, which shows in the same dialog, and the call
goes ahead once the person approves it.

```jsonl trace
{"do":"note","text":"ADR-0269 M3: a grant and a widening are the person's decisions in host chrome"}
{"do":"click","name":"Open operations demo","role":"button","settleMs":1500}
{"do":"click","name":"Request grant","role":"button","settleMs":800}
{"do":"wait","contains":"your turn","role":"button","comment":"the request waits in the person's queue (ADR-0324)"}
{"do":"click","contains":"your turn","role":"button"}
{"do":"click","contains":"Next","role":"button","comment":"its card, non-modal"}
{"do":"wait","valueContains":"asks to start a task","role":"label","nth":0}
{"do":"capture","text":"agentconsole-approve-request"}
{"do":"click","name":"Approve","role":"button"}
{"do":"wait","valueContains":"grant task-","role":"label"}
{"do":"wait","contains":"agent · act","role":"button","comment":"the demo window's badge"}
{"do":"click","name":"Call","role":"button","comment":"k1: get_state"}
{"do":"wait","valueContains":"k1 · get_state · completed","role":"label"}
{"do":"click","contains":"agent · act","role":"button","settleMs":500,"comment":"open the badge's menu"}
{"do":"click","name":"observe","settleMs":500,"comment":"the person lowers the task to observe in this window"}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","contains":"agent · observe","role":"button"}
{"do":"focus","id":17222585601186924707,"comment":"the console's operation field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":17222585601186924707,"text":"set_note"}
{"do":"focus","id":7028873545952381604,"comment":"the console's arguments field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":7028873545952381604,"text":"{\"text\":\"from the agent\"}"}
{"do":"click","name":"Call","role":"button","comment":"k2: a write in observe mode waits as a widening"}
{"do":"wait","contains":"your turn","role":"button","comment":"the request waits in the person's queue (ADR-0324)"}
{"do":"key","text":"F8","comment":"next by weight: the head of the queue opens its card, from the keyboard"}
{"do":"wait","valueContains":"asks to call set_note in window 2","role":"label","nth":0}
{"do":"capture","text":"agentconsole-approve-widen"}
{"do":"click","name":"Approve","role":"button"}
{"do":"wait","valueContains":"k2 · set_note · rendered","role":"label"}
{"do":"wait","contains":"agent · act","role":"button","comment":"the widening raised the mode"}
{"do":"read","id":12013363430533946716,"pattern":"^(?P<note>from the agent)$"}
```
