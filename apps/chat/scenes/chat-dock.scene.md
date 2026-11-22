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

# chat — the dock: a column for the views that are open, and every divider drags

The chat's body is one dock (ADR-0404 §SD7, §SD8, proposed): the transcript,
the composer below it, and a column beside them for the views that are open.
With none open there is no column. Settings opens from the bar's words and
Conversations from the View menu; the second joins the first as a tab of the
column. The scene drags the divider above the composer up, which gives the
composer more rows, and the column's divider left, which widens it.
Conversations closes from the × on its tab, Settings by unchecking it in the
View menu, and with both closed the column goes; Settings opened again
brings the column back where it was declared, beside the transcript and the
composer both. The transcript and the composer, each alone in its pane, have
no tab strip. The coordinates are the dock's dividers at this window size:
a divider has no node to anchor.

```jsonl trace
{"do":"note","text":"chat: the dock's column and its dividers"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"capture","text":"chat-dock-initial","sidecars":["svg","tree"]}
{"do":"hover","x":300,"y":543}
{"do":"drag","x":300,"y":543,"toX":300,"toY":440,"steps":12,"durationMs":500,"settleMs":600,"comment":"the divider above the composer, up"}
{"do":"click","contains":"Model:","role":"button","comment":"the bar's words open Settings"}
{"do":"wait","name":"Ask first","role":"radio_button","comment":"Settings in the column"}
{"do":"click","name":"View","role":"button","comment":"the window's View menu"}
{"do":"click","pointer":true,"contains":"Conversations","role":"check_box"}
{"do":"wait","name":"Conversations","role":"button","comment":"two views: tabs of one column"}
{"do":"wait","name":"Close Conversations","role":"button","comment":"a side view closes from its tab"}
{"do":"capture","text":"chat-dock-two","sidecars":["svg","tree"]}
{"do":"hover","x":429,"y":400}
{"do":"drag","x":429,"y":400,"toX":340,"toY":400,"steps":12,"durationMs":500,"settleMs":600,"comment":"the column's divider, left"}
{"do":"capture","text":"chat-dock-dragged","sidecars":["svg","tree"],"settleMs":600}
{"do":"click","name":"Close Conversations","role":"button","comment":"the tab's own close button; the View menu's check follows"}
{"do":"click","name":"View","role":"button"}
{"do":"click","pointer":true,"name":"Settings","role":"check_box","settleMs":500}
{"do":"capture","text":"chat-dock-closed","sidecars":["svg","tree"]}
{"do":"click","contains":"Model:","role":"button","comment":"Settings again"}
{"do":"wait","name":"Ask first","role":"radio_button","comment":"the column comes back where it was declared"}
{"do":"capture","text":"chat-dock-returned","sidecars":["svg","tree"]}
```
