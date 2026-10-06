---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1600x1000
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-artefact-pixels.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_PIXELS: "ask"
    BOXER_CHAT_DRAFT: "Look at the demo app."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the model asks to see a screenshot

ADR-0287 on the headless host, against the scripted model in
[chat-artefact-pixels.script.jsonl](./chat-artefact-pixels.script.jsonl),
with Pixels at Ask each time. The model captures the operations demo's
window into the artefact's screenshots, then asks to see it. The view waits
in the Artefact panel, showing the image, its size and source, and the model
it would go to; Allow lets it through, and the transcript's tool line says
it was shown.

```jsonl trace
{"do":"note","text":"ADR-0287: a screenshot's pixels reach the model only as the Pixels setting allows"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"The model asks to see a screenshot","role":"label","settleMs":500}
{"do":"wait","valueContains":"screenshot-1.png ·","role":"label"}
{"do":"wait","valueContains":"It would go to","role":"label"}
{"do":"capture","text":"chat-artefact-pixels-consent","settleMs":500}
{"do":"click","contains":"Allow","role":"button"}
{"do":"wait","valueContains":"I looked at the demo window's capture.","role":"label","settleMs":500}
{"do":"wait","valueContains":"artefact_view_image · shown · screenshot-1.png","role":"label","comment":"the transcript's tool line"}
{"do":"capture","text":"chat-artefact-pixels","settleMs":500}
```
