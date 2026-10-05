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
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-artefact-images.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_DRAFT: "Capture the demo app for the artefact."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — screenshots beside the artefact

ADR-0284 on the headless host, against the scripted model in
[chat-artefact-images.script.jsonl](./chat-artefact-images.script.jsonl).
The model gets a grant for the operations demo, opens it, captures its
window into the artefact's screenshots — cut to the window's bounds, since
a capture is of the whole frame — and crops a corner out of the capture. The Images tab shows both, with their thumbnails, where they came
from and the budget's use; the text of the artefact is untouched.

```jsonl trace
{"do":"note","text":"ADR-0284: screenshots collected beside the artefact, sealed on disk"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"Captured the demo window","role":"label","settleMs":500}
{"do":"wait","valueContains":"artefact_crop_image · revision 2 · corner.png 240×120","role":"label","comment":"the transcript's tool line"}
{"do":"click","contains":"Images (2)","role":"button"}
{"do":"wait","valueContains":"corner.png · 240×120","role":"label"}
{"do":"wait","valueContains":"cut from screenshot-1.png at 0,0 · 240×120","role":"label"}
{"do":"wait","valueContains":"captured from window 2, cut to the window bounds","role":"label"}
{"do":"capture","text":"chat-artefact-images","settleMs":500}
```
