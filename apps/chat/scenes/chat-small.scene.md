---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-stream.script.jsonl"
    BOXER_CHAT_DRAFT: "How does streaming look?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the first open, at the smallest size the window is drawn for

The window opens at its preferred size, 860 by 720 points; the person makes
it smaller by its corner, here to about 620 by 500. The bar wraps onto a
second line when it runs out of room rather than running past the window's
edge, and the composer keeps its three rows (ADR-0404 §SD7, proposed; keelson-ux V3). The coordinates are the
window's resize corner at its default place: the corner has no node.

```jsonl trace
{"do":"note","text":"the chat at its smallest size"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"hover","x":871,"y":731}
{"do":"drag","x":871,"y":731,"toX":611,"toY":491,"steps":12,"durationMs":500,"settleMs":800,"comment":"the window's corner, 260 left and 240 up"}
{"do":"capture","text":"chat-small-open","sidecars":["svg","tree"]}
{"do":"wait","contains":"Send","role":"button","comment":"the composer is still there"}
```
