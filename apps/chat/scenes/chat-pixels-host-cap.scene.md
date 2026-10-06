---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x1200
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_PIXELS: "captures"
    BOXER_CHAT_PIXELS_MAX: "ask-once"
    BOXER_CHAT_PIXELS_LOCAL_REQUIRED: "true"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the host caps the Pixels setting

ADR-0287 on the headless host. The window starts at This chat's captures,
but the host allows at most Ask once per image and requires Only to a local
model: the Settings panel offers no level above the cap, says so, and
shows the local switch as the host's rule rather than a checkbox.

```jsonl trace
{"do":"note","text":"ADR-0287: the host's cap binds over the window's setting"}
{"do":"click","contains":"Settings","role":"button"}
{"do":"wait","valueContains":"This host allows at most Ask once per image.","role":"label","settleMs":500}
{"do":"wait","valueContains":"Only to a local model: required by this host","role":"label"}
{"do":"capture","text":"chat-pixels-host-cap","settleMs":500}
```
