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
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-inscribe.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Where do I clear the note?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the coordinator annotates a window

ADR-0297 against the scripted model in
[chat-inscribe.script.jsonl](./chat-inscribe.script.jsonl): the chat opens
the operations demo under a test grant, reads its window tree, puts a
callout on the demo's Clear button by its tree part and a numbered step on
the whole window. The captures show the marks; the marks following the
window after the person cascades the windows; the outlines dashed once the
chat is raised over the demo; and the overlay empty after the person clears
it from the Window menu.

The cascade widens the demo window, its text wraps onto fewer lines, and
the callout keeps the place the button had before: a mark follows its
window, not a widget moving inside it (ADR-0297 §SD4). The script names the
button as `#8` of the tree; a change to the demo's layout moves it, which
the first capture shows.

```jsonl trace
{"do":"note","text":"chat: the coordinator annotates the demo window"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"I marked the Clear button","role":"label","settleMs":1500}
{"do":"read","valueContains":"Pointing at Clear","role":"label","pattern":"(?P<call>callout \"clear\")$","comment":"the callout completed: a refusal would add its phase"}
{"do":"capture","text":"chat-inscribe-marks","settleMs":800}
{"do":"click","name":"Window","role":"button"}
{"do":"click","name":"Cascade","role":"button","settleMs":1500}
{"do":"key","text":"Escape"}
{"do":"capture","text":"chat-inscribe-cascaded","settleMs":800}
{"do":"click","x":620,"y":34,"settleMs":800,"comment":"raise the chat by the strip of its title bar the cascade leaves uncovered"}
{"do":"capture","text":"chat-inscribe-behind","settleMs":800}
{"do":"click","name":"Window","role":"button"}
{"do":"click","name":"Clear annotations","role":"button","settleMs":600}
{"do":"key","text":"Escape"}
{"do":"capture","text":"chat-inscribe-cleared","settleMs":800}
```
