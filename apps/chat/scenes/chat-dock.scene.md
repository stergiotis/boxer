---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-urgent.script.jsonl"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the window's panes are a dock, and every divider drags

The chat's body is one dock, as play's is: the transcript, the composer
below it, and a column of tabs — Conversations, Settings, Analytics,
Artefact — beside them. The scene drags the divider above the composer up,
which gives the composer more rows, and the divider before the column
left, which widens it; the scale's line in the bar then brings the
Settings tab to the front of the column. The bar has no buttons for the
tabs: the tab strip is how a pane is reached. The coordinates are the dock's dividers at this
window size: the dividers are drawn by the dock, with no node to anchor.

```jsonl trace
{"do":"note","text":"chat: the dock"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"capture","text":"chat-dock-initial"}
{"do":"hover","x":260,"y":543}
{"do":"drag","x":260,"y":543,"toX":260,"toY":380,"steps":12,"durationMs":500,"settleMs":600,"comment":"the divider above the composer, up"}
{"do":"hover","x":437,"y":400}
{"do":"drag","x":437,"y":400,"toX":330,"toY":400,"steps":12,"durationMs":500,"settleMs":600,"comment":"the divider before the column, left"}
{"do":"read","valueContains":"→ scripted","role":"label","pattern":"(?P<model>scripted)","comment":"the composer is drawn against a model"}
{"do":"click","contains":"now talk only","role":"button","comment":"the scale's line in the bar opens Settings"}
{"do":"wait","name":"Ask first","role":"radio_button","comment":"the Settings tab in front"}
{"do":"capture","text":"chat-dock-dragged","settleMs":600}
```
