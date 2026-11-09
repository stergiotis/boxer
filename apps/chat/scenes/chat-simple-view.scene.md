---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-simple-view.script.jsonl"
    BOXER_CHAT_DRAFT: "Write a short note."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the Simple view of the plain chat

The headless host, against the scripted model in
[chat-simple-view.script.jsonl](./chat-simple-view.script.jsonl). The window
starts with the full bar. Ticking **Simple view** in Settings folds it under
**Details**. The running turn then reads "working…" and counts steps, and the
model's first write brings the Artefact tab forward. A scene's window neither
reads nor writes the kept choice, since it shares the store of the machine it
runs on: that the choice outlives the window is
`TestTheSimpleViewIsRememberedForTheNextWindow`.

```jsonl trace
{"do":"note","text":"the plain chat's Simple view, from Settings"}
{"do":"wait","valueContains":"→ scripted","role":"label","comment":"the full bar's model line"}
{"do":"click","name":"Settings","role":"button"}
{"do":"wait","contains":"Simple view","role":"check_box"}
{"do":"capture","text":"chat-simple-view-full","sidecars":["svg","tree"]}
{"do":"click","contains":"Simple view","role":"check_box"}
{"do":"wait","contains":" Details","role":"button","comment":"the bar folded"}
{"do":"capture","text":"chat-simple-view-folded","sidecars":["svg","tree"]}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"working…","role":"label","comment":"plain words for a running turn"}
{"do":"wait","valueContains":"step 2","role":"label"}
{"do":"wait","valueContains":"The scripted model wrote this note","role":"label","comment":"the Artefact tab came forward: a hidden tab is not laid out"}
{"do":"capture","text":"chat-simple-view-artefact","sidecars":["svg","tree"]}
{"do":"wait","valueContains":"The note is in the Artefact tab.","role":"label","settleMs":500,"comment":"the turn landed"}
{"do":"click","contains":" Details","role":"button"}
{"do":"wait","valueContains":"→ scripted","role":"label","comment":"Details unfolds what was folded"}
{"do":"capture","text":"chat-simple-view-details","sidecars":["svg","tree"]}
```
