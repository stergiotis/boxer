---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias IN ('chat','widgets','opsdemo')"
  size: 1400x900
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-coordinator-windows.script.jsonl"
    BOXER_CHAT_DRAFT: "Lay my windows out side by side."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the coordinator reads the desktop and arranges it

ADR-0276 against the scripted model in
[chat-coordinator-windows.script.jsonl](./chat-coordinator-windows.script.jsonl):
the chat reads `keelson('windows')`, asks for the desktop under a test
grant, lays every window side by side with `arrange_windows`, and reads
`keelson('desktop')` and `keelson('windows')` again. The capture shows the
three windows in columns; the chat's transcript lists the calls.

```jsonl trace
{"do":"note","text":"chat: the coordinator arranges the desktop"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"The windows are side by side","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ Reading the desktop · read 3 row(s) of keelson('windows')","role":"label"}
{"do":"wait","valueContains":"⚙ Laying the windows side by side · columns every window","role":"label"}
{"do":"capture","text":"chat-coordinator-windows","settleMs":1000}
```
