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
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-settings.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Write a note in the demo app."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the Settings panel and the scale of what the model may do

ADR-0280 on the headless host, against the scripted model in
[chat-settings.script.jsonl](./chat-settings.script.jsonl). A test grant
stands in for the person's approval.

- Before a task, the scale's two markers read "now talk only" and "may run":
  nothing is shared, and the settings would let the model run.
- Turn 1 opens the demo app and writes its note. The task now holds a window
  it may edit, so "now" moves to edit.
- The person opens Settings and sets *At most* to Read. Both markers move to
  read: the host caps what the task was granted.
- Turn 2 tries the same write. The host refuses it, with the settings as the
  reason.

```jsonl trace
{"do":"note","text":"ADR-0280: the chat's settings are a ceiling the host enforces"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"wait","valueContains":"now talk only · may run","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"wait","valueContains":"The note is written.","role":"label","settleMs":1000}
{"do":"wait","valueContains":"set_note in window 2 · rendered","role":"label"}
{"do":"wait","valueContains":"now edit · may run","role":"label","comment":"the demo app's catalog offers edits, and the task acts in it"}
{"do":"click","contains":"Settings","role":"button"}
{"do":"wait","valueContains":"What the model may do","role":"label"}
{"do":"capture","text":"chat-settings"}
{"do":"click","name":"Read","role":"button","comment":"At most: read"}
{"do":"wait","valueContains":"now read · may read","role":"label","settleMs":500}
{"do":"capture","text":"chat-settings-read"}
{"do":"focus","id":13282790709624229129,"comment":"the chat's composer"}
{"do":"type","id":13282790709624229129,"text":"Write it again."}
{"do":"click","contains":"Send","role":"button","comment":"turn 2"}
{"do":"wait","valueContains":"The settings let me only read now","role":"label","settleMs":1000}
{"do":"wait","valueContains":"set_note in window 2 · refused","role":"label","comment":"refused by the host, above the ceiling"}
{"do":"capture","text":"chat-settings-refused"}
```
