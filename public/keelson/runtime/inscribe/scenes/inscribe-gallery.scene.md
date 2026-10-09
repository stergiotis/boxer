---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1500x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# inscribe — the gallery demo

The widget gallery's `inscribe` demo (ADR-0297): the overlay's marks on a
sample app, drawn by the real scene, layout and drawing without an agent.
The first capture shows the default marks — numbered steps on the panel
and on Save, a callout on Save, a swipe and a tab on the status line, an
arrow from Cancel to a row. The second turns on the spotlight, the third
draws every target as behind another window, and the fourth puts the arrow
under a second agent's hue.

```jsonl trace
{"do":"note","text":"ADR-0297 — the inscribe demo in the widget gallery"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted; its filter box is the only text input while every demo is folded"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"inscribe"}
{"do":"wait","contains":"inscribe —","settleMs":400}
{"do":"click","contains":"inscribe —","comment":"expand the demo"}
{"do":"wait","name":"Spotlight the row","role":"check_box","settleMs":600}
{"do":"scroll_into_view","name":"Spotlight the row","settleMs":600}
{"do":"capture","text":"inscribe-gallery-marks","settleMs":800}
{"do":"click","name":"Spotlight the row","role":"check_box","settleMs":400}
{"do":"capture","text":"inscribe-gallery-spotlight","settleMs":800}
{"do":"click","name":"Spotlight the row","role":"check_box"}
{"do":"click","name":"Targets behind another window","role":"check_box","settleMs":400}
{"do":"capture","text":"inscribe-gallery-behind","settleMs":800}
{"do":"click","name":"Targets behind another window","role":"check_box"}
{"do":"click","name":"The arrow from a second agent","role":"check_box","settleMs":400}
{"do":"capture","text":"inscribe-gallery-second-agent","settleMs":800}
```
