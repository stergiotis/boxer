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

- Before a task, the bar says in words what the settings let the model do:
  edit and run things in the windows the person shares. Nothing is shared
  yet. The state chip's inspector holds the conversation's id.
- Turn 1 opens the demo app and writes its note. The task now holds a window
  it may edit; the state chip's inspector says what the task was granted.
- The person opens Settings from those words and sets *At most* to Read. The
  bar follows: the host caps what the task was granted.
- Turn 2 tries the same write. The host refuses it, with the settings as the
  reason.

```jsonl trace
{"do":"note","text":"ADR-0280: the chat's settings are a ceiling the host enforces"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"wait","contains":"Model: may edit and run in your windows","role":"button"}
{"do":"click","name":"Conversation","role":"unknown","pointer":true,"comment":"the state chip's inspector"}
{"do":"wait","valueContains":"chat-","role":"label","nth":0,"comment":"the conversation's id, readable in the inspector, ahead of the roster, which names an untitled run of the same store by its id"}
{"do":"click","name":"Conversation","role":"unknown","pointer":true,"comment":"closed again"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"wait","valueContains":"The note is written.","role":"label","settleMs":1000}
{"do":"wait","valueContains":"set note in window 2 · done","role":"label"}
{"do":"wait","value":"answered","role":"label","comment":"the state chip: the turn ended answered"}
{"do":"click","name":"Conversation","role":"unknown","pointer":true,"comment":"the state chip's inspector"}
{"do":"wait","valueContains":"Granted: edit windows you share","role":"label","comment":"the demo app's catalog offers edits, and the task acts in it"}
{"do":"click","name":"Conversation","role":"unknown","pointer":true}
{"do":"click","contains":"Model:","role":"button","comment":"the bar's words open Settings"}
{"do":"wait","valueContains":"What the model may do","role":"label"}
{"do":"capture","text":"chat-settings"}
{"do":"click","name":"Read","role":"radio_button","comment":"At most: read"}
{"do":"wait","contains":"Model: may read your windows","role":"button","settleMs":500}
{"do":"capture","text":"chat-settings-read"}
{"do":"focus","name":"Message","role":"multiline_text_input","comment":"the chat's composer"}
{"do":"type","name":"Message","role":"multiline_text_input","text":"Write it again."}
{"do":"click","contains":"Send","role":"button","comment":"turn 2"}
{"do":"wait","valueContains":"The settings let me only read now","role":"label","settleMs":1000}
{"do":"wait","valueContains":"set note in window 2 · refused","role":"label","comment":"refused by the host, above the ceiling"}
{"do":"capture","text":"chat-settings-refused"}
```
