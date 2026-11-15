---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1100x1000
  stepSettleMs: 400
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imageview — a rendered source, magnified, its marks named, its tiles kept

The gallery's `imageview-render` demo shows a test chart that is drawn when
a tile is asked for (ADR-0374). The scene zooms to sixteen times on the
chart's star, where the chart is rendered at a level above full resolution,
and captures it, the demo scrolled so the line under the viewer is in the
gallery window. It finds the overlay's marks by the labels of their hit
regions, which is how a host's links or annotations reach the
accessibility tree. It then reads how many tiles were rendered, inverts the
colours — a recipe change — and asserts that no tile was rendered again,
since the viewer maps the samples it kept.

```jsonl trace
{"do":"wait","role":"text_input"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"imageview-render"}
{"do":"wait","contains":"imageview (a rendered source)","settleMs":400}
{"do":"click","contains":"imageview (a rendered source)"}

{"do":"note","text":"--- sixteen times on the star: rendered, not enlarged ---"}
{"do":"click","name":"16× on the star","role":"button"}
{"do":"wait","valueContains":"page 1 · ×16 ·","role":"label","settleMs":1500}
{"do":"scroll_into_view","valueContains":"· tiles rendered ","role":"label","settleMs":2000,"comment":"the gallery window is shorter than the demo"}
{"do":"wait","name":"star centre","role":"unknown","comment":"the overlay's mark, named by its hit region"}
{"do":"capture","text":"rendered-16x"}

{"do":"note","text":"--- a recipe change renders nothing ---"}
{"do":"sleep","settleMs":1500}
{"do":"read","valueContains":"· tiles rendered ","role":"label","pattern":"tiles rendered (?P<before>[0-9]+)"}
{"do":"click","name":"invert","role":"button"}
{"do":"sleep","settleMs":1500}
{"do":"read","valueContains":"· tiles rendered ","role":"label","pattern":"tiles rendered (?P<after>[0-9]+)"}
{"do":"expect","of":"after","minus":"before","eq":0,"comment":"no tile rendered again"}
{"do":"capture","text":"rendered-16x-inverted"}

{"do":"note","text":"--- the next page keeps the view ---"}
{"do":"click","name":"next page","role":"button"}
{"do":"wait","valueContains":"page 2 · ×16 ·","role":"label","settleMs":2500}
{"do":"capture","text":"rendered-page-2"}
```
