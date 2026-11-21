---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1500x1100
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "bands"
    BOXER_IMAGESCOPE_REGION: "60,20,40,40"
  layoutExceptions:
    - invariant: outside
      capture: imagescope-regions-measured
      role: button
      name: "B2"
      reason: the regions table is wider than the side panel and scrolls sideways; the cell of its plane column lies past the window until it is scrolled
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — a region kept, measured, and stale after the selection moves

The window opens on the synthetic stack from `BOXER_IMAGESCOPE_SAMPLE` with
the rectangle of `BOXER_IMAGESCOPE_REGION` marked. **Keep as region** makes
a region of it — the person's, in pixel coordinates, for every position of
the stack — and selects it, so its fields and its row in the table show.
**Measure** computes its figures off the frame over the selection shown;
the scene reads how many pixels they cover, which for a rectangle on whole
coordinates is the marked rectangle's, then steps the time axis and finds
the figures marked stale, measured on the selection that was shown. The
region is drawn by the overlay and named in the tree by its hit region.

```jsonl trace
{"do":"note","text":"--- the stack opens with the rectangle marked ---"}
{"do":"wait","valueContains":"Region: columns 60–99, rows 20–59","role":"label","settleMs":1500}
{"do":"wait","valueContains":"No regions.","role":"label"}

{"do":"note","text":"--- the marked rectangle is kept as a region and selected ---"}
{"do":"click","name":"Keep as region","role":"button"}
{"do":"wait","valueContains":"Selected: rectangle 1 · rectangle · pixel frame · every position · made by person","role":"label"}
{"do":"wait","valueContains":"1 regions","role":"label"}
{"do":"wait","name":"region rectangle 1","comment":"the overlay's hit region names the region drawn"}

{"do":"note","text":"--- its figures, computed off the frame over the selection shown ---"}
{"do":"click","name":"Measure","role":"button"}
{"do":"wait","valueContains":"regions/1 · identity","role":"label","settleMs":800}
{"do":"read","valueContains":"regions/1 · identity","role":"label","pattern":"identity · (?P<px>[0-9]+) pixels by centre"}
{"do":"expect","of":"px","is":"1600"}
{"do":"wait","valueContains":"B2: valid","role":"label"}
{"do":"capture","text":"imagescope-regions-measured","sidecars":["tree"]}

{"do":"note","text":"--- another time is shown: the figures stay, marked stale ---"}
{"do":"click","name":"›","role":"button","nth":0,"settleMs":1200}
{"do":"wait","valueContains":"day 16 · 16 d · 2 of 3","role":"label"}
{"do":"wait","valueContains":"stale — regions/1","role":"label"}
{"do":"read","valueContains":"stale — regions/1","role":"label","pattern":"(?P<sel>time day 0)"}
{"do":"expect","of":"sel","is":"time day 0"}
{"do":"capture","text":"imagescope-regions-stale"}
```
